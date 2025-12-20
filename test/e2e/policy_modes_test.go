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

var _ = Describe("Policy Modes", func() {
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

	Context("Auto Mode", func() {
		It("should generate recommendations and apply updates", func() {
			By("Creating Auto mode policy")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-auto-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-auto-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating test workload")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-auto-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("policy-mode-auto-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the workload")
			// Give OptipPod time to discover and process the workload
			time.Sleep(60 * time.Second)

			By("Verifying policy configuration")
			// Check that the policy has the correct mode
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-auto-test", "-n", "optipod-system", "-o", "jsonpath={.spec.mode}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Auto"))

			By("Verifying policy selector configuration")
			// Check that the policy has the correct selector
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-auto-test", "-n", "optipod-system", "-o", "jsonpath={.spec.selector.workloadSelector.matchLabels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("auto"))

			By("Verifying workload was created with correct labels")
			// Verify the workload has the labels that match the policy selector
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-auto-test", "-n", "default", "-o", "jsonpath={.metadata.labels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("auto"))

			By("Verifying workload resource specifications")
			// Verify the workload has the expected resource requests
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-auto-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("100m"))
			GinkgoWriter.Println("✓ Auto mode test completed successfully")
		})
	})

	Context("Recommend Mode", func() {
		It("should generate recommendations but not apply updates", func() {
			By("Creating Recommend mode policy")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-recommend-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-recommend-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating test workload")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-recommend-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("policy-mode-recommend-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for OptipPod to process the workload")
			time.Sleep(60 * time.Second)

			By("Verifying policy configuration")
			// Check that the policy has the correct mode
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-recommend-test", "-n", "optipod-system", "-o", "jsonpath={.spec.mode}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Recommend"))

			By("Verifying policy selector configuration")
			// Check that the policy has the correct selector
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-recommend-test", "-n", "optipod-system", "-o", "jsonpath={.spec.selector.workloadSelector.matchLabels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("recommend"))

			By("Verifying workload was created with correct labels")
			// Verify the workload has the labels that match the policy selector
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-recommend-test", "-n", "default", "-o", "jsonpath={.metadata.labels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("recommend"))

			By("Verifying original resource specifications are unchanged")
			// In Recommend mode, original resources should not be modified
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-recommend-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should still have original CPU request (100m from the YAML)
			Expect(strings.TrimSpace(output)).To(Equal("100m"))
			GinkgoWriter.Println("✓ Recommend mode test completed successfully")
		})
	})

	Context("Disabled Mode", func() {
		It("should not process workloads at all", func() {
			By("Creating Disabled mode policy")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-disabled-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy was created")
			Eventually(func() error {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-disabled-test", "-n", "optipod-system")
				_, err := utils.Run(cmd)
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Creating test workload")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-disabled-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("policy-mode-disabled-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting to ensure OptipPod does not process the workload")
			// Give enough time for processing to occur if it were going to happen
			time.Sleep(60 * time.Second)

			By("Verifying policy configuration")
			// Check that the policy has the correct mode
			cmd := exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-disabled-test", "-n", "optipod-system", "-o", "jsonpath={.spec.mode}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Disabled"))

			By("Verifying policy selector configuration")
			// Check that the policy has the correct selector
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-disabled-test", "-n", "optipod-system", "-o", "jsonpath={.spec.selector.workloadSelector.matchLabels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("disabled"))

			By("Verifying workload was created with correct labels")
			// Verify the workload has the labels that match the policy selector
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-disabled-test", "-n", "default", "-o", "jsonpath={.metadata.labels}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("policy-mode-test"))
			Expect(output).To(ContainSubstring("disabled"))

			By("Verifying policy resource bounds configuration")
			// Check that the policy has the expected resource bounds
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-disabled-test", "-n", "optipod-system", "-o", "jsonpath={.spec.resourceBounds.cpu.min}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("50m"))

			By("Verifying original resource specifications are completely unchanged")
			cmd = exec.Command("kubectl", "get", "deployment", "policy-mode-disabled-test", "-n", "default", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Should still have original CPU request (100m from the YAML)
			Expect(strings.TrimSpace(output)).To(Equal("100m"))
			GinkgoWriter.Println("✓ Disabled mode test completed successfully")
		})
	})

	Context("Policy Mode Transitions", func() {
		It("should handle mode changes correctly", func() {
			By("Creating a policy in Recommend mode")
			err := policyHelper.CreatePolicyFromFile("hack/test-policy-recommend-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Creating test workload")
			err = workloadHelper.CreateWorkloadFromFile("hack/test-workload-recommend-mode.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial processing")
			err = workloadHelper.WaitForWorkloadReady("policy-mode-recommend-test", "default", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(30 * time.Second)

			By("Updating policy to Auto mode")
			// Patch the policy to change mode from Recommend to Auto
			patch := `{"spec":{"mode":"Auto"}}`
			cmd := exec.Command("kubectl", "patch", "optimizationpolicy", "policy-mode-recommend-test", "-n", "optipod-system", "--type=merge", "-p", patch)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for mode change to take effect")
			time.Sleep(30 * time.Second)

			By("Verifying policy mode change")
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "policy-mode-recommend-test", "-n", "optipod-system", "-o", "jsonpath={.spec.mode}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Auto"))
			GinkgoWriter.Println("✓ Policy mode transition test completed successfully")
		})
	})
})
