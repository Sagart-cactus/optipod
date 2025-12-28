package e2e

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Memory Safety Integration Tests", func() {
	var (
		policyHelper     *helpers.PolicyHelper
		workloadHelper   *helpers.WorkloadHelper
		validationHelper *helpers.ValidationHelper
		cleanupHelper    *helpers.CleanupHelper
	)

	BeforeEach(func() {
		policyHelper = helpers.NewPolicyHelper()
		workloadHelper = helpers.NewWorkloadHelper()
		validationHelper = helpers.NewValidationHelper()
		cleanupHelper = helpers.NewCleanupHelper()

		// Clean up any existing test resources
		_ = cleanupHelper.CleanupAllPolicies()
		_ = cleanupHelper.CleanupAllWorkloads()
		time.Sleep(10 * time.Second) // Allow cleanup to complete
	})

	AfterEach(func() {
		// Clean up test resources
		_ = cleanupHelper.CleanupAllPolicies()
		_ = cleanupHelper.CleanupAllWorkloads()
	})

	Context("Real Workload Testing", func() {
		It("should optimize underutilized workload with default limits", func() {
			By("Deploying underutilized test workload")
			err := workloadHelper.CreateWorkloadFromFile("test-memory-safety-workloads.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("memory-safety-underutilized", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial resource configuration")
			initialResources := getWorkloadResources("memory-safety-underutilized", "default")
			Expect(initialResources).NotTo(BeEmpty())
			GinkgoWriter.Printf("Initial resources: %+v\n", initialResources)

			By("Applying optimization policy with default limits")
			err = policyHelper.CreatePolicyFromFile("test-memory-safety-policies.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("memory-safety-default-limits", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute) // Allow metrics collection and optimization

			By("Verifying optimization was applied")
			Eventually(func() bool {
				finalResources := getWorkloadResources("memory-safety-underutilized", "default")
				GinkgoWriter.Printf("Final resources: %+v\n", finalResources)

				// Check if memory request was reduced (underutilized workload)
				initialMemory := parseMemoryValue(initialResources["memory_request"])
				finalMemory := parseMemoryValue(finalResources["memory_request"])

				return finalMemory < initialMemory
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying default limit multipliers were applied")
			finalResources := getWorkloadResources("memory-safety-underutilized", "default")
			memoryRequest := parseMemoryValue(finalResources["memory_request"])
			memoryLimit := parseMemoryValue(finalResources["memory_limit"])

			// Default memory multiplier is 1.3
			expectedLimit := float64(memoryRequest) * 1.3
			tolerance := expectedLimit * 0.1 // 10% tolerance

			Expect(float64(memoryLimit)).To(BeNumerically("~", expectedLimit, tolerance))
		})

		It("should optimize overutilized workload with custom limits", func() {
			By("Deploying overutilized test workload")
			err := workloadHelper.CreateWorkloadFromFile("test-memory-safety-workloads.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("memory-safety-overutilized", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial resource configuration")
			initialResources := getWorkloadResources("memory-safety-overutilized", "default")
			Expect(initialResources).NotTo(BeEmpty())
			GinkgoWriter.Printf("Initial resources: %+v\n", initialResources)

			By("Creating custom limits policy")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-custom-test
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
    workloadSelector:
      matchLabels:
        app: memory-safety-overutilized
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
    limitConfig:
      cpuLimitMultiplier: 2.0
      memoryLimitMultiplier: 1.5
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("memory-safety-custom-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute) // Allow metrics collection and optimization

			By("Verifying optimization was applied")
			Eventually(func() bool {
				finalResources := getWorkloadResources("memory-safety-overutilized", "default")
				GinkgoWriter.Printf("Final resources: %+v\n", finalResources)

				// Check if memory request was increased (overutilized workload)
				initialMemory := parseMemoryValue(initialResources["memory_request"])
				finalMemory := parseMemoryValue(finalResources["memory_request"])

				return finalMemory > initialMemory
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying custom limit multipliers were applied")
			finalResources := getWorkloadResources("memory-safety-overutilized", "default")
			memoryRequest := parseMemoryValue(finalResources["memory_request"])
			memoryLimit := parseMemoryValue(finalResources["memory_limit"])

			// Custom memory multiplier is 1.5
			expectedLimit := float64(memoryRequest) * 1.5
			tolerance := expectedLimit * 0.1 // 10% tolerance

			Expect(float64(memoryLimit)).To(BeNumerically("~", expectedLimit, tolerance))
		})

		It("should respect updateRequestsOnly flag", func() {
			By("Deploying moderate workload")
			err := workloadHelper.CreateWorkloadFromFile("test-memory-safety-workloads.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("memory-safety-moderate", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial resource configuration")
			initialResources := getWorkloadResources("memory-safety-moderate", "default")
			Expect(initialResources).NotTo(BeEmpty())
			GinkgoWriter.Printf("Initial resources: %+v\n", initialResources)

			By("Creating requests-only policy")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-requests-only-test
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
    workloadSelector:
      matchLabels:
        app: memory-safety-moderate
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    useServerSideApply: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("memory-safety-requests-only-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute) // Allow metrics collection and optimization

			By("Verifying only requests were updated, limits unchanged")
			Eventually(func() bool {
				finalResources := getWorkloadResources("memory-safety-moderate", "default")
				GinkgoWriter.Printf("Final resources: %+v\n", finalResources)

				// Limits should remain unchanged
				initialMemoryLimit := parseMemoryValue(initialResources["memory_limit"])
				finalMemoryLimit := parseMemoryValue(finalResources["memory_limit"])

				initialCPULimit := parseCPUValue(initialResources["cpu_limit"])
				finalCPULimit := parseCPUValue(finalResources["cpu_limit"])

				return initialMemoryLimit == finalMemoryLimit && initialCPULimit == finalCPULimit
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())
		})
	})

	Context("End-to-End Optimization Flow", func() {
		It("should complete full optimization cycle without safety check blocking", func() {
			By("Deploying all test workloads")
			err := workloadHelper.CreateWorkloadFromFile("test-memory-safety-workloads.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all workloads to be ready")
			workloads := []string{"memory-safety-underutilized", "memory-safety-overutilized", "memory-safety-moderate"}
			for _, workload := range workloads {
				err = workloadHelper.WaitForWorkloadReady(workload, "default", 2*time.Minute)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Applying comprehensive optimization policy")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-comprehensive
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
    workloadSelector:
      matchLabels:
        test-type: memory-safety
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
    limitConfig:
      cpuLimitMultiplier: 1.5
      memoryLimitMultiplier: 1.3
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("memory-safety-comprehensive", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization cycles to complete")
			time.Sleep(3 * time.Minute) // Allow multiple optimization cycles

			By("Verifying no safety check blocking occurred")
			found, err := validationHelper.CheckOptipodLogs("isUnsafeMemoryDecrease", 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse(), "Safety check function should not be called")

			By("Verifying optimization decisions were logged")
			found, err = validationHelper.CheckOptipodLogs("optimization applied", 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue(), "Optimization decisions should be logged")

			By("Verifying all workloads have OptipPod annotations")
			for _, workload := range workloads {
				Eventually(func() bool {
					annotations, err := workloadHelper.GetWorkloadAnnotations(workload, "default")
					if err != nil {
						return false
					}
					return annotations["optipod.io/managed"] == "true"
				}, 2*time.Minute, 15*time.Second).Should(BeTrue())
			}
		})
	})

	Context("Backward Compatibility", func() {
		It("should work with existing policy configurations", func() {
			By("Applying legacy policy configuration")
			err := policyHelper.CreatePolicyFromFile("test-policy.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload")
			err = workloadHelper.CreateWorkloadFromFile("test-deployments.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("underutilized", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("test-policy", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy processes workloads without errors")
			time.Sleep(2 * time.Minute) // Allow optimization cycles

			By("Checking for any error logs")
			found, err := validationHelper.CheckOptipodLogs("error", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			if found {
				// Get actual logs to see what errors occurred
				cmd := exec.Command("kubectl", "logs", "-n", "optipod-system",
					"deployment/optipod-controller-manager", "--since=3m")
				output, _ := utils.Run(cmd)
				GinkgoWriter.Printf("Controller logs:\n%s\n", output)
			}
			Expect(found).To(BeFalse(), "No errors should occur with legacy configurations")
		})

		It("should maintain existing behavior when no limitConfig is specified", func() {
			By("Creating policy without limitConfig")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-no-limit-config
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload")
			err = workloadHelper.CreateWorkloadFromFile("test-deployments.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("underutilized", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("memory-safety-no-limit-config", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute)

			By("Verifying default multipliers were applied")
			Eventually(func() bool {
				resources := getWorkloadResources("underutilized", "default")
				if len(resources) == 0 {
					return false
				}

				memoryRequest := parseMemoryValue(resources["memory_request"])
				memoryLimit := parseMemoryValue(resources["memory_limit"])

				if memoryRequest == 0 || memoryLimit == 0 {
					return false
				}

				// Default memory multiplier should be 1.3
				actualRatio := float64(memoryLimit) / float64(memoryRequest)
				GinkgoWriter.Printf("Memory request: %d, limit: %d, ratio: %.2f, expected: 1.3\n",
					memoryRequest, memoryLimit, actualRatio)

				return actualRatio >= 1.2 && actualRatio <= 1.4 // Allow some tolerance
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())
		})
	})
})

// Helper functions

// getWorkloadResources retrieves resource configuration for a workload
// nolint:unparam // namespace parameter kept for test flexibility
func getWorkloadResources(workloadName, namespace string) map[string]string {
	resources := make(map[string]string)

	// Get CPU request
	cmd := exec.Command("kubectl", "get", "deployment", workloadName, "-n", namespace,
		"-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
	output, err := utils.Run(cmd)
	if err == nil {
		resources["cpu_request"] = strings.TrimSpace(output)
	}

	// Get CPU limit
	cmd = exec.Command("kubectl", "get", "deployment", workloadName, "-n", namespace,
		"-o", "jsonpath={.spec.template.spec.containers[0].resources.limits.cpu}")
	output, err = utils.Run(cmd)
	if err == nil {
		resources["cpu_limit"] = strings.TrimSpace(output)
	}

	// Get Memory request
	cmd = exec.Command("kubectl", "get", "deployment", workloadName, "-n", namespace,
		"-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
	output, err = utils.Run(cmd)
	if err == nil {
		resources["memory_request"] = strings.TrimSpace(output)
	}

	// Get Memory limit
	cmd = exec.Command("kubectl", "get", "deployment", workloadName, "-n", namespace,
		"-o", "jsonpath={.spec.template.spec.containers[0].resources.limits.memory}")
	output, err = utils.Run(cmd)
	if err == nil {
		resources["memory_limit"] = strings.TrimSpace(output)
	}

	return resources
}

func parseMemoryValue(memStr string) int64 {
	if memStr == "" {
		return 0
	}

	// Handle different memory units
	memStr = strings.TrimSpace(memStr)
	if strings.HasSuffix(memStr, "Gi") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "Gi"), 64)
		return int64(val * 1024 * 1024 * 1024)
	} else if strings.HasSuffix(memStr, "Mi") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "Mi"), 64)
		return int64(val * 1024 * 1024)
	} else if strings.HasSuffix(memStr, "Ki") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "Ki"), 64)
		return int64(val * 1024)
	} else if strings.HasSuffix(memStr, "G") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "G"), 64)
		return int64(val * 1000 * 1000 * 1000)
	} else if strings.HasSuffix(memStr, "M") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "M"), 64)
		return int64(val * 1000 * 1000)
	} else if strings.HasSuffix(memStr, "K") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(memStr, "K"), 64)
		return int64(val * 1000)
	}

	// Assume bytes if no unit
	val, _ := strconv.ParseInt(memStr, 10, 64)
	return val
}

func parseCPUValue(cpuStr string) int64 {
	if cpuStr == "" {
		return 0
	}

	cpuStr = strings.TrimSpace(cpuStr)
	if strings.HasSuffix(cpuStr, "m") {
		val, _ := strconv.ParseInt(strings.TrimSuffix(cpuStr, "m"), 10, 64)
		return val
	}

	// Assume cores, convert to millicores
	val, _ := strconv.ParseFloat(cpuStr, 64)
	return int64(val * 1000)
}
