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
	"fmt"
	"os"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/optipod/optipod/internal/observability"
)

var lifecycleLog = logf.Log.WithName("webhook-lifecycle")

// LifecycleManager manages the webhook lifecycle including registration and cleanup
type LifecycleManager struct {
	client            client.Client
	webhookName       string
	serviceName       string
	serviceNamespace  string
	servicePath       string
	certPath          string
	keyPath           string
	caBundle          []byte
	failurePolicy     *admissionregistrationv1.FailurePolicyType
	namespaceSelector *metav1.LabelSelector
	server            *Server
	eventRecorder     *observability.EventRecorder
}

// LifecycleConfig contains configuration for webhook lifecycle management
type LifecycleConfig struct {
	WebhookName       string
	ServiceName       string
	ServiceNamespace  string
	ServicePath       string
	CertPath          string
	KeyPath           string
	CABundle          []byte
	FailurePolicy     *admissionregistrationv1.FailurePolicyType
	NamespaceSelector *metav1.LabelSelector
	Port              int
}

// NewLifecycleManager creates a new webhook lifecycle manager
func NewLifecycleManager(k8sClient client.Client, config LifecycleConfig) *LifecycleManager {
	// Set default failure policy if not specified
	failurePolicy := config.FailurePolicy
	if failurePolicy == nil {
		defaultPolicy := admissionregistrationv1.Fail
		failurePolicy = &defaultPolicy
	}

	// Set default namespace selector if not specified (all namespaces)
	namespaceSelector := config.NamespaceSelector
	if namespaceSelector == nil {
		namespaceSelector = &metav1.LabelSelector{}
	}

	// Create event recorder
	eventRecorder := observability.NewEventRecorder(nil)

	// Create webhook server
	server := NewServer(k8sClient, config.CertPath, config.KeyPath, config.Port, eventRecorder)

	return &LifecycleManager{
		client:            k8sClient,
		webhookName:       config.WebhookName,
		serviceName:       config.ServiceName,
		serviceNamespace:  config.ServiceNamespace,
		servicePath:       config.ServicePath,
		certPath:          config.CertPath,
		keyPath:           config.KeyPath,
		caBundle:          config.CABundle,
		failurePolicy:     failurePolicy,
		namespaceSelector: namespaceSelector,
		server:            server,
		eventRecorder:     eventRecorder,
	}
}

// Start starts the webhook lifecycle management
func (lm *LifecycleManager) Start(ctx context.Context) error {
	lifecycleLog.Info("Starting webhook lifecycle management",
		"webhookName", lm.webhookName,
		"serviceName", lm.serviceName,
		"serviceNamespace", lm.serviceNamespace)

	// Validate certificates before starting
	if err := lm.validateCertificates(); err != nil {
		return fmt.Errorf("certificate validation failed: %w", err)
	}

	// Register the mutating webhook configuration
	if err := lm.registerWebhook(ctx); err != nil {
		return fmt.Errorf("failed to register webhook: %w", err)
	}

	// Start the webhook server
	go func() {
		if err := lm.server.Start(ctx); err != nil {
			lifecycleLog.Error(err, "Webhook server failed")
		}
	}()

	// Wait for server to be ready
	if err := lm.waitForServerReady(ctx); err != nil {
		return fmt.Errorf("webhook server failed to become ready: %w", err)
	}

	lifecycleLog.Info("Webhook lifecycle management started successfully")
	return nil
}

// Stop stops the webhook lifecycle management and cleans up resources
func (lm *LifecycleManager) Stop(ctx context.Context) error {
	lifecycleLog.Info("Stopping webhook lifecycle management")

	// Stop the webhook server first
	if lm.server != nil {
		if err := lm.server.Stop(ctx); err != nil {
			lifecycleLog.Error(err, "Failed to stop webhook server")
		}
	}

	// Clean up webhook registration
	if err := lm.cleanupWebhook(ctx); err != nil {
		lifecycleLog.Error(err, "Failed to cleanup webhook registration")
		return err
	}

	lifecycleLog.Info("Webhook lifecycle management stopped successfully")
	return nil
}

// registerWebhook registers the mutating webhook configuration
func (lm *LifecycleManager) registerWebhook(ctx context.Context) error {
	lifecycleLog.Info("Registering mutating webhook configuration", "name", lm.webhookName)

	// Create the mutating webhook configuration
	webhook := &admissionregistrationv1.MutatingWebhook{
		Name: lm.webhookName + ".optipod.io",
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &admissionregistrationv1.ServiceReference{
				Name:      lm.serviceName,
				Namespace: lm.serviceNamespace,
				Path:      &lm.servicePath,
			},
			CABundle: lm.caBundle,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		},
		AdmissionReviewVersions: []string{"v1", "v1beta1"},
		SideEffects: func() *admissionregistrationv1.SideEffectClass {
			s := admissionregistrationv1.SideEffectClassNone
			return &s
		}(),
		FailurePolicy:     lm.failurePolicy,
		NamespaceSelector: lm.namespaceSelector,
	}

	// Create or update the mutating webhook configuration
	webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: lm.webhookName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "optipod",
				"app.kubernetes.io/component":  "webhook",
				"app.kubernetes.io/managed-by": "optipod-controller",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{*webhook},
	}

	// Try to get existing webhook configuration
	existing := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := lm.client.Get(ctx, client.ObjectKey{Name: lm.webhookName}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new webhook configuration
			if err := lm.client.Create(ctx, webhookConfig); err != nil {
				return fmt.Errorf("failed to create webhook configuration: %w", err)
			}
			lifecycleLog.Info("Created mutating webhook configuration", "name", lm.webhookName)
		} else {
			return fmt.Errorf("failed to get existing webhook configuration: %w", err)
		}
	} else {
		// Update existing webhook configuration
		existing.Webhooks = webhookConfig.Webhooks
		existing.Labels = webhookConfig.Labels
		if err := lm.client.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update webhook configuration: %w", err)
		}
		lifecycleLog.Info("Updated mutating webhook configuration", "name", lm.webhookName)
	}

	return nil
}

// cleanupWebhook removes the mutating webhook configuration
func (lm *LifecycleManager) cleanupWebhook(ctx context.Context) error {
	lifecycleLog.Info("Cleaning up mutating webhook configuration", "name", lm.webhookName)

	webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: lm.webhookName,
		},
	}

	err := lm.client.Delete(ctx, webhookConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			lifecycleLog.Info("Webhook configuration already deleted", "name", lm.webhookName)
			return nil
		}
		return fmt.Errorf("failed to delete webhook configuration: %w", err)
	}

	lifecycleLog.Info("Successfully cleaned up webhook configuration", "name", lm.webhookName)
	return nil
}

// validateCertificates validates the webhook certificates
func (lm *LifecycleManager) validateCertificates() error {
	lifecycleLog.Info("Validating webhook certificates", "certPath", lm.certPath, "keyPath", lm.keyPath)

	// Check if certificate files exist
	if _, err := os.Stat(lm.certPath); os.IsNotExist(err) {
		return fmt.Errorf("certificate file does not exist: %s", lm.certPath)
	}

	if _, err := os.Stat(lm.keyPath); os.IsNotExist(err) {
		return fmt.Errorf("key file does not exist: %s", lm.keyPath)
	}

	// Load and validate the certificate
	cert, err := tls.LoadX509KeyPair(lm.certPath, lm.keyPath)
	if err != nil {
		return fmt.Errorf("failed to load certificate pair: %w", err)
	}

	if len(cert.Certificate) == 0 {
		return fmt.Errorf("no certificates found in certificate file")
	}

	// Additional certificate validation could be added here
	// For example, checking expiration dates, subject names, etc.

	lifecycleLog.Info("Certificate validation successful")
	return nil
}

// waitForServerReady waits for the webhook server to become ready
func (lm *LifecycleManager) waitForServerReady(ctx context.Context) error {
	lifecycleLog.Info("Waiting for webhook server to become ready")

	// Use shorter timeout in test environments (5 seconds instead of 30)
	timeout := 30 * time.Second
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		// Running in test environment
		timeout = 5 * time.Second
	}

	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		// Check if server is ready by making a health check request
		// This is a simple check - in a real implementation, you might want to
		// make an actual HTTP request to the health endpoint
		if lm.server != nil && lm.server.server != nil {
			return true, nil
		}
		return false, nil
	})
}

// RotateCertificates handles certificate rotation
func (lm *LifecycleManager) RotateCertificates(ctx context.Context, newCertPath, newKeyPath string, newCABundle []byte) error {
	lifecycleLog.Info("Rotating webhook certificates",
		"oldCertPath", lm.certPath,
		"newCertPath", newCertPath,
		"oldKeyPath", lm.keyPath,
		"newKeyPath", newKeyPath)

	// Validate new certificates
	if _, err := tls.LoadX509KeyPair(newCertPath, newKeyPath); err != nil {
		return fmt.Errorf("failed to validate new certificates: %w", err)
	}

	// Update certificate paths
	lm.certPath = newCertPath
	lm.keyPath = newKeyPath
	lm.caBundle = newCABundle

	// Update webhook configuration with new CA bundle
	if err := lm.updateWebhookCABundle(ctx, newCABundle); err != nil {
		return fmt.Errorf("failed to update webhook CA bundle: %w", err)
	}

	// Restart the webhook server with new certificates
	if err := lm.server.Stop(ctx); err != nil {
		lifecycleLog.Error(err, "Failed to stop server during certificate rotation")
	}

	// Create new server with updated certificate paths
	lm.server = NewServer(lm.client, lm.certPath, lm.keyPath, lm.server.port, lm.eventRecorder)

	// Start the server again
	go func() {
		if err := lm.server.Start(ctx); err != nil {
			lifecycleLog.Error(err, "Failed to restart server after certificate rotation")
		}
	}()

	// Wait for server to be ready again
	if err := lm.waitForServerReady(ctx); err != nil {
		return fmt.Errorf("server failed to become ready after certificate rotation: %w", err)
	}

	lifecycleLog.Info("Certificate rotation completed successfully")
	return nil
}

// updateWebhookCABundle updates the CA bundle in the webhook configuration
func (lm *LifecycleManager) updateWebhookCABundle(ctx context.Context, caBundle []byte) error {
	webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := lm.client.Get(ctx, client.ObjectKey{Name: lm.webhookName}, webhookConfig); err != nil {
		return fmt.Errorf("failed to get webhook configuration: %w", err)
	}

	// Update CA bundle in all webhooks
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caBundle
	}

	if err := lm.client.Update(ctx, webhookConfig); err != nil {
		return fmt.Errorf("failed to update webhook configuration: %w", err)
	}

	return nil
}

// GetHealthStatus returns the health status of the webhook
func (lm *LifecycleManager) GetHealthStatus() map[string]interface{} {
	status := map[string]interface{}{
		"webhook_name":      lm.webhookName,
		"service_name":      lm.serviceName,
		"service_namespace": lm.serviceNamespace,
		"certificate_path":  lm.certPath,
		"key_path":          lm.keyPath,
	}

	// Check certificate validity
	if err := lm.validateCertificates(); err != nil {
		status["certificate_status"] = "invalid"
		status["certificate_error"] = err.Error()
	} else {
		status["certificate_status"] = "valid"
	}

	// Check server status
	if lm.server != nil && lm.server.server != nil {
		status["server_status"] = "running"
	} else {
		status["server_status"] = "stopped"
	}

	return status
}

// NeedsLeaderElection returns false since webhook lifecycle should run on all replicas
func (lm *LifecycleManager) NeedsLeaderElection() bool {
	return false
}

// Ensure LifecycleManager implements manager.Runnable
var _ manager.Runnable = &LifecycleManager{}
