package contract

import (
	"fmt"
	"strings"
	"time"
)

const (
	StatusFailed = "FAILED"
	StatusPassed = "PASSED"
)

// ContractError represents an error in contract E2E testing with actionable messages
type ContractError struct {
	TestName    string
	Phase       string
	UserMessage string
	Suggestion  string
	ErrorCode   string
	Timestamp   time.Time
}

// Error implements the error interface with actionable messages
// Requirement 1.4: error messages do not contain internal implementation details
func (ce *ContractError) Error() string {
	return fmt.Sprintf("[%s] %s: %s. %s", ce.ErrorCode, ce.Phase, ce.UserMessage, ce.Suggestion)
}

// ContractErrorHandler provides centralized error handling for contract tests
type ContractErrorHandler struct {
	testName string
}

// NewContractErrorHandler creates a new error handler for contract tests
func NewContractErrorHandler(testName string) *ContractErrorHandler {
	return &ContractErrorHandler{testName: testName}
}

// Installation Error Handling
// Requirement 1.4: Provide actionable error messages without internal details

// HandleClusterCreationError creates actionable error for cluster creation failures
func (eh *ContractErrorHandler) HandleClusterCreationError(err error) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Cluster Setup",
		UserMessage: "Failed to create test cluster",
		Suggestion:  "Ensure Kind is installed and Docker is running. Run 'kind version' and 'docker ps' to verify.",
		ErrorCode:   "CLUSTER_CREATE_FAILED",
		Timestamp:   time.Now(),
	}
}

// HandleManifestApplicationError creates actionable error for manifest application failures
func (eh *ContractErrorHandler) HandleManifestApplicationError(err error) *ContractError {
	// Filter out internal implementation details from error message
	userMessage := eh.sanitizeErrorMessage(err.Error())

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "OptiPod Installation",
		UserMessage: fmt.Sprintf("Failed to install OptiPod: %s", userMessage),
		Suggestion:  "Verify cluster has sufficient resources and CRDs are properly defined. Check 'kubectl get nodes' and 'kubectl get crd'.",
		ErrorCode:   "INSTALL_FAILED",
		Timestamp:   time.Now(),
	}
}

// HandleControllerReadinessError creates actionable error for controller readiness failures
func (eh *ContractErrorHandler) HandleControllerReadinessError(timeout time.Duration) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Controller Readiness",
		UserMessage: fmt.Sprintf("Controller pod did not become Ready within %v", timeout),
		Suggestion:  "Check if controller image is available and cluster has sufficient resources. Verify with 'kubectl get pods -n optipod-system'.",
		ErrorCode:   "CONTROLLER_NOT_READY",
		Timestamp:   time.Now(),
	}
}

// HandleControllerRestartError creates actionable error for controller restart issues
func (eh *ContractErrorHandler) HandleControllerRestartError(restartCount int32) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Controller Stability",
		UserMessage: fmt.Sprintf("Controller pod restarted %d times", restartCount),
		Suggestion:  "Controller may be crashing due to configuration issues. Check resource limits and configuration validity.",
		ErrorCode:   "CONTROLLER_UNSTABLE",
		Timestamp:   time.Now(),
	}
}

// HandleControllerNamespaceError creates actionable error for controller namespace deployment issues
func (eh *ContractErrorHandler) HandleControllerNamespaceError(expectedNamespace string) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Controller Deployment",
		UserMessage: fmt.Sprintf("Controller not found in expected namespace '%s'", expectedNamespace),
		Suggestion:  "Verify controller manifests specify correct namespace and deployment succeeded. Check 'kubectl get pods -n " + expectedNamespace + "'.",
		ErrorCode:   "CONTROLLER_NAMESPACE_MISMATCH",
		Timestamp:   time.Now(),
	}
}

// CR Acceptance Error Handling

// HandleCRCreationError creates actionable error for CR creation failures
func (eh *ContractErrorHandler) HandleCRCreationError(crName string, err error) *ContractError {
	userMessage := eh.sanitizeErrorMessage(err.Error())

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "CR Creation",
		UserMessage: fmt.Sprintf("Failed to create OptimizationPolicy '%s': %s", crName, userMessage),
		Suggestion:  "Verify CRD is installed and CR specification is valid. Check 'kubectl get crd optimizationpolicies.optipod.optipod.io'.",
		ErrorCode:   "CR_CREATE_FAILED",
		Timestamp:   time.Now(),
	}
}

// HandleCRValidationError creates actionable error for CR validation failures
func (eh *ContractErrorHandler) HandleCRValidationError(crName string, validationErrors []string) *ContractError {
	// Sanitize validation errors to remove internal details
	sanitizedErrors := make([]string, len(validationErrors))
	for i, err := range validationErrors {
		sanitizedErrors[i] = eh.sanitizeErrorMessage(err)
	}

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "CR Validation",
		UserMessage: fmt.Sprintf("OptimizationPolicy '%s' failed validation: %s", crName, strings.Join(sanitizedErrors, "; ")),
		Suggestion:  "Review CR specification against the schema. Ensure all required fields are present and valid.",
		ErrorCode:   "CR_VALIDATION_FAILED",
		Timestamp:   time.Now(),
	}
}

// HandleCRRetrievalError creates actionable error for CR retrieval failures
func (eh *ContractErrorHandler) HandleCRRetrievalError(crName, namespace string, err error) *ContractError {
	userMessage := eh.sanitizeErrorMessage(err.Error())

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "CR Retrieval",
		UserMessage: fmt.Sprintf("Failed to retrieve OptimizationPolicy '%s' from namespace '%s': %s", crName, namespace, userMessage),
		Suggestion:  "Verify CR was created successfully and namespace exists. Check 'kubectl get optimizationpolicies -n " + namespace + "'.",
		ErrorCode:   "CR_RETRIEVAL_FAILED",
		Timestamp:   time.Now(),
	}
}

// Status Convergence Error Handling

// HandleStatusTimeoutError creates actionable error for status convergence timeouts
func (eh *ContractErrorHandler) HandleStatusTimeoutError(crName string, timeout time.Duration) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Status Convergence",
		UserMessage: fmt.Sprintf("OptimizationPolicy '%s' did not reach Ready=True within %v", crName, timeout),
		Suggestion:  "Controller may need more time to process the policy. Verify controller is running and has access to metrics provider.",
		ErrorCode:   "STATUS_TIMEOUT",
		Timestamp:   time.Now(),
	}
}

// HandleStatusConditionError creates actionable error for missing status conditions
func (eh *ContractErrorHandler) HandleStatusConditionError(crName string, expectedCondition string) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Status Validation",
		UserMessage: fmt.Sprintf("OptimizationPolicy '%s' missing expected condition '%s'", crName, expectedCondition),
		Suggestion:  "Controller may still be processing the policy. Verify controller logs for any processing issues.",
		ErrorCode:   "STATUS_CONDITION_MISSING",
		Timestamp:   time.Now(),
	}
}

// Test Execution Error Handling

// HandleTestTimeoutError creates actionable error for test execution timeouts
func (eh *ContractErrorHandler) HandleTestTimeoutError(timeout time.Duration) *ContractError {
	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Test Execution",
		UserMessage: fmt.Sprintf("Test execution exceeded maximum timeout of %v", timeout),
		Suggestion:  "Test environment may be under-resourced or experiencing network issues. Verify cluster resources and connectivity.",
		ErrorCode:   "TEST_TIMEOUT",
		Timestamp:   time.Now(),
	}
}

// HandleCleanupError creates actionable error for cleanup failures
func (eh *ContractErrorHandler) HandleCleanupError(resource string, err error) *ContractError {
	userMessage := eh.sanitizeErrorMessage(err.Error())

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Cleanup",
		UserMessage: fmt.Sprintf("Failed to cleanup %s: %s", resource, userMessage),
		Suggestion:  "Manual cleanup may be required. Check for remaining resources with 'kubectl get all --all-namespaces'.",
		ErrorCode:   "CLEANUP_FAILED",
		Timestamp:   time.Now(),
	}
}

// HandleEnvironmentInitializationError creates actionable error for environment initialization failures
func (eh *ContractErrorHandler) HandleEnvironmentInitializationError(err error) *ContractError {
	userMessage := eh.sanitizeErrorMessage(err.Error())

	return &ContractError{
		TestName:    eh.testName,
		Phase:       "Environment Setup",
		UserMessage: fmt.Sprintf("Failed to initialize test environment: %s", userMessage),
		Suggestion:  "Ensure all required tools are installed (Kind, kubectl, Docker) and accessible in PATH. Verify Docker is running.",
		ErrorCode:   "ENV_INIT_FAILED",
		Timestamp:   time.Now(),
	}
}

// Utility Methods

// sanitizeErrorMessage removes internal implementation details from error messages
// Requirement 1.4: error messages do not contain internal implementation details
func (eh *ContractErrorHandler) sanitizeErrorMessage(errorMsg string) string {
	// Remove internal implementation details that should not be exposed to users
	internalPatterns := []string{
		"reconcile loop",
		"controller-runtime",
		"internal error",
		"stack trace",
		"goroutine",
		"panic",
		"runtime error",
		"memory address",
		"nil pointer",
		"index out of range",
	}

	sanitized := errorMsg
	for _, pattern := range internalPatterns {
		if strings.Contains(strings.ToLower(sanitized), pattern) {
			sanitized = "internal processing error occurred"
			break
		}
	}

	// Remove file paths and line numbers
	if strings.Contains(sanitized, ".go:") {
		sanitized = "configuration or resource error occurred"
	}

	// Limit message length to keep it actionable
	if len(sanitized) > 200 {
		sanitized = sanitized[:197] + "..."
	}

	return sanitized
}

// TestResultReporter provides clear pass/fail status reporting
// Requirement 3.5: Provide clear pass/fail status reporting
type TestResultReporter struct {
	testName  string
	startTime time.Time
	errors    []*ContractError
	warnings  []string
}

// NewTestResultReporter creates a new test result reporter
func NewTestResultReporter(testName string) *TestResultReporter {
	return &TestResultReporter{
		testName:  testName,
		startTime: time.Now(),
		errors:    make([]*ContractError, 0),
		warnings:  make([]string, 0),
	}
}

// AddError adds an error to the test results
func (trr *TestResultReporter) AddError(err *ContractError) {
	trr.errors = append(trr.errors, err)
}

// AddWarning adds a warning to the test results
func (trr *TestResultReporter) AddWarning(warning string) {
	trr.warnings = append(trr.warnings, warning)
}

// GenerateReport generates a clear pass/fail status report
// Requirement 3.5: Provide clear pass/fail status reporting
func (trr *TestResultReporter) GenerateReport() TestReport {
	duration := time.Since(trr.startTime)
	status := StatusPassed

	if len(trr.errors) > 0 {
		status = StatusFailed
	}

	return TestReport{
		TestName:     trr.testName,
		Status:       status,
		Duration:     duration,
		ErrorCount:   len(trr.errors),
		WarningCount: len(trr.warnings),
		Errors:       trr.errors,
		Warnings:     trr.warnings,
		Timestamp:    time.Now(),
	}
}

// TestReport represents the final test execution report
type TestReport struct {
	TestName     string
	Status       string
	Duration     time.Duration
	ErrorCount   int
	WarningCount int
	Errors       []*ContractError
	Warnings     []string
	Timestamp    time.Time
}

// String returns a formatted test report
func (tr *TestReport) String() string {
	var report strings.Builder

	// Header with clear pass/fail status
	report.WriteString("=== CONTRACT TEST REPORT ===\n")
	report.WriteString(fmt.Sprintf("Test: %s\n", tr.TestName))
	report.WriteString(fmt.Sprintf("Status: %s\n", tr.Status))
	report.WriteString(fmt.Sprintf("Duration: %v\n", tr.Duration))
	report.WriteString(fmt.Sprintf("Errors: %d, Warnings: %d\n", tr.ErrorCount, tr.WarningCount))
	report.WriteString(fmt.Sprintf("Timestamp: %s\n", tr.Timestamp.Format(time.RFC3339)))
	report.WriteString("\n")

	// Error details with actionable messages
	if len(tr.Errors) > 0 {
		report.WriteString("ERRORS:\n")
		for i, err := range tr.Errors {
			report.WriteString(fmt.Sprintf("%d. %s\n", i+1, err.Error()))
		}
		report.WriteString("\n")
	}

	// Warning details
	if len(tr.Warnings) > 0 {
		report.WriteString("WARNINGS:\n")
		for i, warning := range tr.Warnings {
			report.WriteString(fmt.Sprintf("%d. %s\n", i+1, warning))
		}
		report.WriteString("\n")
	}

	// Summary
	if tr.Status == "PASSED" {
		report.WriteString("✅ All contract validations passed successfully.\n")
	} else {
		report.WriteString("❌ Contract validation failed. Review errors above for resolution steps.\n")
	}

	return report.String()
}

// ContractTestSummary provides overall test suite summary
type ContractTestSummary struct {
	TotalTests    int
	PassedTests   int
	FailedTests   int
	TotalDuration time.Duration
	Reports       []TestReport
}

// GenerateSummary creates a summary of all contract test results
func (cts *ContractTestSummary) GenerateSummary() string {
	var summary strings.Builder

	summary.WriteString("=== CONTRACT E2E TEST SUITE SUMMARY ===\n")
	summary.WriteString(fmt.Sprintf("Total Tests: %d\n", cts.TotalTests))
	summary.WriteString(fmt.Sprintf("Passed: %d\n", cts.PassedTests))
	summary.WriteString(fmt.Sprintf("Failed: %d\n", cts.FailedTests))
	summary.WriteString(fmt.Sprintf("Total Duration: %v\n", cts.TotalDuration))
	summary.WriteString("\n")

	// Overall status
	if cts.FailedTests == 0 {
		summary.WriteString("✅ ALL CONTRACT TESTS PASSED\n")
		summary.WriteString("OptiPod user contracts are validated and working correctly.\n")
	} else {
		summary.WriteString("❌ SOME CONTRACT TESTS FAILED\n")
		summary.WriteString("Review individual test reports for resolution steps.\n")
	}

	return summary.String()
}
