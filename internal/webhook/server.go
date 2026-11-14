/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/optipod/optipod/internal/observability"
)

var log = logf.Log.WithName("webhook-server")

// Server represents the webhook server
type Server struct {
	serverMu      sync.Mutex
	ready         atomic.Bool
	client        client.Client
	server        *http.Server
	certPath      string
	keyPath       string
	port          int
	mutator       *Mutator
	eventRecorder *observability.EventRecorder
}

// Admission must run on every replica, including while another replica owns
// the reconciliation lease. Otherwise a healthy standby silently fails open.
func (s *Server) NeedLeaderElection() bool { return false }

func (s *Server) CheckReady(_ *http.Request) error {
	if !s.ready.Load() {
		return fmt.Errorf("admission listener is not ready")
	}
	return nil
}

// AdmissionRequest represents a webhook admission request
type AdmissionRequest struct {
	Pod    *corev1.Pod
	DryRun bool
}

// AdmissionResponse represents a webhook admission response
type AdmissionResponse struct {
	Allowed bool
	Patches []PatchOperation
	Message string
}

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// NewServer creates a new webhook server instance
func NewServer(k8sClient client.Client, certPath, keyPath string, port int, eventRecorder *observability.EventRecorder, dryRun ...bool) *Server {
	mutator := NewMutator(k8sClient, eventRecorder)
	if len(dryRun) > 0 {
		mutator.dryRun = dryRun[0]
	}

	return &Server{
		client:        k8sClient,
		certPath:      certPath,
		keyPath:       keyPath,
		port:          port,
		mutator:       mutator,
		eventRecorder: eventRecorder,
	}
}

// Start starts the webhook server
func (s *Server) Start(ctx context.Context) error {
	log.Info("Starting webhook server", "port", s.port, "certPath", s.certPath, "keyPath", s.keyPath)

	// Validate certificate paths are set
	if s.certPath == "" || s.keyPath == "" {
		err := fmt.Errorf("certificate paths not configured: certPath=%q keyPath=%q", s.certPath, s.keyPath)
		log.Error(err, "Certificate paths must be set")
		observability.SetWebhookServerHealthStatus("server", false)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookCertificateError(nil, "config", err)
		}
		return err
	}

	// Check certificate files exist before attempting to load them
	if err := s.checkCertificateFiles(); err != nil {
		log.Error(err, "Certificate files validation failed", "certPath", s.certPath, "keyPath", s.keyPath)
		observability.SetWebhookServerHealthStatus("server", false)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookCertificateError(nil, "file_check", err)
		}
		return fmt.Errorf("certificate files validation failed: %w", err)
	}

	// Load TLS certificates
	cert, err := tls.LoadX509KeyPair(s.certPath, s.keyPath)
	if err != nil {
		log.Error(err, "Failed to load TLS certificates", "certPath", s.certPath, "keyPath", s.keyPath)
		observability.SetWebhookServerHealthStatus("server", false)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookCertificateError(nil, "load", err)
		}
		return fmt.Errorf("failed to load TLS certificates: %w", err)
	}

	// Validate certificates
	if err := s.validateCertificates(cert); err != nil {
		log.Error(err, "Certificate validation failed")
		observability.SetWebhookServerHealthStatus("server", false)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookCertificateError(nil, "validation", err)
		}
		return fmt.Errorf("certificate validation failed: %w", err)
	}

	// Create HTTP server with TLS configuration
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", s.HandleAdmission)
	mux.HandleFunc("/health", s.HandleHealth)
	mux.HandleFunc("/ready", s.HandleReady)
	mux.HandleFunc("/debug/config", s.HandleDebugConfig)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Publish a fully initialized listener/server as one state transition.
	s.serverMu.Lock()
	if s.server != nil {
		s.serverMu.Unlock()
		return fmt.Errorf("admission server is already started")
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		s.serverMu.Unlock()
		return fmt.Errorf("listen for admission: %w", err)
	}
	s.server = server
	s.ready.Store(true)
	s.serverMu.Unlock()
	defer s.clearServer(server)
	observability.SetWebhookServerHealthStatus("server", true)

	serveErrors := make(chan error, 1)
	go func() {
		log.Info("Webhook server listening", "address", server.Addr)
		serveErrors <- server.ServeTLS(listener, "", "")
	}()
	log.Info("Webhook server started successfully")
	select {
	case <-ctx.Done():
		return s.stopServer(server)
	case err := <-serveErrors:
		if err == http.ErrServerClosed {
			return nil
		}
		observability.SetWebhookServerHealthStatus("server", false)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "server", err)
		}
		return err
	}
}

func (s *Server) currentServer() *http.Server {
	s.serverMu.Lock()
	defer s.serverMu.Unlock()
	return s.server
}

func (s *Server) clearServer(server *http.Server) {
	s.serverMu.Lock()
	defer s.serverMu.Unlock()
	if s.server == server {
		s.ready.Store(false)
		s.server = nil
	}
}

// Stop stops accepting admission before draining the current server. Each Start
// captures its own server so cancellation of an old run cannot stop a newer run.
func (s *Server) Stop(_ context.Context) error {
	return s.stopServer(s.currentServer())
}

func (s *Server) stopServer(server *http.Server) error {
	if server == nil {
		return nil
	}
	s.serverMu.Lock()
	if s.server == server {
		s.ready.Store(false)
	}
	s.serverMu.Unlock()
	defer s.clearServer(server)
	observability.SetWebhookServerHealthStatus("server", false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "shutdown", err)
		}
		return err
	}
	log.Info("Webhook server stopped successfully")
	return nil
}

// HandleAdmission handles admission webhook requests
func (s *Server) HandleAdmission(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	log.V(1).Info("Received admission request", "method", r.Method, "url", r.URL.Path, "remoteAddr", r.RemoteAddr)

	if r.Method != http.MethodPost {
		log.Error(nil, "Invalid HTTP method for admission request", "method", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error(err, "Failed to read request body")
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "/mutate", err)
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			log.Error(err, "Failed to close request body")
		}
	}()

	// Parse admission review
	admissionReview, err := s.parseAdmissionReview(body)
	if err != nil {
		log.Error(err, "Failed to parse admission review")
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "/mutate", err)
		}
		http.Error(w, "Failed to parse admission review", http.StatusBadRequest)
		return
	}

	// Extract pod information for metrics
	podName := "unknown"
	namespace := "unknown"
	if admissionReview.Request != nil && len(admissionReview.Request.Object.Raw) > 0 {
		var pod corev1.Pod
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, &pod); err == nil {
			podName = pod.Name
			namespace = pod.Namespace
		}
	}

	// Record admission request metric
	dryRun := admissionReview.Request != nil && admissionReview.Request.DryRun != nil && *admissionReview.Request.DryRun
	observability.RecordWebhookAdmissionRequest(namespace, podName, dryRun)

	// Process admission request
	response := s.processAdmissionRequest(admissionReview.Request)

	// Record metrics based on response
	if response.Allowed {
		log.Info("Admission request processed successfully",
			"namespace", namespace,
			"pod", podName,
			"patches", len(response.Patches),
			"message", response.Message,
			"duration", time.Since(startTime))
	} else {
		log.Error(nil, "Admission request denied",
			"namespace", namespace,
			"pod", podName,
			"message", response.Message,
			"duration", time.Since(startTime))
	}

	// Create admission response
	admissionResponse := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: response.Allowed,
			Result: &metav1.Status{
				Message: response.Message,
			},
		},
	}

	// Add patches if any
	if len(response.Patches) > 0 {
		patchBytes, err := json.Marshal(response.Patches)
		if err != nil {
			log.Error(err, "Failed to marshal patches", "namespace", namespace, "pod", podName)
			if s.eventRecorder != nil {
				s.eventRecorder.RecordWebhookServerError(nil, "/mutate", err)
			}
			http.Error(w, "Failed to marshal patches", http.StatusInternalServerError)
			return
		}
		patchType := admissionv1.PatchTypeJSONPatch
		admissionResponse.Response.Patch = patchBytes
		admissionResponse.Response.PatchType = &patchType
	}

	// Marshal response
	responseBytes, err := json.Marshal(admissionResponse)
	if err != nil {
		log.Error(err, "Failed to marshal admission response", "namespace", namespace, "pod", podName)
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "/mutate", err)
		}
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(responseBytes); err != nil {
		log.Error(err, "Failed to write response", "namespace", namespace, "pod", podName)
	}

	log.V(1).Info("Admission request completed",
		"namespace", namespace,
		"pod", podName,
		"allowed", response.Allowed,
		"patches", len(response.Patches),
		"message", response.Message,
		"totalDuration", time.Since(startTime))
}

// HandleHealth handles health check requests
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	log.V(2).Info("Health check requested")

	// Set health status metric
	observability.SetWebhookServerHealthStatus("health", true)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"healthy","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`)); err != nil {
		log.Error(err, "Failed to write health response")
		observability.SetWebhookServerHealthStatus("health", false)
	}
}

// HandleReady handles readiness check requests
func (s *Server) HandleReady(w http.ResponseWriter, r *http.Request) {
	log.V(2).Info("Readiness check requested")

	// Check if server is ready (certificates loaded, mutator initialized)
	ready := s.ready.Load() && s.mutator != nil

	// Set readiness status metric
	observability.SetWebhookServerHealthStatus("ready", ready)

	w.Header().Set("Content-Type", "application/json")

	if ready {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ready","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`)); err != nil {
			log.Error(err, "Failed to write readiness response")
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte(`{"status":"not ready","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`)); err != nil {
			log.Error(err, "Failed to write readiness response")
		}
	}
}

// HandleDebugConfig handles debug configuration inspection requests
func (s *Server) HandleDebugConfig(w http.ResponseWriter, r *http.Request) {
	log.Info("Debug configuration requested", "remoteAddr", r.RemoteAddr)

	// Create debug information
	debugInfo := map[string]interface{}{
		"timestamp":     time.Now().Format(time.RFC3339),
		"server_port":   s.port,
		"cert_path":     s.certPath,
		logKeyPath:      s.keyPath,
		"server_ready":  s.ready.Load(),
		"mutator_ready": s.mutator != nil,
	}

	// Add certificate information if available
	if server := s.currentServer(); server != nil && server.TLSConfig != nil && len(server.TLSConfig.Certificates) > 0 {
		cert := server.TLSConfig.Certificates[0]
		if len(cert.Certificate) > 0 {
			if x509Cert, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
				debugInfo["certificate"] = map[string]interface{}{
					"subject":    x509Cert.Subject.String(),
					"issuer":     x509Cert.Issuer.String(),
					"not_before": x509Cert.NotBefore.Format(time.RFC3339),
					"not_after":  x509Cert.NotAfter.Format(time.RFC3339),
					"dns_names":  x509Cert.DNSNames,
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(debugInfo); err != nil {
		log.Error(err, "Failed to write debug configuration response")
		if s.eventRecorder != nil {
			s.eventRecorder.RecordWebhookServerError(nil, "/debug/config", err)
		}
	}
}

// parseAdmissionReview parses the admission review from request body
func (s *Server) parseAdmissionReview(body []byte) (*admissionv1.AdmissionReview, error) {
	var admissionReview admissionv1.AdmissionReview

	// Create a decoder for the admission review
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	decoder := codecs.UniversalDeserializer()

	if _, _, err := decoder.Decode(body, nil, &admissionReview); err != nil {
		return nil, fmt.Errorf("failed to decode admission review: %w", err)
	}

	if admissionReview.Request == nil {
		return nil, fmt.Errorf("admission review request is nil")
	}

	return &admissionReview, nil
}

// processAdmissionRequest processes an admission request and returns a response
func (s *Server) processAdmissionRequest(req *admissionv1.AdmissionRequest) *AdmissionResponse {
	// Parse pod from request
	pod, err := s.parsePodFromRequest(req)
	if err != nil {
		log.Error(err, "Failed to parse pod from request")
		return &AdmissionResponse{
			Allowed: true, // Allow pod creation even if we can't parse it
			Message: fmt.Sprintf("Failed to parse pod: %v", err),
		}
	}

	// Create admission request
	admissionReq := &AdmissionRequest{
		Pod:    pod,
		DryRun: req.DryRun != nil && *req.DryRun,
	}

	// Process mutation
	return s.MutatePod(admissionReq)
}

// parsePodFromRequest extracts a pod from the admission request
func (s *Server) parsePodFromRequest(req *admissionv1.AdmissionRequest) (*corev1.Pod, error) {
	if req == nil {
		return nil, fmt.Errorf("admission request is nil")
	}

	if len(req.Object.Raw) == 0 {
		return nil, fmt.Errorf("admission request object is empty")
	}

	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pod: %w", err)
	}

	return &pod, nil
}

// MutatePod processes a pod mutation request
func (s *Server) MutatePod(req *AdmissionRequest) *AdmissionResponse {
	if s.mutator == nil {
		log.Error(nil, "Mutator is not initialized")
		return &AdmissionResponse{
			Allowed: true,
			Message: "Mutator not initialized",
		}
	}

	return s.mutator.MutatePod(req)
}

// checkCertificateFiles verifies that certificate files exist and are readable
func (s *Server) checkCertificateFiles() error {
	// Check certificate file
	if err := s.checkFileExists(s.certPath, "certificate"); err != nil {
		return err
	}

	// Check key file
	if err := s.checkFileExists(s.keyPath, "private key"); err != nil {
		return err
	}

	log.Info("Certificate files validation passed", "certPath", s.certPath, "keyPath", s.keyPath)
	return nil
}

// checkFileExists checks if a file exists and is readable
func (s *Server) checkFileExists(filePath, fileType string) error {
	// Use os.Stat to check file existence
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file does not exist: %s", fileType, filePath)
		}
		return fmt.Errorf("failed to stat %s file %s: %w", fileType, filePath, err)
	}

	// Check if it's actually a file (not a directory)
	if info.IsDir() {
		return fmt.Errorf("%s path is a directory, not a file: %s", fileType, filePath)
	}

	// Check file size (empty files are invalid)
	if info.Size() == 0 {
		return fmt.Errorf("%s file is empty: %s", fileType, filePath)
	}

	log.V(1).Info("Certificate file validated", "type", fileType, "path", filePath, "size", info.Size())
	return nil
}

// validateCertificates validates the loaded TLS certificates
func (s *Server) validateCertificates(cert tls.Certificate) error {
	if len(cert.Certificate) == 0 {
		return fmt.Errorf("no certificates found")
	}

	// Parse the certificate to get expiry information
	if x509Cert, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
		// Set certificate expiry metric
		expiryTime := float64(x509Cert.NotAfter.Unix())
		observability.SetWebhookCertificateExpiryTime("webhook", expiryTime)

		// Log certificate information
		log.Info("TLS certificate validated successfully",
			"subject", x509Cert.Subject.String(),
			"issuer", x509Cert.Issuer.String(),
			"notBefore", x509Cert.NotBefore.Format(time.RFC3339),
			"notAfter", x509Cert.NotAfter.Format(time.RFC3339),
			"dnsNames", x509Cert.DNSNames)

		// Check if certificate is expiring soon (within 30 days)
		if time.Until(x509Cert.NotAfter) < 30*24*time.Hour {
			log.Error(nil, "Certificate expiring soon",
				"expiresAt", x509Cert.NotAfter.Format(time.RFC3339),
				"timeRemaining", time.Until(x509Cert.NotAfter).String())
			if s.eventRecorder != nil {
				s.eventRecorder.RecordWebhookCertificateError(nil, "expiring",
					fmt.Errorf("certificate expires at %s", x509Cert.NotAfter.Format(time.RFC3339)))
			}
		}
	} else {
		log.Error(err, "Failed to parse certificate for validation")
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	return nil
}

// GetDebugConfiguration returns debug configuration information
func (s *Server) GetDebugConfiguration() map[string]interface{} {
	debugInfo := map[string]interface{}{
		"timestamp":     time.Now().Format(time.RFC3339),
		"server_port":   s.port,
		"cert_path":     s.certPath,
		logKeyPath:      s.keyPath,
		"server_ready":  s.ready.Load(),
		"mutator_ready": s.mutator != nil,
	}

	// Add certificate information if available
	if server := s.currentServer(); server != nil && server.TLSConfig != nil && len(server.TLSConfig.Certificates) > 0 {
		cert := server.TLSConfig.Certificates[0]
		if len(cert.Certificate) > 0 {
			if x509Cert, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
				debugInfo["certificate"] = map[string]interface{}{
					"subject":    x509Cert.Subject.String(),
					"issuer":     x509Cert.Issuer.String(),
					"not_before": x509Cert.NotBefore.Format(time.RFC3339),
					"not_after":  x509Cert.NotAfter.Format(time.RFC3339),
					"dns_names":  x509Cert.DNSNames,
				}
			}
		}
	}

	return debugInfo
}
