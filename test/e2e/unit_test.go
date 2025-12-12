package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCoverageValidatorUnit tests the coverage validator functionality
func TestCoverageValidatorUnit(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "coverage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to clean up temp dir: %v", err)
		}
	}()

	validator := &CoverageValidator{
		RequirementsFile: filepath.Join(tempDir, "requirements.md"),
		DesignFile:       filepath.Join(tempDir, "design.md"),
		TestDirectory:    tempDir,
	}

	t.Run("ParseRequirements", func(t *testing.T) {
		// Create a sample requirements file
		requirementsContent := `# Requirements Document

## Requirements

### Requirement 1
1. WHEN a user creates a policy THEN the system SHALL validate the configuration
2. WHEN a policy is invalid THEN the system SHALL reject it with an error message

### Requirement 2
1. WHEN a workload is processed THEN the system SHALL generate recommendations
`
		err := os.WriteFile(validator.RequirementsFile, []byte(requirementsContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write requirements file: %v", err)
		}

		requirements, err := validator.parseRequirements()
		if err != nil {
			t.Fatalf("Failed to parse requirements: %v", err)
		}

		if len(requirements) != 3 {
			t.Errorf("Expected 3 requirements, got %d", len(requirements))
		}

		if requirements[0] == "" {
			t.Error("First requirement should not be empty")
		}
	})

	t.Run("ParseProperties", func(t *testing.T) {
		designContent := `# Design Document

## Correctness Properties

Property 1: Policy mode behavior consistency
*For any* optimization policy configuration and workload, the policy mode should consistently determine whether recommendations are generated
**Validates: Requirements 1.1, 1.2, 1.3**

Property 2: Resource bounds enforcement
*For any* optimization policy with resource bounds and any workload, recommendations should always respect the configured limits
**Validates: Requirements 2.2, 2.3, 2.4**
`
		err := os.WriteFile(validator.DesignFile, []byte(designContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write design file: %v", err)
		}

		properties, err := validator.parseProperties()
		if err != nil {
			t.Fatalf("Failed to parse properties: %v", err)
		}

		if len(properties) != 2 {
			t.Errorf("Expected 2 properties, got %d", len(properties))
		}
	})

	t.Run("ScanTestFiles", func(t *testing.T) {
		// Create some test files
		testFiles := []string{
			"policy_test.go",
			"workload_test.go",
		}

		for _, file := range testFiles {
			err := os.WriteFile(filepath.Join(tempDir, file), []byte("package test"), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file %s: %v", file, err)
			}
		}

		// Create a non-test file
		err := os.WriteFile(filepath.Join(tempDir, "helper.go"), []byte("package helper"), 0644)
		if err != nil {
			t.Fatalf("Failed to create helper file: %v", err)
		}

		foundFiles, err := validator.scanTestFiles()
		if err != nil {
			t.Fatalf("Failed to scan test files: %v", err)
		}

		if len(foundFiles) != 2 {
			t.Errorf("Expected 2 test files, got %d", len(foundFiles))
		}
	})

	t.Run("ExtractRequirementID", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"1.1 WHEN a user creates a policy", "1.1"},
			{"2.3 WHEN workloads are processed", "2.3"},
			{"10 WHEN something happens", "10"},
			{"WHEN no number", ""},
		}

		for _, tc := range testCases {
			result := validator.extractRequirementID(tc.input)
			if result != tc.expected {
				t.Errorf("extractRequirementID(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		}
	})

	t.Run("ExtractPropertyID", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"Property 1: Policy mode behavior", "1"},
			{"Property 10: Resource bounds", "10"},
			{"Property 2: Something else", "2"},
			{"No property here", ""},
		}

		for _, tc := range testCases {
			result := validator.extractPropertyID(tc.input)
			if result != tc.expected {
				t.Errorf("extractPropertyID(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		}
	})

	t.Run("CalculateCoveragePercent", func(t *testing.T) {
		report := &TestCoverageReport{
			Requirements: []RequirementCoverage{
				{ID: "1.1", Covered: true},
				{ID: "1.2", Covered: true},
				{ID: "2.1", Covered: false},
				{ID: "2.2", Covered: true},
			},
			Properties: []PropertyCoverage{
				{ID: "1", Implemented: true},
				{ID: "2", Implemented: false},
			},
		}

		percentage := validator.calculateCoveragePercent(report)
		// 3 covered requirements + 1 implemented property = 4 out of 6 total = 66.67%
		expected := 66.66666666666666
		if percentage < expected-0.1 || percentage > expected+0.1 {
			t.Errorf("Expected coverage ~%.2f%%, got %.2f%%", expected, percentage)
		}
	})

	t.Run("IdentifyMissingCoverage", func(t *testing.T) {
		report := &TestCoverageReport{
			Requirements: []RequirementCoverage{
				{ID: "1.1", Description: "Policy creation", Covered: true},
				{ID: "1.2", Description: "Policy validation", Covered: false},
			},
			Properties: []PropertyCoverage{
				{ID: "1", Description: "Property 1: Consistency", Implemented: true},
				{ID: "2", Description: "Property 2: Bounds", Implemented: false},
			},
		}

		missing := validator.identifyMissingCoverage(report)
		if len(missing) != 2 {
			t.Errorf("Expected 2 missing items, got %d", len(missing))
		}
	})
}

// TestHealthStatusCalculation tests health status calculation logic
func TestHealthStatusCalculation(t *testing.T) {
	validator := &TestSuiteValidator{}

	t.Run("HealthyStatus", func(t *testing.T) {
		result := &ValidationResult{
			ComponentHealth: map[string]HealthStatus{
				"component1": HealthStatusHealthy,
				"component2": HealthStatusHealthy,
			},
			Issues: []ValidationIssue{
				{Severity: IssueSeverityMinor},
			},
		}

		health := validator.calculateOverallHealth(result)
		if health != HealthStatusHealthy {
			t.Errorf("Expected healthy status, got %s", health)
		}
	})

	t.Run("DegradedStatus", func(t *testing.T) {
		result := &ValidationResult{
			ComponentHealth: map[string]HealthStatus{
				"component1": HealthStatusDegraded,
				"component2": HealthStatusDegraded,
				"component3": HealthStatusDegraded,
				"component4": HealthStatusDegraded,
			},
			Issues: []ValidationIssue{
				{Severity: IssueSeverityMajor},
				{Severity: IssueSeverityMajor},
				{Severity: IssueSeverityMajor},
			},
		}

		health := validator.calculateOverallHealth(result)
		if health != HealthStatusDegraded {
			t.Errorf("Expected degraded status, got %s", health)
		}
	})

	t.Run("UnhealthyStatus", func(t *testing.T) {
		result := &ValidationResult{
			ComponentHealth: map[string]HealthStatus{
				"component1": HealthStatusUnhealthy,
				"component2": HealthStatusUnhealthy,
				"component3": HealthStatusUnhealthy,
			},
			Issues: []ValidationIssue{
				{Severity: IssueSeverityCritical},
			},
		}

		health := validator.calculateOverallHealth(result)
		if health != HealthStatusUnhealthy {
			t.Errorf("Expected unhealthy status, got %s", health)
		}
	})
}

// TestRecommendationGeneration tests recommendation generation
func TestRecommendationGeneration(t *testing.T) {
	validator := &TestSuiteValidator{}

	t.Run("GenerateRecommendations", func(t *testing.T) {
		result := &ValidationResult{
			OverallHealth: HealthStatusDegraded,
			ComponentHealth: map[string]HealthStatus{
				"test-structure": HealthStatusUnhealthy,
			},
			QualityMetrics: QualityMetrics{
				TestCoverage:     0.6,
				PropertyCoverage: 0.5,
				PerformanceScore: 0.6,
			},
		}

		validator.generateRecommendations(result)
		if len(result.Recommendations) == 0 {
			t.Error("Expected recommendations to be generated")
		}
	})
}

// TestValidationReportPrinting tests that report printing doesn't panic
func TestValidationReportPrinting(t *testing.T) {
	t.Run("PrintReport", func(t *testing.T) {
		result := &ValidationResult{
			Timestamp:     time.Now(),
			OverallHealth: HealthStatusHealthy,
			ComponentHealth: map[string]HealthStatus{
				"component1": HealthStatusHealthy,
			},
			QualityMetrics: QualityMetrics{
				TestCoverage: 0.85,
			},
			Issues:          []ValidationIssue{},
			Recommendations: []string{"Test recommendation"},
		}

		// This should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("PrintValidationReport panicked: %v", r)
			}
		}()

		result.PrintValidationReport()
	})
}
