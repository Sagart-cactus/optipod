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

// RecordWorkloadUpdateSuccess records a successful workload update event
func (er *EventRecorder) RecordWorkloadUpdateSuccess(object runtime.Object, workloadName, namespace string, method string) {
	message := fmt.Sprintf("Successfully updated resource requests for workload %s/%s using %s method", namespace, workloadName, method)
	er.recorder.Event(object, corev1.EventTypeNormal, "UpdateSuccess", message)
}

// RecordWorkloadUpdateFailure records a failed workload update event with actionable suggestions
func (er *EventRecorder) RecordWorkloadUpdateFailure(object runtime.Object, workloadName, namespace string, err error) {
	message := fmt.Sprintf("Failed to update workload %s/%s: %v. Suggestion: Check RBAC permissions and ensure the workload exists", namespace, workloadName, err)
	er.recorder.Event(object, corev1.EventTypeWarning, "UpdateFailed", message)
}

// RecordPolicyValidationError records a policy validation error event
func (er *EventRecorder) RecordPolicyValidationError(object runtime.Object, policyName string, err error) {
	message := fmt.Sprintf("Policy validation failed for %s: %v. Suggestion: Review policy configuration and ensure all required fields are valid", policyName, err)
	er.recorder.Event(object, corev1.EventTypeWarning, "ValidationFailed", message)
}

// RecordMetricsCollectionError records a metrics collection error event
func (er *EventRecorder) RecordMetricsCollectionError(object runtime.Object, workloadName, namespace, provider string, err error) {
	message := fmt.Sprintf("Failed to collect metrics for workload %s/%s from %s: %v. Suggestion: Verify metrics provider is accessible and workload has sufficient runtime data", namespace, workloadName, provider, err)
	er.recorder.Event(object, corev1.EventTypeWarning, "MetricsCollectionFailed", message)
}

// RecordRBACError records an RBAC permission error event
func (er *EventRecorder) RecordRBACError(object runtime.Object, workloadName, namespace, operation string) {
	message := fmt.Sprintf("Insufficient RBAC permissions to %s workload %s/%s. Suggestion: Grant OptiPod service account appropriate permissions (get, list, patch, update) for this resource type", operation, namespace, workloadName)
	er.recorder.Event(object, corev1.EventTypeWarning, "RBACError", message)
}

// RecordInPlaceResizeUnavailable records an event when in-place resize is not available
func (er *EventRecorder) RecordInPlaceResizeUnavailable(object runtime.Object, workloadName, namespace string) {
	message := fmt.Sprintf("In-place resize not available for workload %s/%s. Suggestion: Enable InPlacePodVerticalScaling feature gate or allow recreate strategy in policy", namespace, workloadName)
	er.recorder.Event(object, corev1.EventTypeWarning, "InPlaceResizeUnavailable", message)
}

// RecordRecommendationGenerated records when a recommendation is generated
func (er *EventRecorder) RecordRecommendationGenerated(object runtime.Object, workloadName, namespace string, containerCount int) {
	message := fmt.Sprintf("Generated resource recommendations for %d container(s) in workload %s/%s", containerCount, namespace, workloadName)
	er.recorder.Event(object, corev1.EventTypeNormal, "RecommendationGenerated", message)
}

// RecordWorkloadSkipped records when a workload is skipped with a reason
func (er *EventRecorder) RecordWorkloadSkipped(object runtime.Object, workloadName, namespace, reason string) {
	message := fmt.Sprintf("Skipped workload %s/%s: %s", namespace, workloadName, reason)
	er.recorder.Event(object, corev1.EventTypeNormal, "WorkloadSkipped", message)
}

// RecordSSAOwnershipTaken records an event when OptipPod takes field ownership via Server-Side Apply
func (er *EventRecorder) RecordSSAOwnershipTaken(object runtime.Object, workloadName, namespace, previousOwner string) {
	var message string
	if previousOwner != "" {
		message = fmt.Sprintf("Took ownership of resource fields for workload %s/%s via Server-Side Apply (previous owner: %s)", namespace, workloadName, previousOwner)
	} else {
		message = fmt.Sprintf("Took ownership of resource fields for workload %s/%s via Server-Side Apply", namespace, workloadName)
	}
	er.recorder.Event(object, corev1.EventTypeNormal, EventReasonSSAOwnershipTaken, message)
}

// RecordSSAConflict records an event when a field ownership conflict occurs during Server-Side Apply
func (er *EventRecorder) RecordSSAConflict(object runtime.Object, workloadName, namespace, conflictingManager string, err error) {
	message := fmt.Sprintf("Server-Side Apply conflict for workload %s/%s: field manager '%s' owns conflicting fields. Error: %v. Suggestion: Review field ownership or enable Force flag to take ownership", namespace, workloadName, conflictingManager, err)
	er.recorder.Event(object, corev1.EventTypeWarning, EventReasonSSAConflict, message)
}
