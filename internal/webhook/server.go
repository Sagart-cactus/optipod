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
	"net/http"
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
	client        client.Client
	server        *http.Server
	certPath      string
	keyPath       string
	port          int
	mutator       *Mutator
	eventRecorder *observability.EventRecorder
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
func NewServer(k8sClient client.Client, certPath, keyPath string, port int, eventRecorder *observability.EventRecorder) *Server {
	mutator := NewMutator(k8sClient, eventRecorder)

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
	log.Info("Starting webhook server", "port", s.port)

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

	s.server = &http.Server{
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

	// Set server health status to healthy
	observability.SetWebhookServerHealthStatus("server", true)

	// Start server in a goroutine
	go func() {
		log.Info("Webhook server listening", "address", s.server.Addr)
		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Error(err, "Failed to start webhook server")
			observability.SetWebhookServerHealthStatus("server", false)
			if s.eventRecorder != nil {
				s.eventRecorder.RecordWebhookServerError(nil, "server", err)
			}
		}
	}()

	log.Info("Webhook server started successfully")

	// Wait for context cancellation
	<-ctx.Done()
	return s.Stop(ctx)
}

// Stop gracefully stops the webhook server
func (s *Server) Stop(ctx context.Context) error {
	log.Info("Stopping webhook server")

	if s.server == nil {
		return nil
	}

	// Set server health status to unhealthy
	observability.SetWebhookServerHealthStatus("server", false)

	// Create a context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		log.Error(err, "Failed to gracefully shutdown webhook server")
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
	if admissionReview.Request != nil && admissionReview.Request.Object.Object != nil {
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
	ready := s.server != nil && s.mutator != nil

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
		"key_path":      s.keyPath,
		"server_ready":  s.server != nil,
		"mutator_ready": s.mutator != nil,
	}

	// Add certificate information if available
	if s.server != nil && s.server.TLSConfig != nil && len(s.server.TLSConfig.Certificates) > 0 {
		cert := s.server.TLSConfig.Certificates[0]
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
	if req.Object.Object == nil {
		return nil, fmt.Errorf("admission request object is nil")
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
		"key_path":      s.keyPath,
		"server_ready":  s.server != nil,
		"mutator_ready": s.mutator != nil,
	}

	// Add certificate information if available
	if s.server != nil && s.server.TLSConfig != nil && len(s.server.TLSConfig.Certificates) > 0 {
		cert := s.server.TLSConfig.Certificates[0]
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
