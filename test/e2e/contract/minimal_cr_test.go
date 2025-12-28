package contract

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Minimal CR Acceptance Contract", func() {
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
		errorHandler = NewContractErrorHandler("Minimal CR Acceptance Contract Test")
		reporter = NewTestResultReporter("Minimal CR Acceptance Contract Test")
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

	Context("when applying a minimal OptimizationPolicy CR", func() {
		// Requirement 2.2: WHEN testing CR application, THE Test_Suite SHALL only verify the CR is accepted by the API server
		It("should be accepted by the API server without validation errors", func() {
			By("creating a minimal OptimizationPolicy with only required fields")
			minimalCR := createMinimalOptimizationPolicy("test-minimal-cr", testNamespace)

			By("applying the CR to the cluster and verifying API server acceptance - user-visible behavior only")
			Eventually(func() error {
				err := k8sClient.Create(ctx, minimalCR)
				if err != nil {
					// Use actionable error handling - Requirement 1.4
					contractErr := errorHandler.HandleCRCreationError(minimalCR.Name, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())

			By("cleaning up the test CR")
			Eventually(func() error {
				return k8sClient.Delete(ctx, minimalCR)
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())
		})

		// Requirement 2.2: Only verify API server acceptance, no internal processing details
		It("should be retrievable from the API server after creation", func() {
			By("creating a minimal OptimizationPolicy with only required fields")
			minimalCR := createMinimalOptimizationPolicy("test-retrieval-cr", testNamespace)

			By("applying the CR to the cluster")
			Eventually(func() error {
				err := k8sClient.Create(ctx, minimalCR)
				if err != nil {
					contractErr := errorHandler.HandleCRCreationError(minimalCR.Name, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())

			By("verifying the CR can be retrieved from the API server - user-visible behavior only")
			Eventually(func() error {
				retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      minimalCR.Name,
					Namespace: minimalCR.Namespace,
				}, retrievedCR)
				if err != nil {
					contractErr := errorHandler.HandleCRRetrievalError(minimalCR.Name, minimalCR.Namespace, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())

			By("cleaning up the test CR")
			Eventually(func() error {
				return k8sClient.Delete(ctx, minimalCR)
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())
		})

		// Requirements 2.4, 2.5: No validation of internal processing, only API server acceptance
		It("should persist in the cluster without validation errors", func() {
			By("creating a minimal OptimizationPolicy with only required fields")
			minimalCR := createMinimalOptimizationPolicy("test-validation-cr", testNamespace)

			By("applying the CR and verifying API server acceptance - no internal details checked")
			Eventually(func() error {
				err := k8sClient.Create(ctx, minimalCR)
				if err != nil {
					contractErr := errorHandler.HandleCRCreationError(minimalCR.Name, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed(), "CR should be accepted without validation errors")

			By("verifying the CR exists and is accessible - user-visible behavior only")
			Eventually(func() error {
				retrievedCR := &optipodv1alpha1.OptimizationPolicy{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      minimalCR.Name,
					Namespace: minimalCR.Namespace,
				}, retrievedCR)
				if err != nil {
					contractErr := errorHandler.HandleCRRetrievalError(minimalCR.Name, minimalCR.Namespace, err)
					reporter.AddError(contractErr)
					return contractErr
				}
				return nil
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())

			By("cleaning up the test CR")
			Eventually(func() error {
				return k8sClient.Delete(ctx, minimalCR)
			}, config.CRTimeout, config.CRPollInterval).Should(Succeed())
		})
	})
})

// createMinimalOptimizationPolicy creates an OptimizationPolicy with only required fields
func createMinimalOptimizationPolicy(name, namespace string) *optipodv1alpha1.OptimizationPolicy {
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
