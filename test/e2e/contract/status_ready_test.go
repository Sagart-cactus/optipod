package contract

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Status Convergence Contract", func() {
	var (
		k8sClient     client.Client
		ctx           context.Context
		testNamespace string
		errorHandler  *ContractErrorHandler
		reporter      *TestResultReporter
		config        *ConsistencyConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		k8sClient, err = utils.GetK8sClient()
		Expect(err).NotTo(HaveOccurred())

		testNamespace = "default" // Use default namespace for contract tests

		// Get consistent configuration
		config = GetConsistencyConfig()

		// Initialize error handling for this test
		errorHandler = NewContractErrorHandler("Status Convergence Contract Test")
		reporter = NewTestResultReporter("Status Convergence Contract Test")
	})

	AfterEach(func() {
		// Generate test report - Requirement 3.5: clear pass/fail status reporting
		if reporter != nil {
			report := reporter.GenerateReport()
			if report.Status == "FAILED" {
				fmt.Printf("\n%s\n", report.String())
			}
		}
	})

	Context("when an OptimizationPolicy CR is applied", func() {
		// Requirement 2.3: WHEN checking status convergence, THE Test_Suite SHALL only verify a Ready=True condition appears
		It("should converge to Ready=True condition within timeout period", func() {
			By("creating an OptimizationPolicy CR")
			testCR := createStatusTestOptimizationPolicy("test-status-cr", testNamespace)

			By("applying the CR to the cluster")
			Eventually(func() error {
				err := k8sClient.Create(ctx, testCR)
				if err != nil {
					contractErr := errorHandler.HandleCRCreationError(testCR.Name, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying controller is running before checking status")
			Eventually(func() bool {
				return isControllerPodReady(ctx, k8sClient)
			}, 30*time.Second, 5*time.Second).Should(BeTrue(), "Controller should be running")

			By("polling for Ready=True condition only - user-visible behavior per Requirements 2.3, 2.4, 2.5")
			Eventually(func() error {
				retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      testCR.Name,
					Namespace: testCR.Namespace,
				}, retrievedCR)
				if err != nil {
					return err
				}

				// Debug: Print the current status
				if config.VerboseLogging {
					fmt.Printf("DEBUG: CR %s status: %+v\n", testCR.Name, retrievedCR.Status)
				}

				if !hasReadyCondition(ctx, k8sClient, testCR.Name, testCR.Namespace) {
					return fmt.Errorf("Ready=True condition not found yet")
				}
				return nil
			}, config.StatusTimeout, config.StatusPollInterval).Should(Succeed())

			By("cleaning up the test CR")
			Eventually(func() error {
				return k8sClient.Delete(ctx, testCR)
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		// Requirements 2.4, 2.5: No detailed status messages or timing validation, only basic status presence
		It("should show status processing activity within timeout period", func() {
			By("creating an OptimizationPolicy CR")
			testCR := createStatusTestOptimizationPolicy("test-convergence-cr", testNamespace)

			By("applying the CR to the cluster")
			Eventually(func() error {
				err := k8sClient.Create(ctx, testCR)
				if err != nil {
					contractErr := errorHandler.HandleCRCreationError(testCR.Name, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying controller is running before checking status")
			Eventually(func() bool {
				return isControllerPodReady(ctx, k8sClient)
			}, 30*time.Second, 5*time.Second).Should(BeTrue(), "Controller should be running")

			By("verifying status convergence occurs - checking only for condition presence, no internal details")
			Eventually(func() error {
				retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      testCR.Name,
					Namespace: testCR.Namespace,
				}, retrievedCR)
				if err != nil {
					return err
				}

				// Debug: Print the current status
				if config.VerboseLogging {
					fmt.Printf("DEBUG: CR %s status: %+v\n", testCR.Name, retrievedCR.Status)
				}

				// Only check that status has been updated (any condition present indicates processing)
				// Requirements 2.4, 2.5: No detailed status messages or internal processing details
				if len(retrievedCR.Status.Conditions) == 0 {
					return fmt.Errorf("no status conditions found yet")
				}
				return nil
			}, config.StatusTimeout, config.StatusPollInterval).Should(Succeed())

			By("cleaning up the test CR")
			Eventually(func() error {
				return k8sClient.Delete(ctx, testCR)
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})
})

// createStatusTestOptimizationPolicy creates an OptimizationPolicy for status testing
func createStatusTestOptimizationPolicy(name, namespace string) *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			// Required field: Mode
			Mode: optipodv1alpha1.ModeRecommend,

			// Required field: Selector (at least one selector must be specified)
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{
					Allow: []string{"default"},
				},
			},

			// Required field: MetricsConfig
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider: "prometheus", // Required field
			},

			// Required field: ResourceBounds
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("10m"),   // Required field
					Max: resource.MustParse("1000m"), // Required field
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"), // Required field
					Max: resource.MustParse("1Gi"),  // Required field
				},
			},

			// Required field: UpdateStrategy
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				// Using default values for optional fields within UpdateStrategy
			},
		},
	}
}

// hasReadyCondition checks if the OptimizationPolicy has a Ready=True condition
// Requirements 2.3, 2.4, 2.5: Only check Ready=True condition, no detailed status messages or internal fields
func hasReadyCondition(ctx context.Context, k8sClient client.Client, name, namespace string) bool {
	retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, retrievedCR)
	if err != nil {
		return false
	}

	// ONLY check for Ready=True condition - this is user-visible behavior
	// Requirements 2.4, 2.5: Do NOT check detailed status messages, timing, or internal fields
	for _, condition := range retrievedCR.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}

	return false
}
