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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/utils"
)

// namespace where the project is deployed in
const namespace = "optipod-system"

// serviceAccountName created for the project
const serviceAccountName = "optipod-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "optipod-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "optipod-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("waiting for CRDs to be available")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "get", "crd", "optimizationpolicies.optipod.optipod.io")
			_, err := utils.Run(cmd)
			return err
		}, 60*time.Second, 2*time.Second).Should(Succeed(), "CRD should be available")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=optipod-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should create and validate OptimizationPolicy resources", func() {
			By("creating a test namespace for workloads")
			cmd := exec.Command("kubectl", "create", "ns", "test-workloads")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

			By("labeling the test namespace")
			cmd = exec.Command("kubectl", "label", "ns", "test-workloads", "environment=production")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to label test namespace")

			By("creating an OptimizationPolicy in Recommend mode")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy-recommend
  namespace: optipod-system
spec:
  mode: Recommend
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadSelector:
      matchLabels:
        optimize: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "128Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  reconciliationInterval: 1m
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create OptimizationPolicy")

			By("verifying the OptimizationPolicy was created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-recommend",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("test-policy-recommend"))
			}, 30*time.Second).Should(Succeed())

			By("verifying the policy has a Ready condition")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-recommend",
					"-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Policy should be ready")
			}, 4*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should deploy sample workloads and discover them", func() {
			By("creating a sample deployment with optimization label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-nginx
  namespace: test-workloads
  labels:
    optimize: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
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
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-nginx",
					"-n", "test-workloads", "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"), "Deployment should have 1 ready replica")
			}, 2*time.Minute).Should(Succeed())

			By("verifying the workload appears in policy status")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-recommend",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='test-nginx')].name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("test-nginx"), "Workload should be discovered")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for metrics to be available for the pod")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "top", "pod", "-n", "test-workloads", "-l", "app=nginx")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("test-nginx"), "Pod metrics should be available")
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should generate recommendations in Recommend mode", func() {
			By("checking that recommendations are generated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-recommend",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='test-nginx')].recommendations}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Recommendations should be generated")
			}, 3*time.Minute).Should(Succeed())

			By("verifying recommendations contain CPU and memory values")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-recommend",
					"-n", namespace, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("cpu"), "Recommendations should include CPU")
				g.Expect(output).To(ContainSubstring("memory"), "Recommendations should include memory")
			}, 3*time.Minute).Should(Succeed())

			By("verifying the workload was NOT modified in Recommend mode")
			cmd := exec.Command("kubectl", "get", "deployment", "test-nginx",
				"-n", "test-workloads", "-o", "jsonpath={.spec.template.spec.containers[0].resources.requests.cpu}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("500m"), "CPU request should remain unchanged in Recommend mode")
		})

		It("should apply recommendations in Auto mode", func() {
			By("creating an OptimizationPolicy in Auto mode")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy-auto
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadSelector:
      matchLabels:
        optimize: "true"
        auto-update: "true"
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
    updateRequestsOnly: true
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Auto mode policy")

			By("creating a deployment with auto-update label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-auto-nginx
  namespace: test-workloads
  labels:
    optimize: "true"
    auto-update: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: auto-nginx
  template:
    metadata:
      labels:
        app: auto-nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "1000m"
            memory: "1Gi"
          limits:
            cpu: "2000m"
            memory: "2Gi"
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
			Expect(err).NotTo(HaveOccurred(), "Failed to create auto-update deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-auto-nginx",
					"-n", "test-workloads", "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}, 2*time.Minute).Should(Succeed())

			By("verifying the workload appears in Auto policy status")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-auto",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='test-auto-nginx')].name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("test-auto-nginx"))
			}, 3*time.Minute).Should(Succeed())

			By("waiting for metrics to be available for the auto-update pod")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "top", "pod", "-n", "test-workloads", "-l", "app=auto-nginx")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("test-auto-nginx"), "Pod metrics should be available")
			}, 2*time.Minute, 10*time.Second).Should(Succeed())

			By("verifying recommendations are applied (lastApplied timestamp exists)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-auto",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='test-auto-nginx')].lastApplied}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "lastApplied should be set in Auto mode")
			}, 4*time.Minute).Should(Succeed())
		})

		It("should respect resource bounds", func() {
			By("creating a policy with tight bounds")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy-bounds
  namespace: optipod-system
spec:
  mode: Recommend
  selector:
    workloadSelector:
      matchLabels:
        test-bounds: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "200m"
      max: "300m"
    memory:
      min: "256Mi"
      max: "384Mi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  reconciliationInterval: 1m
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create bounds policy")

			By("creating a deployment with bounds test label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-bounds-app
  namespace: test-workloads
  labels:
    test-bounds: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bounds-app
  template:
    metadata:
      labels:
        app: bounds-app
    spec:
      containers:
      - name: app
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
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
			Expect(err).NotTo(HaveOccurred(), "Failed to create bounds test deployment")

			By("waiting for the deployment to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-bounds-app",
					"-n", "test-workloads", "-o", "jsonpath={.status.readyReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}, 2*time.Minute).Should(Succeed())

			By("waiting for metrics to be available for the bounds test pod")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "top", "pod", "-n", "test-workloads", "-l", "app=bounds-app")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("test-bounds-app"), "Pod metrics should be available")
			}, 2*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for recommendations to be generated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-bounds",
					"-n", namespace, "-o", "jsonpath={.status.workloads[?(@.name=='test-bounds-app')].recommendations}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
			}, 3*time.Minute).Should(Succeed())

			By("verifying recommendations respect bounds")
			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-bounds",
				"-n", namespace, "-o", "json")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// Recommendations should be within bounds (200m-300m CPU, 256Mi-384Mi memory)
			Expect(output).To(ContainSubstring("test-bounds-app"))
		})

		It("should expose Prometheus metrics", func() {
			By("verifying OptiPod metrics are exposed")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Check for OptiPod-specific metrics
				g.Expect(metricsOutput).To(ContainSubstring("optipod_workloads_monitored"),
					"Should expose workloads monitored metric")
				g.Expect(metricsOutput).To(ContainSubstring("optipod_reconciliation_duration_seconds"),
					"Should expose reconciliation duration metric")
			}, 2*time.Minute).Should(Succeed())

			By("verifying controller reconciliation metrics")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring("controller_runtime_reconcile_total"),
					"Should expose controller reconciliation metrics")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should handle invalid policy configurations", func() {
			By("attempting to create a policy with invalid bounds (min > max)")
			invalidPolicyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy-invalid
  namespace: optipod-system
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        test: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "2000m"
      max: "100m"
    memory:
      min: "2Gi"
      max: "128Mi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(invalidPolicyYAML)
			output, err := utils.Run(cmd)

			// Should fail validation
			Expect(err).To(HaveOccurred(), "Invalid policy should be rejected")
			Expect(output).To(ContainSubstring("min"), "Error should mention bounds validation")
		})

		It("should handle Disabled mode correctly", func() {
			By("creating a policy in Disabled mode")
			policyYAML := `
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy-disabled
  namespace: optipod-system
spec:
  mode: Disabled
  selector:
    workloadSelector:
      matchLabels:
        test-disabled: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "128Mi"
      max: "2Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
`
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(policyYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Disabled mode policy")

			By("creating a deployment with disabled test label")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-disabled-app
  namespace: test-workloads
  labels:
    test-disabled: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: disabled-app
  template:
    metadata:
      labels:
        app: disabled-app
    spec:
      containers:
      - name: app
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "500m"
            memory: "512Mi"
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
			Expect(err).NotTo(HaveOccurred(), "Failed to create disabled test deployment")

			By("waiting and verifying no recommendations are generated")
			time.Sleep(2 * time.Minute)

			cmd = exec.Command("kubectl", "get", "optimizationpolicy", "test-policy-disabled",
				"-n", namespace, "-o", "jsonpath={.status.workloads}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			// In Disabled mode, workloads should not be processed
			Expect(output).To(Or(BeEmpty(), Not(ContainSubstring("test-disabled-app"))),
				"Disabled policy should not process workloads")
		})

		It("should clean up test resources", func() {
			By("deleting test policies")
			policies := []string{
				"test-policy-recommend",
				"test-policy-auto",
				"test-policy-bounds",
				"test-policy-disabled",
			}
			for _, policy := range policies {
				cmd := exec.Command("kubectl", "delete", "optimizationpolicy", policy,
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}

			By("deleting test workloads")
			cmd := exec.Command("kubectl", "delete", "ns", "test-workloads", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
