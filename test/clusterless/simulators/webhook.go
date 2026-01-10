package simulators

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/optipod/optipod/test/clusterless/harness"
)

// WebhookSimulator simulates webhook mutations without HTTP server
type WebhookSimulator struct {
	harness *harness.TestHarness
}

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

// NewWebhookSimulator creates a new webhook simulator
func NewWebhookSimulator(h *harness.TestHarness) *WebhookSimulator {
	return &WebhookSimulator{
		harness: h,
	}
}

// MutatePod simulates admission request for pod mutation
func (w *WebhookSimulator) MutatePod(
	pod *corev1.Pod,
	dryRun bool,
) *admissionv1.AdmissionResponse {
	// Create admission request
	admissionReq := &admissionv1.AdmissionRequest{
		UID: "test-uid",
		Kind: metav1.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		},
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Object: runtime.RawExtension{
			Object: pod,
		},
		DryRun: &dryRun,
	}

	// Simulate webhook mutation logic
	return w.mutatePodInternal(admissionReq, pod)
}

// mutatePodInternal contains the core mutation logic
func (w *WebhookSimulator) mutatePodInternal(
	req *admissionv1.AdmissionRequest,
	pod *corev1.Pod,
) *admissionv1.AdmissionResponse {
	// Check if webhook is enabled for this pod
	if !w.isWebhookEnabled(pod) {
		return &admissionv1.AdmissionResponse{
			UID:     req.UID,
			Allowed: true,
			Result:  &metav1.Status{Code: 200},
		}
	}

	// Generate patches based on annotations
	patches := w.generatePatches(pod)

	// Convert patches to JSON
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			UID:     req.UID,
			Allowed: false,
			Result: &metav1.Status{
				Code:    500,
				Message: fmt.Sprintf("Failed to marshal patches: %v", err),
			},
		}
	}

	patchType := admissionv1.PatchTypeJSONPatch
	response := &admissionv1.AdmissionResponse{
		UID:       req.UID,
		Allowed:   true,
		Result:    &metav1.Status{Code: 200},
		Patch:     patchBytes,
		PatchType: &patchType,
	}

	// Store patches in a custom field for testing (we'll extend this in tests)
	return response
}

// isWebhookEnabled checks if webhook is enabled for the pod
func (w *WebhookSimulator) isWebhookEnabled(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}

	enabled, exists := pod.Annotations["optipod.io/webhook-enabled"]
	if !exists {
		return false
	}

	return enabled == "true"
}

// generatePatches creates JSON patches based on OptiPod annotations
func (w *WebhookSimulator) generatePatches(pod *corev1.Pod) []PatchOperation {
	var patches []PatchOperation

	if pod.Annotations == nil {
		return patches
	}

	// Process each container
	for i, container := range pod.Spec.Containers {
		containerPatches := w.generateContainerPatches(pod, container.Name, i)
		patches = append(patches, containerPatches...)
	}

	return patches
}

// generateContainerPatches generates patches for a specific container
func (w *WebhookSimulator) generateContainerPatches(pod *corev1.Pod, containerName string, containerIndex int) []PatchOperation {
	var patches []PatchOperation

	// CPU request patch
	if cpuRequest, exists := pod.Annotations[fmt.Sprintf("optipod.io/cpu-request.%s", containerName)]; exists {
		patch := PatchOperation{
			Op:    "replace",
			Path:  fmt.Sprintf("/spec/containers/%d/resources/requests/cpu", containerIndex),
			Value: cpuRequest,
		}
		patches = append(patches, patch)
	}

	// Memory request patch
	if memRequest, exists := pod.Annotations[fmt.Sprintf("optipod.io/memory-request.%s", containerName)]; exists {
		patch := PatchOperation{
			Op:    "replace",
			Path:  fmt.Sprintf("/spec/containers/%d/resources/requests/memory", containerIndex),
			Value: memRequest,
		}
		patches = append(patches, patch)
	}

	// CPU limit patch
	if cpuLimit, exists := pod.Annotations[fmt.Sprintf("optipod.io/cpu-limit.%s", containerName)]; exists {
		patch := PatchOperation{
			Op:    "replace",
			Path:  fmt.Sprintf("/spec/containers/%d/resources/limits/cpu", containerIndex),
			Value: cpuLimit,
		}
		patches = append(patches, patch)
	}

	// Memory limit patch
	if memLimit, exists := pod.Annotations[fmt.Sprintf("optipod.io/memory-limit.%s", containerName)]; exists {
		patch := PatchOperation{
			Op:    "replace",
			Path:  fmt.Sprintf("/spec/containers/%d/resources/limits/memory", containerIndex),
			Value: memLimit,
		}
		patches = append(patches, patch)
	}

	return patches
}

// ExtractResourcesFromPatches parses JSON patches to extract resource requirements
func (w *WebhookSimulator) ExtractResourcesFromPatches(
	patches []PatchOperation,
) (map[string]corev1.ResourceRequirements, error) {
	resources := make(map[string]corev1.ResourceRequirements)

	for _, patch := range patches {
		// Parse path to extract container index and resource type
		containerIndex, resourceType, resourceName, err := w.parsePatchPath(patch.Path)
		if err != nil {
			continue // Skip invalid patches
		}

		// Get or create resource requirements for this container
		containerKey := fmt.Sprintf("container-%d", containerIndex)
		reqs, exists := resources[containerKey]
		if !exists {
			reqs = corev1.ResourceRequirements{
				Requests: make(corev1.ResourceList),
				Limits:   make(corev1.ResourceList),
			}
		}

		// Parse resource value
		resourceValue, err := resource.ParseQuantity(fmt.Sprintf("%v", patch.Value))
		if err != nil {
			return nil, fmt.Errorf("failed to parse resource value %v: %w", patch.Value, err)
		}

		// Apply patch to appropriate resource list
		var resourceList corev1.ResourceList
		switch resourceType {
		case "requests":
			resourceList = reqs.Requests
		case "limits":
			resourceList = reqs.Limits
		default:
			continue // Skip unknown resource types
		}

		// Set resource value
		switch resourceName {
		case "cpu":
			resourceList[corev1.ResourceCPU] = resourceValue
		case "memory":
			resourceList[corev1.ResourceMemory] = resourceValue
		}

		resources[containerKey] = reqs
	}

	return resources, nil
}

// parsePatchPath parses a JSON patch path to extract container index and resource info
// Example: "/spec/containers/0/resources/requests/cpu" -> (0, "requests", "cpu", nil)
func (w *WebhookSimulator) parsePatchPath(path string) (int, string, string, error) {
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		return 0, "", "", fmt.Errorf("invalid patch path: %s", path)
	}

	// Expected format: ["", "spec", "containers", "INDEX", "resources", "TYPE", "RESOURCE"]
	if parts[1] != "spec" || parts[2] != "containers" || parts[4] != "resources" {
		return 0, "", "", fmt.Errorf("unexpected patch path format: %s", path)
	}

	// Parse container index
	containerIndex, err := strconv.Atoi(parts[3])
	if err != nil {
		return 0, "", "", fmt.Errorf("invalid container index in path %s: %w", path, err)
	}

	resourceType := parts[5] // "requests" or "limits"
	resourceName := parts[6] // "cpu" or "memory"

	return containerIndex, resourceType, resourceName, nil
}

// ApplyPatchesToPod applies patches to a pod (for testing)
func (w *WebhookSimulator) ApplyPatchesToPod(pod *corev1.Pod, patches []PatchOperation) (*corev1.Pod, error) {
	// Create a deep copy of the pod
	podCopy := pod.DeepCopy()

	// Apply each patch
	for _, patch := range patches {
		if err := w.applyPatch(podCopy, patch); err != nil {
			return nil, fmt.Errorf("failed to apply patch %+v: %w", patch, err)
		}
	}

	return podCopy, nil
}

// applyPatch applies a single patch to a pod
func (w *WebhookSimulator) applyPatch(pod *corev1.Pod, patch PatchOperation) error {
	containerIndex, resourceType, resourceName, err := w.parsePatchPath(patch.Path)
	if err != nil {
		return err
	}

	if containerIndex >= len(pod.Spec.Containers) {
		return fmt.Errorf("container index %d out of range", containerIndex)
	}

	container := &pod.Spec.Containers[containerIndex]

	// Initialize resources if needed
	if container.Resources.Requests == nil {
		container.Resources.Requests = make(corev1.ResourceList)
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = make(corev1.ResourceList)
	}

	// Parse resource value
	resourceValue, err := resource.ParseQuantity(fmt.Sprintf("%v", patch.Value))
	if err != nil {
		return fmt.Errorf("failed to parse resource value %v: %w", patch.Value, err)
	}

	// Apply patch
	var resourceList corev1.ResourceList
	switch resourceType {
	case "requests":
		resourceList = container.Resources.Requests
	case "limits":
		resourceList = container.Resources.Limits
	default:
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	switch resourceName {
	case "cpu":
		resourceList[corev1.ResourceCPU] = resourceValue
	case "memory":
		resourceList[corev1.ResourceMemory] = resourceValue
	default:
		return fmt.Errorf("unknown resource name: %s", resourceName)
	}

	return nil
}

// GetPatchesFromResponse extracts patches from admission response for testing
func (w *WebhookSimulator) GetPatchesFromResponse(response *admissionv1.AdmissionResponse) ([]PatchOperation, error) {
	if response.Patch == nil {
		return []PatchOperation{}, nil
	}

	var patches []PatchOperation
	if err := json.Unmarshal(response.Patch, &patches); err != nil {
		return nil, fmt.Errorf("failed to unmarshal patches: %w", err)
	}

	return patches, nil
}
