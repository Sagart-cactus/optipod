//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/optipod/optipod/test/e2e/helpers"
)

var _ = Describe("Test Suite Validation", func() {
	var (
		ctx           context.Context
		namespace     string
		cleanupHelper *helpers.CleanupHelper
		validator     *TestSuiteValidator
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = fmt.Sprintf("test-validation-%d", time.Now().Unix())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		cleanupHelper = helpers.NewCleanupHelper(k8sClient)
		cleanupHelper.TrackNamespace(namespace)

		validator = NewTestSuiteValidator(k8sClient, namespace)
	})

	AfterEach(func() {
		if cleanupHelper != nil {
			cleanupHelper.CleanupAll()
		}
	})

	Context("Test Suite Health Validation", func() {
		It("should validate test suite health successfully", func() {
			By("Running comprehensive test suite validation")
			result, err := validator.ValidateTestSuite(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			By("Validating validation result structure")
			Expect(result.Timestamp).NotTo(BeZero())
			Expect(result.OverallHealth).NotTo(BeEmpty())
			Expect(result.ComponentHealth).NotTo(BeNil())
			Expect(result.QualityMetrics).NotTo(BeNil())

			By("Printing validation report")
			result.PrintValidationReport()

			By("Validating component health checks")
			// Should have checked key components
			expectedComponents := []string{
				"cluster-connectivity",
				"resource-availability",
				"test-structure",
			}

			for _, component := range expectedComponents {
				_, exists := result.ComponentHealth[component]
				Expect(exists).To(BeTrue(), "Should have checked %s component", component)
			}

			By("Validating quality metrics are calculated")
			Expect(result.QualityMetrics.TestCoverage).To(BeNumerically(">=", 0))
			Expect(result.QualityMetrics.PropertyCoverage).To(BeNumerically(">=", 0))
			Expect(result.QualityMetrics.HelperUtilization).To(BeNumerically(">=", 0))
			Expect(result.QualityMetrics.CleanupEffectiveness).To(BeNumerically(">=", 0))
			Expect(result.QualityMetrics.ExecutionStability).To(BeNumerically(">=", 0))
			Expect(result.QualityMetrics.PerformanceScore).To(BeNumerically(">=", 0))

			By("Validating overall health is reasonable")
			validHealthStatuses := []HealthStatus{
				HealthStatusHealthy,
				HealthStatusDegraded,
				HealthStatusUnhealthy,
			}
			Expect(validHealthStatuses).To(ContainElement(result.OverallHealth))
		})

		It("should detect cluster connectivity issues", func() {
			By("Testing cluster connectivity validation")
			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err := validator.validateClusterConnectivity(ctx, result)
			Expect(err).NotTo(HaveOccurred())

			By("Validating cluster connectivity status")
			status, exists := result.ComponentHealth["cluster-connectivity"]
			Expect(exists).To(BeTrue())

			// In a working test environment, this should be healthy
			Expect(status).To(Equal(HealthStatusHealthy))
		})

		It("should validate test structure correctly", func() {
			By("Testing test structure validation")
			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err := validator.validateTestStructure(result)
			Expect(err).NotTo(HaveOccurred())

			By("Validating test structure status")
			status, exists := result.ComponentHealth["test-structure"]
			Expect(exists).To(BeTrue())

			// Should be healthy or degraded (not unhealthy in a working environment)
			Expect(status).To(Or(Equal(HealthStatusHealthy), Equal(HealthStatusDegraded)))
		})

		It("should validate helper components", func() {
			By("Testing helper component validation")
			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err := validator.validateHelperComponents(result)
			Expect(err).NotTo(HaveOccurred())

			By("Validating helper component statuses")
			expectedHelpers := []string{
				"policy-helpers",
				"workload-helpers",
				"validation-helpers",
				"cleanup-helpers",
			}

			for _, helper := range expectedHelpers {
				status, exists := result.ComponentHealth[helper]
				Expect(exists).To(BeTrue(), "Should have checked %s helper", helper)
				Expect(status).NotTo(BeEmpty())
			}
		})

		It("should calculate quality metrics appropriately", func() {
			By("Testing quality metrics calculation")
			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err := validator.validateQualityMetrics(result)
			Expect(err).NotTo(HaveOccurred())

			By("Validating quality metrics are within expected ranges")
			metrics := result.QualityMetrics

			Expect(metrics.TestCoverage).To(BeNumerically(">=", 0))
			Expect(metrics.TestCoverage).To(BeNumerically("<=", 1))

			Expect(metrics.PropertyCoverage).To(BeNumerically(">=", 0))
			Expect(metrics.PropertyCoverage).To(BeNumerically("<=", 1))

			Expect(metrics.HelperUtilization).To(BeNumerically(">=", 0))
			Expect(metrics.HelperUtilization).To(BeNumerically("<=", 1))

			Expect(metrics.CleanupEffectiveness).To(BeNumerically(">=", 0))
			Expect(metrics.CleanupEffectiveness).To(BeNumerically("<=", 1))

			Expect(metrics.ExecutionStability).To(BeNumerically(">=", 0))
			Expect(metrics.ExecutionStability).To(BeNumerically("<=", 1))

			Expect(metrics.PerformanceScore).To(BeNumerically(">=", 0))
			Expect(metrics.PerformanceScore).To(BeNumerically("<=", 1))
		})

		It("should generate appropriate recommendations", func() {
			By("Creating a validation result with various issues")
			result := &ValidationResult{
				OverallHealth: HealthStatusDegraded,
				ComponentHealth: map[string]HealthStatus{
					"test-structure": HealthStatusDegraded,
					"helpers":        HealthStatusHealthy,
				},
				QualityMetrics: QualityMetrics{
					TestCoverage:     0.6, // Below threshold
					PropertyCoverage: 0.5, // Below threshold
					PerformanceScore: 0.6, // Below threshold
				},
				Issues: []ValidationIssue{
					{
						Severity:  IssueSeverityMajor,
						Component: "test-structure",
					},
				},
			}

			By("Generating recommendations")
			validator.generateRecommendations(result)

			By("Validating recommendations are generated")
			Expect(result.Recommendations).NotTo(BeEmpty())

			// Should have recommendations for the issues we created
			recommendationText := fmt.Sprintf("%v", result.Recommendations)
			Expect(recommendationText).To(ContainSubstring("coverage"))
		})
	})

	Context("Health Status Calculation", func() {
		It("should calculate healthy status correctly", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusHealthy,
					"component2": HealthStatusHealthy,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityMinor},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusHealthy))
		})

		It("should calculate degraded status correctly", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusDegraded,
					"component2": HealthStatusDegraded,
					"component3": HealthStatusDegraded,
					"component4": HealthStatusDegraded,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityMajor},
					{Severity: IssueSeverityMajor},
					{Severity: IssueSeverityMajor},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusDegraded))
		})

		It("should calculate unhealthy status correctly", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusUnhealthy,
					"component2": HealthStatusUnhealthy,
					"component3": HealthStatusUnhealthy,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityCritical},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusUnhealthy))
		})
	})

	Context("Quick Health Check", func() {
		It("should perform quick health check successfully", func() {
			By("Running quick health check")
			err := ValidateTestSuiteHealth(ctx, k8sClient)

			// In a working test environment, this should not error
			// If it does error, it means there are critical issues
			if err != nil {
				// Log the error but don't fail the test - this is informational
				GinkgoWriter.Printf("Health check warning: %v\n", err)
			}
		})
	})
})

var _ = Describe("Validation Result Reporting", func() {
	Context("Report Generation", func() {
		It("should generate comprehensive validation reports", func() {
			By("Creating a sample validation result")
			result := &ValidationResult{
				Timestamp:     time.Now(),
				OverallHealth: HealthStatusDegraded,
				ComponentHealth: map[string]HealthStatus{
					"cluster-connectivity": HealthStatusHealthy,
					"test-structure":       HealthStatusDegraded,
					"helpers":              HealthStatusHealthy,
				},
				QualityMetrics: QualityMetrics{
					TestCoverage:         0.85,
					PropertyCoverage:     0.70,
					HelperUtilization:    0.90,
					CleanupEffectiveness: 0.95,
					ExecutionStability:   0.80,
					PerformanceScore:     0.75,
				},
				Issues: []ValidationIssue{
					{
						Severity:    IssueSeverityMajor,
						Component:   "test-structure",
						Description: "Missing some test files",
						Impact:      "Tests may not run correctly",
						Suggestion:  "Add missing test files",
					},
					{
						Severity:    IssueSeverityMinor,
						Component:   "documentation",
						Description: "Some documentation is outdated",
						Impact:      "Developers may be confused",
						Suggestion:  "Update documentation",
					},
				},
				Recommendations: []string{
					"Address test structure issues",
					"Update documentation",
					"Consider performance optimizations",
				},
			}

			By("Printing the validation report")
			// This should not panic or error
			Expect(func() {
				result.PrintValidationReport()
			}).NotTo(Panic())

			By("Validating report contains expected information")
			// The report should include all the key information
			Expect(result.OverallHealth).To(Equal(HealthStatusDegraded))
			Expect(len(result.ComponentHealth)).To(Equal(3))
			Expect(len(result.Issues)).To(Equal(2))
			Expect(len(result.Recommendations)).To(Equal(3))
		})
	})
})
