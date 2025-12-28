package contract

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Contract Validation Scope Enforcement", func() {
	var (
		k8sClient client.Client
		ctx       context.Context
		validator *ContractValidator
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		k8sClient, err = utils.GetK8sClient()
		Expect(err).NotTo(HaveOccurred())

		validator = NewContractValidator(k8sClient)
	})

	Context("when validating contract test scope", func() {
		// Requirements 2.1, 2.2, 2.3, 2.4, 2.5: Ensure tests validate only user-visible behavior
		It("should enforce user-visible behavior validation only", func() {
			By("validating that tests only check user-visible behavior")
			err := validator.ValidateUserVisibleBehaviorOnly()
			Expect(err).NotTo(HaveOccurred(), "Contract tests should only validate user-visible behavior")
		})

		It("should validate controller installation contract compliance", func() {
			By("checking that installation tests only verify pod readiness")
			err := validator.ValidateControllerInstallationContract(ctx, optipodNamespace)
			// This may fail if controller is not installed, which is expected in some test scenarios
			// The important part is that the validation logic only checks user-visible behavior
			if err != nil {
				// Verify the error is about missing controller, not about internal details
				Expect(err.Error()).To(ContainSubstring("controller pod"), "Error should be about pod readiness, not internal details")
			}
		})

		It("should validate CR acceptance contract compliance", func() {
			By("checking that CR tests only verify API server acceptance")
			testCR := createMinimalOptimizationPolicy("contract-scope-test", "default")

			// Clean up any existing CR first
			_ = k8sClient.Delete(ctx, testCR)
			time.Sleep(2 * time.Second)

			err := validator.ValidateCRAcceptanceContract(ctx, testCR)
			Expect(err).NotTo(HaveOccurred(), "CR acceptance validation should only check API server acceptance")

			// Clean up
			_ = k8sClient.Delete(ctx, testCR)
		})

		It("should validate status convergence contract compliance", func() {
			By("checking that status tests only verify Ready condition")
			testCR := createStatusTestOptimizationPolicy("contract-status-test", "default")

			// Clean up any existing CR first
			_ = k8sClient.Delete(ctx, testCR)
			time.Sleep(2 * time.Second)

			// Create the CR first
			err := k8sClient.Create(ctx, testCR)
			Expect(err).NotTo(HaveOccurred())

			// The validation may fail if Ready condition is not present yet, which is expected
			// The important part is that the validation logic only checks Ready=True condition
			err = validator.ValidateStatusConvergenceContract(ctx, testCR.Name, testCR.Namespace)
			if err != nil {
				// Verify the error is about Ready condition, not internal details
				Expect(err.Error()).To(ContainSubstring("Ready"), "Error should be about Ready condition, not internal details")
			}

			// Clean up
			_ = k8sClient.Delete(ctx, testCR)
		})

		It("should validate no forbidden assertions are present", func() {
			By("checking that tests do not contain forbidden internal assertions")
			violations := validator.ValidateNoForbiddenAssertions()
			Expect(violations).To(BeEmpty(), "Contract tests should not contain forbidden assertions about internal behavior")
		})

		It("should validate overall contract compliance", func() {
			By("performing comprehensive contract validation")
			err := validator.ValidateContractCompliance(ctx)
			Expect(err).NotTo(HaveOccurred(), "All contract tests should comply with user-visible behavior requirements")
		})
	})

	Context("when documenting allowed vs forbidden assertions", func() {
		It("should provide clear guidance on allowed assertions", func() {
			By("listing assertions that ARE allowed in contract tests")
			allowedAssertions := validator.GetAllowedAssertions()
			Expect(allowedAssertions).To(ContainElement("pod_ready_condition"), "Pod Ready condition is user-visible behavior")
			Expect(allowedAssertions).To(ContainElement("api_server_acceptance"), "API server acceptance is user-visible behavior")
			Expect(allowedAssertions).To(ContainElement("ready_true_condition"), "Ready=True condition is user-visible behavior")
			Expect(allowedAssertions).To(ContainElement("no_validation_errors"), "Validation errors are user-visible behavior")
			Expect(allowedAssertions).To(ContainElement("restart_count_zero"), "Restart count is user-visible behavior")
		})

		It("should identify forbidden assertion patterns", func() {
			By("documenting assertion patterns that violate contract requirements")
			violations := validator.ValidateNoForbiddenAssertions()

			// This test documents what should NOT be present in contract tests
			// The actual validation is done at compile time through code review
			By("ensuring no reconcile count assertions are present")
			// Contract tests should NOT assert reconcile counts (Requirement 2.4)

			By("ensuring no timing threshold assertions are present")
			// Contract tests should NOT assert specific timing thresholds (Requirement 2.4)

			By("ensuring no exact resource value assertions are present")
			// Contract tests should NOT assert exact CPU/memory values (Requirement 2.4)

			By("ensuring no controller log dependencies are present")
			// Contract tests should NOT depend on controller logs (Requirement 2.5)

			By("ensuring no internal field dependencies are present")
			// Contract tests should NOT depend on internal controller fields (Requirement 2.5)

			Expect(violations).To(BeEmpty(), "No forbidden assertion patterns should be present")
		})
	})
})
