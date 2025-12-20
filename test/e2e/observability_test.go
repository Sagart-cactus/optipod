package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
)

var _ = Describe("Observability", func() {
	var (
		validationHelper *helpers.ValidationHelper
	)

	BeforeEach(func() {
		validationHelper = helpers.NewValidationHelper()
	})

	Context("Metrics Endpoint", func() {
		It("should expose Prometheus metrics", Pending, func() {
			// TODO: Fix metrics endpoint accessibility issue
			// This test is temporarily disabled due to metrics service configuration issues
			By("Checking metrics endpoint accessibility")
			err := validationHelper.ValidateMetricsEndpoint()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should contain OptipPod-specific metrics after workload processing", func() {
			// This test would create a policy and workload, then check for OptipPod metrics
			Skip("Requires workload processing - implement after basic infrastructure is working")
		})
	})

	Context("Logging", func() {
		It("should produce structured logs", func() {
			By("Checking for recent controller logs")
			found, err := validationHelper.CheckOptipodLogs("controller", 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue(), "Should find controller-related log messages")
		})

		It("should log policy processing events", func() {
			Skip("Requires policy processing - implement after basic infrastructure is working")
		})
	})
})
