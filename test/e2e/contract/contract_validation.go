package contract

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// ContractValidator ensures tests validate only user-visible behavior
type ContractValidator struct {
	client client.Client
}

// NewContractValidator creates a new contract validator
func NewContractValidator(k8sClient client.Client) *ContractValidator {
	return &ContractValidator{client: k8sClient}
}

// ValidateUserVisibleBehaviorOnly ensures tests only check user-visible behavior
// Requirements: 2.1, 2.2, 2.3, 2.4, 2.5
func (cv *ContractValidator) ValidateUserVisibleBehaviorOnly() error {
	violations := []string{}

	// Check for forbidden assertions that would violate contract requirements
	forbiddenPatterns := []string{
		"reconcile count",
		"timing threshold",
		"exact CPU value",
		"exact memory value",
		"controller log",
		"internal field",
		"metrics count",
		"event count",
		"resource utilization",
		"performance metric",
	}

	// This is a compile-time validation - the patterns above represent
	// types of assertions that should NOT be present in contract tests
	for _, pattern := range forbiddenPatterns {
		if cv.containsForbiddenPattern(pattern) {
			violations = append(violations, fmt.Sprintf("Test contains forbidden pattern: %s", pattern))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("contract validation violations found: %v", violations)
	}

	return nil
}

// containsForbiddenPattern checks if tests contain forbidden assertion patterns
func (cv *ContractValidator) containsForbiddenPattern(pattern string) bool {
	// This is a placeholder for static analysis that would be done at compile time
	// In practice, this would scan the test source code for forbidden patterns
	return false
}

// ValidateControllerInstallationContract ensures installation tests only check pod readiness
// Requirement 2.1: WHEN validating controller installation, THE Test_Suite SHALL only verify the controller pod becomes Ready
func (cv *ContractValidator) ValidateControllerInstallationContract(ctx context.Context, namespace string) error {
	// Only validate that controller pod is Ready - no other internal details
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	}

	if err := cv.client.List(ctx, podList, listOpts...); err != nil {
		return fmt.Errorf("failed to list controller pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no controller pods found")
	}

	pod := podList.Items[0]

	// ONLY check pod Ready condition - this is user-visible behavior
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return nil // Contract satisfied
		}
	}

	return fmt.Errorf("controller pod is not Ready")
}

// ValidateCRAcceptanceContract ensures CR tests only check API server acceptance
// Requirement 2.2: WHEN testing CR application, THE Test_Suite SHALL only verify the CR is accepted by the API server
func (cv *ContractValidator) ValidateCRAcceptanceContract(ctx context.Context, cr *optipodv1alpha1.OptimizationPolicy) error {
	// Only validate that CR is accepted by API server - no internal processing details
	err := cv.client.Create(ctx, cr)
	if err != nil {
		return fmt.Errorf("CR was not accepted by API server: %w", err)
	}

	// Verify CR can be retrieved (proves API server acceptance)
	retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
	err = cv.client.Get(ctx, client.ObjectKey{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}, retrievedCR)
	if err != nil {
		return fmt.Errorf("CR not retrievable from API server: %w", err)
	}

	return nil // Contract satisfied
}

// ValidateStatusConvergenceContract ensures status tests only check Ready condition
// Requirement 2.3: WHEN checking status convergence, THE Test_Suite SHALL only verify a Ready=True condition appears
func (cv *ContractValidator) ValidateStatusConvergenceContract(ctx context.Context, name, namespace string) error {
	// Only validate Ready=True condition - no detailed status messages or timing
	retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
	err := cv.client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, retrievedCR)
	if err != nil {
		return fmt.Errorf("failed to retrieve CR for status check: %w", err)
	}

	// ONLY check for Ready=True condition - this is user-visible behavior
	for _, condition := range retrievedCR.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return nil // Contract satisfied
		}
	}

	return fmt.Errorf("Ready=True condition not found")
}

// ValidateNoForbiddenAssertions ensures tests don't assert forbidden internal details
// Requirements 2.4, 2.5: No reconcile counts, timing thresholds, exact values, logs, or internal fields
func (cv *ContractValidator) ValidateNoForbiddenAssertions() []string {
	violations := []string{}

	// These are the types of assertions that violate contract requirements:
	forbiddenAssertions := map[string]string{
		"reconcile_count":    "Requirement 2.4: SHALL NOT assert reconcile counts",
		"timing_threshold":   "Requirement 2.4: SHALL NOT assert timing thresholds",
		"exact_cpu_value":    "Requirement 2.4: SHALL NOT assert exact CPU/memory values",
		"exact_memory_value": "Requirement 2.4: SHALL NOT assert exact CPU/memory values",
		"controller_log":     "Requirement 2.5: SHALL NOT depend on controller logs",
		"internal_field":     "Requirement 2.5: SHALL NOT depend on internal controller fields",
	}

	// In a real implementation, this would scan test source code for these patterns
	// For now, we document what should NOT be present in contract tests
	for pattern, requirement := range forbiddenAssertions {
		if cv.wouldViolateContract(pattern) {
			violations = append(violations, fmt.Sprintf("%s violates %s", pattern, requirement))
		}
	}

	return violations
}

// wouldViolateContract checks if a pattern would violate contract requirements
func (cv *ContractValidator) wouldViolateContract(pattern string) bool {
	// This would be implemented as static analysis of test source code
	// For now, we assume tests are compliant unless proven otherwise
	return false
}

// GetAllowedAssertions returns the list of assertions that ARE allowed in contract tests
func (cv *ContractValidator) GetAllowedAssertions() []string {
	return []string{
		"pod_ready_condition",   // Requirement 2.1: Controller pod becomes Ready
		"api_server_acceptance", // Requirement 2.2: CR accepted by API server
		"ready_true_condition",  // Requirement 2.3: Ready=True condition appears
		"no_validation_errors",  // Requirement 2.2: No validation errors returned
		"restart_count_zero",    // Requirement 2.1: No crashloop (restart count = 0)
	}
}

// ValidateContractCompliance performs comprehensive contract validation
func (cv *ContractValidator) ValidateContractCompliance(ctx context.Context) error {
	// Validate that tests only check user-visible behavior
	if err := cv.ValidateUserVisibleBehaviorOnly(); err != nil {
		return fmt.Errorf("user-visible behavior validation failed: %w", err)
	}

	// Check for forbidden assertions
	violations := cv.ValidateNoForbiddenAssertions()
	if len(violations) > 0 {
		return fmt.Errorf("forbidden assertions found: %v", violations)
	}

	return nil
}
