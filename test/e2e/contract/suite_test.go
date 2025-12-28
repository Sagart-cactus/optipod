package contract

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// Global test client for contract tests
	testClient client.Client
	// Global Kubernetes clientset for contract tests
	kubeClient kubernetes.Interface
	// Test context with timeout
	testCtx context.Context
	// Test context cancel function
	testCancel context.CancelFunc
	// Global error handler for contract tests
	globalErrorHandler *ContractErrorHandler
	// Global test result reporter
	globalReporter *TestResultReporter
	// Global consistency configuration
	globalConfig *ConsistencyConfig
	// Global execution environment
	globalExecEnv *ExecutionEnvironment
)

func TestContractE2E(t *testing.T) {
	// Initialize consistency configuration - Requirements 1.5, 10.1, 10.2, 10.3, 10.4, 10.5
	globalConfig = GetConsistencyConfig()

	// Initialize execution environment for consistent behavior
	globalExecEnv = GetExecutionEnvironment()

	// Set up test timeout using consistent configuration - Requirements 1.1: complete within 10 minutes maximum
	testCtx, testCancel = context.WithTimeout(context.Background(), globalConfig.ContractTestTimeout)

	// Initialize error handling and reporting
	globalErrorHandler = NewContractErrorHandler("Contract E2E Test Suite")
	globalReporter = NewTestResultReporter("Contract E2E Test Suite")

	// Log environment information for debugging
	if globalConfig.VerboseLogging {
		fmt.Printf("Contract E2E Test Environment:\n")
		envInfo := globalConfig.GetEnvironmentInfo()
		for key, value := range envInfo {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "OptiPod Contract E2E Test Suite")
}

var _ = BeforeSuite(func() {
	By("Setting up Contract E2E test environment with consistent configuration")

	// Initialize execution environment - Requirements 10.1, 10.2, 10.3, 10.4
	By("Initializing consistent execution environment")
	Eventually(func() error {
		err := globalExecEnv.Initialize(testCtx)
		if err != nil {
			contractErr := globalErrorHandler.HandleEnvironmentInitializationError(err)
			globalReporter.AddError(contractErr)
			return contractErr
		}
		return nil
	}, 2*time.Minute, 10*time.Second).Should(Succeed())

	// Requirements 1.2: create its own Kind cluster for complete isolation
	By("Creating dedicated Kind cluster for contract tests with consistent configuration")
	Eventually(func() error {
		err := globalExecEnv.CreateConsistentCluster(testCtx)
		if err != nil {
			// Use actionable error handling - Requirement 1.4
			contractErr := globalErrorHandler.HandleClusterCreationError(err)
			globalReporter.AddError(contractErr)
			return contractErr
		}
		return nil
	}, globalConfig.ClusterCreateTimeout, globalConfig.ClusterPollInterval).Should(Succeed())

	By("Setting up Kubernetes clients with consistent configuration")
	Eventually(func() error {
		return setupKubernetesClientsConsistently()
	}, 1*time.Minute, 5*time.Second).Should(Succeed())

	By("Building and loading OptiPod controller Docker image consistently")
	Eventually(func() error {
		err := globalExecEnv.BuildAndLoadConsistentImage(testCtx)
		if err != nil {
			// Use actionable error handling for image build failures
			contractErr := globalErrorHandler.HandleManifestApplicationError(err)
			globalReporter.AddError(contractErr)
			return contractErr
		}
		return nil
	}, 5*time.Minute, 10*time.Second).Should(Succeed())

	By("Installing CRDs and deploying OptiPod controller")
	Eventually(func() error {
		err := installCRDsAndController()
		if err != nil {
			contractErr := globalErrorHandler.HandleManifestApplicationError(err)
			globalReporter.AddError(contractErr)
			return contractErr
		}
		return nil
	}, globalConfig.InstallTimeout, 10*time.Second).Should(Succeed())

	By("Waiting for controller to become ready")
	Eventually(func() error {
		if !isControllerPodReadyInSuite(testCtx) {
			contractErr := globalErrorHandler.HandleControllerReadinessError(globalConfig.InstallTimeout)
			globalReporter.AddError(contractErr)
			return contractErr
		}
		return nil
	}, globalConfig.InstallTimeout, globalConfig.PollInterval).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("Cleaning up Contract E2E test environment with consistent configuration")

	// Generate and display final test report - Requirement 3.5: clear pass/fail status reporting
	if globalReporter != nil {
		report := globalReporter.GenerateReport()
		fmt.Printf("\n%s\n", report.String())
	}

	// Requirements 1.3: clean up automatically even on test failures
	defer func() {
		if testCancel != nil {
			testCancel()
		}
	}()

	// Always attempt cleanup, even if tests failed
	By("Cleaning up Kind cluster with consistent configuration")
	if globalExecEnv != nil && globalExecEnv.IsInitialized() {
		err := globalExecEnv.CleanupConsistentCluster(testCtx)
		if err != nil && globalErrorHandler != nil {
			// Report cleanup errors but don't fail the suite
			contractErr := globalErrorHandler.HandleCleanupError("Kind cluster", err)
			globalReporter.AddWarning(contractErr.Error())
		}
	}
})

// setupKubernetesClientsConsistently sets up the Kubernetes clients using consistent configuration
func setupKubernetesClientsConsistently() error {
	// Get kubeconfig for the Kind cluster using consistent configuration
	kubeconfig, err := globalExecEnv.GetKubeconfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// Create controller-runtime client
	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return fmt.Errorf("failed to create REST config: %w", err)
	}

	testClient, err = client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create controller-runtime client: %w", err)
	}

	// Create Kubernetes clientset
	kubeClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	return nil
}

// GetTestClient returns the global test client for use in contract tests
func GetTestClient() client.Client {
	return testClient
}

// GetKubeClient returns the global Kubernetes clientset for use in contract tests
func GetKubeClient() kubernetes.Interface {
	return kubeClient
}

// GetTestContext returns the test context with timeout
func GetTestContext() context.Context {
	return testCtx
}

// installCRDsAndController installs CRDs and deploys the controller
func installCRDsAndController() error {
	projectDir := globalExecEnv.GetProjectRoot()

	// First install CRDs
	crdFile := projectDir + "/config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml"
	cmd := exec.Command(globalConfig.KubectlBinary, "apply", "-f", crdFile, "--validate=false")
	cmd.Dir = projectDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("CRD installation failed: %w, output: %s", err, string(output))
	}

	// Wait for CRDs to be established
	cmd = exec.Command(globalConfig.KubectlBinary, "wait", "--for=condition=established", "--timeout=60s", "crd/optimizationpolicies.optipod.optipod.io")
	cmd.Dir = projectDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("CRD establishment wait failed: %w, output: %s", err, string(output))
	}

	// Update the image reference in kustomization
	if globalConfig.VerboseLogging {
		fmt.Println("Updating image reference in kustomization...")
	}
	cmd = exec.Command(globalConfig.KustomizeBinary, "edit", "set", "image", "controller="+globalConfig.ImageTag)
	cmd.Dir = projectDir + "/config/manager"
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("image reference update failed: %w, output: %s", err, string(output))
	}

	// Generate and apply controller manifests
	cmd = exec.Command(globalConfig.KustomizeBinary, "build", "config/default")
	cmd.Dir = projectDir
	manifestsOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("manifest generation failed: %w, output: %s", err, string(manifestsOutput))
	}

	cmd = exec.Command(globalConfig.KubectlBinary, "apply", "-f", "-")
	cmd.Dir = projectDir
	cmd.Stdin = strings.NewReader(string(manifestsOutput))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("manifest application failed: %w, output: %s", err, string(output))
	}

	return nil
}

// isControllerPodReadyInSuite checks if the controller pod is ready during suite setup
func isControllerPodReadyInSuite(ctx context.Context) bool {
	if testClient == nil {
		return false
	}

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace("optipod-system"),
		client.MatchingLabels{"control-plane": "controller-manager"},
	}

	if err := testClient.List(ctx, podList, listOpts...); err != nil {
		return false
	}

	if len(podList.Items) == 0 {
		return false
	}

	pod := podList.Items[0]
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
