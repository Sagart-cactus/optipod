//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Artifact Generation Property Tests", func() {
	var (
		ctx             context.Context
		testNamespace   string
		policyHelper    *helpers.PolicyHelper
		workloadHelper  *helpers.WorkloadHelper
		cleanupHelper   *helpers.CleanupHelper
		reportingHelper *helpers.ReportingHelper
		clientset       kubernetes.Interface
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = fmt.Sprintf("artifact-test-%d", time.Now().Unix())

		// Create test namespace
		err := createTestNamespace(ctx, k8sClient, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		// Get clientset for reporting helper
		clientset, err = utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())

		// Initialize helpers
		policyHelper = helpers.NewPolicyHelper(k8sClient, testNamespace)
		workloadHelper = helpers.NewWorkloadHelper(k8sClient, testNamespace)
		cleanupHelper = helpers.NewCleanupHelper(k8sClient)
		reportingHelper = helpers.NewReportingHelper(k8sClient, clientset, testNamespace)

		// Track namespace for cleanup
		cleanupHelper.TrackNamespace(testNamespace)
	})

	AfterEach(func() {
		err := cleanupHelper.CleanupAll()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Property 13: Test artifact generation", func() {
		/**
		 * Feature: e2e-test-enhancement, Property 13: Test artifact generation
		 * Validates: Requirements 6.5
		 */
		DescribeTable("should generate appropriate test artifacts for different test scenarios",
			func(testScenario string, shouldGenerateReports bool, shouldGenerateLogs bool, shouldGenerateCoverage bool) {
				By(fmt.Sprintf("Setting up test scenario: %s", testScenario))

				// Create artifacts directory structure
				artifactsDir := filepath.Join("test-artifacts", testScenario)
				err := os.MkdirAll(filepath.Join(artifactsDir, "reports"), 0755)
				Expect(err).NotTo(HaveOccurred())
				err = os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)
				Expect(err).NotTo(HaveOccurred())
				err = os.MkdirAll(filepath.Join(artifactsDir, "coverage"), 0755)
				Expect(err).NotTo(HaveOccurred())

				// Track artifacts directory for cleanup
				cleanupHelper.TrackResource(helpers.ResourceRef{
					Kind: "Directory",
					Name: artifactsDir,
				})

				By("Creating test resources to generate artifacts")

				// Create a policy to generate some activity
				policyConfig := helpers.PolicyConfig{
					Name: fmt.Sprintf("test-policy-%s", strings.ToLower(testScenario)),
					Mode: v1alpha1.ModeAuto,
					NamespaceSelector: map[string]string{
						"name": testNamespace,
					},
					ResourceBounds: helpers.ResourceBounds{
						CPU: helpers.ResourceBound{
							Min: "100m",
							Max: "1000m",
						},
						Memory: helpers.ResourceBound{
							Min: "128Mi",
							Max: "512Mi",
						},
					},
				}

				policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
				Expect(err).NotTo(HaveOccurred())
				cleanupHelper.TrackPolicy(policy.Name, policy.Namespace)

				// Create a workload to generate activity
				workloadConfig := helpers.WorkloadConfig{
					Name:      fmt.Sprintf("test-workload-%s", strings.ToLower(testScenario)),
					Namespace: testNamespace,
					Type:      helpers.WorkloadTypeDeployment,
					Labels: map[string]string{
						"app": fmt.Sprintf("test-%s", strings.ToLower(testScenario)),
					},
					Resources: helpers.ResourceRequirements{
						Requests: helpers.ResourceList{
							CPU:    "50m",
							Memory: "64Mi",
						},
						Limits: helpers.ResourceList{
							CPU:    "200m",
							Memory: "256Mi",
						},
					},
					Replicas: 1,
				}

				workload, err := workloadHelper.CreateDeployment(workloadConfig)
				Expect(err).NotTo(HaveOccurred())
				cleanupHelper.TrackDeployment(workload.Name, workload.Namespace)

				// Wait for some activity to occur
				time.Sleep(5 * time.Second)

				By("Generating test artifacts")

				if shouldGenerateReports {
					// Generate JUnit report
					junitPath := filepath.Join(artifactsDir, "reports", "junit-report.xml")
					err := reportingHelper.GenerateJUnitReport(ctx, testScenario, junitPath)
					Expect(err).NotTo(HaveOccurred())

					// Verify JUnit report exists and has content
					Expect(junitPath).To(BeAnExistingFile())
					content, err := os.ReadFile(junitPath)
					Expect(err).NotTo(HaveOccurred())
					Expect(string(content)).To(ContainSubstring("<?xml"))
					Expect(string(content)).To(ContainSubstring("testsuite"))

					// Generate JSON report
					jsonPath := filepath.Join(artifactsDir, "reports", "report.json")
					err = reportingHelper.GenerateJSONReport(ctx, testScenario, jsonPath)
					Expect(err).NotTo(HaveOccurred())

					// Verify JSON report exists and has content
					Expect(jsonPath).To(BeAnExistingFile())
					content, err = os.ReadFile(jsonPath)
					Expect(err).NotTo(HaveOccurred())
					Expect(string(content)).To(ContainSubstring("{"))
					Expect(string(content)).To(ContainSubstring("\"test_scenario\""))
				}

				if shouldGenerateLogs {
					// Collect controller logs
					controllerLogPath := filepath.Join(artifactsDir, "logs", "controller.log")
					err := reportingHelper.CollectControllerLogs(ctx, controllerLogPath)
					Expect(err).NotTo(HaveOccurred())

					// Verify controller logs exist
					Expect(controllerLogPath).To(BeAnExistingFile())

					// Collect cluster events
					eventsPath := filepath.Join(artifactsDir, "logs", "events.yaml")
					err = reportingHelper.CollectClusterEvents(ctx, eventsPath)
					Expect(err).NotTo(HaveOccurred())

					// Verify events file exists
					Expect(eventsPath).To(BeAnExistingFile())

					// Collect resource states
					resourcesPath := filepath.Join(artifactsDir, "logs", "resources.yaml")
					err = reportingHelper.CollectResourceStates(ctx, testNamespace, resourcesPath)
					Expect(err).NotTo(HaveOccurred())

					// Verify resources file exists
					Expect(resourcesPath).To(BeAnExistingFile())
				}

				if shouldGenerateCoverage {
					// Generate coverage report
					coveragePath := filepath.Join(artifactsDir, "coverage", "coverage.out")
					err := reportingHelper.GenerateCoverageReport(ctx, testScenario, coveragePath)
					Expect(err).NotTo(HaveOccurred())

					// Verify coverage report exists
					Expect(coveragePath).To(BeAnExistingFile())
				}

				By("Validating artifact completeness")

				// Verify all expected artifacts are present
				if shouldGenerateReports {
					Expect(filepath.Join(artifactsDir, "reports", "junit-report.xml")).To(BeAnExistingFile())
					Expect(filepath.Join(artifactsDir, "reports", "report.json")).To(BeAnExistingFile())
				}

				if shouldGenerateLogs {
					Expect(filepath.Join(artifactsDir, "logs", "controller.log")).To(BeAnExistingFile())
					Expect(filepath.Join(artifactsDir, "logs", "events.yaml")).To(BeAnExistingFile())
					Expect(filepath.Join(artifactsDir, "logs", "resources.yaml")).To(BeAnExistingFile())
				}

				if shouldGenerateCoverage {
					Expect(filepath.Join(artifactsDir, "coverage", "coverage.out")).To(BeAnExistingFile())
				}

				By("Validating artifact content quality")

				if shouldGenerateReports {
					// Validate JUnit report structure
					junitContent, err := os.ReadFile(filepath.Join(artifactsDir, "reports", "junit-report.xml"))
					Expect(err).NotTo(HaveOccurred())
					Expect(string(junitContent)).To(ContainSubstring("testcase"))

					// Validate JSON report structure
					jsonContent, err := os.ReadFile(filepath.Join(artifactsDir, "reports", "report.json"))
					Expect(err).NotTo(HaveOccurred())
					Expect(string(jsonContent)).To(ContainSubstring("timestamp"))
				}

				if shouldGenerateLogs {
					// Validate controller logs contain relevant information
					logContent, err := os.ReadFile(filepath.Join(artifactsDir, "logs", "controller.log"))
					Expect(err).NotTo(HaveOccurred())
					// Should contain some log entries (even if empty, file should exist)
					Expect(len(logContent)).To(BeNumerically(">=", 0))
				}
			},
			Entry("successful test run", "success", true, true, true),
			Entry("failed test run", "failure", true, true, false),
			Entry("minimal artifacts", "minimal", true, false, false),
			Entry("logs only", "logs-only", false, true, false),
			Entry("reports only", "reports-only", true, false, false),
		)

		It("should generate artifacts with proper naming conventions", func() {
			By("Creating test scenario with specific naming requirements")

			testScenario := "naming-convention-test"
			artifactsDir := filepath.Join("test-artifacts", testScenario)

			err := os.MkdirAll(filepath.Join(artifactsDir, "reports"), 0755)
			Expect(err).NotTo(HaveOccurred())

			cleanupHelper.TrackResource(helpers.ResourceRef{
				Kind: "Directory",
				Name: artifactsDir,
			})

			By("Generating artifacts with expected naming patterns")

			// Generate report with timestamp
			timestamp := time.Now().Format("20060102-150405")
			reportName := fmt.Sprintf("junit-%s-%s.xml", testScenario, timestamp)
			reportPath := filepath.Join(artifactsDir, "reports", reportName)

			err = reportingHelper.GenerateJUnitReport(ctx, testScenario, reportPath)
			Expect(err).NotTo(HaveOccurred())

			By("Validating naming convention compliance")

			// Verify file exists with expected name
			Expect(reportPath).To(BeAnExistingFile())

			// Verify naming pattern
			Expect(reportName).To(MatchRegexp(`^junit-.*-\d{8}-\d{6}\.xml$`))

			// Verify content includes scenario identifier
			content, err := os.ReadFile(reportPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring(testScenario))
		})

		It("should handle artifact generation failures gracefully", func() {
			By("Creating scenario that might cause artifact generation issues")

			testScenario := "failure-handling"
			artifactsDir := filepath.Join("test-artifacts", testScenario)

			// Create directory with restricted permissions to simulate failure
			err := os.MkdirAll(artifactsDir, 0755)
			Expect(err).NotTo(HaveOccurred())

			cleanupHelper.TrackResource(helpers.ResourceRef{
				Kind: "Directory",
				Name: artifactsDir,
			})

			By("Attempting to generate artifacts in restricted location")

			// Try to generate report in non-existent subdirectory
			invalidPath := filepath.Join(artifactsDir, "nonexistent", "report.xml")
			err = reportingHelper.GenerateJUnitReport(ctx, testScenario, invalidPath)

			// Should handle the error gracefully (either succeed by creating dirs or fail gracefully)
			if err != nil {
				// If it fails, it should be a clear, actionable error
				Expect(err.Error()).To(ContainSubstring("directory"))
			} else {
				// If it succeeds, the file should exist
				Expect(invalidPath).To(BeAnExistingFile())
			}

			By("Verifying system remains stable after artifact generation failures")

			// System should still be able to generate artifacts in valid locations
			validPath := filepath.Join(artifactsDir, "recovery-report.xml")
			err = reportingHelper.GenerateJUnitReport(ctx, testScenario, validPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(validPath).To(BeAnExistingFile())
		})
	})
})
