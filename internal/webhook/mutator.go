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

	appsv1 "k8s.io/api/apps/v1"
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

const (
	// Constants for string literals
	trueValue      = "true"
	appsV1APIGroup = "apps/v1"
)

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
// For ArgoCD compatibility, it checks both pod annotations and parent deployment annotations
func (m *Mutator) isWebhookEnabled(pod *corev1.Pod) bool {
	ctx := context.Background()

	// First check pod annotations
	if pod.Annotations != nil {
		// Check webhook enabled annotation
		if enabled, exists := pod.Annotations[optipodv1alpha1.AnnotationWebhookEnabled]; exists {
			return enabled == trueValue
		}

		// Check strategy annotation
		if strategy, exists := pod.Annotations[optipodv1alpha1.AnnotationStrategy]; exists {
			return strategy == string(optipodv1alpha1.StrategyWebhook)
		}
	}

	// Fall back to checking parent deployment annotations (ArgoCD-compatible)
	parentAnnotations := m.getParentDeploymentAnnotations(ctx, pod)
	if len(parentAnnotations) > 0 {
		// Check webhook enabled annotation
		if enabled, exists := parentAnnotations[optipodv1alpha1.AnnotationWebhookEnabled]; exists {
			return enabled == trueValue
		}

		// Check strategy annotation
		if strategy, exists := parentAnnotations[optipodv1alpha1.AnnotationStrategy]; exists {
			return strategy == string(optipodv1alpha1.StrategyWebhook)
		}
	}

	return false
}

// getParentDeploymentAnnotations retrieves annotations from the parent workload (Deployment, StatefulSet, or DaemonSet)
// This is needed for ArgoCD compatibility since ArgoCD's self-heal reverts pod template annotations
// but allows extra annotations on workload metadata
func (m *Mutator) getParentDeploymentAnnotations(ctx context.Context, pod *corev1.Pod) map[string]string {
	log := mutatorLog.WithValues("pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))

	// Check if pod has owner references
	if len(pod.OwnerReferences) == 0 {
		log.V(2).Info("Pod has no owner references, cannot lookup parent workload")
		return nil
	}

	// Check for direct StatefulSet or DaemonSet ownership
	for _, owner := range pod.OwnerReferences {
		if owner.APIVersion == appsV1APIGroup {
			switch owner.Kind {
			case "StatefulSet":
				return m.getStatefulSetAnnotations(ctx, pod.Namespace, owner.Name)
			case "DaemonSet":
				return m.getDaemonSetAnnotations(ctx, pod.Namespace, owner.Name)
			}
		}
	}

	// Check for Deployment ownership (via ReplicaSet)
	var replicaSetName string
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" && owner.APIVersion == appsV1APIGroup {
			replicaSetName = owner.Name
			break
		}
	}

	if replicaSetName == "" {
		log.V(2).Info("Pod is not owned by a ReplicaSet, StatefulSet, or DaemonSet")
		return nil
	}

	// Get the ReplicaSet
	replicaSet := &appsv1.ReplicaSet{}
	if err := m.client.Get(ctx, client.ObjectKey{
		Namespace: pod.Namespace,
		Name:      replicaSetName,
	}, replicaSet); err != nil {
		log.V(1).Info("Failed to get ReplicaSet", "replicaset", replicaSetName, "error", err)
		return nil
	}

	// Find Deployment owner of ReplicaSet
	var deploymentName string
	for _, owner := range replicaSet.OwnerReferences {
		if owner.Kind == "Deployment" && owner.APIVersion == appsV1APIGroup {
			deploymentName = owner.Name
			break
		}
	}

	if deploymentName == "" {
		log.V(2).Info("ReplicaSet is not owned by a Deployment", "replicaset", replicaSetName)
		return nil
	}

	// Get the Deployment
	deployment := &appsv1.Deployment{}
	if err := m.client.Get(ctx, client.ObjectKey{
		Namespace: pod.Namespace,
		Name:      deploymentName,
	}, deployment); err != nil {
		log.V(1).Info("Failed to get Deployment", "deployment", deploymentName, "error", err)
		return nil
	}

	log.V(1).Info("Found parent deployment", "deployment", deploymentName, "annotations", len(deployment.Annotations))
	return deployment.Annotations
}

// getStatefulSetAnnotations retrieves annotations from a StatefulSet
func (m *Mutator) getStatefulSetAnnotations(ctx context.Context, namespace, name string) map[string]string {
	log := mutatorLog.WithValues("statefulset", fmt.Sprintf("%s/%s", namespace, name))

	statefulSet := &appsv1.StatefulSet{}
	if err := m.client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, statefulSet); err != nil {
		log.V(1).Info("Failed to get StatefulSet", "error", err)
		return nil
	}

	log.V(1).Info("Found parent StatefulSet", "annotations", len(statefulSet.Annotations))
	return statefulSet.Annotations
}

// getDaemonSetAnnotations retrieves annotations from a DaemonSet
func (m *Mutator) getDaemonSetAnnotations(ctx context.Context, namespace, name string) map[string]string {
	log := mutatorLog.WithValues("daemonset", fmt.Sprintf("%s/%s", namespace, name))

	daemonSet := &appsv1.DaemonSet{}
	if err := m.client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, daemonSet); err != nil {
		log.V(1).Info("Failed to get DaemonSet", "error", err)
		return nil
	}

	log.V(1).Info("Found parent DaemonSet", "annotations", len(daemonSet.Annotations))
	return daemonSet.Annotations
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
// For ArgoCD compatibility, it first tries to read from the parent deployment's metadata annotations
// since ArgoCD's self-heal will revert pod template annotations but allows extra deployment metadata annotations
func (m *Mutator) getRecommendationsFromAnnotations(pod *corev1.Pod) []ResourceRecommendation {
	ctx := context.Background()
	log := mutatorLog.WithValues("pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))

	// Try to get annotations from parent deployment first (ArgoCD-compatible approach)
	parentAnnotations := m.getParentDeploymentAnnotations(ctx, pod)

	// Fall back to pod annotations if no parent annotations found
	annotationsToUse := parentAnnotations
	if len(annotationsToUse) == 0 {
		if pod.Annotations == nil {
			return nil
		}
		annotationsToUse = pod.Annotations
		log.V(1).Info("Using pod annotations (no parent deployment annotations found)")
	} else {
		log.V(1).Info("Using parent deployment annotations for ArgoCD compatibility")
	}

	containerMap := make(map[string]*ResourceRecommendation)
	recommendations := make([]ResourceRecommendation, 0)

	// Parse annotations for each container
	for key, value := range annotationsToUse {
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
