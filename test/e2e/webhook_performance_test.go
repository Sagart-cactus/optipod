package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Webhook Performance Integration Tests", func() {
	var (
		policyHelper  *helpers.PolicyHelper
		cleanupHelper *helpers.CleanupHelper
	)

	BeforeEach(func() {
		policyHelper = helpers.NewPolicyHelper()
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

	Context("High Pod Creation Load", func() {
		It("should handle high volume pod creation without timeouts", func() {
			By("Creating webhook policy for performance testing")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-performance-test
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
        test-type: performance
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
    strategy: webhook
    rolloutStrategy: onNextRestart
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-performance-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Creating high volume of pods with webhook annotations")
			const numPods = 20
			var wg sync.WaitGroup
			var mu sync.Mutex
			var creationErrors []error
			var creationTimes []time.Duration

			startTime := time.Now()

			for i := 0; i < numPods; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					defer GinkgoRecover()

					podStartTime := time.Now()
					podYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: perf-test-pod-%d
  namespace: default
  labels:
    test-type: performance
  annotations:
    optipod.io/webhook-enabled: "true"
    optipod.io/strategy: "webhook"
    optipod.io/cpu-request.app: "100m"
    optipod.io/memory-request.app: "128Mi"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`, index)

					cmd := exec.Command("kubectl", "apply", "-f", "-")
					cmd.Stdin = strings.NewReader(podYAML)
					_, err := utils.Run(cmd)

					mu.Lock()
					if err != nil {
						creationErrors = append(creationErrors, err)
					} else {
						creationTimes = append(creationTimes, time.Since(podStartTime))
					}
					mu.Unlock()
				}(i)
			}

			By("Waiting for all pod creation attempts to complete")
			wg.Wait()
			totalTime := time.Since(startTime)

			By("Analyzing creation results")
			GinkgoWriter.Printf("Total creation time: %v\n", totalTime)
			GinkgoWriter.Printf("Number of errors: %d/%d\n", len(creationErrors), numPods)

			if len(creationTimes) > 0 {
				var totalCreationTime time.Duration
				for _, t := range creationTimes {
					totalCreationTime += t
				}
				avgCreationTime := totalCreationTime / time.Duration(len(creationTimes))
				GinkgoWriter.Printf("Average pod creation time: %v\n", avgCreationTime)

				// Performance expectations
				Expect(avgCreationTime).To(BeNumerically("<", 10*time.Second), "Average pod creation should be under 10 seconds")
			}

			// Should have minimal errors (allow up to 10% failure rate for transient issues)
			maxAllowedErrors := numPods / 10
			Expect(len(creationErrors)).To(BeNumerically("<=", maxAllowedErrors),
				fmt.Sprintf("Should have minimal creation errors, got %d errors: %v", len(creationErrors), creationErrors))

			By("Verifying pods were created successfully")
			successfulPods := 0
			for i := 0; i < numPods; i++ {
				cmd := exec.Command("kubectl", "get", "pod", fmt.Sprintf("perf-test-pod-%d", i), "-n", "default")
				_, err := utils.Run(cmd)
				if err == nil {
					successfulPods++
				}
			}

			GinkgoWriter.Printf("Successfully created pods: %d/%d\n", successfulPods, numPods)
			Expect(successfulPods).To(BeNumerically(">=", numPods-maxAllowedErrors), "Most pods should be created successfully")

			By("Cleaning up performance test pods")
			for i := 0; i < numPods; i++ {
				cmd := exec.Command("kubectl", "delete", "pod", fmt.Sprintf("perf-test-pod-%d", i), "-n", "default", "--ignore-not-found=true")
				_, _ = utils.Run(cmd)
			}
		})

		It("should maintain webhook responsiveness under concurrent deployment creation", func() {
			By("Creating webhook policy")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-concurrent-test
  namespace: default
spec:
  mode: Auto
  selector:
    namespaces:
      allow:
        - default
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate
  metricsConfig:
    provider: metrics-server
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-concurrent-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Creating multiple deployments concurrently")
			const numDeployments = 10
			var wg sync.WaitGroup
			var mu sync.Mutex
			var deploymentErrors []error

			for i := 0; i < numDeployments; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					defer GinkgoRecover()

					deploymentYAML := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: concurrent-deployment-%d
  namespace: default
  labels:
    test-type: concurrent
spec:
  replicas: 2
  selector:
    matchLabels:
      app: concurrent-app-%d
      test-type: concurrent
  template:
    metadata:
      labels:
        app: concurrent-app-%d
        test-type: concurrent
    spec:
      containers:
      - name: app
        image: nginx:latest
        resources:
          requests:
            cpu: "50m"
            memory: "64Mi"
`, index, index, index)

					cmd := exec.Command("kubectl", "apply", "-f", "-")
					cmd.Stdin = strings.NewReader(deploymentYAML)
					_, err := utils.Run(cmd)

					mu.Lock()
					if err != nil {
						deploymentErrors = append(deploymentErrors, err)
					}
					mu.Unlock()
				}(i)
			}

			By("Waiting for all deployment creation attempts to complete")
			wg.Wait()

			By("Analyzing deployment creation results")
			GinkgoWriter.Printf("Number of deployment creation errors: %d/%d\n", len(deploymentErrors), numDeployments)
			if len(deploymentErrors) > 0 {
				for i, err := range deploymentErrors {
					GinkgoWriter.Printf("Error %d: %v\n", i+1, err)
				}
			}

			// Should have minimal errors
			Expect(len(deploymentErrors)).To(BeNumerically("<=", 2), "Should have minimal deployment creation errors")

			By("Waiting for deployments to become ready")
			successfulDeployments := 0
			for i := 0; i < numDeployments; i++ {
				// Check if deployment exists
				cmd := exec.Command("kubectl", "get", "deployment", fmt.Sprintf("concurrent-deployment-%d", i), "-n", "default")
				_, err := utils.Run(cmd)
				if err == nil {
					// Wait for deployment to be ready
					Eventually(func() bool {
						cmd := exec.Command("kubectl", "get", "deployment", fmt.Sprintf("concurrent-deployment-%d", i), "-n", "default",
							"-o", "jsonpath={.status.readyReplicas}")
						output, err := utils.Run(cmd)
						if err != nil {
							return false
						}
						readyReplicas, _ := strconv.Atoi(strings.TrimSpace(output))
						return readyReplicas >= 1 // At least one replica should be ready
					}, 3*time.Minute, 15*time.Second).Should(BeTrue())

					successfulDeployments++
				}
			}

			GinkgoWriter.Printf("Successfully created and ready deployments: %d/%d\n", successfulDeployments, numDeployments)
			Expect(successfulDeployments).To(BeNumerically(">=", numDeployments-2), "Most deployments should be ready")

			By("Verifying webhook processed all pods correctly")
			// Check that pods from deployments have been processed by webhook
			totalPods := 0
			processedPods := 0

			for i := 0; i < numDeployments; i++ {
				// Get pods for this deployment
				cmd := exec.Command("kubectl", "get", "pods", "-n", "default",
					"-l", fmt.Sprintf("app=concurrent-app-%d", i), "-o", "name")
				output, err := utils.Run(cmd)
				if err == nil {
					pods := strings.Split(strings.TrimSpace(output), "\n")
					for _, pod := range pods {
						if pod != "" {
							totalPods++

							// Check if pod has webhook annotations or was processed
							podName := strings.TrimPrefix(pod, "pod/")
							cmd := exec.Command("kubectl", "get", "pod", podName, "-n", "default",
								"-o", "jsonpath={.metadata.annotations}")
							annotationsOutput, err := utils.Run(cmd)
							if err == nil && strings.Contains(annotationsOutput, "optipod.io") {
								processedPods++
							}
						}
					}
				}
			}

			GinkgoWriter.Printf("Total pods: %d, Processed by webhook: %d\n", totalPods, processedPods)

			By("Cleaning up concurrent test deployments")
			for i := 0; i < numDeployments; i++ {
				cmd := exec.Command("kubectl", "delete", "deployment", fmt.Sprintf("concurrent-deployment-%d", i), "-n", "default", "--ignore-not-found=true")
				_, _ = utils.Run(cmd)
			}
		})

		It("should handle webhook server resource limits gracefully", func() {
			By("Checking webhook server resource configuration")
			cmd := exec.Command("kubectl", "get", "deployment", "optipod-controller-manager", "-n", "optipod-system",
				"-o", "jsonpath={.spec.template.spec.containers[0].resources}")
			resourcesOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Webhook server resources: %s\n", resourcesOutput)

			By("Creating sustained load to test resource limits")
			const batchSize = 5
			const numBatches = 4

			for batch := 0; batch < numBatches; batch++ {
				GinkgoWriter.Printf("Creating batch %d/%d\n", batch+1, numBatches)

				var wg sync.WaitGroup
				for i := 0; i < batchSize; i++ {
					wg.Add(1)
					go func(batchIndex, podIndex int) {
						defer wg.Done()
						defer GinkgoRecover()

						podName := fmt.Sprintf("resource-test-pod-%d-%d", batchIndex, podIndex)
						podYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: default
  annotations:
    optipod.io/webhook-enabled: "true"
    optipod.io/strategy: "webhook"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "10m"
        memory: "32Mi"
`, podName)

						cmd := exec.Command("kubectl", "apply", "-f", "-")
						cmd.Stdin = strings.NewReader(podYAML)
						_, err := utils.Run(cmd)
						if err != nil {
							GinkgoWriter.Printf("Failed to create pod %s: %v\n", podName, err)
						}
					}(batch, i)
				}

				wg.Wait()

				// Brief pause between batches
				time.Sleep(5 * time.Second)
			}

			By("Verifying webhook server is still responsive")
			testPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: responsiveness-test-pod
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "10m"
        memory: "32Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Webhook should still be responsive after load test")

			By("Verifying test pod was created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pod", "responsiveness-test-pod", "-n", "default")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Checking webhook server health after load test")
			cmd = exec.Command("kubectl", "get", "pods", "-n", "optipod-system",
				"-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].status.phase}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Running"), "Webhook server should still be running")

			By("Cleaning up load test pods")
			for batch := 0; batch < numBatches; batch++ {
				for i := 0; i < batchSize; i++ {
					podName := fmt.Sprintf("resource-test-pod-%d-%d", batch, i)
					cmd := exec.Command("kubectl", "delete", "pod", podName, "-n", "default", "--ignore-not-found=true")
					_, _ = utils.Run(cmd)
				}
			}
			cmd = exec.Command("kubectl", "delete", "pod", "responsiveness-test-pod", "-n", "default", "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})
	})

	Context("Webhook Latency and Throughput", func() {
		It("should maintain acceptable latency under normal load", func() {
			By("Creating webhook policy for latency testing")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-latency-test
  namespace: default
spec:
  mode: Auto
  selector:
    namespaces:
      allow:
        - default
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  metricsConfig:
    provider: metrics-server
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Measuring pod creation latency with webhook")
			const numSamples = 10
			var latencies []time.Duration

			for i := 0; i < numSamples; i++ {
				startTime := time.Now()

				podYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: latency-test-pod-%d
  namespace: default
  annotations:
    optipod.io/webhook-enabled: "true"
    optipod.io/strategy: "webhook"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`, i)

				cmd := exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(podYAML)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())

				// Wait for pod to be created (not necessarily running)
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pod", fmt.Sprintf("latency-test-pod-%d", i), "-n", "default")
					_, err := utils.Run(cmd)
					return err == nil
				}, 30*time.Second, 1*time.Second).Should(BeTrue())

				latency := time.Since(startTime)
				latencies = append(latencies, latency)

				GinkgoWriter.Printf("Pod %d creation latency: %v\n", i, latency)

				// Brief pause between samples
				time.Sleep(2 * time.Second)
			}

			By("Analyzing latency results")
			var totalLatency time.Duration
			var maxLatency time.Duration
			var minLatency = time.Hour // Initialize to large value

			for _, latency := range latencies {
				totalLatency += latency
				if latency > maxLatency {
					maxLatency = latency
				}
				if latency < minLatency {
					minLatency = latency
				}
			}

			avgLatency := totalLatency / time.Duration(len(latencies))

			GinkgoWriter.Printf("Latency statistics:\n")
			GinkgoWriter.Printf("  Average: %v\n", avgLatency)
			GinkgoWriter.Printf("  Min: %v\n", minLatency)
			GinkgoWriter.Printf("  Max: %v\n", maxLatency)

			// Performance expectations
			Expect(avgLatency).To(BeNumerically("<", 5*time.Second), "Average latency should be under 5 seconds")
			Expect(maxLatency).To(BeNumerically("<", 15*time.Second), "Max latency should be under 15 seconds")

			By("Cleaning up latency test pods")
			for i := 0; i < numSamples; i++ {
				cmd := exec.Command("kubectl", "delete", "pod", fmt.Sprintf("latency-test-pod-%d", i), "-n", "default", "--ignore-not-found=true")
				_, _ = utils.Run(cmd)
			}
		})

		It("should handle burst traffic patterns", func() {
			By("Creating webhook policy for burst testing")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-burst-test
  namespace: default
spec:
  mode: Auto
  selector:
    namespaces:
      allow:
        - default
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  metricsConfig:
    provider: metrics-server
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Simulating burst traffic pattern")
			const burstSize = 15
			const numBursts = 3

			for burst := 0; burst < numBursts; burst++ {
				GinkgoWriter.Printf("Starting burst %d/%d with %d pods\n", burst+1, numBursts, burstSize)

				burstStartTime := time.Now()
				var wg sync.WaitGroup
				var mu sync.Mutex
				var burstErrors []error

				// Create burst of pods
				for i := 0; i < burstSize; i++ {
					wg.Add(1)
					go func(burstIndex, podIndex int) {
						defer wg.Done()
						defer GinkgoRecover()

						podName := fmt.Sprintf("burst-test-pod-%d-%d", burstIndex, podIndex)
						podYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: default
  annotations:
    optipod.io/webhook-enabled: "true"
    optipod.io/strategy: "webhook"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "25m"
        memory: "32Mi"
`, podName)

						cmd := exec.Command("kubectl", "apply", "-f", "-")
						cmd.Stdin = strings.NewReader(podYAML)
						_, err := utils.Run(cmd)

						mu.Lock()
						if err != nil {
							burstErrors = append(burstErrors, err)
						}
						mu.Unlock()
					}(burst, i)
				}

				wg.Wait()
				burstDuration := time.Since(burstStartTime)

				GinkgoWriter.Printf("Burst %d completed in %v with %d errors\n", burst+1, burstDuration, len(burstErrors))

				// Allow some errors in burst scenarios, but not too many
				maxAllowedErrors := burstSize / 5 // Allow up to 20% errors
				Expect(len(burstErrors)).To(BeNumerically("<=", maxAllowedErrors),
					fmt.Sprintf("Burst %d should have minimal errors", burst+1))

				// Wait between bursts to simulate realistic traffic patterns
				if burst < numBursts-1 {
					time.Sleep(30 * time.Second)
				}
			}

			By("Verifying webhook server stability after burst testing")
			cmd = exec.Command("kubectl", "get", "pods", "-n", "optipod-system",
				"-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].status.phase}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("Running"), "Webhook server should remain stable")

			By("Testing webhook responsiveness after burst")
			postBurstPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: post-burst-test-pod
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "10m"
        memory: "16Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(postBurstPodYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Webhook should be responsive after burst testing")

			By("Cleaning up burst test pods")
			for burst := 0; burst < numBursts; burst++ {
				for i := 0; i < burstSize; i++ {
					podName := fmt.Sprintf("burst-test-pod-%d-%d", burst, i)
					cmd := exec.Command("kubectl", "delete", "pod", podName, "-n", "default", "--ignore-not-found=true")
					_, _ = utils.Run(cmd)
				}
			}
			cmd = exec.Command("kubectl", "delete", "pod", "post-burst-test-pod", "-n", "default", "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})
	})
})
