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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// Event reason constants
const (
	// EventReasonSSAOwnershipTaken indicates OptipPod took ownership of resource fields
	EventReasonSSAOwnershipTaken = "SSAOwnershipTaken"

	// EventReasonSSAConflict indicates a field ownership conflict
	EventReasonSSAConflict = "SSAConflict"

	// Webhook-specific event reasons
	// EventReasonWebhookMutationSuccess indicates successful webhook mutation
	EventReasonWebhookMutationSuccess = "WebhookMutationSuccess"

	// EventReasonWebhookMutationFailure indicates failed webhook mutation
	EventReasonWebhookMutationFailure = "WebhookMutationFailure"

	// EventReasonWebhookAnnotationError indicates annotation parsing error
	EventReasonWebhookAnnotationError = "WebhookAnnotationError"

	// EventReasonWebhookPolicyMismatch indicates no matching policies found
	EventReasonWebhookPolicyMismatch = "WebhookPolicyMismatch"

	// EventReasonWebhookServerError indicates webhook server error
	EventReasonWebhookServerError = "WebhookServerError"

	// EventReasonWebhookCertificateError indicates certificate-related error
	EventReasonWebhookCertificateError = "WebhookCertificateError"
)

// EventRecorder wraps the Kubernetes event recorder with OptiPod-specific event creation methods
type EventRecorder struct {
	recorder record.EventRecorder
}

// NewEventRecorder creates a new OptiPod event recorder
func NewEventRecorder(recorder record.EventRecorder) *EventRecorder {
	return &EventRecorder{
		recorder: recorder,
	}
}

// recordEvent safely records an event, handling nil recorder gracefully
func (er *EventRecorder) recordEvent(object runtime.Object, eventType, reason, message string) {
	if er.recorder == nil {
		return // Skip event recording when recorder is nil (useful for tests)
	}
	er.recorder.Event(object, eventType, reason, message)
}

// RecordWorkloadUpdateSuccess records a successful workload update event
func (er *EventRecorder) RecordWorkloadUpdateSuccess(object runtime.Object, workloadName, namespace string, method string) {
	message := fmt.Sprintf("Successfully updated resource requests for workload %s/%s using %s method", namespace, workloadName, method)
	er.recordEvent(object, corev1.EventTypeNormal, "UpdateSuccess", message)
}

// RecordWorkloadUpdateFailure records a failed workload update event with actionable suggestions
func (er *EventRecorder) RecordWorkloadUpdateFailure(object runtime.Object, workloadName, namespace string, err error) {
	message := fmt.Sprintf("Failed to update workload %s/%s: %v. Suggestion: Check RBAC permissions and ensure the workload exists", namespace, workloadName, err)
	er.recordEvent(object, corev1.EventTypeWarning, "UpdateFailed", message)
}

// RecordPolicyValidationError records a policy validation error event
func (er *EventRecorder) RecordPolicyValidationError(object runtime.Object, policyName string, err error) {
	message := fmt.Sprintf("Policy validation failed for %s: %v. Suggestion: Review policy configuration and ensure all required fields are valid", policyName, err)
	er.recordEvent(object, corev1.EventTypeWarning, "ValidationFailed", message)
}

// RecordMetricsCollectionError records a metrics collection error event
func (er *EventRecorder) RecordMetricsCollectionError(object runtime.Object, workloadName, namespace, provider string, err error) {
	message := fmt.Sprintf("Failed to collect metrics for workload %s/%s from %s: %v. Suggestion: Verify metrics provider is accessible and workload has sufficient runtime data", namespace, workloadName, provider, err)
	er.recordEvent(object, corev1.EventTypeWarning, "MetricsCollectionFailed", message)
}

// RecordRBACError records an RBAC permission error event
func (er *EventRecorder) RecordRBACError(object runtime.Object, workloadName, namespace, operation string) {
	message := fmt.Sprintf("Insufficient RBAC permissions to %s workload %s/%s. Suggestion: Grant OptiPod service account appropriate permissions (get, list, patch, update) for this resource type", operation, namespace, workloadName)
	er.recordEvent(object, corev1.EventTypeWarning, "RBACError", message)
}

// RecordInPlaceResizeUnavailable records an event when in-place resize is not available
func (er *EventRecorder) RecordInPlaceResizeUnavailable(object runtime.Object, workloadName, namespace string) {
	message := fmt.Sprintf("In-place resize not available for workload %s/%s. Suggestion: Enable InPlacePodVerticalScaling feature gate or allow recreate strategy in policy", namespace, workloadName)
	er.recordEvent(object, corev1.EventTypeWarning, "InPlaceResizeUnavailable", message)
}

// RecordRecommendationGenerated records when a recommendation is generated
func (er *EventRecorder) RecordRecommendationGenerated(object runtime.Object, workloadName, namespace string, containerCount int) {
	message := fmt.Sprintf("Generated resource recommendations for %d container(s) in workload %s/%s", containerCount, namespace, workloadName)
	er.recordEvent(object, corev1.EventTypeNormal, "RecommendationGenerated", message)
}

// RecordWorkloadSkipped records when a workload is skipped with a reason
func (er *EventRecorder) RecordWorkloadSkipped(object runtime.Object, workloadName, namespace, reason string) {
	message := fmt.Sprintf("Skipped workload %s/%s: %s", namespace, workloadName, reason)
	er.recordEvent(object, corev1.EventTypeNormal, "WorkloadSkipped", message)
}

// RecordSSAOwnershipTaken records an event when OptipPod takes field ownership via Server-Side Apply
func (er *EventRecorder) RecordSSAOwnershipTaken(object runtime.Object, workloadName, namespace, previousOwner string) {
	var message string
	if previousOwner != "" {
		message = fmt.Sprintf("Took ownership of resource fields for workload %s/%s via Server-Side Apply (previous owner: %s)", namespace, workloadName, previousOwner)
	} else {
		message = fmt.Sprintf("Took ownership of resource fields for workload %s/%s via Server-Side Apply", namespace, workloadName)
	}
	er.recordEvent(object, corev1.EventTypeNormal, EventReasonSSAOwnershipTaken, message)
}

// RecordSSAConflict records an event when a field ownership conflict occurs during Server-Side Apply
func (er *EventRecorder) RecordSSAConflict(object runtime.Object, workloadName, namespace, conflictingManager string, err error) {
	message := fmt.Sprintf("Server-Side Apply conflict for workload %s/%s: field manager '%s' owns conflicting fields. Error: %v. Suggestion: Review field ownership or enable Force flag to take ownership", namespace, workloadName, conflictingManager, err)
	er.recordEvent(object, corev1.EventTypeWarning, EventReasonSSAConflict, message)
}

// Webhook-specific event recording methods

// RecordWebhookMutationSuccess records a successful webhook mutation event
func (er *EventRecorder) RecordWebhookMutationSuccess(object runtime.Object, podName, namespace string, patchesApplied int) {
	message := fmt.Sprintf("Successfully applied %d resource patches to pod %s/%s via webhook mutation", patchesApplied, namespace, podName)
	er.recordEvent(object, corev1.EventTypeNormal, EventReasonWebhookMutationSuccess, message)
}

// RecordWebhookMutationFailure records a failed webhook mutation event
func (er *EventRecorder) RecordWebhookMutationFailure(object runtime.Object, podName, namespace string, err error) {
	message := fmt.Sprintf("Failed to mutate pod %s/%s via webhook: %v. Suggestion: Check webhook configuration and ensure annotations are valid", namespace, podName, err)
	er.recordEvent(object, corev1.EventTypeWarning, EventReasonWebhookMutationFailure, message)
}

// RecordWebhookAnnotationError records an annotation parsing error event
func (er *EventRecorder) RecordWebhookAnnotationError(object runtime.Object, podName, namespace, annotationKey string, err error) {
	message := fmt.Sprintf("Failed to parse annotation '%s' for pod %s/%s: %v. Suggestion: Verify annotation format and resource quantity values", annotationKey, namespace, podName, err)
	er.recordEvent(object, corev1.EventTypeWarning, EventReasonWebhookAnnotationError, message)
}

// RecordWebhookPolicyMismatch records when no matching policies are found
func (er *EventRecorder) RecordWebhookPolicyMismatch(object runtime.Object, podName, namespace string) {
	message := fmt.Sprintf("No matching OptimizationPolicy found for pod %s/%s. Suggestion: Ensure policy selectors match pod labels and namespace", namespace, podName)
	er.recordEvent(object, corev1.EventTypeNormal, EventReasonWebhookPolicyMismatch, message)
}

// RecordWebhookServerError records a webhook server error event
func (er *EventRecorder) RecordWebhookServerError(object runtime.Object, endpoint string, err error) {
	message := fmt.Sprintf("Webhook server error on endpoint %s: %v. Suggestion: Check webhook server health and certificate configuration", endpoint, err)
	er.recordEvent(object, corev1.EventTypeWarning, EventReasonWebhookServerError, message)
}

// RecordWebhookCertificateError records a certificate-related error event
func (er *EventRecorder) RecordWebhookCertificateError(object runtime.Object, certType string, err error) {
	message := fmt.Sprintf("Webhook certificate error (%s): %v. Suggestion: Verify certificate validity and rotation configuration", certType, err)
	er.recordEvent(object, corev1.EventTypeWarning, EventReasonWebhookCertificateError, message)
}
