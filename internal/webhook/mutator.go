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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/constraints"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/nodesizing"
	"github.com/optipod/optipod/internal/observability"
	"github.com/optipod/optipod/internal/policy"
)

var mutatorLog = logf.Log.WithName("webhook-mutator")

const (
	// Constants for string literals
	trueValue      = "true"
	appsV1APIGroup = "apps/v1"
	// patchOpAdd is the JSON patch "add" operation.
	patchOpAdd = "add"
	// logKeyPath is the structured-log key carrying the TLS key path.
	logKeyPath = "key_path"
)

// Mutator handles pod mutation logic
type Mutator struct {
	client        client.Client
	eventRecorder *observability.EventRecorder
	dryRun        bool
}

// ResourceRecommendation represents resource recommendations for a container
type ResourceRecommendation struct {
	RemoveCPURequest    bool
	RemoveMemoryRequest bool
	// Excluded from the resource hash: renewing freshness is not a resource change.
	ValidUntil        time.Time `json:"-"`
	RemoveCPULimit    bool
	RemoveMemoryLimit bool
	ContainerName     string
	CPU               *resource.Quantity
	Memory            *resource.Quantity
	CPULimit          *resource.Quantity
	MemoryLimit       *resource.Quantity
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
	if m.dryRun {
		return &AdmissionResponse{Allowed: true, Message: "Global dry-run mode is enabled"}
	}
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
	if selectedPolicy.Spec.Mode != optipodv1alpha1.ModeAuto {
		return &AdmissionResponse{Allowed: true, Message: "Selected policy is not in Auto mode"}
	}
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
	owner, err := discovery.ResolvePodWorkload(ctx, m.client, req.Pod)
	if err != nil {
		return &AdmissionResponse{Allowed: true, Message: "Cannot resolve workload"}
	}
	recommendations, rollback, failure := m.admissionRecommendations(ctx, req, owner, selectedPolicy)
	if failure != nil {
		return failure
	}

	restrictions, err := coexist.Read(ctx, m.client, owner.Namespace, owner.Kind, owner.Name)
	if err != nil {
		return &AdmissionResponse{Allowed: true, Message: "Cannot verify horizontal scaler coexistence"}
	}
	template, templateErr := discovery.Template(owner.Object)
	// Ownerless Pod admission remains supported for legacy callers.
	if owner.Kind == "Pod" {
		template = &corev1.PodTemplateSpec{Spec: req.Pod.Spec}
		templateErr = nil
	}
	if templateErr != nil {
		return &AdmissionResponse{Allowed: true, Message: "Cannot verify container ownership"}
	}
	recommendations = filterRecommendations(req.Pod, template, recommendations, selectedPolicy, restrictions, rollback)

	if len(recommendations) == 0 {
		log.V(1).Info("No resource recommendations found in annotations, skipping mutation")
		return &AdmissionResponse{
			Allowed: true,
			Message: "No resource recommendations found",
		}
	}

	updates := recommendationUpdates(recommendations)
	candidate, err := constraints.Overlay(req.Pod, updates)
	if err != nil {
		return &AdmissionResponse{Allowed: true, Message: "Invalid resource plan"}
	}
	if err := constraints.Validate(ctx, m.client, req.Pod.Namespace, []constraints.Change{{Before: req.Pod, After: candidate, NewPod: true}}, selectedPolicy.Spec.UpdateStrategy.PreserveQoS); err != nil {
		return &AdmissionResponse{Allowed: true, Message: "Resource plan blocked by constraints: " + err.Error()}
	}
	// Recheck the winning revision after source and constraint reads. All mutation
	// paths, including node sizing, must honor policy changes during admission.
	if failure := m.revalidateAdmission(ctx, req, owner, selectedPolicy); failure != nil {
		return failure
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

	if selectedPolicy.Spec.DaemonSetSizing != nil && !rollback {
		patches, err = nodeReceiptPatches(req, owner, selectedPolicy, updates, patches)
		if err != nil {
			return &AdmissionResponse{Allowed: true, Message: err.Error()}
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
	owner, err := discovery.ResolvePodWorkload(ctx, m.client, pod)
	if err != nil || owner.Kind == "Pod" {
		return nil
	}
	return owner.Object.GetAnnotations()

}

// findMatchingPolicies finds optimization policies that match the pod
func (m *Mutator) findMatchingPolicies(ctx context.Context, pod *corev1.Pod) ([]*optipodv1alpha1.OptimizationPolicy, error) {
	workload, err := discovery.ResolvePodWorkload(ctx, m.client, pod)
	if err != nil {
		return nil, err
	}
	matches, err := policy.NewPolicySelector(m.client).GetMatchingPolicies(ctx, workload)
	if err != nil {
		return nil, err
	}
	result := make([]*optipodv1alpha1.OptimizationPolicy, 0, len(matches))
	for _, match := range matches {
		result = append(result, match.Policy)
	}
	return result, nil
}

// Parse the same owner snapshot whose policy identity and expiry were checked.
func (m *Mutator) parseRecommendationAnnotations(pod *corev1.Pod, annotationsToUse map[string]string) []ResourceRecommendation {
	containerMap := make(map[string]*ResourceRecommendation)
	recommendations := make([]ResourceRecommendation, 0, len(annotationsToUse))

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
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		container := slot.Container
		rec, exists := recMap[container.Name]
		if !exists {
			continue // No recommendations for this container
		}

		containerPath := fmt.Sprintf("/spec/%s/%d", slot.Field, slot.Index)
		if _, exists := container.Resources.Requests[corev1.ResourceCPU]; exists && rec.RemoveCPURequest {
			patches = append(patches, PatchOperation{Op: patchRemove, Path: containerPath + "/resources/requests/cpu"})
		}
		if _, exists := container.Resources.Requests[corev1.ResourceMemory]; exists && rec.RemoveMemoryRequest {
			patches = append(patches, PatchOperation{Op: patchRemove, Path: containerPath + "/resources/requests/memory"})
		}
		if _, exists := container.Resources.Limits[corev1.ResourceCPU]; exists && rec.RemoveCPULimit {
			patches = append(patches, PatchOperation{Op: patchRemove, Path: containerPath + "/resources/limits/cpu"})
		}
		if _, exists := container.Resources.Limits[corev1.ResourceMemory]; exists && rec.RemoveMemoryLimit {
			patches = append(patches, PatchOperation{Op: patchRemove, Path: containerPath + "/resources/limits/memory"})
		}

		// Initialize resources if not present
		if container.Resources.Requests == nil && (rec.CPU != nil || rec.Memory != nil) {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
				Path:  containerPath + "/resources/requests",
				Value: map[string]interface{}{},
			})
		}

		if container.Resources.Limits == nil && (rec.CPULimit != nil || rec.MemoryLimit != nil) {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
				Path:  containerPath + "/resources/limits",
				Value: map[string]interface{}{},
			})
		}

		// Add CPU request patch
		if rec.CPU != nil && container.Resources.Requests.Cpu().Cmp(*rec.CPU) != 0 {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
				Path:  containerPath + "/resources/requests/cpu",
				Value: rec.CPU.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "cpu-request")
		}

		// Add memory request patch
		if rec.Memory != nil && container.Resources.Requests.Memory().Cmp(*rec.Memory) != 0 {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
				Path:  containerPath + "/resources/requests/memory",
				Value: rec.Memory.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "memory-request")
		}

		// Add CPU limit patch
		if rec.CPULimit != nil && container.Resources.Limits.Cpu().Cmp(*rec.CPULimit) != 0 {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
				Path:  containerPath + "/resources/limits/cpu",
				Value: rec.CPULimit.String(),
			})
			// Record patch metric
			observability.RecordWebhookPatchApplied(pod.Namespace, policyName, container.Name, "cpu-limit")
		}

		// Add memory limit patch
		if rec.MemoryLimit != nil && container.Resources.Limits.Memory().Cmp(*rec.MemoryLimit) != 0 {
			patches = append(patches, PatchOperation{
				Op:    patchOpAdd,
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

func (m *Mutator) admissionRecommendations(ctx context.Context, req *AdmissionRequest, owner *discovery.Workload, selectedPolicy *optipodv1alpha1.OptimizationPolicy) ([]ResourceRecommendation, bool, *AdmissionResponse) {
	rollback := false
	var recommendations []ResourceRecommendation
	if owner.Object.GetAnnotations()[history.Paused] == trueValue {
		a := owner.Object.GetAnnotations()
		name := a[history.RollbackTo]
		record, err := history.Read(ctx, m.client, owner.Namespace, name)
		if err != nil || name != a[history.Latest] || record.Digest != a[history.LatestDigest] || record.Workload.UID != owner.Object.GetUID() || record.Policy.UID != selectedPolicy.UID || record.Policy.Generation != selectedPolicy.Generation || (record.Phase != "RollbackPending" && record.Phase != "RolledBack") {
			return nil, false, &AdmissionResponse{Allowed: true, Message: "Workload is paused"}
		}
		template, err := discovery.Template(owner.Object)
		if err != nil || history.TemplateIdentity(template) != record.TemplateIdentity {
			return nil, false, &AdmissionResponse{Allowed: true, Message: "Paused workload template has changed"}
		}
		plan, err := history.Restoration(record)
		if err != nil {
			return nil, false, &AdmissionResponse{Allowed: true, Message: err.Error()}
		}
		recommendations, err = FromResourcePlan(plan)
		if err != nil {
			return nil, false, &AdmissionResponse{Allowed: true, Message: err.Error()}
		}
		rollback = true
	} else if selectedPolicy.Spec.DaemonSetSizing != nil {
		if err := selectedPolicy.ValidateCreate(); err != nil {
			return nil, false, &AdmissionResponse{Allowed: true, Message: err.Error()}
		}
		if owner.Kind != "DaemonSet" {
			return nil, false, &AdmissionResponse{Allowed: true, Message: "Node sizing requires DaemonSet owner"}
		}
		_, plan, err := nodesizing.Plan(ctx, m.client, req.Pod, selectedPolicy)
		if err != nil {
			return nil, false, &AdmissionResponse{Allowed: true, Message: "Node sizing blocked: " + err.Error()}
		}
		recommendations, err = FromResourcePlan(plan)
		if err != nil {
			return nil, false, &AdmissionResponse{Allowed: true, Message: err.Error()}
		}
	} else {
		if !ValidRecommendations(owner.Object.GetAnnotations(), selectedPolicy, time.Now()) {
			return nil, false, &AdmissionResponse{Allowed: true, Message: "Recommendation is expired or belongs to another policy revision"}
		}
		recommendations = m.parseRecommendationAnnotations(req.Pod, owner.Object.GetAnnotations())
	}

	return recommendations, rollback, nil
}

func filterRecommendations(pod *corev1.Pod, template *corev1.PodTemplateSpec, recommendations []ResourceRecommendation, selectedPolicy *optipodv1alpha1.OptimizationPolicy, restrictions *coexist.Restrictions, rollback bool) []ResourceRecommendation {
	templateNames := map[string]bool{}
	for _, c := range template.Spec.Containers {
		templateNames[c.Name] = true
	}
	for _, c := range template.Spec.InitContainers {
		templateNames[c.Name] = true
	}
	eligible := map[string]bool{}
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		if slot.Field == "initContainers" && !selectedPolicy.IncludesNativeSidecars() {
			continue
		}
		if !templateNames[slot.Container.Name] && !selectedPolicy.IncludesInjectedSidecars() {
			continue
		}
		eligible[slot.Container.Name] = true
	}
	filtered := make([]ResourceRecommendation, 0, len(recommendations))
	for _, rec := range recommendations {
		if !eligible[rec.ContainerName] || selectedPolicy.ExcludesContainer(rec.ContainerName) {
			continue
		}
		applyLimitPolicy(pod, &rec, selectedPolicy, rollback)
		if restrictions.Reason(rec.ContainerName, "cpu") != "" {
			rec.RemoveCPULimit = false
			rec.CPU = nil
			rec.RemoveCPURequest = false
			rec.CPULimit = nil
			rec.RemoveCPULimit = false
		}
		if restrictions.Reason(rec.ContainerName, "memory") != "" {
			rec.RemoveMemoryLimit = false
			rec.Memory = nil
			rec.RemoveMemoryRequest = false
			rec.MemoryLimit = nil
			rec.RemoveMemoryLimit = false
		}
		filtered = append(filtered, rec)
	}
	return filtered

}

func applyLimitPolicy(pod *corev1.Pod, rec *ResourceRecommendation, selectedPolicy *optipodv1alpha1.OptimizationPolicy, rollback bool) {
	if !selectedPolicy.ControlsResource("cpu") {
		rec.RemoveCPULimit = false
		rec.CPU = nil
		rec.RemoveCPURequest = false
		rec.CPULimit = nil
	} else if !rollback && (selectedPolicy.Spec.DaemonSetSizing == nil || rec.CPU != nil) {
		rec.RemoveCPULimit = selectedPolicy.LimitMode(true) == limitNone
		if selectedPolicy.LimitMode(true) != "Multiplier" {
			rec.CPULimit = nil
		}
	}
	if !selectedPolicy.ControlsResource("memory") {
		rec.RemoveMemoryLimit = false
		rec.Memory = nil
		rec.RemoveMemoryRequest = false
		rec.MemoryLimit = nil
	} else if !rollback && (selectedPolicy.Spec.DaemonSetSizing == nil || rec.Memory != nil) {
		rec.RemoveMemoryLimit = selectedPolicy.LimitMode(false) == limitNone
		if selectedPolicy.LimitMode(false) == optipodv1alpha1.LimitAutomatic && rec.Memory != nil {
			limit := optipodv1alpha1.AutomaticMemoryLimit(*rec.Memory, rec.MemoryLimit)
			for _, slot := range discovery.ContainerSlots(&pod.Spec) {
				if slot.Container.Name == rec.ContainerName && slot.Container.Resources.Limits.Memory().Cmp(limit) > 0 {
					limit = slot.Container.Resources.Limits.Memory().DeepCopy()
				}
			}
			rec.MemoryLimit = &limit
		} else if selectedPolicy.LimitMode(false) != "Multiplier" {
			rec.MemoryLimit = nil
		}
	}
}

func recommendationUpdates(recommendations []ResourceRecommendation) map[string]map[string]any {
	updates := map[string]map[string]any{}
	for _, r := range recommendations {
		requests, limits := map[string]any{}, map[string]any{}
		if r.CPU != nil {
			requests["cpu"] = r.CPU.String()
		}
		if r.Memory != nil {
			requests["memory"] = r.Memory.String()
		}
		if r.CPULimit != nil {
			limits["cpu"] = r.CPULimit.String()
		}
		if r.MemoryLimit != nil {
			limits["memory"] = r.MemoryLimit.String()
		}
		if r.RemoveCPURequest {
			requests["cpu"] = nil
		}
		if r.RemoveMemoryRequest {
			requests["memory"] = nil
		}
		if r.RemoveCPULimit {
			limits["cpu"] = nil
		}
		if r.RemoveMemoryLimit {
			limits["memory"] = nil
		}
		updates[r.ContainerName] = map[string]any{"requests": requests, "limits": limits}
	}
	return updates
}

func nodeReceiptPatches(req *AdmissionRequest, owner *discovery.Workload, selectedPolicy *optipodv1alpha1.OptimizationPolicy, updates map[string]map[string]any, patches []PatchOperation) ([]PatchOperation, error) {
	node, err := nodesizing.NodeName(req.Pod)
	if err != nil {
		return nil, err
	}
	receipt := nodesizing.Receipt{PolicySpec: selectedPolicy.Spec.DeepCopy(), WorkloadUID: owner.Object.GetUID(), PolicyNamespace: selectedPolicy.Namespace, PolicyName: selectedPolicy.Name, PolicyUID: selectedPolicy.UID, PolicyGeneration: selectedPolicy.Generation, Node: node, Before: history.Resources(&req.Pod.Spec), Plan: updates, Created: time.Now().UTC()}
	// Reinvocation after another injector preserves the original admission
	// baseline, while adding baseline entries for newly injected containers.
	var prior nodesizing.Receipt
	if json.Unmarshal([]byte(req.Pod.Annotations[nodesizing.ReceiptAnnotation]), &prior) == nil && prior.WorkloadUID == receipt.WorkloadUID && prior.PolicyUID == receipt.PolicyUID && prior.PolicyGeneration == receipt.PolicyGeneration && prior.Node == receipt.Node {
		for name, before := range prior.Before {
			receipt.Before[name] = before
		}
		receipt.Created = prior.Created
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("node admission receipt exceeds 64KiB")
	}
	if req.Pod.Annotations == nil {
		patches = append(patches, PatchOperation{Op: "add", Path: "/metadata/annotations", Value: map[string]string{nodesizing.ReceiptAnnotation: string(data)}})
	} else {
		patches = append(patches, PatchOperation{Op: "add", Path: "/metadata/annotations/optipod.io~1node-sizing-receipt", Value: string(data)})
	}
	return patches, nil
}

func (m *Mutator) revalidateAdmission(ctx context.Context, req *AdmissionRequest, owner *discovery.Workload, selectedPolicy *optipodv1alpha1.OptimizationPolicy) *AdmissionResponse {
	finalPolicies, err := m.findMatchingPolicies(ctx, req.Pod)
	if err != nil || len(finalPolicies) == 0 || finalPolicies[0].UID != selectedPolicy.UID || finalPolicies[0].Generation != selectedPolicy.Generation || finalPolicies[0].Spec.Mode != optipodv1alpha1.ModeAuto {
		return &AdmissionResponse{Allowed: true, Message: "Winning policy changed during admission"}
	}
	finalOwner, err := discovery.ResolvePodWorkload(ctx, m.client, req.Pod)
	if err != nil || finalOwner.Object.GetUID() != owner.Object.GetUID() || finalOwner.Object.GetAnnotations()[history.Paused] != owner.Object.GetAnnotations()[history.Paused] || finalOwner.Object.GetAnnotations()[history.RollbackTo] != owner.Object.GetAnnotations()[history.RollbackTo] {
		return &AdmissionResponse{Allowed: true, Message: "Workload state changed during admission"}
	}
	return nil
}
