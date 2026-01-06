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

package observability

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// WorkloadsMonitored tracks the number of workloads currently monitored by OptiPod
	WorkloadsMonitored = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_monitored",
			Help: "Number of workloads currently monitored by OptiPod",
		},
		[]string{"namespace", "policy"},
	)

	// WorkloadsUpdated tracks the number of workloads updated in the last reconciliation cycle
	WorkloadsUpdated = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_updated",
			Help: "Number of workloads updated in the last reconciliation cycle",
		},
		[]string{"namespace", "policy"},
	)

	// WorkloadsSkipped tracks the number of workloads skipped with reasons
	WorkloadsSkipped = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_skipped",
			Help: "Number of workloads skipped in the last reconciliation cycle",
		},
		[]string{"namespace", "policy", "reason"},
	)

	// ReconciliationDuration tracks the duration of reconciliation cycles
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_reconciliation_duration_seconds",
			Help:    "Duration of reconciliation cycles in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy"},
	)

	// MetricsCollectionDuration tracks the duration of metrics collection operations
	MetricsCollectionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_metrics_collection_duration_seconds",
			Help:    "Duration of metrics collection operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider"},
	)

	// ReconciliationErrors tracks errors during reconciliation
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"policy", "error_type"},
	)

	// RecommendationsTotal tracks the total number of recommendations generated
	RecommendationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_recommendations_total",
			Help: "Total number of recommendations generated",
		},
		[]string{"policy"},
	)

	// ApplicationsTotal tracks the total number of applications (updates) performed
	ApplicationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_applications_total",
			Help: "Total number of resource updates applied",
		},
		[]string{"policy", "method"},
	)

	// SSAPatchTotal tracks the total number of Server-Side Apply patch operations
	SSAPatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_ssa_patch_total",
			Help: "Total number of Server-Side Apply patch operations",
		},
		[]string{"policy", "namespace", "workload", "kind", "status", "patch_type"},
	)

	// OptimizationSuccessTotal tracks successful optimizations
	OptimizationSuccessTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_optimization_success_total",
			Help: "Total number of successful optimizations",
		},
		[]string{"policy", "namespace", "workload", "kind", "method"},
	)

	// OptimizationFailureTotal tracks failed optimizations
	OptimizationFailureTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_optimization_failure_total",
			Help: "Total number of failed optimizations",
		},
		[]string{"policy", "namespace", "workload", "kind", "method", "reason"},
	)

	// ResourceChangesMagnitude tracks the magnitude of resource changes
	ResourceChangesMagnitude = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_resource_changes_magnitude",
			Help:    "Magnitude of resource changes in percentage",
			Buckets: []float64{-90, -75, -50, -25, -10, -5, 0, 5, 10, 25, 50, 75, 100, 200, 500},
		},
		[]string{"policy", "namespace", "workload", "resource_type"},
	)

	// DefaultMultiplierUsage tracks usage of default multipliers
	DefaultMultiplierUsage = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_default_multiplier_usage_total",
			Help: "Total number of times default multipliers were used",
		},
		[]string{"policy", "resource_type", "multiplier_value"},
	)

	// OptimizationDecisionDuration tracks time spent making optimization decisions
	OptimizationDecisionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_optimization_decision_duration_seconds",
			Help:    "Duration of optimization decision making in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy", "workload_kind"},
	)

	// Webhook-specific metrics

	// WebhookAdmissionRequestsTotal tracks the total number of webhook admission requests
	WebhookAdmissionRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_webhook_admission_requests_total",
			Help: "Total number of webhook admission requests received",
		},
		[]string{"namespace", "pod_name", "dry_run"},
	)

	// WebhookAdmissionSuccessTotal tracks successful webhook admissions
	WebhookAdmissionSuccessTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_webhook_admission_success_total",
			Help: "Total number of successful webhook admissions",
		},
		[]string{"namespace", "policy", "patches_applied"},
	)

	// WebhookAdmissionFailuresTotal tracks failed webhook admissions
	WebhookAdmissionFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_webhook_admission_failures_total",
			Help: "Total number of failed webhook admissions",
		},
		[]string{"namespace", "failure_reason"},
	)

	// WebhookMutationDuration tracks the duration of webhook mutation operations
	WebhookMutationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_webhook_mutation_duration_seconds",
			Help:    "Duration of webhook mutation operations in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"namespace", "policy"},
	)

	// WebhookPatchesAppliedTotal tracks the number of patches applied by the webhook
	WebhookPatchesAppliedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_webhook_patches_applied_total",
			Help: "Total number of patches applied by the webhook",
		},
		[]string{"namespace", "policy", "container_name", "resource_type"},
	)

	// WebhookAnnotationParsingErrorsTotal tracks annotation parsing errors
	WebhookAnnotationParsingErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_webhook_annotation_parsing_errors_total",
			Help: "Total number of annotation parsing errors in webhook",
		},
		[]string{"namespace", "pod_name", "annotation_key", "error_type"},
	)

	// WebhookPolicyMatchingDuration tracks time spent matching policies
	WebhookPolicyMatchingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_webhook_policy_matching_duration_seconds",
			Help:    "Duration of policy matching operations in webhook",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
		[]string{"namespace", "policies_found"},
	)

	// WebhookServerHealthStatus tracks webhook server health status
	WebhookServerHealthStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_webhook_server_health_status",
			Help: "Webhook server health status (1 = healthy, 0 = unhealthy)",
		},
		[]string{"endpoint"},
	)

	// WebhookCertificateExpiryTime tracks webhook certificate expiry time
	WebhookCertificateExpiryTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_webhook_certificate_expiry_timestamp_seconds",
			Help: "Webhook certificate expiry time as Unix timestamp",
		},
		[]string{"cert_type"},
	)
)

func init() {
	// Register metrics automatically when the package is imported
	// This ensures metrics work in both production and test environments
	RegisterMetrics()
}

// RegisterMetrics registers all OptiPod metrics with the controller-runtime metrics registry
func RegisterMetrics() {
	// Use Register instead of MustRegister to avoid panics if metrics are already registered
	_ = metrics.Registry.Register(WorkloadsMonitored)
	_ = metrics.Registry.Register(WorkloadsUpdated)
	_ = metrics.Registry.Register(WorkloadsSkipped)
	_ = metrics.Registry.Register(ReconciliationDuration)
	_ = metrics.Registry.Register(MetricsCollectionDuration)
	_ = metrics.Registry.Register(ReconciliationErrors)
	_ = metrics.Registry.Register(RecommendationsTotal)
	_ = metrics.Registry.Register(ApplicationsTotal)
	_ = metrics.Registry.Register(SSAPatchTotal)
	_ = metrics.Registry.Register(OptimizationSuccessTotal)
	_ = metrics.Registry.Register(OptimizationFailureTotal)
	_ = metrics.Registry.Register(ResourceChangesMagnitude)
	_ = metrics.Registry.Register(DefaultMultiplierUsage)
	_ = metrics.Registry.Register(OptimizationDecisionDuration)

	// Register webhook-specific metrics
	_ = metrics.Registry.Register(WebhookAdmissionRequestsTotal)
	_ = metrics.Registry.Register(WebhookAdmissionSuccessTotal)
	_ = metrics.Registry.Register(WebhookAdmissionFailuresTotal)
	_ = metrics.Registry.Register(WebhookMutationDuration)
	_ = metrics.Registry.Register(WebhookPatchesAppliedTotal)
	_ = metrics.Registry.Register(WebhookAnnotationParsingErrorsTotal)
	_ = metrics.Registry.Register(WebhookPolicyMatchingDuration)
	_ = metrics.Registry.Register(WebhookServerHealthStatus)
	_ = metrics.Registry.Register(WebhookCertificateExpiryTime)
}

// RecordSSAPatch records an SSA patch operation
func RecordSSAPatch(policy, namespace, workload, kind, status, patchType string) {
	SSAPatchTotal.WithLabelValues(policy, namespace, workload, kind, status, patchType).Inc()
}

// RecordOptimizationSuccess records a successful optimization
func RecordOptimizationSuccess(policy, namespace, workload, kind, method string) {
	OptimizationSuccessTotal.WithLabelValues(policy, namespace, workload, kind, method).Inc()
}

// RecordOptimizationFailure records a failed optimization
func RecordOptimizationFailure(policy, namespace, workload, kind, method, reason string) {
	OptimizationFailureTotal.WithLabelValues(policy, namespace, workload, kind, method, reason).Inc()
}

// RecordResourceChangeMagnitude records the magnitude of a resource change
func RecordResourceChangeMagnitude(policy, namespace, workload, resourceType string, changePercent float64) {
	ResourceChangesMagnitude.WithLabelValues(policy, namespace, workload, resourceType).Observe(changePercent)
}

// RecordDefaultMultiplierUsage records usage of default multipliers
func RecordDefaultMultiplierUsage(policy, resourceType, multiplierValue string) {
	DefaultMultiplierUsage.WithLabelValues(policy, resourceType, multiplierValue).Inc()
}

// RecordOptimizationDecisionDuration records the duration of optimization decision making
func RecordOptimizationDecisionDuration(policy, workloadKind string, duration float64) {
	OptimizationDecisionDuration.WithLabelValues(policy, workloadKind).Observe(duration)
}

// Webhook metric recording functions

// RecordWebhookAdmissionRequest records a webhook admission request
func RecordWebhookAdmissionRequest(namespace, podName string, dryRun bool) {
	dryRunStr := "false"
	if dryRun {
		dryRunStr = "true"
	}
	WebhookAdmissionRequestsTotal.WithLabelValues(namespace, podName, dryRunStr).Inc()
}

// RecordWebhookAdmissionSuccess records a successful webhook admission
func RecordWebhookAdmissionSuccess(namespace, policy string, patchesApplied int) {
	patchesStr := "0"
	if patchesApplied > 0 {
		patchesStr = "1+"
	}
	WebhookAdmissionSuccessTotal.WithLabelValues(namespace, policy, patchesStr).Inc()
}

// RecordWebhookAdmissionFailure records a failed webhook admission
func RecordWebhookAdmissionFailure(namespace, failureReason string) {
	WebhookAdmissionFailuresTotal.WithLabelValues(namespace, failureReason).Inc()
}

// RecordWebhookMutationDuration records the duration of a webhook mutation operation
func RecordWebhookMutationDuration(namespace, policy string, duration float64) {
	WebhookMutationDuration.WithLabelValues(namespace, policy).Observe(duration)
}

// RecordWebhookPatchApplied records a patch applied by the webhook
func RecordWebhookPatchApplied(namespace, policy, containerName, resourceType string) {
	WebhookPatchesAppliedTotal.WithLabelValues(namespace, policy, containerName, resourceType).Inc()
}

// RecordWebhookAnnotationParsingError records an annotation parsing error
func RecordWebhookAnnotationParsingError(namespace, podName, annotationKey, errorType string) {
	WebhookAnnotationParsingErrorsTotal.WithLabelValues(namespace, podName, annotationKey, errorType).Inc()
}

// RecordWebhookPolicyMatchingDuration records the duration of policy matching
func RecordWebhookPolicyMatchingDuration(namespace string, policiesFound int, duration float64) {
	policiesFoundStr := fmt.Sprintf("%d", policiesFound)
	WebhookPolicyMatchingDuration.WithLabelValues(namespace, policiesFoundStr).Observe(duration)
}

// SetWebhookServerHealthStatus sets the webhook server health status
func SetWebhookServerHealthStatus(endpoint string, healthy bool) {
	status := 0.0
	if healthy {
		status = 1.0
	}
	WebhookServerHealthStatus.WithLabelValues(endpoint).Set(status)
}

// SetWebhookCertificateExpiryTime sets the webhook certificate expiry time
func SetWebhookCertificateExpiryTime(certType string, expiryTime float64) {
	WebhookCertificateExpiryTime.WithLabelValues(certType).Set(expiryTime)
}
