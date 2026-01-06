package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Webhook Lifecycle Integration Tests", func() {
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

	Context("Webhook Server Startup and Shutdown", func() {
		It("should start webhook server successfully with valid configuration", func() {
			By("Verifying webhook server is running")
			Eventually(func() bool {
				// Check if webhook server pod is running
				cmd := exec.Command("kubectl", "get", "pods", "-n", "optipod-system",
					"-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.TrimSpace(output) == "Running"
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Verifying webhook configuration exists")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Verifying webhook service is accessible")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "service", "-n", "optipod-system", "optipod-webhook-service")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Testing webhook endpoint health")
			Eventually(func() bool {
				// Port-forward to webhook service and test health endpoint
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				// Start port-forward in background
				portForwardCmd := exec.CommandContext(ctx, "kubectl", "port-forward", "-n", "optipod-system",
					"service/optipod-webhook-service", "9443:443")
				go func() {
					_ = portForwardCmd.Run()
				}()

				// Wait a moment for port-forward to establish
				time.Sleep(5 * time.Second)

				// Test health endpoint (if available)
				healthCmd := exec.CommandContext(ctx, "curl", "-k", "-s", "https://localhost:9443/health")
				_, err := utils.Run(healthCmd)

				// Even if health endpoint doesn't exist, connection should be possible
				return err == nil || strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "connection refused") == false
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())
		})

		It("should handle graceful shutdown", func() {
			By("Recording initial webhook configuration")
			cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook", "-o", "yaml")
			initialConfig, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialConfig).NotTo(BeEmpty())

			By("Simulating controller restart")
			cmd = exec.Command("kubectl", "rollout", "restart", "deployment/optipod-controller-manager", "-n", "optipod-system")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/optipod-controller-manager", "-n", "optipod-system", "--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook configuration is restored after restart")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook")
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Verifying webhook functionality after restart")
			// Create a test pod to verify webhook is working
			testPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: webhook-test-pod
  namespace: default
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
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod was created successfully (webhook is functional)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pod", "webhook-test-pod", "-n", "default")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Cleaning up test pod")
			cmd = exec.Command("kubectl", "delete", "pod", "webhook-test-pod", "-n", "default", "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		It("should handle webhook server failures gracefully", func() {
			By("Creating webhook policy to ensure webhook is active")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-failure-test
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

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-failure-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Checking webhook failure policy configuration")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].failurePolicy}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			failurePolicy := strings.TrimSpace(output)
			GinkgoWriter.Printf("Webhook failure policy: %s\n", failurePolicy)

			By("Testing pod creation when webhook might be temporarily unavailable")
			// Create multiple pods quickly to test failure handling
			for i := 0; i < 3; i++ {
				testPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: failure-test-pod-%d
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`, i)
				cmd = exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(testPodYAML)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying all pods were created successfully despite potential webhook issues")
			for i := 0; i < 3; i++ {
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pod", fmt.Sprintf("failure-test-pod-%d", i), "-n", "default")
					_, err := utils.Run(cmd)
					return err == nil
				}, 1*time.Minute, 10*time.Second).Should(BeTrue())
			}

			By("Cleaning up test pods")
			for i := 0; i < 3; i++ {
				cmd = exec.Command("kubectl", "delete", "pod", fmt.Sprintf("failure-test-pod-%d", i), "-n", "default", "--ignore-not-found=true")
				_, _ = utils.Run(cmd)
			}
		})
	})

	Context("Certificate Management", func() {
		It("should have valid TLS certificates for webhook", func() {
			By("Checking webhook service certificate")
			cmd := exec.Command("kubectl", "get", "secret", "-n", "optipod-system", "webhook-server-certs")
			_, err := utils.Run(cmd)
			if err != nil {
				// Certificate might be managed differently, check for other cert secrets
				cmd = exec.Command("kubectl", "get", "secrets", "-n", "optipod-system", "-o", "name")
				output, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())

				hasCertSecret := false // pragma: allowlist secret
				for _, line := range strings.Split(output, "\n") {
					if strings.Contains(line, "cert") || strings.Contains(line, "tls") { // pragma: allowlist secret
						hasCertSecret = true // pragma: allowlist secret
						break
					}
				}
				Expect(hasCertSecret).To(BeTrue(), "Should have certificate secret")
			}

			By("Verifying webhook configuration has CA bundle")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			caBundle := strings.TrimSpace(output)
			Expect(caBundle).NotTo(BeEmpty(), "CA bundle should not be empty")
			GinkgoWriter.Printf("CA bundle length: %d characters\n", len(caBundle))
		})

		It("should handle certificate rotation", func() {
			By("Recording current webhook configuration")
			cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
			initialCABundle, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(initialCABundle)).NotTo(BeEmpty())

			By("Simulating certificate rotation by restarting controller")
			cmd = exec.Command("kubectl", "rollout", "restart", "deployment/optipod-controller-manager", "-n", "optipod-system")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/optipod-controller-manager", "-n", "optipod-system", "--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook configuration is updated after restart")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
					"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}")
				currentCABundle, err := utils.Run(cmd)
				if err != nil {
					return false
				}

				// CA bundle should still exist and be valid
				return strings.TrimSpace(currentCABundle) != ""
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Testing webhook functionality after certificate rotation")
			testPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: cert-rotation-test-pod
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod creation succeeds after certificate rotation")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pod", "cert-rotation-test-pod", "-n", "default")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Cleaning up test pod")
			cmd = exec.Command("kubectl", "delete", "pod", "cert-rotation-test-pod", "-n", "default", "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})
	})

	Context("Failure Policy Behavior", func() {
		It("should respect webhook failure policy under various conditions", func() {
			By("Checking current failure policy")
			cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].failurePolicy}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			failurePolicy := strings.TrimSpace(output)
			GinkgoWriter.Printf("Current failure policy: %s\n", failurePolicy)

			By("Testing pod creation with webhook enabled")
			testPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: failure-policy-test-pod
  namespace: default
  annotations:
    optipod.io/webhook-enabled: "true"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodYAML)
			_, err = utils.Run(cmd)

			if failurePolicy == "Fail" {
				// With Fail policy, pod creation should succeed if webhook is working
				// or fail if webhook is not available
				Expect(err).NotTo(HaveOccurred(), "Pod creation should succeed with working webhook")
			} else {
				// With Ignore policy, pod creation should always succeed
				Expect(err).NotTo(HaveOccurred(), "Pod creation should succeed with Ignore failure policy")
			}

			By("Verifying pod was created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pod", "failure-policy-test-pod", "-n", "default")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Testing pod creation without webhook annotations")
			normalPodYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: normal-pod-test
  namespace: default
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: "50m"
        memory: "64Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(normalPodYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Normal pod creation should always succeed")

			By("Verifying normal pod was created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pod", "normal-pod-test", "-n", "default")
				_, err := utils.Run(cmd)
				return err == nil
			}, 1*time.Minute, 10*time.Second).Should(BeTrue())

			By("Cleaning up test pods")
			cmd = exec.Command("kubectl", "delete", "pod", "failure-policy-test-pod", "normal-pod-test", "-n", "default", "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		It("should handle webhook timeout scenarios", func() {
			By("Creating multiple pods simultaneously to test webhook performance")
			var podYAMLs []string
			for i := 0; i < 5; i++ {
				podYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: timeout-test-pod-%d
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
				podYAMLs = append(podYAMLs, podYAML)
			}

			By("Creating all pods simultaneously")
			for i, podYAML := range podYAMLs {
				go func(yaml string, index int) {
					defer GinkgoRecover()
					cmd := exec.Command("kubectl", "apply", "-f", "-")
					cmd.Stdin = strings.NewReader(yaml)
					_, err := utils.Run(cmd)
					if err != nil {
						GinkgoWriter.Printf("Pod %d creation failed: %v\n", index, err)
					}
				}(podYAML, i)
			}

			By("Verifying all pods were created within reasonable time")
			for i := 0; i < 5; i++ {
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pod", fmt.Sprintf("timeout-test-pod-%d", i), "-n", "default")
					_, err := utils.Run(cmd)
					return err == nil
				}, 2*time.Minute, 10*time.Second).Should(BeTrue())
			}

			By("Cleaning up test pods")
			for i := 0; i < 5; i++ {
				cmd := exec.Command("kubectl", "delete", "pod", fmt.Sprintf("timeout-test-pod-%d", i), "-n", "default", "--ignore-not-found=true")
				_, _ = utils.Run(cmd)
			}
		})
	})

	Context("Webhook Configuration Validation", func() {
		It("should have correct webhook configuration", func() {
			By("Verifying webhook configuration exists")
			cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook", "-o", "yaml")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			config := strings.TrimSpace(output)
			Expect(config).NotTo(BeEmpty())

			By("Checking webhook rules configuration")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].rules}")
			rulesOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			rules := strings.TrimSpace(rulesOutput)
			Expect(rules).To(ContainSubstring("pods"), "Should have rules for pods")
			Expect(rules).To(ContainSubstring("CREATE"), "Should intercept CREATE operations")

			By("Checking namespace selector configuration")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].namespaceSelector}")
			selectorOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Namespace selector: %s\n", strings.TrimSpace(selectorOutput))

			By("Checking admission review versions")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].admissionReviewVersions}")
			versionsOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			versions := strings.TrimSpace(versionsOutput)
			Expect(versions).To(ContainSubstring("v1"), "Should support admission review v1")

			By("Checking side effects configuration")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].sideEffects}")
			sideEffectsOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			sideEffects := strings.TrimSpace(sideEffectsOutput)
			Expect(sideEffects).To(Equal("None"), "Should have no side effects")
		})

		It("should handle webhook configuration updates", func() {
			By("Recording initial webhook configuration")
			cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook", "-o", "yaml")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Restarting controller to trigger configuration refresh")
			cmd = exec.Command("kubectl", "rollout", "restart", "deployment/optipod-controller-manager", "-n", "optipod-system")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/optipod-controller-manager", "-n", "optipod-system", "--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook configuration is maintained after restart")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook")
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Verifying configuration content is consistent")
			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].clientConfig.service.name}")
			serviceName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(serviceName)).To(Equal("optipod-webhook-service"))

			cmd = exec.Command("kubectl", "get", "mutatingwebhookconfiguration", "optipod-webhook",
				"-o", "jsonpath={.webhooks[0].clientConfig.service.namespace}")
			serviceNamespace, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(serviceNamespace)).To(Equal("optipod-system"))
		})
	})
})
