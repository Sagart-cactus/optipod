package contract

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/optipod/optipod/test/utils"
)

const (
	optipodNamespace = "optipod-system"
)

var _ = Describe("OptiPod Installation Contract", func() {
	var (
		k8sClient    client.Client
		ctx          context.Context
		errorHandler *ContractErrorHandler
		reporter     *TestResultReporter
		config       *ConsistencyConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		k8sClient, err = utils.GetK8sClient()
		Expect(err).NotTo(HaveOccurred())

		// Get consistent configuration
		config = GetConsistencyConfig()

		// Initialize error handling for this test
		errorHandler = NewContractErrorHandler("Installation Contract Test")
		reporter = NewTestResultReporter("Installation Contract Test")
	})

	AfterEach(func() {
		// Generate test report - Requirement 3.5: clear pass/fail status reporting
		if reporter != nil {
			report := reporter.GenerateReport()
			if report.Status == StatusFailed {
				fmt.Printf("\n%s\n", report.String())
			}
		}
	})

	Context("when installing OptiPod to a clean cluster", func() {
		It("should maintain stable operation without restarts", func() {
			// Controller is already deployed in BeforeSuite, just verify it's stable
			By("verifying controller pod is Ready - user-visible behavior only")
			Eventually(func() error {
				if !isControllerPodReady(ctx, k8sClient) {
					contractErr := errorHandler.HandleControllerReadinessError(config.InstallTimeout)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.InstallTimeout, config.PollInterval).Should(Succeed())

			// Requirements 2.4, 2.5: No internal details, only user-visible restart count
			By("verifying no restarts occur - checking only user-visible restart count")
			Consistently(func() error {
				if !hasNoCrashLoopOrRestarts(ctx, k8sClient) {
					// Check for restart count and provide actionable error
					restartCount := getControllerRestartCount(ctx, k8sClient)
					contractErr := errorHandler.HandleControllerRestartError(restartCount)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, 30*time.Second, 5*time.Second).Should(Succeed())
		})
	})

})

// isControllerPodReady checks if the controller pod is Ready
func isControllerPodReady(ctx context.Context, k8sClient client.Client) bool {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(optipodNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	}

	if err := k8sClient.List(ctx, podList, listOpts...); err != nil {
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

// hasNoCrashLoopOrRestarts verifies no crashloop or restart events
// Only checks user-visible behavior: restart count = 0 (Requirements 2.1, 2.4, 2.5)
func hasNoCrashLoopOrRestarts(ctx context.Context, k8sClient client.Client) bool {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(optipodNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	}

	if err := k8sClient.List(ctx, podList, listOpts...); err != nil {
		return false
	}

	if len(podList.Items) == 0 {
		return false
	}

	pod := podList.Items[0]

	// ONLY check restart count - this is user-visible behavior
	// Requirements 2.4, 2.5: Do NOT check internal events or detailed error messages
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.RestartCount > 0 {
			return false
		}
	}

	// Contract validation: Only check restart count, not internal events
	// Removed event checking as it depends on internal controller behavior
	return true
}

// getControllerRestartCount gets the restart count for error reporting
func getControllerRestartCount(ctx context.Context, k8sClient client.Client) int32 {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(optipodNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	}

	if err := k8sClient.List(ctx, podList, listOpts...); err != nil {
		return -1 // Indicate error in retrieval
	}

	if len(podList.Items) == 0 {
		return -1 // No pods found
	}

	pod := podList.Items[0]
	var maxRestarts int32 = 0

	// Get the highest restart count among all containers
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.RestartCount > maxRestarts {
			maxRestarts = containerStatus.RestartCount
		}
	}

	return maxRestarts
}
