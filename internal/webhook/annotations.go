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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

const (
	// Resource type constants
	cpuRequestType    = "cpu-request"
	memoryRequestType = "memory-request"
	cpuLimitType      = "cpu-limit"
	memoryLimitType   = "memory-limit"
)

var annotationLog = logf.Log.WithName("annotation-manager")

// AnnotationManager handles annotation storage and parsing for resource recommendations
type AnnotationManager struct {
	client client.Client
}

// NewAnnotationManager creates a new annotation manager instance
func NewAnnotationManager(k8sClient client.Client) *AnnotationManager {
	return &AnnotationManager{
		client: k8sClient,
	}
}

// StoreRecommendations stores resource recommendations as annotations on a workload
func (am *AnnotationManager) StoreRecommendations(ctx context.Context, workload client.Object, recommendations []ResourceRecommendation) error {
	log := annotationLog.WithValues("workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()))

	log.V(1).Info("Storing resource recommendations as annotations", "recommendations", len(recommendations))

	// Get the current workload to ensure we have the latest version
	current := workload.DeepCopyObject().(client.Object)
	if err := am.client.Get(ctx, client.ObjectKeyFromObject(workload), current); err != nil {
		return fmt.Errorf("failed to get current workload: %w", err)
	}

	// Initialize annotations if nil
	if current.GetAnnotations() == nil {
		current.SetAnnotations(make(map[string]string))
	}

	annotations := current.GetAnnotations()

	// Clear existing recommendation annotations
	am.clearRecommendationAnnotations(annotations)

	// Set webhook-specific annotations
	annotations[optipodv1alpha1.AnnotationWebhookEnabled] = "true"
	annotations[optipodv1alpha1.AnnotationStrategy] = string(optipodv1alpha1.StrategyWebhook)

	// Store recommendations as annotations
	for _, rec := range recommendations {
		if rec.CPU != nil {
			key := fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationCPURequestPrefix, rec.ContainerName)
			annotations[key] = rec.CPU.String()
		}

		if rec.Memory != nil {
			key := fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationMemoryRequestPrefix, rec.ContainerName)
			annotations[key] = rec.Memory.String()
		}

		if rec.CPULimit != nil {
			key := fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationCPULimitPrefix, rec.ContainerName)
			annotations[key] = rec.CPULimit.String()
		}

		if rec.MemoryLimit != nil {
			key := fmt.Sprintf("%s.%s", optipodv1alpha1.AnnotationMemoryLimitPrefix, rec.ContainerName)
			annotations[key] = rec.MemoryLimit.String()
		}
	}

	// Update the workload with new annotations
	if err := am.updateWorkloadAnnotations(ctx, current, annotations); err != nil {
		return fmt.Errorf("failed to update workload annotations: %w", err)
	}

	log.Info("Successfully stored resource recommendations as annotations")
	return nil
}

// GetRecommendations extracts resource recommendations from pod annotations
func (am *AnnotationManager) GetRecommendations(pod *corev1.Pod) ([]ResourceRecommendation, error) {
	log := annotationLog.WithValues("pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))

	if pod.Annotations == nil {
		log.V(1).Info("Pod has no annotations")
		return nil, nil
	}

	log.V(1).Info("Extracting resource recommendations from pod annotations")

	containerMap := make(map[string]*ResourceRecommendation)
	recommendations := make([]ResourceRecommendation, 0)

	// Parse annotations for each container
	for key, value := range pod.Annotations {
		containerName, resourceType, err := am.parseAnnotationKey(key)
		if err != nil {
			// Skip non-recommendation annotations
			continue
		}

		if containerName == "" {
			log.V(1).Info("Skipping annotation with empty container name", "key", key)
			continue
		}

		// Parse resource quantity with error handling
		quantity, err := am.parseResourceQuantity(value)
		if err != nil {
			log.Error(err, "Failed to parse resource quantity, skipping annotation", "key", key, "value", value)
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
		case cpuRequestType:
			rec.CPU = &quantity
		case memoryRequestType:
			rec.Memory = &quantity
		case cpuLimitType:
			rec.CPULimit = &quantity
		case memoryLimitType:
			rec.MemoryLimit = &quantity
		}
	}

	// Convert map to slice
	for _, rec := range containerMap {
		recommendations = append(recommendations, *rec)
	}

	log.V(1).Info("Extracted resource recommendations", "recommendations", len(recommendations))
	return recommendations, nil
}

// ClearRecommendations removes all recommendation annotations from a workload
func (am *AnnotationManager) ClearRecommendations(ctx context.Context, workload client.Object) error {
	log := annotationLog.WithValues("workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()))

	log.V(1).Info("Clearing resource recommendation annotations")

	// Get the current workload to ensure we have the latest version
	current := workload.DeepCopyObject().(client.Object)
	if err := am.client.Get(ctx, client.ObjectKeyFromObject(workload), current); err != nil {
		return fmt.Errorf("failed to get current workload: %w", err)
	}

	annotations := current.GetAnnotations()
	if annotations == nil {
		log.V(1).Info("Workload has no annotations to clear")
		return nil
	}

	// Clear recommendation annotations
	originalCount := len(annotations)
	am.clearRecommendationAnnotations(annotations)

	// Only update if annotations were actually removed
	if len(annotations) < originalCount {
		if err := am.updateWorkloadAnnotations(ctx, current, annotations); err != nil {
			return fmt.Errorf("failed to update workload annotations: %w", err)
		}
		log.Info("Successfully cleared resource recommendation annotations")
	} else {
		log.V(1).Info("No recommendation annotations found to clear")
	}

	return nil
}

// ValidateAnnotations validates that all recommendation annotations have valid values
func (am *AnnotationManager) ValidateAnnotations(annotations map[string]string) []error {
	var errors []error

	if annotations == nil {
		return errors
	}

	for key, value := range annotations {
		containerName, resourceType, err := am.parseAnnotationKey(key)
		if err != nil {
			// Skip non-recommendation annotations
			continue
		}

		if containerName == "" {
			errors = append(errors, fmt.Errorf("annotation %s has empty container name", key))
			continue
		}

		// Validate resource quantity
		if _, err := am.parseResourceQuantity(value); err != nil {
			errors = append(errors, fmt.Errorf("annotation %s has invalid resource quantity %q: %w", key, value, err))
		}

		// Validate resource type
		validTypes := map[string]bool{
			cpuRequestType:    true,
			memoryRequestType: true,
			cpuLimitType:      true,
			memoryLimitType:   true,
		}

		if !validTypes[resourceType] {
			errors = append(errors, fmt.Errorf("annotation %s has invalid resource type %s", key, resourceType))
		}
	}

	return errors
}

// HasRecommendationAnnotations checks if the workload has any recommendation annotations
func (am *AnnotationManager) HasRecommendationAnnotations(workload client.Object) bool {
	annotations := workload.GetAnnotations()
	if annotations == nil {
		return false
	}

	for key := range annotations {
		if am.isRecommendationAnnotation(key) {
			return true
		}
	}

	return false
}

// GetContainerNames extracts all container names that have recommendations
func (am *AnnotationManager) GetContainerNames(annotations map[string]string) []string {
	containerSet := make(map[string]struct{})

	if annotations == nil {
		return nil
	}

	for key := range annotations {
		containerName, _, err := am.parseAnnotationKey(key)
		if err != nil {
			continue
		}

		if containerName != "" {
			containerSet[containerName] = struct{}{}
		}
	}

	// Convert set to slice
	containers := make([]string, 0, len(containerSet))
	for container := range containerSet {
		containers = append(containers, container)
	}

	return containers
}

// parseAnnotationKey parses an annotation key and returns container name and resource type
func (am *AnnotationManager) parseAnnotationKey(key string) (containerName, resourceType string, err error) {
	// Check CPU request annotations
	if strings.HasPrefix(key, optipodv1alpha1.AnnotationCPURequestPrefix+".") {
		containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationCPURequestPrefix+".")
		resourceType = cpuRequestType
		return containerName, resourceType, nil
	}

	// Check memory request annotations
	if strings.HasPrefix(key, optipodv1alpha1.AnnotationMemoryRequestPrefix+".") {
		containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationMemoryRequestPrefix+".")
		resourceType = memoryRequestType
		return containerName, resourceType, nil
	}

	// Check CPU limit annotations
	if strings.HasPrefix(key, optipodv1alpha1.AnnotationCPULimitPrefix+".") {
		containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationCPULimitPrefix+".")
		resourceType = cpuLimitType
		return containerName, resourceType, nil
	}

	// Check memory limit annotations
	if strings.HasPrefix(key, optipodv1alpha1.AnnotationMemoryLimitPrefix+".") {
		containerName = strings.TrimPrefix(key, optipodv1alpha1.AnnotationMemoryLimitPrefix+".")
		resourceType = memoryLimitType
		return containerName, resourceType, nil
	}

	// Not a recommendation annotation
	return "", "", fmt.Errorf("not a recommendation annotation")
}

// parseResourceQuantity parses a resource quantity string with enhanced error handling
func (am *AnnotationManager) parseResourceQuantity(value string) (resource.Quantity, error) {
	if value == "" {
		return resource.Quantity{}, fmt.Errorf("empty resource quantity")
	}

	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid resource quantity format: %w", err)
	}

	// Validate that the quantity is positive
	if quantity.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("resource quantity must be positive, got %s", value)
	}

	return quantity, nil
}

// isRecommendationAnnotation checks if a key is a recommendation annotation
func (am *AnnotationManager) isRecommendationAnnotation(key string) bool {
	prefixes := []string{
		optipodv1alpha1.AnnotationCPURequestPrefix + ".",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".",
		optipodv1alpha1.AnnotationCPULimitPrefix + ".",
		optipodv1alpha1.AnnotationMemoryLimitPrefix + ".",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

// clearRecommendationAnnotations removes all recommendation annotations from the map
func (am *AnnotationManager) clearRecommendationAnnotations(annotations map[string]string) {
	var keysToDelete []string

	for key := range annotations {
		if am.isRecommendationAnnotation(key) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Also clear webhook-specific annotations
	webhookKeys := []string{
		optipodv1alpha1.AnnotationWebhookEnabled,
		optipodv1alpha1.AnnotationStrategy,
	}

	for _, key := range webhookKeys {
		if _, exists := annotations[key]; exists {
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Delete the keys
	for _, key := range keysToDelete {
		delete(annotations, key)
	}
}

// updateWorkloadAnnotations updates the workload's annotations based on its type
func (am *AnnotationManager) updateWorkloadAnnotations(ctx context.Context, workload client.Object, annotations map[string]string) error {
	// Set the annotations on the workload
	workload.SetAnnotations(annotations)

	// For workloads with pod templates, we also need to update the pod template annotations
	switch obj := workload.(type) {
	case *appsv1.Deployment:
		if obj.Spec.Template.Annotations == nil {
			obj.Spec.Template.Annotations = make(map[string]string)
		}
		// Copy recommendation annotations to pod template
		am.copyRecommendationAnnotations(annotations, obj.Spec.Template.Annotations)

	case *appsv1.StatefulSet:
		if obj.Spec.Template.Annotations == nil {
			obj.Spec.Template.Annotations = make(map[string]string)
		}
		// Copy recommendation annotations to pod template
		am.copyRecommendationAnnotations(annotations, obj.Spec.Template.Annotations)

	case *appsv1.DaemonSet:
		if obj.Spec.Template.Annotations == nil {
			obj.Spec.Template.Annotations = make(map[string]string)
		}
		// Copy recommendation annotations to pod template
		am.copyRecommendationAnnotations(annotations, obj.Spec.Template.Annotations)
	}

	// Update the workload
	if err := am.client.Update(ctx, workload); err != nil {
		return fmt.Errorf("failed to update workload: %w", err)
	}

	return nil
}

// copyRecommendationAnnotations copies recommendation annotations from source to destination
func (am *AnnotationManager) copyRecommendationAnnotations(source, destination map[string]string) {
	// Copy webhook-specific annotations
	webhookKeys := []string{
		optipodv1alpha1.AnnotationWebhookEnabled,
		optipodv1alpha1.AnnotationStrategy,
	}

	for _, key := range webhookKeys {
		if value, exists := source[key]; exists {
			destination[key] = value
		}
	}

	// Copy recommendation annotations
	for key, value := range source {
		if am.isRecommendationAnnotation(key) {
			destination[key] = value
		}
	}
}

// GetPodLevelRecommendations extracts pod-level resource recommendations (if any)
// This supports future pod-level annotation formats
func (am *AnnotationManager) GetPodLevelRecommendations(pod *corev1.Pod) (map[string]resource.Quantity, error) {
	podLevelRecs := make(map[string]resource.Quantity)

	if pod.Annotations == nil {
		return podLevelRecs, nil
	}

	// Currently, we only support container-specific annotations
	// This method is a placeholder for future pod-level annotation support
	// For now, it returns an empty map

	return podLevelRecs, nil
}

// MergeRecommendations merges container-specific and pod-level recommendations
// Container-specific recommendations take precedence over pod-level ones
func (am *AnnotationManager) MergeRecommendations(containerRecs []ResourceRecommendation, podLevelRecs map[string]resource.Quantity) []ResourceRecommendation {
	// For now, we only support container-specific recommendations
	// Pod-level recommendations are not yet implemented
	// This method is a placeholder for future functionality

	return containerRecs
}

// ErrorHandler handles annotation-related errors gracefully
type ErrorHandler struct {
	// No need to store logger as field, use package-level logger
}

// NewErrorHandler creates a new error handler
func NewErrorHandler() *ErrorHandler {
	return &ErrorHandler{}
}

// HandleAnnotationError handles annotation parsing errors gracefully
func (eh *ErrorHandler) HandleAnnotationError(err error, key, value string, pod *corev1.Pod) {
	annotationLog.Error(err, "Annotation parsing error - pod creation will proceed",
		"annotation", key,
		"value", value,
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
	)

	// Emit metrics for monitoring (placeholder for future metrics implementation)
	// metrics.AnnotationParsingErrors.Inc()
}

// HandleValidationError handles annotation validation errors
func (eh *ErrorHandler) HandleValidationError(errors []error, workload client.Object) {
	if len(errors) == 0 {
		return
	}

	annotationLog.Error(fmt.Errorf("annotation validation failed with %d errors", len(errors)),
		"Annotation validation errors - operation will continue",
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		"errors", errors,
	)

	// Log each error individually for better debugging
	for i, err := range errors {
		annotationLog.V(1).Info("Validation error detail", "index", i, "error", err.Error())
	}
}

// HandleStorageError handles annotation storage errors
func (eh *ErrorHandler) HandleStorageError(err error, workload client.Object, recommendations []ResourceRecommendation) {
	annotationLog.Error(err, "Failed to store annotations - recommendations will not be applied",
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		"recommendations", len(recommendations),
	)
}

// HandleRetrievalError handles annotation retrieval errors
func (eh *ErrorHandler) HandleRetrievalError(err error, pod *corev1.Pod) {
	annotationLog.Error(err, "Failed to retrieve annotations - pod will proceed without modifications",
		"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
	)
}

// SafeParseAnnotations safely parses annotations with comprehensive error handling
func (am *AnnotationManager) SafeParseAnnotations(pod *corev1.Pod) ([]ResourceRecommendation, error) {
	errorHandler := NewErrorHandler()

	if pod.Annotations == nil {
		return nil, nil
	}

	containerMap := make(map[string]*ResourceRecommendation)
	recommendations := make([]ResourceRecommendation, 0)
	parseErrors := make([]error, 0)

	// Parse annotations with error collection
	for key, value := range pod.Annotations {
		containerName, resourceType, err := am.parseAnnotationKey(key)
		if err != nil {
			// Skip non-recommendation annotations silently
			continue
		}

		if containerName == "" {
			parseError := fmt.Errorf("empty container name in annotation %s", key)
			parseErrors = append(parseErrors, parseError)
			errorHandler.HandleAnnotationError(parseError, key, value, pod)
			continue
		}

		// Parse resource quantity with error handling
		quantity, err := am.parseResourceQuantity(value)
		if err != nil {
			parseError := fmt.Errorf("invalid resource quantity in annotation %s: %w", key, err)
			parseErrors = append(parseErrors, parseError)
			errorHandler.HandleAnnotationError(parseError, key, value, pod)
			continue // Skip invalid quantities but continue processing
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
		case cpuRequestType:
			rec.CPU = &quantity
		case memoryRequestType:
			rec.Memory = &quantity
		case cpuLimitType:
			rec.CPULimit = &quantity
		case memoryLimitType:
			rec.MemoryLimit = &quantity
		default:
			parseError := fmt.Errorf("unknown resource type %s in annotation %s", resourceType, key)
			parseErrors = append(parseErrors, parseError)
			errorHandler.HandleAnnotationError(parseError, key, value, pod)
		}
	}

	// Convert map to slice
	for _, rec := range containerMap {
		recommendations = append(recommendations, *rec)
	}

	// Log summary of parsing results
	if len(parseErrors) > 0 {
		annotationLog.Info("Annotation parsing completed with errors",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"successful_recommendations", len(recommendations),
			"parse_errors", len(parseErrors),
		)
	} else {
		annotationLog.V(1).Info("Annotation parsing completed successfully",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			"recommendations", len(recommendations),
		)
	}

	// Return recommendations even if there were parse errors
	// This ensures pod creation proceeds
	return recommendations, nil
}

// SafeStoreRecommendations safely stores recommendations with error handling
func (am *AnnotationManager) SafeStoreRecommendations(ctx context.Context, workload client.Object, recommendations []ResourceRecommendation) error {
	errorHandler := NewErrorHandler()

	// Validate recommendations before storing
	if err := am.validateRecommendations(recommendations); err != nil {
		errorHandler.HandleValidationError([]error{err}, workload)
		return fmt.Errorf("recommendation validation failed: %w", err)
	}

	// Attempt to store recommendations
	if err := am.StoreRecommendations(ctx, workload, recommendations); err != nil {
		errorHandler.HandleStorageError(err, workload, recommendations)
		return err
	}

	return nil
}

// validateRecommendations validates resource recommendations before storage
func (am *AnnotationManager) validateRecommendations(recommendations []ResourceRecommendation) error {
	if len(recommendations) == 0 {
		return fmt.Errorf("no recommendations provided")
	}

	for i, rec := range recommendations {
		if rec.ContainerName == "" {
			return fmt.Errorf("recommendation %d has empty container name", i)
		}

		// Validate that at least one resource is specified
		if rec.CPU == nil && rec.Memory == nil && rec.CPULimit == nil && rec.MemoryLimit == nil {
			return fmt.Errorf("recommendation for container %s has no resource values", rec.ContainerName)
		}

		// Validate resource values are positive
		if rec.CPU != nil && rec.CPU.Sign() <= 0 {
			return fmt.Errorf("CPU request for container %s must be positive", rec.ContainerName)
		}

		if rec.Memory != nil && rec.Memory.Sign() <= 0 {
			return fmt.Errorf("memory request for container %s must be positive", rec.ContainerName)
		}

		if rec.CPULimit != nil && rec.CPULimit.Sign() <= 0 {
			return fmt.Errorf("CPU limit for container %s must be positive", rec.ContainerName)
		}

		if rec.MemoryLimit != nil && rec.MemoryLimit.Sign() <= 0 {
			return fmt.Errorf("memory limit for container %s must be positive", rec.ContainerName)
		}

		// Validate that limits are not less than requests
		if rec.CPU != nil && rec.CPULimit != nil && rec.CPULimit.Cmp(*rec.CPU) < 0 {
			return fmt.Errorf("CPU limit (%s) cannot be less than CPU request (%s) for container %s",
				rec.CPULimit.String(), rec.CPU.String(), rec.ContainerName)
		}

		if rec.Memory != nil && rec.MemoryLimit != nil && rec.MemoryLimit.Cmp(*rec.Memory) < 0 {
			return fmt.Errorf("memory limit (%s) cannot be less than memory request (%s) for container %s",
				rec.MemoryLimit.String(), rec.Memory.String(), rec.ContainerName)
		}
	}

	return nil
}

// RecoverFromPanic recovers from panics during annotation operations
func (am *AnnotationManager) RecoverFromPanic(operation string, workload client.Object) {
	if r := recover(); r != nil {
		annotationLog.Error(fmt.Errorf("panic recovered: %v", r),
			"Panic during annotation operation - operation failed gracefully",
			"operation", operation,
			"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
		)
	}
}

// SafeOperationWrapper wraps annotation operations with panic recovery
func (am *AnnotationManager) SafeOperationWrapper(operation string, workload client.Object, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			am.RecoverFromPanic(operation, workload)
			err = fmt.Errorf("operation %s failed due to panic: %v", operation, r)
		}
	}()

	return fn()
}

// IsRecoverableError determines if an error is recoverable
func (am *AnnotationManager) IsRecoverableError(err error) bool {
	if err == nil {
		return true
	}

	// Network errors, temporary failures, etc. are recoverable
	// Validation errors, malformed data, etc. are not recoverable
	errorStr := err.Error()

	recoverablePatterns := []string{
		"connection refused",
		"timeout",
		"temporary failure",
		"server unavailable",
		"network",
	}

	for _, pattern := range recoverablePatterns {
		if strings.Contains(strings.ToLower(errorStr), pattern) {
			return true
		}
	}

	return false
}

// LogOperationResult logs the result of annotation operations for debugging
func (am *AnnotationManager) LogOperationResult(operation string, workload client.Object, err error, details map[string]interface{}) {
	log := annotationLog.WithValues(
		"operation", operation,
		"workload", fmt.Sprintf("%s/%s", workload.GetNamespace(), workload.GetName()),
	)

	// Add details to log context
	for key, value := range details {
		log = log.WithValues(key, value)
	}

	if err != nil {
		if am.IsRecoverableError(err) {
			log.Info("Operation failed with recoverable error", "error", err)
		} else {
			log.Error(err, "Operation failed with non-recoverable error")
		}
	} else {
		log.V(1).Info("Operation completed successfully")
	}
}
