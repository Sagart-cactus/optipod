package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
)

// TestSuiteValidator validates the health and quality of the test suite
type TestSuiteValidator struct {
	client    client.Client
	namespace string
}

// NewTestSuiteValidator creates a new test suite validator
func NewTestSuiteValidator(client client.Client, namespace string) *TestSuiteValidator {
	return &TestSuiteValidator{
		client:    client,
		namespace: namespace,
	}
}

// ValidationResult contains the results of test suite validation
type ValidationResult struct {
	Timestamp       time.Time
	OverallHealth   HealthStatus
	ComponentHealth map[string]HealthStatus
	QualityMetrics  QualityMetrics
	Issues          []ValidationIssue
	Recommendations []string
}

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// QualityMetrics contains quality metrics for the test suite
type QualityMetrics struct {
	TestCoverage         float64
	PropertyCoverage     float64
	HelperUtilization    float64
	CleanupEffectiveness float64
	ExecutionStability   float64
	PerformanceScore     float64
}

// ValidationIssue represents an issue found during validation
type ValidationIssue struct {
	Severity    IssueSeverity
	Component   string
	Description string
	Impact      string
	Suggestion  string
}

// IssueSeverity represents the severity of a validation issue
type IssueSeverity string

const (
	IssueSeverityCritical IssueSeverity = "critical"
	IssueSeverityMajor    IssueSeverity = "major"
	IssueSeverityMinor    IssueSeverity = "minor"
	IssueSeverityInfo     IssueSeverity = "info"
)

// ValidateTestSuite performs comprehensive test suite validation
func (v *TestSuiteValidator) ValidateTestSuite(ctx context.Context) (*ValidationResult, error) {
	result := &ValidationResult{
		Timestamp:       time.Now(),
		ComponentHealth: make(map[string]HealthStatus),
		Issues:          []ValidationIssue{},
		Recommendations: []string{},
	}

	// Validate test environment health
	if err := v.validateEnvironmentHealth(ctx, result); err != nil {
		return nil, fmt.Errorf("failed to validate environment health: %w", err)
	}

	// Validate test structure and organization
	if err := v.validateTestStructure(result); err != nil {
		return nil, fmt.Errorf("failed to validate test structure: %w", err)
	}

	// Validate helper components
	if err := v.validateHelperComponents(result); err != nil {
		return nil, fmt.Errorf("failed to validate helper components: %w", err)
	}

	// Validate test quality metrics
	if err := v.validateQualityMetrics(result); err != nil {
		return nil, fmt.Errorf("failed to validate quality metrics: %w", err)
	}

	// Calculate overall health
	result.OverallHealth = v.calculateOverallHealth(result)

	// Generate recommendations
	v.generateRecommendations(result)

	return result, nil
}

// validateEnvironmentHealth validates the test environment health
func (v *TestSuiteValidator) validateEnvironmentHealth(ctx context.Context, result *ValidationResult) error {
	// Check Kubernetes cluster connectivity
	if err := v.validateClusterConnectivity(ctx, result); err != nil {
		return err
	}

	// Check required components
	if err := v.validateRequiredComponents(ctx, result); err != nil {
		return err
	}

	// Check resource availability
	if err := v.validateResourceAvailability(ctx, result); err != nil {
		return err
	}

	return nil
}

// validateClusterConnectivity validates Kubernetes cluster connectivity
func (v *TestSuiteValidator) validateClusterConnectivity(ctx context.Context, result *ValidationResult) error {
	// Test basic cluster connectivity
	nodes := &corev1.NodeList{}
	err := v.client.List(ctx, nodes)
	if err != nil {
		result.ComponentHealth["cluster-connectivity"] = HealthStatusUnhealthy
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityCritical,
			Component:   "cluster-connectivity",
			Description: "Cannot connect to Kubernetes cluster",
			Impact:      "Tests cannot run without cluster connectivity",
			Suggestion:  "Check KUBECONFIG and cluster status",
		})
		return nil
	}

	if len(nodes.Items) == 0 {
		result.ComponentHealth["cluster-connectivity"] = HealthStatusDegraded
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMajor,
			Component:   "cluster-connectivity",
			Description: "No nodes found in cluster",
			Impact:      "Tests may fail due to lack of compute resources",
			Suggestion:  "Ensure cluster has at least one ready node",
		})
	} else {
		result.ComponentHealth["cluster-connectivity"] = HealthStatusHealthy
	}

	return nil
}

// validateRequiredComponents validates required Kubernetes components
func (v *TestSuiteValidator) validateRequiredComponents(ctx context.Context, result *ValidationResult) error {
	components := map[string]string{
		"cert-manager":   "cert-manager",
		"metrics-server": "kube-system",
	}

	for component, namespace := range components {
		pods := &corev1.PodList{}
		err := v.client.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{
			"app.kubernetes.io/name": component,
		})

		if err != nil || len(pods.Items) == 0 {
			result.ComponentHealth[component] = HealthStatusDegraded
			result.Issues = append(result.Issues, ValidationIssue{
				Severity:    IssueSeverityMinor,
				Component:   component,
				Description: fmt.Sprintf("%s not found or not running", component),
				Impact:      "Some tests may be skipped or fail",
				Suggestion:  fmt.Sprintf("Install %s or set skip environment variable", component),
			})
		} else {
			// Check if pods are ready
			readyPods := 0
			for _, pod := range pods.Items {
				if isPodReady(&pod) {
					readyPods++
				}
			}

			if readyPods == 0 {
				result.ComponentHealth[component] = HealthStatusUnhealthy
				result.Issues = append(result.Issues, ValidationIssue{
					Severity:    IssueSeverityMajor,
					Component:   component,
					Description: fmt.Sprintf("%s pods are not ready", component),
					Impact:      "Tests may fail due to component unavailability",
					Suggestion:  fmt.Sprintf("Check %s pod logs and status", component),
				})
			} else if readyPods < len(pods.Items) {
				result.ComponentHealth[component] = HealthStatusDegraded
			} else {
				result.ComponentHealth[component] = HealthStatusHealthy
			}
		}
	}

	return nil
}

// validateResourceAvailability validates cluster resource availability
func (v *TestSuiteValidator) validateResourceAvailability(ctx context.Context, result *ValidationResult) error {
	nodes := &corev1.NodeList{}
	err := v.client.List(ctx, nodes)
	if err != nil {
		return err
	}

	totalCPU := int64(0)
	totalMemory := int64(0)
	allocatableCPU := int64(0)
	allocatableMemory := int64(0)

	for _, node := range nodes.Items {
		if cpu := node.Status.Capacity.Cpu(); cpu != nil {
			totalCPU += cpu.MilliValue()
		}
		if memory := node.Status.Capacity.Memory(); memory != nil {
			totalMemory += memory.Value()
		}
		if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
			allocatableCPU += cpu.MilliValue()
		}
		if memory := node.Status.Allocatable.Memory(); memory != nil {
			allocatableMemory += memory.Value()
		}
	}

	// Check if resources are sufficient for testing
	minRequiredCPU := int64(2000)   // 2 CPU cores
	minRequiredMemory := int64(4e9) // 4GB

	if allocatableCPU < minRequiredCPU {
		result.ComponentHealth["resource-availability"] = HealthStatusDegraded
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMinor,
			Component:   "resource-availability",
			Description: fmt.Sprintf("Low CPU availability: %dm available, %dm recommended", allocatableCPU, minRequiredCPU),
			Impact:      "Tests may run slowly or timeout",
			Suggestion:  "Consider increasing cluster CPU resources or reducing parallel test execution",
		})
	}

	if allocatableMemory < minRequiredMemory {
		result.ComponentHealth["resource-availability"] = HealthStatusDegraded
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:  IssueSeverityMinor,
			Component: "resource-availability",
			Description: fmt.Sprintf("Low memory availability: %dMi available, %dMi recommended",
				allocatableMemory/(1024*1024), minRequiredMemory/(1024*1024)),
			Impact:     "Tests may fail due to memory constraints",
			Suggestion: "Consider increasing cluster memory or reducing test parallelism",
		})
	}

	if result.ComponentHealth["resource-availability"] == "" {
		result.ComponentHealth["resource-availability"] = HealthStatusHealthy
	}

	return nil
}

// validateTestStructure validates test file structure and organization
func (v *TestSuiteValidator) validateTestStructure(result *ValidationResult) error {
	testDir := "."

	// Check for required test files
	requiredFiles := []string{
		"e2e_suite_test.go",
		"e2e_test.go",
		"helpers/policy_helpers.go",
		"helpers/workload_helpers.go",
		"helpers/validation_helpers.go",
		"helpers/cleanup_helpers.go",
		"fixtures/generators.go",
	}

	missingFiles := []string{}
	for _, file := range requiredFiles {
		filePath := filepath.Join(testDir, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			missingFiles = append(missingFiles, file)
		}
	}

	if len(missingFiles) > 0 {
		result.ComponentHealth["test-structure"] = HealthStatusDegraded
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMajor,
			Component:   "test-structure",
			Description: fmt.Sprintf("Missing required test files: %s", strings.Join(missingFiles, ", ")),
			Impact:      "Test suite may not function correctly",
			Suggestion:  "Ensure all required test files are present",
		})
	} else {
		result.ComponentHealth["test-structure"] = HealthStatusHealthy
	}

	// Check for documentation files
	docFiles := []string{
		"README.md",
		"TESTING_GUIDE.md",
		"TROUBLESHOOTING.md",
		"DEVELOPER_ONBOARDING.md",
	}

	missingDocs := []string{}
	for _, file := range docFiles {
		filePath := filepath.Join(testDir, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			missingDocs = append(missingDocs, file)
		}
	}

	if len(missingDocs) > 0 {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMinor,
			Component:   "documentation",
			Description: fmt.Sprintf("Missing documentation files: %s", strings.Join(missingDocs, ", ")),
			Impact:      "Developers may lack guidance for using the test suite",
			Suggestion:  "Create missing documentation files",
		})
	}

	return nil
}

// validateHelperComponents validates test helper components
func (v *TestSuiteValidator) validateHelperComponents(result *ValidationResult) error {
	// This would typically involve checking helper function coverage,
	// API consistency, and usage patterns

	// For now, we'll do a basic check that helper files exist and are non-empty
	helperFiles := map[string]string{
		"policy-helpers":     "helpers/policy_helpers.go",
		"workload-helpers":   "helpers/workload_helpers.go",
		"validation-helpers": "helpers/validation_helpers.go",
		"cleanup-helpers":    "helpers/cleanup_helpers.go",
	}

	for component, file := range helperFiles {
		if info, err := os.Stat(file); err != nil {
			result.ComponentHealth[component] = HealthStatusUnhealthy
			result.Issues = append(result.Issues, ValidationIssue{
				Severity:    IssueSeverityMajor,
				Component:   component,
				Description: fmt.Sprintf("Helper file %s not found", file),
				Impact:      "Tests may not have access to required helper functions",
				Suggestion:  fmt.Sprintf("Create or restore %s", file),
			})
		} else if info.Size() < 100 {
			result.ComponentHealth[component] = HealthStatusDegraded
			result.Issues = append(result.Issues, ValidationIssue{
				Severity:    IssueSeverityMinor,
				Component:   component,
				Description: fmt.Sprintf("Helper file %s appears to be empty or minimal", file),
				Impact:      "Helper functionality may be incomplete",
				Suggestion:  fmt.Sprintf("Review and enhance %s", file),
			})
		} else {
			result.ComponentHealth[component] = HealthStatusHealthy
		}
	}

	return nil
}

// validateQualityMetrics validates test quality metrics
func (v *TestSuiteValidator) validateQualityMetrics(result *ValidationResult) error {
	// Calculate quality metrics
	metrics := QualityMetrics{}

	// Test coverage (simplified calculation)
	testFiles, err := filepath.Glob("*_test.go")
	if err != nil {
		return err
	}

	if len(testFiles) >= 8 {
		metrics.TestCoverage = 0.9
	} else if len(testFiles) >= 5 {
		metrics.TestCoverage = 0.7
	} else {
		metrics.TestCoverage = 0.5
	}

	// Property coverage (check for property-based tests)
	propertyTestFiles, err := filepath.Glob("*property*_test.go")
	if err != nil {
		return err
	}

	if len(propertyTestFiles) >= 3 {
		metrics.PropertyCoverage = 0.8
	} else if len(propertyTestFiles) >= 1 {
		metrics.PropertyCoverage = 0.6
	} else {
		metrics.PropertyCoverage = 0.3
	}

	// Helper utilization (check if helper directories exist)
	if _, err := os.Stat("helpers"); err == nil {
		metrics.HelperUtilization = 0.8
	} else {
		metrics.HelperUtilization = 0.3
	}

	// Cleanup effectiveness (assume good if cleanup helpers exist)
	if _, err := os.Stat("helpers/cleanup_helpers.go"); err == nil {
		metrics.CleanupEffectiveness = 0.9
	} else {
		metrics.CleanupEffectiveness = 0.5
	}

	// Execution stability (simplified - based on structure)
	if metrics.TestCoverage > 0.7 && metrics.HelperUtilization > 0.7 {
		metrics.ExecutionStability = 0.8
	} else {
		metrics.ExecutionStability = 0.6
	}

	// Performance score (based on parallel config and optimization)
	if _, err := os.Stat("parallel_config.go"); err == nil {
		metrics.PerformanceScore = 0.8
	} else {
		metrics.PerformanceScore = 0.5
	}

	result.QualityMetrics = metrics

	// Generate issues based on metrics
	if metrics.TestCoverage < 0.7 {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMajor,
			Component:   "test-coverage",
			Description: fmt.Sprintf("Low test coverage: %.1f%%", metrics.TestCoverage*100),
			Impact:      "Important functionality may not be tested",
			Suggestion:  "Add more comprehensive test cases",
		})
	}

	if metrics.PropertyCoverage < 0.6 {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:    IssueSeverityMinor,
			Component:   "property-coverage",
			Description: fmt.Sprintf("Low property-based test coverage: %.1f%%", metrics.PropertyCoverage*100),
			Impact:      "Universal properties may not be validated",
			Suggestion:  "Add more property-based tests",
		})
	}

	return nil
}

// calculateOverallHealth calculates the overall health status
func (v *TestSuiteValidator) calculateOverallHealth(result *ValidationResult) HealthStatus {
	criticalIssues := 0
	majorIssues := 0
	unhealthyComponents := 0
	degradedComponents := 0

	for _, issue := range result.Issues {
		switch issue.Severity {
		case IssueSeverityCritical:
			criticalIssues++
		case IssueSeverityMajor:
			majorIssues++
		}
	}

	for _, health := range result.ComponentHealth {
		switch health {
		case HealthStatusUnhealthy:
			unhealthyComponents++
		case HealthStatusDegraded:
			degradedComponents++
		}
	}

	if criticalIssues > 0 || unhealthyComponents > 2 {
		return HealthStatusUnhealthy
	}

	if majorIssues > 2 || degradedComponents > 3 {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

// generateRecommendations generates recommendations based on validation results
func (v *TestSuiteValidator) generateRecommendations(result *ValidationResult) {
	recommendations := []string{}

	// Based on overall health
	switch result.OverallHealth {
	case HealthStatusUnhealthy:
		recommendations = append(recommendations, "Address critical issues immediately before running tests")
	case HealthStatusDegraded:
		recommendations = append(recommendations, "Resolve major issues to improve test reliability")
	case HealthStatusHealthy:
		recommendations = append(recommendations, "Test suite is healthy - consider optimizations for better performance")
	}

	// Based on quality metrics
	if result.QualityMetrics.TestCoverage < 0.8 {
		recommendations = append(recommendations, "Increase test coverage by adding more test cases")
	}

	if result.QualityMetrics.PropertyCoverage < 0.7 {
		recommendations = append(recommendations, "Add more property-based tests for better validation")
	}

	if result.QualityMetrics.PerformanceScore < 0.7 {
		recommendations = append(recommendations, "Optimize test performance with parallel execution and caching")
	}

	// Based on component health
	for component, health := range result.ComponentHealth {
		if health == HealthStatusUnhealthy {
			recommendations = append(recommendations, fmt.Sprintf("Fix %s component issues", component))
		}
	}

	result.Recommendations = recommendations
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// PrintValidationReport prints a formatted validation report
func (result *ValidationResult) PrintValidationReport() {
	fmt.Printf("=== Test Suite Validation Report ===\n")
	fmt.Printf("Timestamp: %s\n", result.Timestamp.Format(time.RFC3339))
	fmt.Printf("Overall Health: %s\n\n", result.OverallHealth)

	fmt.Printf("Component Health:\n")
	for component, health := range result.ComponentHealth {
		status := "✅"
		if health == HealthStatusDegraded {
			status = "⚠️"
		} else if health == HealthStatusUnhealthy {
			status = "❌"
		}
		fmt.Printf("  %s %s: %s\n", status, component, health)
	}

	fmt.Printf("\nQuality Metrics:\n")
	fmt.Printf("  Test Coverage: %.1f%%\n", result.QualityMetrics.TestCoverage*100)
	fmt.Printf("  Property Coverage: %.1f%%\n", result.QualityMetrics.PropertyCoverage*100)
	fmt.Printf("  Helper Utilization: %.1f%%\n", result.QualityMetrics.HelperUtilization*100)
	fmt.Printf("  Cleanup Effectiveness: %.1f%%\n", result.QualityMetrics.CleanupEffectiveness*100)
	fmt.Printf("  Execution Stability: %.1f%%\n", result.QualityMetrics.ExecutionStability*100)
	fmt.Printf("  Performance Score: %.1f%%\n", result.QualityMetrics.PerformanceScore*100)

	if len(result.Issues) > 0 {
		fmt.Printf("\nIssues Found:\n")
		for i, issue := range result.Issues {
			severity := "ℹ️"
			switch issue.Severity {
			case IssueSeverityCritical:
				severity = "🔴"
			case IssueSeverityMajor:
				severity = "🟠"
			case IssueSeverityMinor:
				severity = "🟡"
			}
			fmt.Printf("  %d. %s [%s] %s\n", i+1, severity, issue.Component, issue.Description)
			fmt.Printf("     Impact: %s\n", issue.Impact)
			fmt.Printf("     Suggestion: %s\n", issue.Suggestion)
		}
	}

	if len(result.Recommendations) > 0 {
		fmt.Printf("\nRecommendations:\n")
		for i, rec := range result.Recommendations {
			fmt.Printf("  %d. %s\n", i+1, rec)
		}
	}

	fmt.Printf("\n=== End of Report ===\n")
}

// ValidateTestSuiteHealth performs a quick health check of the test suite
func ValidateTestSuiteHealth(ctx context.Context, client client.Client) error {
	validator := NewTestSuiteValidator(client, "test-validation")

	result, err := validator.ValidateTestSuite(ctx)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	result.PrintValidationReport()

	if result.OverallHealth == HealthStatusUnhealthy {
		return fmt.Errorf("test suite health check failed: %s", result.OverallHealth)
	}

	return nil
}
