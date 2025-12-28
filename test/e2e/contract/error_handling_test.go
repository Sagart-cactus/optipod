package contract

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Contract Error Handling", func() {
	var (
		errorHandler *ContractErrorHandler
		reporter     *TestResultReporter
	)

	BeforeEach(func() {
		errorHandler = NewContractErrorHandler("Error Handling Test")
		reporter = NewTestResultReporter("Error Handling Test")
	})

	Context("when handling various error scenarios", func() {
		It("should create actionable error messages without internal details", func() {
			By("handling cluster creation errors")
			clusterErr := errors.New("failed to create cluster: internal error with stack trace at runtime.go:123")
			contractErr := errorHandler.HandleClusterCreationError(clusterErr)

			Expect(contractErr.Error()).To(ContainSubstring("Failed to create test cluster"))
			Expect(contractErr.Error()).To(ContainSubstring("Ensure Kind is installed"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("stack trace"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("runtime.go"))
			Expect(contractErr.ErrorCode).To(Equal("CLUSTER_CREATE_FAILED"))

			By("handling manifest application errors")
			manifestErr := errors.New("reconcile loop failed with goroutine panic")
			contractErr = errorHandler.HandleManifestApplicationError(manifestErr)

			Expect(contractErr.Error()).To(ContainSubstring("Failed to install OptiPod"))
			Expect(contractErr.Error()).To(ContainSubstring("Verify cluster has sufficient resources"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("reconcile loop"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("goroutine"))
			Expect(contractErr.ErrorCode).To(Equal("INSTALL_FAILED"))

			By("handling controller readiness errors")
			contractErr = errorHandler.HandleControllerReadinessError(5 * time.Minute)

			Expect(contractErr.Error()).To(ContainSubstring("Controller pod did not become Ready within 5m0s"))
			Expect(contractErr.Error()).To(ContainSubstring("Check if controller image is available"))
			Expect(contractErr.ErrorCode).To(Equal("CONTROLLER_NOT_READY"))

			By("handling CR creation errors")
			crErr := errors.New("validation failed: nil pointer dereference at controller.go:456")
			contractErr = errorHandler.HandleCRCreationError("test-cr", crErr)

			Expect(contractErr.Error()).To(ContainSubstring("Failed to create OptimizationPolicy 'test-cr'"))
			Expect(contractErr.Error()).To(ContainSubstring("Verify CRD is installed"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("nil pointer"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("controller.go"))
			Expect(contractErr.ErrorCode).To(Equal("CR_CREATE_FAILED"))

			By("handling status timeout errors")
			contractErr = errorHandler.HandleStatusTimeoutError("test-policy", 2*time.Minute)

			Expect(contractErr.Error()).To(ContainSubstring("OptimizationPolicy 'test-policy' did not reach Ready=True within 2m0s"))
			Expect(contractErr.Error()).To(ContainSubstring("Controller may need more time"))
			Expect(contractErr.ErrorCode).To(Equal("STATUS_TIMEOUT"))
		})

		It("should generate clear pass/fail status reports", func() {
			By("creating a test report with errors")
			reporter.AddError(errorHandler.HandleClusterCreationError(errors.New("test error")))
			reporter.AddError(errorHandler.HandleControllerReadinessError(1 * time.Minute))
			reporter.AddWarning("Test warning message")

			report := reporter.GenerateReport()

			Expect(report.Status).To(Equal("FAILED"))
			Expect(report.ErrorCount).To(Equal(2))
			Expect(report.WarningCount).To(Equal(1))
			Expect(report.TestName).To(Equal("Error Handling Test"))

			reportString := report.String()
			Expect(reportString).To(ContainSubstring("=== CONTRACT TEST REPORT ==="))
			Expect(reportString).To(ContainSubstring("Status: FAILED"))
			Expect(reportString).To(ContainSubstring("Errors: 2, Warnings: 1"))
			Expect(reportString).To(ContainSubstring("ERRORS:"))
			Expect(reportString).To(ContainSubstring("WARNINGS:"))
			Expect(reportString).To(ContainSubstring("❌ Contract validation failed"))

			By("creating a test report without errors")
			successReporter := NewTestResultReporter("Success Test")
			successReport := successReporter.GenerateReport()

			Expect(successReport.Status).To(Equal("PASSED"))
			Expect(successReport.ErrorCount).To(Equal(0))
			Expect(successReport.WarningCount).To(Equal(0))

			successString := successReport.String()
			Expect(successString).To(ContainSubstring("Status: PASSED"))
			Expect(successString).To(ContainSubstring("✅ All contract validations passed successfully"))
		})

		It("should sanitize error messages to remove internal details", func() {
			By("testing error message sanitization")
			internalErr := errors.New("controller-runtime panic: index out of range at /path/to/file.go:123")
			contractErr := errorHandler.HandleManifestApplicationError(internalErr)

			// Should not contain internal implementation details
			Expect(contractErr.Error()).NotTo(ContainSubstring("controller-runtime"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("panic"))
			Expect(contractErr.Error()).NotTo(ContainSubstring("index out of range"))
			Expect(contractErr.Error()).NotTo(ContainSubstring(".go:"))

			// Should contain actionable user message
			Expect(contractErr.Error()).To(ContainSubstring("Failed to install OptiPod"))
			Expect(contractErr.Error()).To(ContainSubstring("internal processing error occurred"))
		})

		It("should provide actionable suggestions for common error scenarios", func() {
			By("checking cluster creation error suggestions")
			contractErr := errorHandler.HandleClusterCreationError(errors.New("test"))
			Expect(contractErr.Suggestion).To(ContainSubstring("Ensure Kind is installed"))
			Expect(contractErr.Suggestion).To(ContainSubstring("kind version"))
			Expect(contractErr.Suggestion).To(ContainSubstring("docker ps"))

			By("checking controller readiness error suggestions")
			contractErr = errorHandler.HandleControllerReadinessError(1 * time.Minute)
			Expect(contractErr.Suggestion).To(ContainSubstring("Check if controller image is available"))
			Expect(contractErr.Suggestion).To(ContainSubstring("kubectl get pods"))

			By("checking CR creation error suggestions")
			contractErr = errorHandler.HandleCRCreationError("test", errors.New("test"))
			Expect(contractErr.Suggestion).To(ContainSubstring("Verify CRD is installed"))
			Expect(contractErr.Suggestion).To(ContainSubstring("kubectl get crd"))

			By("checking status timeout error suggestions")
			contractErr = errorHandler.HandleStatusTimeoutError("test", 1*time.Minute)
			Expect(contractErr.Suggestion).To(ContainSubstring("Controller may need more time"))
			Expect(contractErr.Suggestion).To(ContainSubstring("metrics provider"))
		})
	})

	Context("when generating test suite summaries", func() {
		It("should create comprehensive test suite summaries", func() {
			By("creating a test summary with mixed results")
			summary := &ContractTestSummary{
				TotalTests:    5,
				PassedTests:   3,
				FailedTests:   2,
				TotalDuration: 8 * time.Minute,
			}

			summaryString := summary.GenerateSummary()

			Expect(summaryString).To(ContainSubstring("=== CONTRACT E2E TEST SUITE SUMMARY ==="))
			Expect(summaryString).To(ContainSubstring("Total Tests: 5"))
			Expect(summaryString).To(ContainSubstring("Passed: 3"))
			Expect(summaryString).To(ContainSubstring("Failed: 2"))
			Expect(summaryString).To(ContainSubstring("Total Duration: 8m0s"))
			Expect(summaryString).To(ContainSubstring("❌ SOME CONTRACT TESTS FAILED"))

			By("creating a test summary with all passing tests")
			passSummary := &ContractTestSummary{
				TotalTests:    3,
				PassedTests:   3,
				FailedTests:   0,
				TotalDuration: 5 * time.Minute,
			}

			passString := passSummary.GenerateSummary()
			Expect(passString).To(ContainSubstring("✅ ALL CONTRACT TESTS PASSED"))
			Expect(passString).To(ContainSubstring("OptiPod user contracts are validated"))
		})
	})
})
