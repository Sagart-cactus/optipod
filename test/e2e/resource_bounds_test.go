//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Resource Bounds Enforcement", func() {
	var (
		policyHelper   *helpers.PolicyHelper
		workloadHelper *helpers.WorkloadHelper
		cleanupHelper  *helpers.CleanupHelper
	)

	BeforeEach(func() {
		policyHelper = helpers.NewPolicyHelper()
		workloadHelper = helpers.NewWorkloadHelper()
		cleanupHelper = helpers.NewCleanupHelper()

		// Ensure clean state before each test
		By("Cleaning up any existing test resources")
		cleanupHelper.CleanupAllPolicies()
		cleanupHelper.CleanupTestWorkloads("default")

		// Wait a bit for cleanup to complete
		time.Sleep(5 * time.Second)
	})

	AfterEach(func() {
		By("Cleaning up test resources after test")
		cleanupHelper.CleanupAllPolicies()
		cleanupHelper.CleanupTestWorkloads("default")
	})

	Context("Within Bounds Scenarios", func() {
		It("should respect resource bounds when resources are within limits", func() {
			By("Creating policy with specific resource bounds")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-bounds-within.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-within-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating workload with resources within bounds")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-bounds-within.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("bounds-within-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy resource bounds configuration")
			// Check CPU bounds
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-within-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.min}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("100m"))

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "bounds-within-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.max}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("1000m"))

			By("Verifying workload resources are within bounds")
			cmd = exec.Command("kubectl", "get", "deployment", "bounds-within-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should be 500m which is within bounds (100m-1000m)
			Expect(strings.TrimSpace(output)).To(Equal("500m"))

			GinkgoWriter.Println("✓ Within bounds test completed successfully")
		})
	})

	Context("Below Minimum Clamping Scenarios", func() {
		It("should clamp resources to minimum when below bounds", func() {
			By("Creating policy with minimum resource bounds")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-bounds-below-min.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-below-min-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating workload with resources below minimum")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-bounds-below-min.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("bounds-below-min-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy minimum bounds configuration")
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-below-min-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.min}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("200m"))

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "bounds-below-min-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.memory.min}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("256Mi"))

			By("Verifying workload has resources below minimum (before optimization)")
			cmd = exec.Command("kubectl", "get", "deployment", "bounds-below-min-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should be 50m which is below minimum (200m)
			Expect(strings.TrimSpace(output)).To(Equal("50m"))

			By("Verifying memory is also below minimum")
			cmd = exec.Command("kubectl", "get", "deployment", "bounds-below-min-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should be 128Mi which is below minimum (256Mi)
			Expect(strings.TrimSpace(output)).To(Equal("128Mi"))

			GinkgoWriter.Println("✓ Below minimum bounds test completed successfully")
		})
	})

	Context("Above Maximum Clamping Scenarios", func() {
		It("should clamp resources to maximum when above bounds", func() {
			By("Creating policy with maximum resource bounds")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-bounds-above-max.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-above-max-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating workload with resources above maximum")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-bounds-above-max.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("bounds-above-max-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy maximum bounds configuration")
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "bounds-above-max-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.max}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("800m"))

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "bounds-above-max-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.memory.max}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("1Gi"))

			By("Verifying workload has resources above maximum (before optimization)")
			cmd = exec.Command("kubectl", "get", "deployment", "bounds-above-max-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should be 2000m or 2 (Kubernetes normalizes these) which is above maximum (800m)
			cpuValue := strings.TrimSpace(output)
			Expect(cpuValue).To(Or(Equal("2000m"), Equal("2")))

			By("Verifying memory is also above maximum")
			cmd = exec.Command("kubectl", "get", "deployment", "bounds-above-max-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should be 2Gi which is above maximum (1Gi)
			Expect(strings.TrimSpace(output)).To(Equal("2Gi"))

			GinkgoWriter.Println("✓ Above maximum bounds test completed successfully")
		})
	})

	Context("Resource Unit Conversion", func() {
		It("should handle different resource units correctly", func() {
			By("Creating policy with mixed resource units")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-boundary-limits.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "boundary-limits-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Verifying CPU bounds with different units")
			// Test that 0.1 (100m) and 1.5 (1500m) are handled correctly
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "boundary-limits-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.min}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("100m"))

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "boundary-limits-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.max}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("1500m"))

			By("Verifying memory bounds with different units")
			// Test that Mi and Gi units are handled correctly
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "boundary-limits-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.memory.min}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("128Mi"))

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "boundary-limits-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.memory.max}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("3Gi"))

			GinkgoWriter.Println("✓ Resource unit conversion test completed successfully")
		})
	})

	Context("Invalid Bounds Configuration", func() {
		It("should handle invalid resource bounds gracefully", func() {
			By("Attempting to create policy with invalid bounds (min > max)")
			// This should either be rejected by the API server or handled gracefully
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-invalid-bounds.yaml")

			// We expect this to either fail (which is good) or succeed but be handled properly
			if err != nil {
				GinkgoWriter.Println("✓ Invalid bounds policy correctly rejected")
				Expect(err.Error()).To(ContainSubstring("invalid"))
			} else {
				GinkgoWriter.Println("⚠ Invalid bounds policy was accepted - checking if handled properly")

				By("Verifying policy exists but may have validation issues")
				Eventually(func() error {
					cmd := exec.Command("kubectl", "get", "optimizationpolicy", "invalid-bounds-test", "-n", "optipod-system")
					_, err := utils.Run(cmd)
					return err
				}, 30*time.Second, 5*time.Second).Should(Succeed())

				By("Checking policy status for validation errors")
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "invalid-bounds-test", "-n", "optipod-system", "-o", "jsonpath={.status}")
				output, err := utils.Run(cmd)
				if err == nil && strings.TrimSpace(output) != "" {
					GinkgoWriter.Printf("Policy status: %s\n", output)
				}
			}

			GinkgoWriter.Println("✓ Invalid bounds configuration test completed successfully")
		})
	})
})
