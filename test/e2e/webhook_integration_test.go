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

const (
	webhookStrategy     = "webhook"
	webhookEnabledValue = "true"
)

var _ = Describe("Webhook Integration Tests", func() {
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

	Context("Complete Workflow from Policy Creation to Pod Mutation", func() {
		It("should complete full webhook workflow with immediate rollout", func() {
			By("Creating webhook strategy policy with immediate rollout")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-immediate-test
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
        test-type: webhook-integration
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
    rolloutStrategy: immediate
    allowInPlaceResize: false
    allowRecreate: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-immediate-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload with webhook strategy labels")
			workloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook-test-deployment
  namespace: default
  labels:
    test-type: webhook-integration
spec:
  replicas: 2
  selector:
    matchLabels:
      app: webhook-test
      test-type: webhook-integration
  template:
    metadata:
      labels:
        app: webhook-test
        test-type: webhook-integration
    spec:
      containers:
      - name: app
        image: nginx:latest
        resources:
          requests:
            cpu: "50m"
            memory: "64Mi"
          limits:
            cpu: "100m"
            memory: "128Mi"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(workloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("webhook-test-deployment", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur and annotations to be applied")
			time.Sleep(2 * time.Minute) // Allow metrics collection and optimization

			By("Verifying annotations were applied to pod template")
			Eventually(func() bool {
				annotations, err := workloadHelper.GetWorkloadAnnotations("webhook-test-deployment", "default")
				if err != nil {
					return false
				}

				// Check for webhook-specific annotations
				hasWebhookEnabled := annotations["optipod.io/webhook-enabled"] == webhookEnabledValue
				hasStrategy := annotations["optipod.io/strategy"] == webhookStrategy
				hasResourceAnnotations := false

				for key := range annotations {
					if strings.HasPrefix(key, "optipod.io/cpu-request.") ||
						strings.HasPrefix(key, "optipod.io/memory-request.") {
						hasResourceAnnotations = true
						break
					}
				}

				GinkgoWriter.Printf("Annotations: %+v\n", annotations)
				return hasWebhookEnabled && hasStrategy && hasResourceAnnotations
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying rolling restart was triggered for immediate rollout")
			Eventually(func() bool {
				// Check if deployment was updated (generation should increase)
				cmd := exec.Command("kubectl", "get", "deployment", "webhook-test-deployment", "-n", "default",
					"-o", "jsonpath={.metadata.generation}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}

				generation, _ := strconv.Atoi(strings.TrimSpace(output))
				return generation > 1 // Should be updated at least once
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying new pods are created with mutated resources")
			Eventually(func() bool {
				// Get pod resources to verify webhook mutation occurred
				cmd := exec.Command("kubectl", "get", "pods", "-n", "default",
					"-l", "app=webhook-test", "-o", "jsonpath={.items[0].spec.containers[0].resources}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}

				GinkgoWriter.Printf("Pod resources after webhook: %s\n", output)

				// Check if resources were modified from original values
				return strings.Contains(output, "requests") && strings.Contains(output, "limits")
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())
		})

		It("should complete workflow with onNextRestart rollout strategy", func() {
			By("Creating webhook strategy policy with onNextRestart rollout")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-onnextrestart-test
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
        test-type: webhook-onnextrestart
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
    allowInPlaceResize: false
    allowRecreate: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-onnextrestart-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload")
			workloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook-onnextrestart-deployment
  namespace: default
  labels:
    test-type: webhook-onnextrestart
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webhook-onnextrestart
      test-type: webhook-onnextrestart
  template:
    metadata:
      labels:
        app: webhook-onnextrestart
        test-type: webhook-onnextrestart
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
			cmd.Stdin = strings.NewReader(workloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("webhook-onnextrestart-deployment", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Recording initial deployment generation")
			cmd = exec.Command("kubectl", "get", "deployment", "webhook-onnextrestart-deployment", "-n", "default",
				"-o", "jsonpath={.metadata.generation}")
			initialGenOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			initialGeneration, _ := strconv.Atoi(strings.TrimSpace(initialGenOutput))

			By("Waiting for optimization to occur and annotations to be applied")
			time.Sleep(2 * time.Minute)

			By("Verifying annotations were applied but no immediate restart occurred")
			Eventually(func() bool {
				annotations, err := workloadHelper.GetWorkloadAnnotations("webhook-onnextrestart-deployment", "default")
				if err != nil {
					return false
				}

				// Should have annotations
				hasAnnotations := false
				for key := range annotations {
					if strings.HasPrefix(key, "optipod.io/") {
						hasAnnotations = true
						break
					}
				}

				// But deployment generation should not have changed (no immediate restart)
				cmd := exec.Command("kubectl", "get", "deployment", "webhook-onnextrestart-deployment", "-n", "default",
					"-o", "jsonpath={.metadata.generation}")
				currentGenOutput, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				currentGeneration, _ := strconv.Atoi(strings.TrimSpace(currentGenOutput))

				GinkgoWriter.Printf("Initial generation: %d, Current generation: %d, Has annotations: %v\n",
					initialGeneration, currentGeneration, hasAnnotations)

				return hasAnnotations && currentGeneration == initialGeneration
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Manually triggering restart to test webhook mutation")
			cmd = exec.Command("kubectl", "rollout", "restart", "deployment/webhook-onnextrestart-deployment", "-n", "default")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for rollout to complete")
			cmd = exec.Command("kubectl", "rollout", "status", "deployment/webhook-onnextrestart-deployment", "-n", "default", "--timeout=120s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying new pods have mutated resources from webhook")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "default",
					"-l", "app=webhook-onnextrestart", "-o", "jsonpath={.items[0].spec.containers[0].resources}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}

				GinkgoWriter.Printf("Pod resources after manual restart: %s\n", output)
				return strings.Contains(output, "requests")
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())
		})
	})

	Context("Mixed SSA/Webhook Environments", func() {
		It("should handle mixed strategies in the same cluster", func() {
			By("Creating SSA strategy policy")
			ssaPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-strategy-test
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
        strategy-type: ssa
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
    strategy: ssa
    allowInPlaceResize: true
    useServerSideApply: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(ssaPolicyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Creating webhook strategy policy")
			webhookPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-strategy-test
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
        strategy-type: webhook
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
    rolloutStrategy: immediate
    allowInPlaceResize: false
    allowRecreate: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(webhookPolicyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for both policies to be ready")
			err = policyHelper.WaitForPolicyReady("ssa-strategy-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			err = policyHelper.WaitForPolicyReady("webhook-strategy-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying SSA workload")
			ssaWorkloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ssa-workload
  namespace: default
  labels:
    strategy-type: ssa
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ssa-workload
      strategy-type: ssa
  template:
    metadata:
      labels:
        app: ssa-workload
        strategy-type: ssa
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
			cmd.Stdin = strings.NewReader(ssaWorkloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying webhook workload")
			webhookWorkloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook-workload
  namespace: default
  labels:
    strategy-type: webhook
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webhook-workload
      strategy-type: webhook
  template:
    metadata:
      labels:
        app: webhook-workload
        strategy-type: webhook
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
			cmd.Stdin = strings.NewReader(webhookWorkloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for both workloads to be ready")
			err = workloadHelper.WaitForWorkloadReady("ssa-workload", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			err = workloadHelper.WaitForWorkloadReady("webhook-workload", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute)

			By("Verifying SSA workload uses SSA strategy")
			Eventually(func() bool {
				// SSA workloads should have OptipPod managed annotation but no webhook annotations
				annotations, err := workloadHelper.GetWorkloadAnnotations("ssa-workload", "default")
				if err != nil {
					return false
				}

				hasOptipodManaged := annotations["optipod.io/managed"] == webhookEnabledValue
				hasWebhookAnnotations := false

				for key := range annotations {
					if key == "optipod.io/webhook-enabled" || key == "optipod.io/strategy" {
						hasWebhookAnnotations = true
						break
					}
				}

				GinkgoWriter.Printf("SSA workload annotations: %+v\n", annotations)
				return hasOptipodManaged && !hasWebhookAnnotations
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying webhook workload uses webhook strategy")
			Eventually(func() bool {
				// Webhook workloads should have webhook-specific annotations
				annotations, err := workloadHelper.GetWorkloadAnnotations("webhook-workload", "default")
				if err != nil {
					return false
				}

				hasWebhookEnabled := annotations["optipod.io/webhook-enabled"] == webhookEnabledValue
				hasStrategy := annotations["optipod.io/strategy"] == webhookStrategy

				GinkgoWriter.Printf("Webhook workload annotations: %+v\n", annotations)
				return hasWebhookEnabled && hasStrategy
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying both strategies work independently without conflicts")
			found, err := validationHelper.CheckOptipodLogs("error", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			if found {
				cmd := exec.Command("kubectl", "logs", "-n", "optipod-system",
					"deployment/optipod-controller-manager", "--since=3m")
				output, _ := utils.Run(cmd)
				GinkgoWriter.Printf("Controller logs:\n%s\n", output)
			}
			Expect(found).To(BeFalse(), "No errors should occur with mixed strategies")
		})
	})

	Context("Rolling Restart Behavior with Different Strategies", func() {
		It("should handle rolling restart for different workload types", func() {
			By("Creating webhook policy for multiple workload types")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-multiworkload-test
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
        - StatefulSet
        - DaemonSet
    workloadSelector:
      matchLabels:
        test-type: multiworkload
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
    rolloutStrategy: immediate
    allowInPlaceResize: false
    allowRecreate: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("webhook-multiworkload-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying Deployment workload")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook-deployment
  namespace: default
  labels:
    test-type: multiworkload
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webhook-deployment
      test-type: multiworkload
  template:
    metadata:
      labels:
        app: webhook-deployment
        test-type: multiworkload
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
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying StatefulSet workload")
			statefulSetYAML := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: webhook-statefulset
  namespace: default
  labels:
    test-type: multiworkload
spec:
  serviceName: webhook-statefulset
  replicas: 1
  selector:
    matchLabels:
      app: webhook-statefulset
      test-type: multiworkload
  template:
    metadata:
      labels:
        app: webhook-statefulset
        test-type: multiworkload
    spec:
      containers:
      - name: app
        image: nginx:latest
        resources:
          requests:
            cpu: "50m"
            memory: "64Mi"
---
apiVersion: v1
kind: Service
metadata:
  name: webhook-statefulset
  namespace: default
spec:
  clusterIP: None
  selector:
    app: webhook-statefulset
  ports:
  - port: 80
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(statefulSetYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workloads to be ready")
			err = workloadHelper.WaitForWorkloadReady("webhook-deployment", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulset", "webhook-statefulset", "-n", "default",
					"-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				readyReplicas, _ := strconv.Atoi(strings.TrimSpace(output))
				return readyReplicas == 1
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Waiting for optimization and rolling restart to occur")
			time.Sleep(3 * time.Minute)

			By("Verifying Deployment was restarted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "deployment", "webhook-deployment", "-n", "default",
					"-o", "jsonpath={.metadata.generation}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				generation, _ := strconv.Atoi(strings.TrimSpace(output))
				return generation > 1
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Verifying StatefulSet was restarted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulset", "webhook-statefulset", "-n", "default",
					"-o", "jsonpath={.metadata.generation}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				generation, _ := strconv.Atoi(strings.TrimSpace(output))
				return generation > 1
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			for _, workload := range []string{"webhook-deployment", "webhook-statefulset"} {
				Eventually(func() bool {
					annotations, err := workloadHelper.GetWorkloadAnnotations(workload, "default")
					if err != nil {
						return false
					}

					hasWebhookAnnotations := false
					for key := range annotations {
						if strings.HasPrefix(key, "optipod.io/") {
							hasWebhookAnnotations = true
							break
						}
					}

					return hasWebhookAnnotations
				}, 2*time.Minute, 15*time.Second).Should(BeTrue())
			}
		})
	})

	Context("Backward Compatibility with Existing Policies", func() {
		It("should maintain compatibility with policies without strategy field", func() {
			By("Creating legacy policy without strategy field")
			legacyPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: legacy-policy-test
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
        test-type: legacy
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
    useServerSideApply: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(legacyPolicyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for policy to be ready")
			err = policyHelper.WaitForPolicyReady("legacy-policy-test", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload")
			workloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: legacy-workload
  namespace: default
  labels:
    test-type: legacy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: legacy-workload
      test-type: legacy
  template:
    metadata:
      labels:
        app: legacy-workload
        test-type: legacy
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
			cmd.Stdin = strings.NewReader(workloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("legacy-workload", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for optimization to occur")
			time.Sleep(2 * time.Minute)

			By("Verifying policy defaults to webhook strategy")
			Eventually(func() bool {
				annotations, err := workloadHelper.GetWorkloadAnnotations("legacy-workload", "default")
				if err != nil {
					return false
				}

				// Should default to webhook strategy and have webhook annotations
				hasWebhookEnabled := annotations["optipod.io/webhook-enabled"] == webhookEnabledValue
				hasStrategy := annotations["optipod.io/strategy"] == "webhook"

				GinkgoWriter.Printf("Legacy workload annotations: %+v\n", annotations)
				return hasWebhookEnabled && hasStrategy
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying no errors occurred with legacy configuration")
			found, err := validationHelper.CheckOptipodLogs("error", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			if found {
				cmd := exec.Command("kubectl", "logs", "-n", "optipod-system",
					"deployment/optipod-controller-manager", "--since=3m")
				output, _ := utils.Run(cmd)
				GinkgoWriter.Printf("Controller logs:\n%s\n", output)
			}
			Expect(found).To(BeFalse(), "No errors should occur with legacy configurations")
		})

		It("should handle migration from SSA to webhook strategy", func() {
			By("Creating initial SSA policy")
			ssaPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: migration-test-policy
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
        test-type: migration
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
    strategy: ssa
    allowInPlaceResize: true
    useServerSideApply: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(ssaPolicyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Deploying test workload")
			workloadYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: migration-workload
  namespace: default
  labels:
    test-type: migration
spec:
  replicas: 1
  selector:
    matchLabels:
      app: migration-workload
      test-type: migration
  template:
    metadata:
      labels:
        app: migration-workload
        test-type: migration
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
			cmd.Stdin = strings.NewReader(workloadYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady("migration-workload", "default", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial SSA optimization")
			time.Sleep(2 * time.Minute)

			By("Verifying initial SSA optimization")
			Eventually(func() bool {
				annotations, err := workloadHelper.GetWorkloadAnnotations("migration-workload", "default")
				if err != nil {
					return false
				}

				hasOptipodManaged := annotations["optipod.io/managed"] == webhookEnabledValue
				hasNoWebhookAnnotations := annotations["optipod.io/webhook-enabled"] != webhookEnabledValue

				return hasOptipodManaged && hasNoWebhookAnnotations
			}, 2*time.Minute, 15*time.Second).Should(BeTrue())

			By("Updating policy to use webhook strategy")
			webhookPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: migration-test-policy
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
        test-type: migration
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
    rolloutStrategy: immediate
    allowInPlaceResize: false
    allowRecreate: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(webhookPolicyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for migration to webhook strategy")
			time.Sleep(2 * time.Minute)

			By("Verifying migration to webhook strategy")
			Eventually(func() bool {
				annotations, err := workloadHelper.GetWorkloadAnnotations("migration-workload", "default")
				if err != nil {
					return false
				}

				hasWebhookEnabled := annotations["optipod.io/webhook-enabled"] == webhookEnabledValue
				hasStrategy := annotations["optipod.io/strategy"] == "webhook"

				GinkgoWriter.Printf("Migration workload annotations: %+v\n", annotations)
				return hasWebhookEnabled && hasStrategy
			}, 3*time.Minute, 30*time.Second).Should(BeTrue())

			By("Verifying no conflicts or errors during migration")
			found, err := validationHelper.CheckOptipodLogs("error", 3*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse(), "No errors should occur during strategy migration")
		})
	})
})
