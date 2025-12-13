//go:build e2e

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Server-Side Apply (SSA)", Ordered, func() {
	const ssaNamespace = "ssa-test"

	BeforeAll(func() {
		By("creating SSA test namespace")
		cmd := exec.Command("kubectl", "create", "ns", ssaNamespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create SSA test namespace")

		By("labeling the SSA test namespace")
		cmd = exec.Command("kubectl", "label", "ns", ssaNamespace, "ssa-test=true")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label SSA test namespace")
	})

	AfterAll(func() {
		By("cleaning up SSA test resources")
		cmd := exec.Command("kubectl", "delete", "ns", ssaNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
	})

	Context("End-to-End SSA Flow", func() {
		It("should apply resource changes using Server-Side Apply", func() {
			By("creating an OptimizationPolicy with SSA enabled")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-policy
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        ssa-test: "true"
    workloadSelector:
      matchLabels:
        ssa-enabled: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create SSA policy")

			By("creating a test deployment with SSA label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ssa-test-app
  namespace: ssa-test
  labels:
    ssa-enabled: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ssa-test
  template:
    metadata:
      labels:
        app: ssa-test
    spec:
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "500m"
            memory: "512Mi"
          limits:
            cpu: "1000m"
            memory: "1Gi"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create SSA test deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "ssa-test-app",
					"-n", ssaNamespace, "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}, 2*time.Minute).Should(Succeed())

			By("verifying the workload appears in policy status")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='ssa-test-app')].name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("ssa-test-app"))
			}, 3*time.Minute).Should(Succeed())

			By("verifying resources are updated via SSA")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='ssa-test-app')].lastApplied}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "lastApplied should be set")
			}, 4*time.Minute).Should(Succeed())

			By("verifying managedFields shows OptipPod ownership")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "ssa-test-app",
					"-n", ssaNamespace, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				// Parse the deployment JSON
				var deployment map[string]interface{}
				err = json.Unmarshal([]byte(output), &deployment)
				g.Expect(err).NotTo(HaveOccurred())

				// Check managedFields
				metadata, ok := deployment["metadata"].(map[string]interface{})
				g.Expect(ok).To(BeTrue(), "metadata should be present")

				managedFields, ok := metadata["managedFields"].([]interface{})
				g.Expect(ok).To(BeTrue(), "managedFields should be present")

				// Look for optipod manager
				foundOptipod := false
				for _, field := range managedFields {
					fieldMap, ok := field.(map[string]interface{})
					if !ok {
						continue
					}
					manager, ok := fieldMap["manager"].(string)
					if !ok {
						continue
					}
					operation, ok := fieldMap["operation"].(string)
					if !ok {
						continue
					}

					if manager == "optipod" && operation == "Apply" {
						foundOptipod = true
						break
					}
				}

				g.Expect(foundOptipod).To(BeTrue(), "managedFields should show 'optipod' as manager with Apply operation")
			}, 4*time.Minute).Should(Succeed())

			By("verifying other fields remain unchanged")
			cmd = exec.Command("kubectl", "get", "deployment", "ssa-test-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("nginx:1.25-alpine"), "Image should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "ssa-test-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("1"), "Replicas should remain unchanged")
		})

		It("should track apply method in workload status", func() {
			By("checking workload status for lastApplyMethod")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='ssa-test-app')].lastApplyMethod}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("ServerSideApply"), "lastApplyMethod should be ServerSideApply")
			}, 3*time.Minute).Should(Succeed())

			By("checking workload status for fieldOwnership")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='ssa-test-app')].fieldOwnership}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "fieldOwnership should be true")
			}, 3*time.Minute).Should(Succeed())
		})
	})

	Context("SSA with Strategic Merge Patch Fallback", func() {
		It("should use Strategic Merge Patch when SSA is disabled", func() {
			By("creating an OptimizationPolicy with SSA disabled")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: non-ssa-policy
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        ssa-test: "true"
    workloadSelector:
      matchLabels:
        non-ssa: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: false
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create non-SSA policy")

			By("creating a test deployment with non-SSA label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: non-ssa-app
  namespace: ssa-test
  labels:
    non-ssa: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: non-ssa
  template:
    metadata:
      labels:
        app: non-ssa
    spec:
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "500m"
            memory: "512Mi"
          limits:
            cpu: "1000m"
            memory: "1Gi"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create non-SSA test deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "non-ssa-app",
					"-n", ssaNamespace, "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}, 2*time.Minute).Should(Succeed())

			By("verifying the workload appears in policy status")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "non-ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='non-ssa-app')].name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("non-ssa-app"))
			}, 3*time.Minute).Should(Succeed())

			By("verifying lastApplyMethod is StrategicMergePatch")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "non-ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='non-ssa-app')].lastApplyMethod}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("StrategicMergePatch"), "lastApplyMethod should be StrategicMergePatch")
			}, 4*time.Minute).Should(Succeed())

			By("verifying fieldOwnership is false")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "non-ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='non-ssa-app')].fieldOwnership}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("false"), "fieldOwnership should be false")
			}, 3*time.Minute).Should(Succeed())
		})
	})

	Context("SSA with Multiple Field Managers", func() {
		It("should coexist with other field managers", func() {
			By("creating a deployment managed by kubectl")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-manager-app
  namespace: ssa-test
  labels:
    ssa-enabled: "true"
spec:
  replicas: 2
  selector:
    matchLabels:
      app: multi-manager
  template:
    metadata:
      labels:
        app: multi-manager
        version: v1
    spec:
      containers:
      - name: nginx
        image: nginx:1.24-alpine
        resources:
          requests:
            cpu: "300m"
            memory: "256Mi"
          limits:
            cpu: "600m"
            memory: "512Mi"
        env:
        - name: ENV_VAR
          value: "test-value"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create multi-manager deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "multi-manager-app",
					"-n", ssaNamespace, "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("2"))
			}, 2*time.Minute).Should(Succeed())

			By("waiting for OptipPod to apply SSA changes")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "ssa-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='multi-manager-app')].lastApplied}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
			}, 4*time.Minute).Should(Succeed())

			By("verifying managedFields shows both kubectl and optipod")
			cmd = exec.Command("kubectl", "get", "deployment", "multi-manager-app",
				"-n", ssaNamespace, "-o", "json")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			var deployment map[string]interface{}
			err = json.Unmarshal([]byte(output), &deployment)
			Expect(err).NotTo(HaveOccurred())

			metadata, ok := deployment["metadata"].(map[string]interface{})
			Expect(ok).To(BeTrue())

			managedFields, ok := metadata["managedFields"].([]interface{})
			Expect(ok).To(BeTrue())

			foundOptipod := false
			foundKubectl := false
			for _, field := range managedFields {
				fieldMap, ok := field.(map[string]interface{})
				if !ok {
					continue
				}
				manager, ok := fieldMap["manager"].(string)
				if !ok {
					continue
				}

				if manager == "optipod" {
					foundOptipod = true
				}
				if manager == "kubectl-client-side-apply" || manager == "kubectl" {
					foundKubectl = true
				}
			}

			Expect(foundOptipod).To(BeTrue(), "Should find optipod in managedFields")
			Expect(foundKubectl).To(BeTrue(), "Should find kubectl in managedFields")

			By("verifying fields managed by kubectl remain unchanged")
			cmd = exec.Command("kubectl", "get", "deployment", "multi-manager-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("2"), "Replicas should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "multi-manager-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("nginx:1.24-alpine"), "Image should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "multi-manager-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].env[0].value}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("test-value"), "Environment variable should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "multi-manager-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.metadata.labels.version}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("v1"), "Pod labels should remain unchanged")
		})
	})

	Context("SSA Observability", func() {
		It("should emit events for SSA operations", func() {
			By("checking for SSA-related events")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "events", "-n", namespace,
					"--field-selector", "involvedObject.kind=OptimizationPolicy",
					"-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				// Check if events contain SSA-related information
				g.Expect(output).To(ContainSubstring("ssa-policy"), "Should have events for SSA policy")
			}, 3*time.Minute).Should(Succeed())
		})

		It("should expose SSA metrics", func() {
			By("verifying SSA metrics are exposed")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for SSA-specific metrics
				g.Expect(metricsOutput).To(ContainSubstring("optipod_ssa_patch_total"),
					"Should expose SSA patch total metric")
			}, 3*time.Minute).Should(Succeed())
		})
	})

	Context("ArgoCD Compatibility", func() {
		It("should coexist with ArgoCD without sync conflicts", func() {
			By("creating an OptimizationPolicy for ArgoCD test")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: argocd-compat-policy
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        ssa-test: "true"
    workloadSelector:
      matchLabels:
        argocd-managed: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ArgoCD compatibility policy")

			By("deploying app via kubectl with SSA (simulating ArgoCD)")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-app
  namespace: ssa-test
  labels:
    argocd-managed: "true"
    app.kubernetes.io/managed-by: argocd
spec:
  replicas: 2
  selector:
    matchLabels:
      app: argocd-app
  template:
    metadata:
      labels:
        app: argocd-app
        version: v1.0.0
    spec:
      containers:
      - name: app
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "400m"
            memory: "512Mi"
          limits:
            cpu: "800m"
            memory: "1Gi"
        env:
        - name: APP_VERSION
          value: "1.0.0"
        - name: ENVIRONMENT
          value: "production"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
`
			// Use kubectl apply with server-side apply to simulate ArgoCD
			cmd = exec.Command("kubectl", "apply", "--server-side", "--field-manager=argocd", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ArgoCD-managed deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "argocd-app",
					"-n", ssaNamespace, "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("2"))
			}, 2*time.Minute).Should(Succeed())

			By("capturing initial resource values set by ArgoCD")
			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			initialCPU, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialCPU).To(Equal("400m"))

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
			initialMemory, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialMemory).To(Equal("512Mi"))

			By("waiting for OptipPod to apply SSA changes")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "argocd-compat-policy",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='argocd-app')].lastApplied}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "OptipPod should have applied changes")
			}, 4*time.Minute).Should(Succeed())

			By("verifying managedFields shows both argocd and optipod managers")
			var foundArgoCD, foundOptipod bool
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "argocd-app",
					"-n", ssaNamespace, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				var deployment map[string]interface{}
				err = json.Unmarshal([]byte(output), &deployment)
				g.Expect(err).NotTo(HaveOccurred())

				metadata, ok := deployment["metadata"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())

				managedFields, ok := metadata["managedFields"].([]interface{})
				g.Expect(ok).To(BeTrue())

				foundArgoCD = false
				foundOptipod = false
				for _, field := range managedFields {
					fieldMap, ok := field.(map[string]interface{})
					if !ok {
						continue
					}
					manager, ok := fieldMap["manager"].(string)
					if !ok {
						continue
					}
					operation, ok := fieldMap["operation"].(string)
					if !ok {
						continue
					}

					if manager == "argocd" && operation == "Apply" {
						foundArgoCD = true
					}
					if manager == "optipod" && operation == "Apply" {
						foundOptipod = true
					}
				}

				g.Expect(foundArgoCD).To(BeTrue(), "Should find argocd in managedFields")
				g.Expect(foundOptipod).To(BeTrue(), "Should find optipod in managedFields")
			}, 4*time.Minute).Should(Succeed())

			By("capturing resource values after OptipPod changes")
			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			optipodCPU, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
			optipodMemory, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Resources should have changed from initial values
			Expect(optipodCPU).NotTo(Equal(initialCPU), "CPU should be updated by OptipPod")
			Expect(optipodMemory).NotTo(Equal(initialMemory), "Memory should be updated by OptipPod")

			By("verifying ArgoCD-managed fields remain unchanged")
			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("2"), "Replicas should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("nginx:1.25-alpine"), "Image should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].env[0].value}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("1.0.0"), "Environment variable should remain unchanged")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.metadata.labels.version}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("v1.0.0"), "Pod labels should remain unchanged")

			By("simulating ArgoCD sync by re-applying the original manifest")
			cmd = exec.Command("kubectl", "apply", "--server-side", "--field-manager=argocd", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "ArgoCD sync should succeed without conflicts")

			By("verifying OptipPod's resource changes are NOT reverted by ArgoCD sync")
			time.Sleep(5 * time.Second) // Give time for any potential changes to propagate

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			postSyncCPU, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(postSyncCPU).To(Equal(optipodCPU), "CPU should remain at OptipPod's value after ArgoCD sync")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.memory}")
			postSyncMemory, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(postSyncMemory).To(Equal(optipodMemory), "Memory should remain at OptipPod's value after ArgoCD sync")

			By("verifying managedFields still shows both managers after sync")
			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "json")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			var deployment map[string]interface{}
			err = json.Unmarshal([]byte(output), &deployment)
			Expect(err).NotTo(HaveOccurred())

			metadata, ok := deployment["metadata"].(map[string]interface{})
			Expect(ok).To(BeTrue())

			managedFields, ok := metadata["managedFields"].([]interface{})
			Expect(ok).To(BeTrue())

			foundArgoCD = false
			foundOptipod = false
			for _, field := range managedFields {
				fieldMap, ok := field.(map[string]interface{})
				if !ok {
					continue
				}
				manager, ok := fieldMap["manager"].(string)
				if !ok {
					continue
				}

				if manager == "argocd" {
					foundArgoCD = true
				}
				if manager == "optipod" {
					foundOptipod = true
				}
			}

			Expect(foundArgoCD).To(BeTrue(), "ArgoCD should still be in managedFields")
			Expect(foundOptipod).To(BeTrue(), "OptipPod should still be in managedFields")

			By("verifying ArgoCD would not show OutOfSync status")
			// In a real ArgoCD scenario, the app would not be OutOfSync because:
			// 1. ArgoCD owns its fields (replicas, image, env, labels)
			// 2. OptipPod owns resource fields
			// 3. SSA ensures field-level ownership is respected
			// We verify this by confirming all ArgoCD-managed fields match the manifest
			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("2"), "Replicas match manifest")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("nginx:1.25-alpine"), "Image matches manifest")

			cmd = exec.Command("kubectl", "get", "deployment", "argocd-app",
				"-n", ssaNamespace, "-o", "jsonpath={.spec.template.spec.containers[0].env[0].value}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("1.0.0"), "Environment variables match manifest")
		})

		It("should handle multiple OptipPod policies with ArgoCD", func() {
			By("creating a second OptimizationPolicy targeting the same workload")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: argocd-compat-policy-2
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        ssa-test: "true"
    workloadSelector:
      matchLabels:
        argocd-managed: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2h
    percentile: P95
    safetyFactor: 1.3
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create second ArgoCD compatibility policy")

			By("verifying both policies use the same 'optipod' field manager")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "argocd-app",
					"-n", ssaNamespace, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				var deployment map[string]interface{}
				err = json.Unmarshal([]byte(output), &deployment)
				g.Expect(err).NotTo(HaveOccurred())

				metadata, ok := deployment["metadata"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())

				managedFields, ok := metadata["managedFields"].([]interface{})
				g.Expect(ok).To(BeTrue())

				// Count how many times "optipod" appears as a manager
				optipodCount := 0
				for _, field := range managedFields {
					fieldMap, ok := field.(map[string]interface{})
					if !ok {
						continue
					}
					manager, ok := fieldMap["manager"].(string)
					if !ok {
						continue
					}

					if manager == "optipod" {
						optipodCount++
					}
				}

				// Should only have ONE optipod entry, not multiple
				g.Expect(optipodCount).To(Equal(1), "Should have exactly one 'optipod' field manager entry")
			}, 4*time.Minute).Should(Succeed())

			By("cleaning up second policy")
			cmd = exec.Command("kubectl", "delete", "optimizationpolicy", "argocd-compat-policy-2",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})
	})

	Context("Cleanup", func() {
		It("should clean up SSA test resources", func() {
			By("deleting SSA policies")
			policies := []string{"ssa-policy", "non-ssa-policy", "argocd-compat-policy"}
			for _, policy := range policies {
				cmd := exec.Command("kubectl", "delete", "optimizationpolicy", policy,
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}

			By("deleting SSA test deployments")
			deployments := []string{"ssa-test-app", "non-ssa-app", "multi-manager-app", "argocd-app"}
			for _, deployment := range deployments {
				cmd := exec.Command("kubectl", "delete", "deployment", deployment,
					"-n", ssaNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
		})
	})
})
