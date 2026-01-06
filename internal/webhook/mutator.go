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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/observability"
)

var mutatorLog = logf.Log.WithName("webhook-mutator")

// Mutator handles pod mutation logic
type Mutator struct {
	client        client.Client
	eventRecorder *observability.EventRecorder
}

// ResourceRecommendation represents resource recommendations for a container
type ResourceRecommendation struct {
	ContainerName string
	CPU           *resource.Quantity
	Memory        *resource.Quantity
	CPULimit      *resource.Quantity
	MemoryLimit   *resource.Quantity
}

// NewMutator creates a new mutator instance
func NewMutator(k8sClient client.Client, eventRecorder *observability.EventRecorder) *Mutator {
	return &Mutator{
		client:        k8sClient,
		eventRecorder: eventRecorder,
	}
}

// MutatePod processes a pod mutation request
func (m *Mutator) MutatePod(req *AdmissionRequest) *AdmissionResponse {
	startTime := time.Now()
	ctx := context.Background()
	log := mutatorLog.WithValues("pod", fmt.Sprintf("%s/%s", req.Pod.Namespace, req.Pod.Name))

	log.V(1).Info("Processing pod mutation request")

	// Check if webhook strategy is enabled for this pod
	if !m.isWebhookEnabled(req.Pod) {
		log.V(1).Info("Webhook strategy not enabled for pod, skipping mutation")
		return &AdmissionResponse{
			Allowed: true,
			Message: "Webhook strategy not enabled",
		}
	}

	// Find matching policies with timing
	policyMatchStart := time.Now()
	matchingPolicies, err := m.findMatchingPolicies(ctx, req.Pod)
	policyMatchDuration := time.Since(policyMatchStart).Seconds()

	if err != nil {
		log.Error(err, "Failed to find matching policies")
		observability.RecordWebhookAdmissionFailure(req.Pod.Namespace, "policy_lookup_error")
		observability.RecordWebhookPolicyMatchingDuration(req.Pod.Namespace, 0, policyMatchDuration)
		if m.eventRecorder != nil {
			m.eventRecorder.RecordWebhookMutationFailure(req.Pod, req.Pod.Name, req.Pod.Namespace, err)
		}
		return &AdmissionResponse{
			Allowed: true, // Allow pod creation even if policy lookup fails
			Message: fmt.Sprintf("Failed to find matching policies: %v", err),
		}
	}

	observability.RecordWebhookPolicyMatchingDuration(req.Pod.Namespace, len(matchingPolicies), policyMatchDuration)

	if len(matchingPolicies) == 0 {
		log.V(1).Info("No matching policies found, skipping mutation")
		if m.eventRecorder != nil {
			m.eventRecorder.RecordWebhookPolicyMismatch(req.Pod, req.Pod.Name, req.Pod.Namespace)
		}
		return &AdmissionResponse{
			Allowed: true,
			Message: "No matching policies found",
		}
	}

	// Use the highest weight policy
	selectedPolicy := matchingPolicies[0]
	log.V(1).Info("Selected policy for mutation", "policy", selectedPolicy.Name, "weight", selectedPolicy.GetWeight())

	// Check if the selected policy uses webhook strategy
	if !selectedPolicy.IsWebhookStrategy() {
		log.V(1).Info("Selected policy does not use webhook strategy, skipping mutation")
		return &AdmissionResponse{
			Allowed: true,
			Message: "Policy does not use webhook strategy",
		}
	}

	// Extract resource recommendations from annotations
	recommendations := m.getRecommendationsFromAnnotations(req.Pod)

	if len(recommendations) == 0 {
		log.V(1).Info("No resource recommendations found in annotations, skipping mutation")
		return &AdmissionResponse{
			Allowed: true,
			Message: "No resource recommendations found",
		}
	}

	// Generate patches to modify pod resources
	patches := m.generateResourcePatches(req.Pod, recommendations, selectedPolicy.Name)

	if len(patches) == 0 {
		log.V(1).Info("No patches needed, pod resources already match recommendations")
		return &AdmissionResponse{
			Allowed: true,
			Message: "No patches needed",
		}
	}

	// Record successful mutation metrics
	mutationDuration := time.Since(startTime).Seconds()
	observability.RecordWebhookMutationDuration(req.Pod.Namespace, selectedPolicy.Name, mutationDuration)
	observability.RecordWebhookAdmissionSuccess(req.Pod.Namespace, selectedPolicy.Name, len(patches))

	if m.eventRecorder != nil {
		m.eventRecorder.RecordWebhookMutationSuccess(req.Pod, req.Pod.Name, req.Pod.Namespace, len(patches))
	}

	log.Info("Pod mutation successful",
		"policy", selectedPolicy.Name,
		"patches", len(patches),
		"duration", mutationDuration)

	return &AdmissionResponse{
		Allowed: true,
		Patches: patches,
		Message: fmt.Sprintf("Applied %d resource patches using policy %s", len(patches), selectedPolicy.Name),
	}
}

// isWebhookEnabled checks if webhook strategy is enabled for the pod
func (m *Mutator) isWebhookEnabled(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}

	// Check webhook enabled annotation
	if enabled, exists := pod.Annotations[optipodv1alpha1.AnnotationWebhookEnabled]; exists {
		return enabled == "true"
	}

	// Check strategy annotation
	if strategy, exists := pod.Annotations[optipodv1alpha1.AnnotationStrategy]; exists {
		return strategy == string(optipodv1alpha1.StrategyWebhook)
	}

	return false
}

// findMatchingPolicies finds optimization policies that match the pod
func (m *Mutator) findMatchingPolicies(ctx context.Context, pod *corev1.Pod) ([]*optipodv1alpha1.OptimizationPolicy, error) {
	// Get all optimization policies
	policyList := &optipodv1alpha1.OptimizationPolicyList{}
	if err := m.client.List(ctx, policyList); err != nil {
		return nil, fmt.Errorf("failed to list optimization policies: %w", err)
	}

	var matchingPolicies []*optipodv1alpha1.OptimizationPolicy

	for i := range policyList.Items {
		policy := &policyList.Items[i]

		// Skip disabled policies
		if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
			continue
		}

		// Check if policy matches pod
		if m.policyMatchesPod(ctx, policy, pod) {
			matchingPolicies = append(matchingPolicies, policy)
		}
	}

	// Sort by weight (highest first)
	for i := 0; i < len(matchingPolicies)-1; i++ {
		for j := i + 1; j < len(matchingPolicies); j++ {
			if matchingPolicies[i].GetWeight() < matchingPolicies[j].GetWeight() {
				matchingPolicies[i], matchingPolicies[j] = matchingPolicies[j], matchingPolicies[i]
			}
		}
	}

	return matchingPolicies, nil
}

// policyMatchesPod checks if a policy's selectors match a pod
func (m *Mutator) policyMatchesPod(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, pod *corev1.Pod) bool {
	// Check namespace selector
	if policy.Spec.Selector.NamespaceSelector != nil {
		// Get the namespace to check its labels
		namespace := &corev1.Namespace{}
		if err := m.client.Get(ctx, client.ObjectKey{Name: pod.Namespace}, namespace); err != nil {
			// If we can't get the namespace, assume it doesn't match
			return false
		}

		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.NamespaceSelector)
		if err != nil {
			return false
		}

		if !selector.Matches(labels.Set(namespace.Labels)) {
			return false
		}
	}

	// Check namespace allow/deny lists
	if policy.Spec.Selector.Namespaces != nil {
		// Check deny list first (takes precedence)
		for _, denied := range policy.Spec.Selector.Namespaces.Deny {
			if pod.Namespace == denied {
				return false
			}
		}

		// Check allow list if specified
		if len(policy.Spec.Selector.Namespaces.Allow) > 0 {
			allowed := false
			for _, allowedNs := range policy.Spec.Selector.Namespaces.Allow {
				if pod.Namespace == allowedNs {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}

	// Check workload selector - for pods, we check the pod's labels
	if policy.Spec.Selector.WorkloadSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.WorkloadSelector)
		if err != nil {
			return false
		}

		if !selector.Matches(labels.Set(pod.Labels)) {
			return false
		}
	}

	return true
}

// getRecommendationsFromAnnotations extracts resource recommendations from pod annotations
func (m *Mutator) getRecommendationsFromAnnotations(pod *corev1.Pod) []ResourceRecommendation {
	if pod.Annotations == nil {
		return nil
	}

	containerMap := make(map[string]*ResourceRecommendation)
	recommendations := make([]ResourceRecommendation, 0)

	// Parse annotations for each container
	for key, value := range pod.Annotations {
		var containerName string
		var resourceType string

		// Parse CPU request annotations
		if strings.HasPrefix(key, optipodv1alpha1.AnnotationCPURequestPrefix+".") {
			containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationCPURequestPrefix+".")
			resourceType = "cpu-request"
		} else if strings.HasPrefix(key, optipodv1alpha1.AnnotationMemoryRequestPrefix+".") {
			containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationMemoryRequestPrefix+".")
			resourceType = "memory-request"
		} else if strings.HasPrefix(key, optipodv1alpha1.AnnotationCPULimitPrefix+".") {
			containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationCPULimitPrefix+".")
			resourceType = "cpu-limit"
		} else if strings.HasPrefix(key, optipodv1alpha1.AnnotationMemoryLimitPrefix+".") {
			containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationMemoryLimitPrefix+".")
			resourceType = "memory-limit"
		} else {
			continue // Not a resource recommendation annotation
		}

		if containerName == "" {
			continue // Invalid annotation format
		}

		// Parse resource quantity
		quantity, err := resource.ParseQuantity(value)
		if err != nil {
			mutatorLog.Error(err, "Failed to parse resource quantity", "annotation", key, "value", value)

			// Record annotation parsing error metric
			observability.RecordWebhookAnnotationParsingError(pod.Namespace, pod.Name, key, "invalid_quantity")

			// Record event if event recorder is available
			if m.eventRecorder != nil {
				m.eventRecorder.RecordWebhookAnnotationError(pod, pod.Name, pod.Namespace, key, err)
			}

			continue // Skip invalid quantities but don't fail the entire operation
		}

		// Get or create recommendation for this container
		if containerMap[containerName] == nil {
			containerMap[containerName] = &ResourceRecommendation{
				ContainerName: containerName,
			}
		}

		rec := containerMap[containerName]

		// Set the appropriate resource
		switch resourceType {
		case "cpu-request":
			rec.CPU = &quantity
		case "memory-request":
			rec.Memory = &quantity
		case "cpu-limit":
			rec.CPULimit = &quantity
		case "memory-limit":
			rec.MemoryLimit = &quantity
		}
	}

	// Convert map to slice
	for _, rec := range containerMap {
		recommendations = append(recommendations, *rec)
	}

	mutatorLog.V(1).Info("Extracted resource recommendations from annotations",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"containers", len(recommendations))

	return recommendations
}

// generateResourcePatches generates JSON patches to modify pod resources
func (m *Mutator) generateResourcePatches(pod *corev1.Pod, recommendations []ResourceRecommendation, policyName string) []PatchOperation {
	patches := make([]PatchOperation, 0)

	// Create a map of recommendations by container name for quick lookup
	recMap := make(map[string]ResourceRecommendation)
	for _, rec := range recommendations {
		recMap[rec.ContainerName] = rec
	}

	// Process each container in the pod
	for i, container := range pod.Spec.Containers {
		rec, exists := recMap[container.Name]
		if !exists {
			continue // No recommendations for this container
		}

		containerPath := fmt.Sprintf("/spec/containers/%d", i)

		// Initialize resources if not present
		if container.Resources.Requests == nil && (rec.CPU != nil || rec.Memory != nil) {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/requests",
				Value: map[string]interface{}{},
			})
		}

		if container.Resources.Limits == nil && (rec.CPULimit != nil || rec.MemoryLimit != nil) {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/limits",
				Value: map[string]interface{}{},
			})
		}

		// Add CPU request patch
		if rec.CPU != nil {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/requests/cpu",
				Value: rec.CPU.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "cpu-request")
		}

		// Add memory request patch
		if rec.Memory != nil {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/requests/memory",
				Value: rec.Memory.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "memory-request")
		}

		// Add CPU limit patch
		if rec.CPULimit != nil {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/limits/cpu",
				Value: rec.CPULimit.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "cpu-limit")
		}

		// Add memory limit patch
		if rec.MemoryLimit != nil {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  containerPath + "/resources/limits/memory",
				Value: rec.MemoryLimit.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "memory-limit")
		}
	}

	mutatorLog.V(1).Info("Generated resource patches",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
		"policy", policyName,
		"patches", len(patches))

	return patches
}
