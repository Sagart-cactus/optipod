package contract

import (
	"errors"
	"testing"
	"time"
)

// TestContractErrorHandling tests the error handling functionality without requiring cluster setup
func TestContractErrorHandling(t *testing.T) {
	t.Run("should create actionable error messages without internal details", func(t *testing.T) {
		errorHandler := NewContractErrorHandler("Test")

		// Test cluster creation error
		clusterErr := errors.New("failed to create cluster: internal error with stack trace at runtime.go:123")
		contractErr := errorHandler.HandleClusterCreationError(clusterErr)

		if contractErr.ErrorCode != "CLUSTER_CREATE_FAILED" {
			t.Errorf("Expected error code CLUSTER_CREATE_FAILED, got %s", contractErr.ErrorCode)
		}

		errorMsg := contractErr.Error()
		if !contains(errorMsg, "Failed to create test cluster") {
			t.Errorf("Expected actionable message, got: %s", errorMsg)
		}

		if contains(errorMsg, "stack trace") || contains(errorMsg, "runtime.go") {
			t.Errorf("Error message contains internal details: %s", errorMsg)
		}

		// Test manifest application error
		manifestErr := errors.New("reconcile loop failed with goroutine panic")
		contractErr = errorHandler.HandleManifestApplicationError(manifestErr)

		errorMsg = contractErr.Error()
		if !contains(errorMsg, "Failed to install OptiPod") {
			t.Errorf("Expected actionable message, got: %s", errorMsg)
		}

		if contains(errorMsg, "reconcile loop") || contains(errorMsg, "goroutine") {
			t.Errorf("Error message contains internal details: %s", errorMsg)
		}

		// Test controller readiness error
		contractErr = errorHandler.HandleControllerReadinessError(5 * time.Minute)
		errorMsg = contractErr.Error()

		if !contains(errorMsg, "Controller pod did not become Ready within 5m0s") {
			t.Errorf("Expected timeout message, got: %s", errorMsg)
		}

		if !contains(errorMsg, "Check if controller image is available") {
			t.Errorf("Expected actionable suggestion, got: %s", errorMsg)
		}
	})

	t.Run("should generate clear pass/fail status reports", func(t *testing.T) {
		errorHandler := NewContractErrorHandler("Test")
		reporter := NewTestResultReporter("Test Report")

		// Test with errors
		reporter.AddError(errorHandler.HandleClusterCreationError(errors.New("test error")))
		reporter.AddWarning("Test warning")

		report := reporter.GenerateReport()

		if report.Status != StatusFailed {
			t.Errorf("Expected status FAILED, got %s", report.Status)
		}

		if report.ErrorCount != 1 {
			t.Errorf("Expected 1 error, got %d", report.ErrorCount)
		}

		if report.WarningCount != 1 {
			t.Errorf("Expected 1 warning, got %d", report.WarningCount)
		}

		reportString := report.String()
		if !contains(reportString, "=== CONTRACT TEST REPORT ===") {
			t.Errorf("Report missing header: %s", reportString)
		}

		if !contains(reportString, "Status: FAILED") {
			t.Errorf("Report missing status: %s", reportString)
		}

		if !contains(reportString, "❌ Contract validation failed") {
			t.Errorf("Report missing failure indicator: %s", reportString)
		}

		// Test without errors
		successReporter := NewTestResultReporter("Success Test")
		successReport := successReporter.GenerateReport()

		if successReport.Status != StatusPassed {
			t.Errorf("Expected status PASSED, got %s", successReport.Status)
		}

		successString := successReport.String()
		if !contains(successString, "✅ All contract validations passed successfully") {
			t.Errorf("Report missing success indicator: %s", successString)
		}
	})

	t.Run("should sanitize error messages", func(t *testing.T) {
		errorHandler := NewContractErrorHandler("Test")

		// Test various internal error patterns
		testCases := []struct {
			input    string
			expected string
		}{
			{
				"controller-runtime panic: index out of range",
				"internal processing error occurred",
			},
			{
				"goroutine 123 [running]: panic",
				"internal processing error occurred",
			},
			{
				"runtime error: nil pointer dereference",
				"internal processing error occurred",
			},
			{
				"failed at /path/to/file.go:123",
				"configuration or resource error occurred",
			},
		}

		for _, tc := range testCases {
			sanitized := errorHandler.sanitizeErrorMessage(tc.input)
			if sanitized != tc.expected {
				t.Errorf("Expected '%s', got '%s' for input '%s'", tc.expected, sanitized, tc.input)
			}
		}
	})

	t.Run("should provide actionable suggestions", func(t *testing.T) {
		errorHandler := NewContractErrorHandler("Test")

		// Test cluster creation suggestions
		contractErr := errorHandler.HandleClusterCreationError(errors.New("test"))
		if !contains(contractErr.Suggestion, "Ensure Kind is installed") {
			t.Errorf("Missing Kind installation suggestion: %s", contractErr.Suggestion)
		}

		// Test controller readiness suggestions
		contractErr = errorHandler.HandleControllerReadinessError(1 * time.Minute)
		if !contains(contractErr.Suggestion, "Check if controller image is available") {
			t.Errorf("Missing controller image suggestion: %s", contractErr.Suggestion)
		}

		// Test CR creation suggestions
		contractErr = errorHandler.HandleCRCreationError("test", errors.New("test"))
		if !contains(contractErr.Suggestion, "Verify CRD is installed") {
			t.Errorf("Missing CRD verification suggestion: %s", contractErr.Suggestion)
		}
	})
}

// TestContractTestSummary tests the test suite summary functionality
func TestContractTestSummary(t *testing.T) {
	t.Run("should create comprehensive test suite summaries", func(t *testing.T) {
		// Test with mixed results
		summary := &ContractTestSummary{
			TotalTests:    5,
			PassedTests:   3,
			FailedTests:   2,
			TotalDuration: 8 * time.Minute,
		}

		summaryString := summary.GenerateSummary()

		if !contains(summaryString, "=== CONTRACT E2E TEST SUITE SUMMARY ===") {
			t.Errorf("Summary missing header: %s", summaryString)
		}

		if !contains(summaryString, "Total Tests: 5") {
			t.Errorf("Summary missing total tests: %s", summaryString)
		}

		if !contains(summaryString, "Passed: 3") {
			t.Errorf("Summary missing passed count: %s", summaryString)
		}

		if !contains(summaryString, "Failed: 2") {
			t.Errorf("Summary missing failed count: %s", summaryString)
		}

		if !contains(summaryString, "❌ SOME CONTRACT TESTS FAILED") {
			t.Errorf("Summary missing failure indicator: %s", summaryString)
		}

		// Test with all passing
		passSummary := &ContractTestSummary{
			TotalTests:    3,
			PassedTests:   3,
			FailedTests:   0,
			TotalDuration: 5 * time.Minute,
		}

		passString := passSummary.GenerateSummary()
		if !contains(passString, "✅ ALL CONTRACT TESTS PASSED") {
			t.Errorf("Summary missing success indicator: %s", passString)
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
