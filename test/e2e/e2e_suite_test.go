package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/utils"
)

var (
	clusterName      = "optipod-e2e-test"
	optipodNamespace = "optipod-system"
	testNamespace    = "optipod-workloads"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OptipPod E2E Test Suite")
}

var _ = BeforeSuite(func() {
	By("Setting up E2E test environment")

	// Step 1: Create Kind Cluster
	By("Creating Kind cluster")
	createKindCluster()

	// Step 2: Install Prerequisites
	By("Installing prerequisites")
	installMetricsServer()

	// Step 3: Create OptipPod namespace
	By("Creating OptipPod namespace")
	createOptipodNamespace()

	// Step 4: Install OptipPod CRDs
	By("Installing OptipPod CRDs")
	installOptipodCRDs()

	// Step 5: Label default namespace for testing
	By("Labeling default namespace")
	labelDefaultNamespace()

	// Step 6: Install OptipPod Controller
	By("Installing OptipPod Controller")
	installOptipodController()

	// Step 7: Setup RBAC Permissions
	By("Setting up RBAC permissions")
	setupRBACPermissions()

	// Step 8: Verify Everything is Ready
	By("Verifying cluster readiness")
	verifyBasicClusterReadiness()
})

var _ = AfterSuite(func() {
	By("Cleaning up E2E test environment")

	// Clean up test resources
	cleanupTestResources()

	// Optionally delete the Kind cluster
	if os.Getenv("KEEP_CLUSTER") != "true" {
		deleteKindCluster()
	}
})

func createKindCluster() {
	// Check if cluster already exists
	cmd := exec.Command("kind", "get", "clusters")
	output, _ := utils.Run(cmd)

	if !contains(output, clusterName) {
		GinkgoWriter.Printf("Creating Kind cluster: %s\n", clusterName)

		kindConfig := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
`

		cmd := exec.Command("kind", "create", "cluster", "--name", clusterName, "--config", "-")
		cmd.Stdin = strings.NewReader(kindConfig)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
	} else {
		GinkgoWriter.Printf("Kind cluster %s already exists\n", clusterName)
	}

	// Set kubectl context
	cmd = exec.Command("kubectl", "config", "use-context", fmt.Sprintf("kind-%s", clusterName))
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
}

func installMetricsServer() {
	GinkgoWriter.Println("Installing metrics-server...")

	// Install metrics-server
	metricsURL := "https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml"
	cmd := exec.Command("kubectl", "apply", "-f", metricsURL)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Patch for Kind compatibility
	patch := `[
		{
			"op": "add",
			"path": "/spec/template/spec/containers/0/args/-",
			"value": "--kubelet-insecure-tls"
		},
		{
			"op": "add",
			"path": "/spec/template/spec/containers/0/args/-",
			"value": "--kubelet-preferred-address-types=InternalIP"
		},
		{
			"op": "add",
			"path": "/spec/template/spec/containers/0/args/-",
			"value": "--metric-resolution=15s"
		}
	]`

	cmd = exec.Command("kubectl", "patch", "deployment", "metrics-server", "-n", "kube-system", "--type=json", "-p", patch)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Wait for metrics-server to be ready
	Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=available", "--timeout=120s",
			"deployment/metrics-server", "-n", "kube-system")
		_, err := utils.Run(cmd)
		return err
	}, 3*time.Minute, 10*time.Second).Should(Succeed())
}

func createOptipodNamespace() {
	GinkgoWriter.Println("Creating OptipPod namespace...")
	cmd := exec.Command("kubectl", "create", "namespace", optipodNamespace)
	_, err := utils.Run(cmd)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		Expect(err).NotTo(HaveOccurred())
	}
}

func labelDefaultNamespace() {
	GinkgoWriter.Println("Labeling default namespace...")
	cmd := exec.Command("kubectl", "label", "namespace", "default", "environment=development", "--overwrite")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
}

func installOptipodCRDs() {
	GinkgoWriter.Println("Installing OptipPod CRDs...")

	// Install CRDs directly instead of using make install
	crdFile := "config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml"
	cmd := exec.Command("kubectl", "apply", "-f", crdFile, "--validate=false")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Verify CRDs are installed
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "crd", "optimizationpolicies.optipod.optipod.io")
		_, err := utils.Run(cmd)
		return err
	}, 1*time.Minute, 5*time.Second).Should(Succeed())
}

func verifyBasicClusterReadiness() {
	GinkgoWriter.Println("Verifying basic cluster readiness...")

	// Verify CRDs are available
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "crd", "optimizationpolicies.optipod.optipod.io")
		_, err := utils.Run(cmd)
		return err
	}, 1*time.Minute, 5*time.Second).Should(Succeed())

	// Verify namespaces exist
	Eventually(func() error {
		cmd := exec.Command("kubectl", "get", "namespace", optipodNamespace)
		_, err := utils.Run(cmd)
		return err
	}, 30*time.Second, 5*time.Second).Should(Succeed())
}

func installOptipodController() {
	GinkgoWriter.Println("Installing OptipPod Controller...")

	// Build the controller binary first
	GinkgoWriter.Println("Building OptipPod controller binary...")
	cmd := exec.Command("go", "build", "-o", "bin/manager", "cmd/main.go")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Build the controller Docker image
	GinkgoWriter.Println("Building OptipPod controller Docker image...")
	cmd = exec.Command("docker", "build", "-t", "optipod-controller:e2e", ".")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Load the image into Kind cluster
	GinkgoWriter.Println("Loading controller image into Kind cluster...")
	cmd = exec.Command("kind", "load", "docker-image", "optipod-controller:e2e", "--name", clusterName)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Install controller-gen if not available
	GinkgoWriter.Println("Ensuring controller-gen is available...")
	cmd = exec.Command("go", "install", "sigs.k8s.io/controller-tools/cmd/controller-gen@latest")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Install kustomize if not available
	GinkgoWriter.Println("Ensuring kustomize is available...")
	cmd = exec.Command("go", "install", "sigs.k8s.io/kustomize/kustomize/v5@latest")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Generate manifests
	GinkgoWriter.Println("Generating controller manifests...")
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = os.Getenv("HOME") + "/go"
	}
	controllerGenPath := goPath + "/bin/controller-gen"
	cmd = exec.Command(controllerGenPath, "rbac:roleName=manager-role", "crd:allowDangerousTypes=true",
		"webhook", "paths=./...", "output:crd:artifacts:config=config/crd/bases")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Deploy using kustomize
	GinkgoWriter.Println("Deploying OptipPod controller...")
	kustomizePath := goPath + "/bin/kustomize"

	// Update the image in the kustomization
	setImageCmd := fmt.Sprintf("cd config/manager && %s edit set image controller=optipod-controller:e2e", kustomizePath)
	cmd = exec.Command("bash", "-c", setImageCmd)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Build and apply manifests
	cmd = exec.Command(kustomizePath, "build", "config/default")
	manifestsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifestsOutput)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	// Wait for controller deployment to be ready
	GinkgoWriter.Println("Waiting for controller to be ready...")
	Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=available", "--timeout=120s",
			"deployment/optipod-controller-manager", "-n", optipodNamespace)
		_, err := utils.Run(cmd)
		return err
	}, 5*time.Minute, 15*time.Second).Should(Succeed())

	// Verify controller pods are running
	Eventually(func() string {
		cmd := exec.Command("kubectl", "get", "pods", "-n", optipodNamespace,
			"-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].status.phase}")
		output, _ := utils.Run(cmd)
		return strings.TrimSpace(output)
	}, 3*time.Minute, 10*time.Second).Should(Equal("Running"))

	GinkgoWriter.Println("OptipPod controller installed and running successfully")
}

func setupRBACPermissions() {
	GinkgoWriter.Println("Setting up RBAC permissions...")

	// Create ClusterRoleBinding for metrics access (needed for observability tests)
	metricsRoleBinding := `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: optipod-metrics-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: optipod-metrics-reader
subjects:
- kind: ServiceAccount
  name: default
  namespace: ` + optipodNamespace + `
`

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(metricsRoleBinding)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
}

func cleanupTestResources() {
	GinkgoWriter.Println("Cleaning up test resources...")

	// Delete test policies
	cmd := exec.Command("kubectl", "delete", "optimizationpolicy", "--all", "-n", optipodNamespace,
		"--ignore-not-found=true")
	_, _ = utils.Run(cmd) // Ignore cleanup errors

	// Delete test workloads
	cmd = exec.Command("kubectl", "delete", "namespace", testNamespace, "--ignore-not-found=true")
	_, _ = utils.Run(cmd) // Ignore cleanup errors
}

func deleteKindCluster() {
	GinkgoWriter.Printf("Deleting Kind cluster: %s\n", clusterName)

	cmd := exec.Command("kind", "delete", "cluster", "--name", clusterName)
	_, _ = utils.Run(cmd) // Ignore cleanup errors
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
