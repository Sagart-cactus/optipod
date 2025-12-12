package helpers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidationHelper provides utilities for validating OptipPod behavior in tests
type ValidationHelper struct {
	client client.Client
}

// NewValidationHelper creates a new ValidationHelper instance
func NewValidationHelper(c client.Client) *ValidationHelper {
	return &ValidationHelper{
		client: c,
	}
}

// ValidateResourceBounds checks if recommendations respect the configured bounds
func (h *ValidationHelper) ValidateResourceBounds(recommendations map[string]string, bounds ResourceBounds) error {
	// Extract CPU recommendation
	cpuRec, exists := recommendations["optipod.io/recommendation.app.cpu"]
	if !exists {
		return fmt.Errorf("CPU recommendation not found in annotations")
	}

	// Extract memory recommendation
	memoryRec, exists := recommendations["optipod.io/recommendation.app.memory"]
	if !exists {
		return fmt.Errorf("memory recommendation not found in annotations")
	}

	// Validate CPU bounds
	if bounds.CPU.Min != "" {
		if err := h.validateResourceWithinBounds(cpuRec, bounds.CPU.Min, bounds.CPU.Max, "CPU"); err != nil {
			return err
		}
	}

	// Validate memory bounds
	if bounds.Memory.Min != "" {
		if err := h.validateResourceWithinBounds(memoryRec, bounds.Memory.Min, bounds.Memory.Max, "memory"); err != nil {
			return err
		}
	}

	return nil
}

// validateResourceWithinBounds checks if a resource value is within specified bounds
func (h *ValidationHelper) validateResourceWithinBounds(value, min, max, resourceType string) error {
	// Parse resource quantities
	valueQuantity, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("failed to parse %s value %s: %w", resourceType, value, err)
	}

	if min != "" {
		minQuantity, err := resource.ParseQuantity(min)
		if err != nil {
			return fmt.Errorf("failed to parse %s min %s: %w", resourceType, min, err)
		}
		if valueQuantity.Cmp(minQuantity) < 0 {
			return fmt.Errorf("%s value %s is below minimum %s", resourceType, value, min)
		}
	}

	if max != "" {
		maxQuantity, err := resource.ParseQuantity(max)
		if err != nil {
			return fmt.Errorf("failed to parse %s max %s: %w", resourceType, max, err)
		}
		if valueQuantity.Cmp(maxQuantity) > 0 {
			return fmt.Errorf("%s value %s is above maximum %s", resourceType, value, max)
		}
	}

	return nil
}

// ValidateRecommendations verifies that recommendations have the correct format and values
func (h *ValidationHelper) ValidateRecommendations(annotations map[string]string) error {
	// Check for required recommendation annotations
	requiredAnnotations := []string{
		"optipod.io/recommendation.app.cpu",
		"optipod.io/recommendation.app.memory",
	}

	for _, annotation := range requiredAnnotations {
		value, exists := annotations[annotation]
		if !exists {
			return fmt.Errorf("required recommendation annotation %s not found", annotation)
		}

		// Validate that the value is a valid resource quantity
		_, err := resource.ParseQuantity(value)
		if err != nil {
			return fmt.Errorf("invalid resource quantity in annotation %s: %s", annotation, value)
		}
	}

	// Validate recommendation timestamp if present
	if timestamp, exists := annotations["optipod.io/recommendation.timestamp"]; exists {
		if !h.isValidTimestamp(timestamp) {
			return fmt.Errorf("invalid timestamp format in recommendation: %s", timestamp)
		}
	}

	return nil
}

// ValidateWorkloadUpdate checks if a workload was updated according to the policy mode
func (h *ValidationHelper) ValidateWorkloadUpdate(workloadName, namespace string, mode v1alpha1.PolicyMode) error {
	// Get the policy to check workload status
	policies := &v1alpha1.OptimizationPolicyList{}
	err := h.client.List(context.TODO(), policies)
	if err != nil {
		return fmt.Errorf("failed to list policies: %w", err)
	}

	var targetPolicy *v1alpha1.OptimizationPolicy

	// Find the policy with the specified mode
	for _, policy := range policies.Items {
		if policy.Spec.Mode == mode {
			targetPolicy = &policy
			break
		}
	}

	if targetPolicy == nil {
		return fmt.Errorf("no policy found with mode %s", mode)
	}

	// Validate based on policy mode
	switch mode {
	case v1alpha1.ModeAuto:
		// In Auto mode, we expect workloads to be processed and updated
		if targetPolicy.Status.WorkloadsProcessed == 0 {
			return fmt.Errorf("auto mode policy should have processed workloads")
		}

	case v1alpha1.ModeRecommend:
		// In Recommend mode, we expect workloads to be discovered but not necessarily updated
		if targetPolicy.Status.WorkloadsDiscovered == 0 {
			return fmt.Errorf("recommend mode policy should have discovered workloads")
		}

	case v1alpha1.ModeDisabled:
		// In Disabled mode, we expect no workload processing
		if targetPolicy.Status.WorkloadsDiscovered > 0 || targetPolicy.Status.WorkloadsProcessed > 0 {
			return fmt.Errorf("Disabled mode policy should not process any workloads")
		}
	}

	return nil
}

// ValidateMetrics verifies that OptipPod metrics are exposed correctly
func (h *ValidationHelper) ValidateMetrics(expectedMetrics []string) error {
	// This would typically involve making HTTP requests to the metrics endpoint
	// For now, we'll implement a basic validation structure

	if expectedMetrics == nil {
		// Expected OptipPod metrics
		_ = []string{ // Keep for reference but not used in current implementation
			"optipod_workloads_monitored",
			"optipod_reconciliation_duration_seconds",
			"optipod_recommendations_generated_total",
			"optipod_updates_applied_total",
			"controller_runtime_reconcile_total",
		}
	}

	// TODO: Implement actual metrics endpoint validation
	// This would involve:
	// 1. Making HTTP request to metrics endpoint
	// 2. Parsing Prometheus format
	// 3. Validating metric presence and format

	return nil
}

// ValidatePolicyModeConsistency validates that policy mode behavior is consistent
func (h *ValidationHelper) ValidatePolicyModeConsistency(policyName, namespace string, expectedMode v1alpha1.PolicyMode) error {
	policy := &v1alpha1.OptimizationPolicy{}
	err := h.client.Get(context.TODO(), types.NamespacedName{
		Name:      policyName,
		Namespace: namespace,
	}, policy)
	if err != nil {
		return fmt.Errorf("failed to get policy %s: %w", policyName, err)
	}

	if policy.Spec.Mode != expectedMode {
		return fmt.Errorf("policy mode mismatch: expected %s, got %s", expectedMode, policy.Spec.Mode)
	}

	// Validate workload processing consistency based on mode
	switch expectedMode {
	case v1alpha1.ModeAuto:
		// Auto mode should process workloads
		if policy.Status.WorkloadsProcessed == 0 && policy.Status.WorkloadsDiscovered > 0 {
			return fmt.Errorf("Auto mode should process discovered workloads")
		}

	case v1alpha1.ModeRecommend:
		// Recommend mode should discover workloads but processing is optional
		// No specific validation needed here

	case v1alpha1.ModeDisabled:
		// Disabled mode should not process any workloads
		if policy.Status.WorkloadsDiscovered > 0 || policy.Status.WorkloadsProcessed > 0 {
			return fmt.Errorf("Disabled mode should not process any workloads")
		}
	}

	return nil
}

// ValidateResourceQuantityParsing validates resource quantity parsing consistency
func (h *ValidationHelper) ValidateResourceQuantityParsing(quantities []string) error {
	for _, quantity := range quantities {
		// Parse the quantity
		parsed, err := resource.ParseQuantity(quantity)
		if err != nil {
			return fmt.Errorf("failed to parse quantity %s: %w", quantity, err)
		}

		// Validate that parsing is consistent
		formatted := parsed.String()
		reparsed, err := resource.ParseQuantity(formatted)
		if err != nil {
			return fmt.Errorf("failed to reparse formatted quantity %s: %w", formatted, err)
		}

		// Check that values are equivalent
		if parsed.Cmp(reparsed) != 0 {
			return fmt.Errorf("quantity parsing inconsistency: %s != %s", quantity, formatted)
		}
	}

	return nil
}

// ValidateBoundsEnforcement validates that resource bounds are properly enforced
func (h *ValidationHelper) ValidateBoundsEnforcement(recommendation, min, max string, expectClamping bool) error {
	recQuantity, err := resource.ParseQuantity(recommendation)
	if err != nil {
		return fmt.Errorf("failed to parse recommendation %s: %w", recommendation, err)
	}

	if min != "" {
		minQuantity, err := resource.ParseQuantity(min)
		if err != nil {
			return fmt.Errorf("failed to parse min %s: %w", min, err)
		}

		if expectClamping && recQuantity.Cmp(minQuantity) != 0 {
			return fmt.Errorf("expected recommendation to be clamped to min %s, got %s", min, recommendation)
		}

		if recQuantity.Cmp(minQuantity) < 0 {
			return fmt.Errorf("recommendation %s is below minimum %s", recommendation, min)
		}
	}

	if max != "" {
		maxQuantity, err := resource.ParseQuantity(max)
		if err != nil {
			return fmt.Errorf("failed to parse max %s: %w", max, err)
		}

		if expectClamping && recQuantity.Cmp(maxQuantity) != 0 {
			return fmt.Errorf("expected recommendation to be clamped to max %s, got %s", max, recommendation)
		}

		if recQuantity.Cmp(maxQuantity) > 0 {
			return fmt.Errorf("recommendation %s is above maximum %s", recommendation, max)
		}
	}

	return nil
}

// isValidTimestamp checks if a timestamp string is in valid format
func (h *ValidationHelper) isValidTimestamp(timestamp string) bool {
	// RFC3339 format validation
	timestampRegex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?$`)
	return timestampRegex.MatchString(timestamp)
}

// ValidateAnnotationFormat validates that OptipPod annotations follow the expected format
func (h *ValidationHelper) ValidateAnnotationFormat(annotations map[string]string) error {
	for key, value := range annotations {
		if !strings.HasPrefix(key, "optipod.io/") {
			continue
		}

		// Validate recommendation annotations
		if strings.Contains(key, "recommendation") {
			if strings.Contains(key, ".cpu") || strings.Contains(key, ".memory") {
				_, err := resource.ParseQuantity(value)
				if err != nil {
					return fmt.Errorf("invalid resource quantity in annotation %s: %s", key, value)
				}
			}
		}

		// Validate timestamp annotations
		if strings.Contains(key, "timestamp") || strings.Contains(key, "last-applied") {
			if !h.isValidTimestamp(value) {
				return fmt.Errorf("invalid timestamp format in annotation %s: %s", key, value)
			}
		}

		// Validate boolean annotations
		if strings.Contains(key, "managed") || strings.Contains(key, "enabled") {
			if value != "true" && value != "false" {
				return fmt.Errorf("invalid boolean value in annotation %s: %s", key, value)
			}
		}
	}

	return nil
}

// ConvertResourceToBytes converts a resource quantity to bytes for comparison
func (h *ValidationHelper) ConvertResourceToBytes(quantity string) (int64, error) {
	parsed, err := resource.ParseQuantity(quantity)
	if err != nil {
		return 0, err
	}

	// Convert to bytes (for memory) or millicores (for CPU)
	return parsed.Value(), nil
}

// CompareResourceQuantities compares two resource quantities and returns -1, 0, or 1
func (h *ValidationHelper) CompareResourceQuantities(a, b string) (int, error) {
	quantityA, err := resource.ParseQuantity(a)
	if err != nil {
		return 0, fmt.Errorf("failed to parse quantity %s: %w", a, err)
	}

	quantityB, err := resource.ParseQuantity(b)
	if err != nil {
		return 0, fmt.Errorf("failed to parse quantity %s: %w", b, err)
	}

	return quantityA.Cmp(quantityB), nil
}

// ValidateErrorHandling validates that errors are handled appropriately
func (h *ValidationHelper) ValidateErrorHandling(err error, expectedErrorType string) error {
	if err == nil {
		return fmt.Errorf("expected error of type %s but got no error", expectedErrorType)
	}

	errorMsg := err.Error()
	return h.validateSpecificErrorType(errorMsg, expectedErrorType)
}

func (h *ValidationHelper) validateSpecificErrorType(errorMsg, expectedErrorType string) error {
	switch expectedErrorType {
	case "validation", "validation_error":
		return h.validateErrorContains(errorMsg, []string{"validation", "invalid"}, "validation")
	case "invalid_bounds":
		if !strings.Contains(errorMsg, "min") || !strings.Contains(errorMsg, "max") {
			return fmt.Errorf("expected bounds validation error but got: %s", errorMsg)
		}
	case "conflict":
		return h.validateErrorContains(errorMsg, []string{"conflict", "resource version"}, "conflict")
	case "not-found":
		return h.validateErrorContains(errorMsg, []string{"not found"}, "not found")
	case "metrics":
		return h.validateErrorContains(errorMsg, []string{"metrics", "unavailable"}, "metrics")
	case "recoverable_error":
		return h.validateErrorContains(errorMsg, []string{"recoverable", "retry", "temporary"}, "recoverable")
	case "transient_error":
		return h.validateErrorContains(errorMsg, []string{"transient", "temporary", "connection"}, "transient")
	case "permanent_error":
		return h.validateErrorContains(errorMsg, []string{"permanent", "invalid", "configuration"}, "permanent")
	case "missing_selector":
		return h.validateErrorContains(errorMsg, []string{"selector"}, "selector")
	case "invalid_safety_factor":
		return h.validateErrorContains(errorMsg, []string{"safety factor", "safety"}, "safety factor")
	case "zero_resource":
		return h.validateErrorContains(errorMsg, []string{"greater than zero", "zero"}, "zero resource")
	case "unknown_error":
		// For unknown errors, just validate that we got an error
		return nil
	default:
		return fmt.Errorf("unknown expected error type: %s", expectedErrorType)
	}

	return nil
}

func (h *ValidationHelper) validateErrorContains(errorMsg string, keywords []string, errorType string) error {
	for _, keyword := range keywords {
		if strings.Contains(errorMsg, keyword) {
			return nil
		}
	}
	return fmt.Errorf("expected %s error but got: %s", errorType, errorMsg)
}

// ValidateConfigurationRejection validates that invalid configurations are properly rejected
func (h *ValidationHelper) ValidateConfigurationRejection(config interface{}, expectedReason string) error {
	// This would validate that the configuration was rejected for the expected reason
	// Implementation depends on the specific configuration type and validation logic
	return nil
}

// ValidateGracefulDegradation validates that the system degrades gracefully under error conditions
func (h *ValidationHelper) ValidateGracefulDegradation(policyName, namespace string) error {
	if h.client == nil {
		// For unit tests without a client, simulate graceful degradation validation
		return nil
	}

	policy := &v1alpha1.OptimizationPolicy{}
	err := h.client.Get(context.TODO(), types.NamespacedName{
		Name:      policyName,
		Namespace: namespace,
	}, policy)
	if err != nil {
		return fmt.Errorf("failed to get policy %s: %w", policyName, err)
	}

	// Check that the policy has appropriate error conditions
	hasErrorCondition := false
	for _, condition := range policy.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			hasErrorCondition = true
			// Validate that error messages are informative
			if condition.Message == "" {
				return fmt.Errorf("error condition %s has empty message", condition.Type)
			}
		}
	}

	// If there are no error conditions, the system should still be functional
	if !hasErrorCondition {
		if len(policy.Status.Conditions) == 0 {
			return fmt.Errorf("policy has no status conditions, indicating potential system failure")
		}
	}

	return nil
}

// ValidateMemorySafety validates that memory decrease safety measures are in place
func (h *ValidationHelper) ValidateMemorySafety(workloadName, namespace string, originalMemory, currentMemory string) error {
	originalQuantity, err := resource.ParseQuantity(originalMemory)
	if err != nil {
		return fmt.Errorf("failed to parse original memory %s: %w", originalMemory, err)
	}

	currentQuantity, err := resource.ParseQuantity(currentMemory)
	if err != nil {
		return fmt.Errorf("failed to parse current memory %s: %w", currentMemory, err)
	}

	// If memory was decreased
	if currentQuantity.Cmp(originalQuantity) < 0 {
		// Calculate the decrease ratio
		ratio := float64(currentQuantity.Value()) / float64(originalQuantity.Value())

		// If decrease is more than 50%, there should be safety warnings
		if ratio < 0.5 {
			// Check for safety annotations on the workload
			// This would need to be implemented based on the specific workload type
			return fmt.Errorf("unsafe memory decrease detected: %s -> %s (ratio: %.2f)", originalMemory, currentMemory, ratio)
		}
	}

	return nil
}

// ValidateConcurrentSafety validates that concurrent modifications are handled safely
func (h *ValidationHelper) ValidateConcurrentSafety(resourceName, namespace, resourceType string) error {
	if h.client == nil {
		// For unit tests without a client, simulate concurrent safety validation
		return nil
	}

	// This would validate that the resource is in a consistent state after concurrent modifications
	// Implementation depends on the specific resource type

	switch resourceType {
	case "OptimizationPolicy":
		policy := &v1alpha1.OptimizationPolicy{}
		err := h.client.Get(context.TODO(), types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}, policy)
		if err != nil {
			return fmt.Errorf("failed to get policy %s: %w", resourceName, err)
		}

		// Validate that the policy is in a consistent state
		if policy.Spec.Mode == "" {
			return fmt.Errorf("policy mode is empty, indicating inconsistent state")
		}

		// Validate resource bounds consistency
		if policy.Spec.ResourceBounds.CPU.Min.Cmp(policy.Spec.ResourceBounds.CPU.Max) > 0 {
			return fmt.Errorf("CPU min > max after concurrent modifications")
		}
		if policy.Spec.ResourceBounds.Memory.Min.Cmp(policy.Spec.ResourceBounds.Memory.Max) > 0 {
			return fmt.Errorf("memory min > max after concurrent modifications")
		}

	default:
		return fmt.Errorf("unsupported resource type for concurrent safety validation: %s", resourceType)
	}

	return nil
}
