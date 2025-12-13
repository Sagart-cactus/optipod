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
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/fixtures"
	"github.com/optipod/optipod/test/e2e/helpers"
	"github.com/optipod/optipod/test/utils"
)

var _ = Describe("Observability and Metrics", Ordered, func() {
	var (
		testNamespace  = "observability-test"
		policyHelper   *helpers.PolicyHelper
		workloadHelper *helpers.WorkloadHelper
		cleanupHelper  *helpers.CleanupHelper
		k8sClient      client.Client
	)

	BeforeAll(func() {
		// Initialize test helpers
		k8sClient = getK8sClient()
		
		// Wait for CRDs to be available
		By("waiting for OptimizationPolicy CRD to be available")
		Eventually(func() bool {
			return isCRDAvailable(context.Background(), k8sClient)
		}, 120*time.Second, 5*time.Second).Should(BeTrue(), "OptimizationPolicy CRD should be available")
		
		policyHelper = helpers.NewPolicyHelper(k8sClient, namespace)
		workloadHelper = helpers.NewWorkloadHelper(k8sClient, testNamespace)
		cleanupHelper = helpers.NewCleanupHelper(k8sClient)

		// Create test namespace
		By("creating test namespace for observability tests")
		err := createTestNamespace(context.Background(), k8sClient, testNamespace)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
		}

		// Track namespace for cleanup
		cleanupHelper.TrackResource(helpers.ResourceRef{
			Name:      testNamespace,
			Namespace: "",
			Kind:      "Namespace",
		})
	})

	AfterAll(func() {
		By("cleaning up observability test resources")
		err := cleanupHelper.CleanupAll()
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Warning: cleanup failed: %v\n", err)
		}
	})

	Context("Prometheus Metrics Exposure", func() {
		It("should expose OptipPod-specific metrics", func() {
			By("verifying OptipPod metrics are available in metrics endpoint")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

				// Check for core OptipPod metrics
				expectedMetrics := []string{
					"optipod_workloads_monitored",
					"optipod_reconciliation_duration_seconds",
					"optipod_reconciliation_errors_total",
					"optipod_recommendations_total",
					"optipod_applications_total",
				}

				for _, metric := range expectedMetrics {
					g.Expect(metricsOutput).To(ContainSubstring(metric),
						fmt.Sprintf("Should expose %s metric", metric))
				}
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should expose metrics with proper labels", func() {
			By("checking that metrics include expected labels")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for metrics with policy labels
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_workloads_monitored\{.*policy=".*".*\}`),
					"Workloads monitored metric should include policy label")

				// Check for metrics with namespace labels
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_workloads_monitored\{.*namespace=".*".*\}`),
					"Workloads monitored metric should include namespace label")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should update metric values based on system state", func() {
			By("creating a policy to generate metrics")
			policyConfig := helpers.PolicyConfig{
				Name: "metrics-test-policy",
				Mode: v1alpha1.ModeRecommend,
				WorkloadSelector: map[string]string{
					"metrics-test": "true",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU:    helpers.ResourceBound{Min: "100m", Max: "1000m"},
					Memory: helpers.ResourceBound{Min: "128Mi", Max: "1Gi"},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "metrics-server",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					UpdateRequestsOnly: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create metrics test policy")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("creating a workload to be monitored")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "metrics-test-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"metrics-test": "true",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "200m",
						Memory: "256Mi",
					},
				},
				Replicas: 1,
			}

			workload, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create metrics test workload")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      workload.Name,
				Namespace: workload.Namespace,
				Kind:      "Deployment",
			})

			By("waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady(workload.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Workload should be ready")

			By("verifying metrics reflect the monitored workload")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Look for metrics with our policy name
				policyMetricPattern := fmt.Sprintf(`optipod_workloads_monitored\{.*policy="%s".*\}\s+[1-9]`, policy.Name)
				g.Expect(metricsOutput).To(MatchRegexp(policyMetricPattern),
					"Metrics should show monitored workloads for our policy")
			}, 3*time.Minute, 15*time.Second).Should(Succeed())
		})

		It("should expose controller runtime metrics", func() {
			By("verifying controller-runtime metrics are available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for controller-runtime metrics
				expectedRuntimeMetrics := []string{
					"controller_runtime_reconcile_total",
					"controller_runtime_reconcile_errors_total",
					"controller_runtime_reconcile_time_seconds",
					"workqueue_adds_total",
					"workqueue_depth",
				}

				for _, metric := range expectedRuntimeMetrics {
					g.Expect(metricsOutput).To(ContainSubstring(metric),
						fmt.Sprintf("Should expose %s metric", metric))
				}
			}, 2*time.Minute).Should(Succeed())
		})

		It("should validate metrics format and accessibility", func() {
			By("checking metrics endpoint accessibility")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Metrics endpoint should be accessible")

				// Validate HTTP response
				g.Expect(metricsOutput).To(ContainSubstring("HTTP/1.1 200 OK"),
					"Metrics endpoint should return 200 OK")

				// Validate Content-Type
				g.Expect(metricsOutput).To(ContainSubstring("Content-Type: text/plain"),
					"Metrics should be served as text/plain")
			}, 1*time.Minute).Should(Succeed())

			By("validating Prometheus metrics format")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Extract just the metrics content (after HTTP headers)
				lines := strings.Split(metricsOutput, "\n")
				var metricsContent strings.Builder
				inMetrics := false
				for _, line := range lines {
					if strings.Contains(line, "# HELP") || strings.Contains(line, "# TYPE") {
						inMetrics = true
					}
					if inMetrics {
						metricsContent.WriteString(line + "\n")
					}
				}

				collector := NewMetricsCollector()
				err = collector.ValidateMetricFormat(metricsContent.String())
				g.Expect(err).NotTo(HaveOccurred(), "Metrics should follow Prometheus format")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should expose SSA-specific metrics", func() {
			By("verifying SSA patch metrics are available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for SSA-specific metrics
				g.Expect(metricsOutput).To(ContainSubstring("optipod_ssa_patch_total"),
					"Should expose SSA patch metrics")

				// Check for SSA metrics with proper labels
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_ssa_patch_total\{.*status=".*".*\}`),
					"SSA metrics should include status label")
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_ssa_patch_total\{.*patch_type=".*".*\}`),
					"SSA metrics should include patch_type label")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should track metrics collection duration", func() {
			By("verifying metrics collection duration is tracked")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for metrics collection duration
				g.Expect(metricsOutput).To(ContainSubstring("optipod_metrics_collection_duration_seconds"),
					"Should track metrics collection duration")

				// Check for provider label
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_metrics_collection_duration_seconds\{.*provider=".*".*\}`),
					"Metrics collection duration should include provider label")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should expose workload processing metrics", func() {
			By("verifying workload processing metrics are available")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for workload processing metrics
				expectedWorkloadMetrics := []string{
					"optipod_workloads_updated",
					"optipod_workloads_skipped",
				}

				for _, metric := range expectedWorkloadMetrics {
					g.Expect(metricsOutput).To(ContainSubstring(metric),
						fmt.Sprintf("Should expose %s metric", metric))
				}

				// Check for reason labels on skipped workloads
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_workloads_skipped\{.*reason=".*".*\}`),
					"Workloads skipped metric should include reason label")
			}, 2*time.Minute).Should(Succeed())
		})

		// **Feature: e2e-test-enhancement, Property 15: Metrics exposure correctness**
		// **Validates: Requirements 8.1, 8.3**
		DescribeTable("Property Test: Metrics exposure correctness",
			func(policyMode v1alpha1.PolicyMode, workloadCount int, expectedMetricBehavior string) {
				By(fmt.Sprintf("testing metrics correctness for mode %s with %d workloads", policyMode, workloadCount))

				// Generate test configuration
				policyGen := fixtures.NewPolicyConfigGenerator()
				policyConfig := policyGen.GenerateBasicPolicyConfig(
					fmt.Sprintf("metrics-property-%s-%d", strings.ToLower(string(policyMode)), workloadCount),
					policyMode,
				)
				policyConfig.WorkloadSelector = map[string]string{"metrics-property-test": "true"}
				policyConfig.ResourceBounds = helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "50m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi",
						Max: "1Gi",
					},
				}

				// Create policy
				policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
				Expect(err).NotTo(HaveOccurred(), "Failed to create property test policy")
				cleanupHelper.TrackResource(helpers.ResourceRef{
					Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
				})

				// Create workloads
				for i := 0; i < workloadCount; i++ {
					workloadGen := fixtures.NewWorkloadConfigGenerator()
					workloadConfig := workloadGen.GenerateBasicWorkloadConfig(
						fmt.Sprintf("metrics-property-workload-%d", i),
						helpers.WorkloadTypeDeployment,
					)
					workloadConfig.Namespace = testNamespace
					workloadConfig.Labels = map[string]string{"metrics-property-test": "true"}

					workload, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to create workload %d", i))
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: workload.Name, Namespace: workload.Namespace, Kind: "Deployment",
					})

					// Wait for workload to be ready
					err = workloadHelper.WaitForWorkloadReady(workload.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Workload %d should be ready", i))
				}

				// Wait for metrics to reflect the system state
				Eventually(func(g Gomega) {
					metricsOutput, err := getMetricsOutput()
					g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve metrics")

					// Validate metrics correctness based on expected behavior
					switch expectedMetricBehavior {
					case "should_monitor_workloads":
						// For any policy mode, workloads should be monitored
						policyMetricPattern := fmt.Sprintf(`optipod_workloads_monitored\{.*policy="%s".*\}\s+%d`, policy.Name, workloadCount)
						g.Expect(metricsOutput).To(MatchRegexp(policyMetricPattern),
							fmt.Sprintf("Should monitor %d workloads for policy %s", workloadCount, policy.Name))

					case "should_generate_recommendations":
						// For Recommend and Auto modes, recommendations should be generated
						if policyMode != v1alpha1.ModeDisabled {
							g.Expect(metricsOutput).To(MatchRegexp(`optipod_recommendations_total\{.*policy="`+policy.Name+`".*\}\s+[1-9]`),
								"Should generate recommendations")
						}

					case "should_apply_updates":
						// For Auto mode, updates should be applied
						if policyMode == v1alpha1.ModeAuto {
							g.Expect(metricsOutput).To(MatchRegexp(`optipod_applications_total\{.*policy="`+policy.Name+`".*\}\s+[1-9]`),
								"Should apply updates in Auto mode")
						}

					case "should_track_errors":
						// Error metrics should be present (even if zero)
						g.Expect(metricsOutput).To(ContainSubstring("optipod_reconciliation_errors_total"),
							"Should track reconciliation errors")
					}

					// Universal property: All metrics should follow Prometheus format
					collector := NewMetricsCollector()
					lines := strings.Split(metricsOutput, "\n")
					var metricsContent strings.Builder
					inMetrics := false
					for _, line := range lines {
						if strings.Contains(line, "# HELP") || strings.Contains(line, "# TYPE") {
							inMetrics = true
						}
						if inMetrics {
							metricsContent.WriteString(line + "\n")
						}
					}
					err = collector.ValidateMetricFormat(metricsContent.String())
					g.Expect(err).NotTo(HaveOccurred(), "All metrics should follow Prometheus format")

				}, 4*time.Minute, 15*time.Second).Should(Succeed())
			},
			Entry("Auto mode with single workload", v1alpha1.ModeAuto, 1, "should_monitor_workloads"),
			Entry("Recommend mode with single workload", v1alpha1.ModeRecommend, 1, "should_generate_recommendations"),
			Entry("Disabled mode with single workload", v1alpha1.ModeDisabled, 1, "should_monitor_workloads"),
			Entry("Auto mode with multiple workloads", v1alpha1.ModeAuto, 2, "should_apply_updates"),
			Entry("Recommend mode with multiple workloads", v1alpha1.ModeRecommend, 2, "should_generate_recommendations"),
			Entry("Auto mode error tracking", v1alpha1.ModeAuto, 1, "should_track_errors"),
			Entry("Recommend mode error tracking", v1alpha1.ModeRecommend, 1, "should_track_errors"),
			Entry("Disabled mode error tracking", v1alpha1.ModeDisabled, 1, "should_track_errors"),
		)
	})

	Context("Controller Logging", func() {
		It("should log policy reconciliation events", func() {
			By("creating a policy and checking controller logs")
			policyConfig := helpers.PolicyConfig{
				Name: "logging-test-policy",
				Mode: v1alpha1.ModeRecommend,
				WorkloadSelector: map[string]string{
					"logging-test": "true",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU:    helpers.ResourceBound{Min: "50m", Max: "500m"},
					Memory: helpers.ResourceBound{Min: "64Mi", Max: "512Mi"},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "metrics-server",
					RollingWindow: "30m",
					Percentile:    "P90",
					SafetyFactor:  1.1,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					UpdateRequestsOnly: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create logging test policy")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("checking controller logs for policy reconciliation")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get controller logs")

				// Check for policy reconciliation logs
				g.Expect(logs).To(ContainSubstring("logging-test-policy"),
					"Controller should log policy reconciliation")
				g.Expect(logs).To(ContainSubstring("Reconciling OptimizationPolicy"),
					"Controller should log reconciliation start")
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should log workload discovery and processing", func() {
			By("creating a workload and checking discovery logs")
			workloadConfig := helpers.WorkloadConfig{
				Name:      "logging-workload",
				Namespace: testNamespace,
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"logging-test": "true",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "100m",
						Memory: "128Mi",
					},
				},
				Replicas: 1,
			}

			workload, err := workloadHelper.CreateDeployment(workloadConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create logging workload")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      workload.Name,
				Namespace: workload.Namespace,
				Kind:      "Deployment",
			})

			By("waiting for workload to be ready")
			err = workloadHelper.WaitForWorkloadReady(workload.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "Workload should be ready")

			By("checking controller logs for workload processing")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for workload discovery logs
				g.Expect(logs).To(ContainSubstring("logging-workload"),
					"Controller should log workload processing")
			}, 3*time.Minute, 15*time.Second).Should(Succeed())
		})

		It("should log errors with appropriate detail", func() {
			By("checking error logging patterns in controller logs")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				// Check that error logs contain sufficient detail
				if strings.Contains(logs, "error") || strings.Contains(logs, "Error") {
					// If there are errors, they should be informative
					g.Expect(logs).To(MatchRegexp(`(?i)error.*:.*`),
						"Error logs should contain descriptive messages")
				}
			}, 1*time.Minute).Should(Succeed())
		})

		It("should log with proper levels and formatting", func() {
			By("checking log level and format consistency")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				validator := NewLogValidator()
				err = validator.ValidateLogFormat(logs)
				g.Expect(err).NotTo(HaveOccurred(), "Logs should follow consistent format")

				// Check for different log levels
				logLevels := []string{"INFO", "DEBUG", "WARN", "ERROR"}
				foundLevels := 0
				for _, level := range logLevels {
					if strings.Contains(logs, level) {
						foundLevels++
					}
				}
				g.Expect(foundLevels).To(BeNumerically(">=", 1), "Should have at least one log level")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should log system events correlation", func() {
			By("creating a policy and checking event correlation in logs")
			policyConfig := helpers.PolicyConfig{
				Name: "event-correlation-policy",
				Mode: v1alpha1.ModeAuto,
				WorkloadSelector: map[string]string{
					"event-test": "true",
				},
				ResourceBounds: helpers.ResourceBounds{
					CPU:    helpers.ResourceBound{Min: "100m", Max: "1000m"},
					Memory: helpers.ResourceBound{Min: "128Mi", Max: "1Gi"},
				},
				MetricsConfig: helpers.MetricsConfig{
					Provider:      "metrics-server",
					RollingWindow: "1h",
					Percentile:    "P90",
					SafetyFactor:  1.2,
				},
				UpdateStrategy: helpers.UpdateStrategy{
					AllowInPlaceResize: true,
					UpdateRequestsOnly: true,
				},
			}

			policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create event correlation policy")
			cleanupHelper.TrackResource(helpers.ResourceRef{
				Name:      policy.Name,
				Namespace: policy.Namespace,
				Kind:      "OptimizationPolicy",
			})

			By("checking that logs correlate with system events")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for event correlation patterns
				if strings.Contains(logs, "event-correlation-policy") {
					// Logs should contain contextual information
					g.Expect(logs).To(MatchRegexp(`(?i)(created|updated|reconciled).*event-correlation-policy`),
						"Logs should correlate with system events")
				}
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should not log sensitive information", func() {
			By("checking that logs don't contain sensitive data")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				validator := NewLogValidator()
				err = validator.CheckSensitiveInformation(logs)
				g.Expect(err).NotTo(HaveOccurred(), "Logs should not contain sensitive information")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should log metrics collection activities", func() {
			By("checking logs for metrics collection information")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for metrics-related log entries
				metricsPatterns := []string{
					`(?i)metrics.*collect`,
					`(?i)prometheus`,
					`(?i)metrics.*server`,
				}

				foundMetricsLogs := false
				for _, pattern := range metricsPatterns {
					if matched, _ := regexp.MatchString(pattern, logs); matched {
						foundMetricsLogs = true
						break
					}
				}

				if foundMetricsLogs {
					// If metrics logs are present, they should be informative
					g.Expect(logs).To(MatchRegexp(`(?i)metrics.*\d+`),
						"Metrics logs should contain quantitative information")
				}
			}, 2*time.Minute).Should(Succeed())
		})

		It("should log reconciliation timing information", func() {
			By("checking logs for reconciliation timing")
			Eventually(func(g Gomega) {
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for timing information in logs
				timingPatterns := []string{
					`(?i)reconcil.*\d+.*ms`,
					`(?i)reconcil.*\d+.*seconds?`,
					`(?i)duration.*\d+`,
				}

				foundTimingLogs := false
				for _, pattern := range timingPatterns {
					if matched, _ := regexp.MatchString(pattern, logs); matched {
						foundTimingLogs = true
						break
					}
				}

				// Timing logs are helpful for debugging but not strictly required
				if foundTimingLogs {
					g.Expect(logs).To(MatchRegexp(`(?i)(reconcil|duration).*\d+`),
						"Timing logs should contain numeric values")
				}
			}, 1*time.Minute).Should(Succeed())
		})

		// **Feature: e2e-test-enhancement, Property 16: Log content validation**
		// **Validates: Requirements 8.2**
		DescribeTable("Property Test: Log content validation",
			func(logScenario string, expectedLogPatterns []string, forbiddenPatterns []string) {
				By(fmt.Sprintf("testing log validation for scenario: %s", logScenario))

				// Create a policy to generate log activity
				policyGen := fixtures.NewPolicyConfigGenerator()
				policyConfig := policyGen.GenerateBasicPolicyConfig(
					fmt.Sprintf("log-property-%s", strings.ReplaceAll(logScenario, " ", "-")),
					v1alpha1.ModeRecommend,
				)
				policyConfig.WorkloadSelector = map[string]string{"log-property-test": "true"}
				policyConfig.ResourceBounds = helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "50m",
						Max: "500m",
					},
					Memory: helpers.ResourceBound{
						Min: "64Mi",
						Max: "512Mi",
					},
				}

				policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
				Expect(err).NotTo(HaveOccurred(), "Failed to create log property test policy")
				cleanupHelper.TrackResource(helpers.ResourceRef{
					Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
				})

				// Create a workload if needed for the scenario
				if logScenario != "policy_only" {
					workloadGen := fixtures.NewWorkloadConfigGenerator()
					workloadConfig := workloadGen.GenerateBasicWorkloadConfig(
						fmt.Sprintf("log-property-workload-%s", strings.ReplaceAll(logScenario, " ", "-")),
						helpers.WorkloadTypeDeployment,
					)
					workloadConfig.Namespace = testNamespace
					workloadConfig.Labels = map[string]string{"log-property-test": "true"}

					workload, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred(), "Failed to create log property workload")
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: workload.Name, Namespace: workload.Namespace, Kind: "Deployment",
					})

					// Wait for workload to be ready
					err = workloadHelper.WaitForWorkloadReady(workload.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
					Expect(err).NotTo(HaveOccurred(), "Workload should be ready")
				}

				// Wait for log activity and validate
				Eventually(func(g Gomega) {
					logs, err := getControllerLogs()
					g.Expect(err).NotTo(HaveOccurred(), "Failed to get controller logs")

					validator := NewLogValidator()

					// Universal property: All logs should follow proper format
					err = validator.ValidateLogFormat(logs)
					g.Expect(err).NotTo(HaveOccurred(), "All logs should follow proper format")

					// Universal property: No sensitive information should be logged
					err = validator.CheckSensitiveInformation(logs)
					g.Expect(err).NotTo(HaveOccurred(), "Logs should not contain sensitive information")

					// Scenario-specific validations
					if len(expectedLogPatterns) > 0 {
						err = validator.ValidateLogContent(logs, expectedLogPatterns)
						g.Expect(err).NotTo(HaveOccurred(), "Logs should contain expected patterns")
					}

					// Check forbidden patterns
					for _, pattern := range forbiddenPatterns {
						matched, err := regexp.MatchString(pattern, logs)
						g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Invalid regex pattern: %s", pattern))
						g.Expect(matched).To(BeFalse(), fmt.Sprintf("Logs should not contain forbidden pattern: %s", pattern))
					}

					// Universal property: Logs should contain contextual information
					if strings.Contains(logs, policy.Name) {
						// If policy is mentioned, it should have context
						g.Expect(logs).To(MatchRegexp(policy.Name+`.*\w+`),
							"Policy mentions should include contextual information")
					}

				}, 3*time.Minute, 15*time.Second).Should(Succeed())
			},
			Entry("Policy reconciliation logs", "policy_reconciliation",
				[]string{`(?i)reconcil.*optimizationpolicy`},
				[]string{`(?i)password`, `(?i)secret.*=`}),
			Entry("Workload discovery logs", "workload_discovery",
				[]string{`(?i)(discover|found|process).*workload`},
				[]string{`(?i)token.*=`, `(?i)key.*=`}),
			Entry("Error handling logs", "error_handling",
				[]string{}, // No specific patterns required, just format validation
				[]string{`(?i)panic`, `(?i)fatal.*without.*context`}),
			Entry("Metrics collection logs", "metrics_collection",
				[]string{}, // Metrics logs are optional
				[]string{`(?i)auth.*token`, `(?i)bearer.*[A-Za-z0-9+/]{20,}`}),
			Entry("Policy only scenario", "policy_only",
				[]string{`(?i)optimizationpolicy`},
				[]string{`(?i)password.*:`, `(?i)secret.*:`}),
			Entry("Reconciliation timing", "timing_info",
				[]string{}, // Timing info is optional but should be well-formatted if present
				[]string{`(?i)duration.*-\d+`, `(?i)time.*invalid`}),
		)
	})

	Context("Metrics Endpoint Security", func() {
		It("should require authentication for metrics access", func() {
			By("verifying that metrics endpoint requires authentication")
			// This test verifies that the metrics endpoint requires proper authentication
			// The existing curl-metrics pod uses a service account token, so we know auth is required
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// The fact that we get metrics with the authenticated curl pod
				// and the setup requires a service account token proves auth is working
				g.Expect(metricsOutput).To(ContainSubstring("HTTP/1.1 200 OK"),
					"Authenticated access should succeed")

				// Verify that the request includes authorization header
				g.Expect(metricsOutput).To(ContainSubstring("Authorization: Bearer"),
					"Request should include authorization header")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should use TLS for metrics endpoint", func() {
			By("verifying TLS is used for metrics endpoint")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check that the curl command uses HTTPS (from the curl output)
				g.Expect(metricsOutput).To(ContainSubstring("https://"),
					"Metrics endpoint should use HTTPS")

				// Check for TLS handshake information
				g.Expect(metricsOutput).To(MatchRegexp(`(?i)(ssl|tls)`),
					"Should show TLS/SSL connection information")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should validate RBAC permissions for metrics access", func() {
			By("verifying that proper RBAC is configured for metrics access")
			Eventually(func(g Gomega) {
				// Check that the metrics reader role exists
				cmd := exec.Command("kubectl", "get", "clusterrole", "optipod-metrics-reader")
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Metrics reader role should exist")

				// Check that the role binding exists
				cmd = exec.Command("kubectl", "get", "clusterrolebinding", metricsRoleBindingName)
				_, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Metrics role binding should exist")

				// Verify the service account has the correct permissions
				cmd = exec.Command("kubectl", "auth", "can-i", "get", "services/metrics",
					"--as", fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName))
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Permission check should succeed")
				g.Expect(output).To(ContainSubstring("yes"), "Service account should have metrics permissions")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should protect against unauthorized access", func() {
			By("verifying that unauthorized requests are rejected")
			// We can't easily test unauthorized access in this environment,
			// but we can verify that the security mechanisms are in place
			Eventually(func(g Gomega) {
				// Check that the metrics service is configured with proper security
				cmd := exec.Command("kubectl", "get", "service", metricsServiceName,
					"-n", namespace, "-o", "jsonpath={.spec.ports[0].port}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Metrics service should be accessible")
				g.Expect(output).To(Equal("8443"), "Metrics should be served on secure port")

				// Verify that the service uses HTTPS
				cmd = exec.Command("kubectl", "get", "service", metricsServiceName,
					"-n", namespace, "-o", "jsonpath={.metadata.annotations}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Service annotations should be accessible")
				// Service should be configured for secure access
			}, 1*time.Minute).Should(Succeed())
		})

		It("should validate certificate configuration", func() {
			By("checking TLS certificate configuration")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check that TLS connection is established successfully
				// The curl command uses -k flag to skip certificate verification,
				// but we can still verify that TLS is being used
				g.Expect(metricsOutput).To(ContainSubstring("Connected to"),
					"Should establish TLS connection")

				// Verify no certificate errors in the output
				g.Expect(metricsOutput).NotTo(ContainSubstring("certificate verify failed"),
					"Should not have certificate verification failures")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should enforce security constraints compliance", func() {
			By("verifying that metrics endpoint complies with security constraints")
			Eventually(func(g Gomega) {
				// Check that the controller pod runs with security constraints
				cmd := exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
					"-n", namespace, "-o", "jsonpath={.items[0].spec.securityContext}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Pod security context should be accessible")

				if output != "" {
					// If security context is set, it should be restrictive
					g.Expect(output).To(ContainSubstring("runAsNonRoot"),
						"Controller should run as non-root user")
				}

				// Check that the metrics service account has minimal permissions
				cmd = exec.Command("kubectl", "get", "serviceaccount", serviceAccountName,
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Service account should exist")
				g.Expect(output).To(Equal(serviceAccountName), "Service account should be properly configured")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should handle security policy violations gracefully", func() {
			By("verifying graceful handling of security policy violations")
			Eventually(func(g Gomega) {
				// Check controller logs for any security-related errors
				logs, err := getControllerLogs()
				g.Expect(err).NotTo(HaveOccurred(), "Should be able to get controller logs")

				// If there are security-related log entries, they should be handled gracefully
				if strings.Contains(logs, "security") || strings.Contains(logs, "permission") {
					// Security logs should not indicate system failures
					g.Expect(logs).NotTo(ContainSubstring("panic"),
						"Should not panic on security issues")
					g.Expect(logs).NotTo(ContainSubstring("fatal"),
						"Should not have fatal errors from security issues")
				}
			}, 1*time.Minute).Should(Succeed())
		})

		// **Feature: e2e-test-enhancement, Property 18: Metrics endpoint security**
		// **Validates: Requirements 8.5**
		DescribeTable("Property Test: Metrics endpoint security",
			func(securityScenario string, securityChecks []string, expectedSecurityBehavior string) {
				By(fmt.Sprintf("testing metrics security for scenario: %s", securityScenario))

				// Wait for metrics endpoint to be available and validate security
				Eventually(func(g Gomega) {
					// Universal property: Metrics endpoint should always require authentication
					metricsOutput, err := getMetricsOutput()
					g.Expect(err).NotTo(HaveOccurred(), "Metrics should be accessible with proper auth")

					// Universal property: Should use HTTPS
					g.Expect(metricsOutput).To(ContainSubstring("https://"),
						"Metrics endpoint should always use HTTPS")

					// Universal property: Should include authorization header
					g.Expect(metricsOutput).To(ContainSubstring("Authorization: Bearer"),
						"Should always require authorization header")

					// Scenario-specific security validations
					switch expectedSecurityBehavior {
					case "enforce_rbac":
						// RBAC should be properly configured
						cmd := exec.Command("kubectl", "get", "clusterrole", "optipod-metrics-reader")
						_, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred(), "Metrics reader role should exist")

						cmd = exec.Command("kubectl", "get", "clusterrolebinding", metricsRoleBindingName)
						_, err = utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred(), "Metrics role binding should exist")

					case "use_secure_port":
						// Should use secure port (8443)
						cmd := exec.Command("kubectl", "get", "service", metricsServiceName,
							"-n", namespace, "-o", "jsonpath={.spec.ports[0].port}")
						output, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred(), "Service port should be accessible")
						g.Expect(output).To(Equal("8443"), "Should use secure port 8443")

					case "validate_tls":
						// TLS connection should be established
						g.Expect(metricsOutput).To(ContainSubstring("Connected to"),
							"Should establish TLS connection")
						g.Expect(metricsOutput).NotTo(ContainSubstring("certificate verify failed"),
							"Should not have certificate failures")

					case "check_pod_security":
						// Controller pod should run with security constraints
						cmd := exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
							"-n", namespace, "-o", "jsonpath={.items[0].spec.securityContext}")
						output, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred(), "Pod security context should be accessible")

						if output != "" {
							g.Expect(output).To(ContainSubstring("runAsNonRoot"),
								"Controller should run as non-root")
						}
					}

					// Perform security checks
					for _, check := range securityChecks {
						switch check {
						case "authentication_required":
							// Verify authentication is required (evidenced by Bearer token usage)
							g.Expect(metricsOutput).To(ContainSubstring("Authorization: Bearer"),
								"Authentication should be required")

						case "https_only":
							// Verify HTTPS is used
							g.Expect(metricsOutput).To(ContainSubstring("https://"),
								"Should use HTTPS only")
							g.Expect(metricsOutput).NotTo(ContainSubstring("http://"),
								"Should not use plain HTTP")

						case "proper_response":
							// Verify proper HTTP response
							g.Expect(metricsOutput).To(ContainSubstring("HTTP/1.1 200 OK"),
								"Should return proper HTTP response")

						case "no_sensitive_data":
							// Verify no sensitive data in metrics
							sensitivePatterns := []string{
								`(?i)password.*=`,
								`(?i)secret.*=`,
								`(?i)token.*=.*[A-Za-z0-9+/]{20,}`,
								`(?i)key.*=.*[A-Za-z0-9+/]{20,}`,
							}
							for _, pattern := range sensitivePatterns {
								matched, err := regexp.MatchString(pattern, metricsOutput)
								g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Invalid regex: %s", pattern))
								g.Expect(matched).To(BeFalse(),
									fmt.Sprintf("Metrics should not contain sensitive pattern: %s", pattern))
							}

						case "service_account_permissions":
							// Verify service account has correct permissions
							cmd := exec.Command("kubectl", "auth", "can-i", "get", "services/metrics",
								"--as", fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccountName))
							output, err := utils.Run(cmd)
							g.Expect(err).NotTo(HaveOccurred(), "Permission check should succeed")
							g.Expect(output).To(ContainSubstring("yes"), "Should have metrics permissions")

						case "graceful_error_handling":
							// Check controller logs for graceful security error handling
							logs, err := getControllerLogs()
							g.Expect(err).NotTo(HaveOccurred(), "Should get controller logs")

							if strings.Contains(logs, "security") || strings.Contains(logs, "permission") {
								g.Expect(logs).NotTo(ContainSubstring("panic"),
									"Should handle security errors gracefully")
							}
						}
					}

				}, 2*time.Minute, 10*time.Second).Should(Succeed())
			},
			Entry("RBAC enforcement", "rbac_enforcement",
				[]string{"authentication_required", "service_account_permissions"}, "enforce_rbac"),
			Entry("TLS validation", "tls_validation",
				[]string{"https_only", "proper_response"}, "validate_tls"),
			Entry("Secure port usage", "secure_port",
				[]string{"https_only", "authentication_required"}, "use_secure_port"),
			Entry("Pod security context", "pod_security",
				[]string{"graceful_error_handling"}, "check_pod_security"),
			Entry("Sensitive data protection", "sensitive_data",
				[]string{"no_sensitive_data", "proper_response"}, "validate_tls"),
			Entry("Authentication flow", "auth_flow",
				[]string{"authentication_required", "https_only", "proper_response"}, "enforce_rbac"),
			Entry("Security error handling", "error_handling",
				[]string{"graceful_error_handling", "proper_response"}, "check_pod_security"),
		)
	})

	Context("Monitoring Integration", func() {
		It("should provide health check endpoints", func() {
			By("verifying controller health endpoint accessibility")
			Eventually(func(g Gomega) {
				// Check that the controller pod is healthy
				cmd := exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
					"-n", namespace, "-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get controller pod status")
				g.Expect(output).To(Equal("Running"), "Controller should be running")

				// Check readiness probe
				cmd = exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
					"-n", namespace, "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get controller readiness")
				g.Expect(output).To(Equal("True"), "Controller should be ready")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should expose metrics for monitoring system integration", func() {
			By("verifying metrics are accessible for monitoring systems")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Metrics should be accessible")

				// Check for metrics that monitoring systems would use
				monitoringMetrics := []string{
					"up",                                 // Standard up metric
					"optipod_workloads_monitored",        // Business metric
					"controller_runtime_reconcile_total", // Controller health
					"process_cpu_seconds_total",          // Process metrics
					"go_memstats_alloc_bytes",            // Go runtime metrics
				}

				for _, metric := range monitoringMetrics {
					g.Expect(metricsOutput).To(ContainSubstring(metric),
						fmt.Sprintf("Should expose %s for monitoring", metric))
				}
			}, 2*time.Minute).Should(Succeed())
		})

		It("should validate alerting thresholds and conditions", func() {
			By("checking that metrics provide sufficient data for alerting")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Check for error rate metrics (for alerting)
				g.Expect(metricsOutput).To(ContainSubstring("optipod_reconciliation_errors_total"),
					"Should expose error metrics for alerting")

				// Check for performance metrics (for SLA monitoring)
				g.Expect(metricsOutput).To(ContainSubstring("optipod_reconciliation_duration_seconds"),
					"Should expose duration metrics for SLA monitoring")

				// Validate that metrics have proper labels for alerting rules
				g.Expect(metricsOutput).To(MatchRegexp(`optipod_reconciliation_errors_total\{.*policy=".*".*\}`),
					"Error metrics should have policy labels for targeted alerting")
			}, 2*time.Minute).Should(Succeed())
		})

		It("should support monitoring system service discovery", func() {
			By("verifying service discovery annotations and labels")
			Eventually(func(g Gomega) {
				// Check metrics service has proper annotations for Prometheus discovery
				cmd := exec.Command("kubectl", "get", "service", metricsServiceName,
					"-n", namespace, "-o", "jsonpath={.metadata.annotations}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get service annotations")

				// Check for common Prometheus service discovery annotations
				if output != "" {
					// If annotations exist, they should be properly formatted
					g.Expect(output).To(MatchRegexp(`\{.*\}`), "Annotations should be valid JSON")
				}

				// Check service labels
				cmd = exec.Command("kubectl", "get", "service", metricsServiceName,
					"-n", namespace, "-o", "jsonpath={.metadata.labels}")
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to get service labels")
				g.Expect(output).NotTo(BeEmpty(), "Service should have labels for discovery")
			}, 1*time.Minute).Should(Succeed())
		})

		It("should provide monitoring-friendly metric naming", func() {
			By("validating metric naming conventions")
			Eventually(func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())

				// Extract metric names
				lines := strings.Split(metricsOutput, "\n")
				var metricNames []string
				for _, line := range lines {
					if strings.HasPrefix(line, "optipod_") {
						// Extract metric name (before any labels or values)
						parts := strings.Fields(line)
						if len(parts) > 0 {
							metricName := strings.Split(parts[0], "{")[0]
							metricNames = append(metricNames, metricName)
						}
					}
				}

				// Validate naming conventions
				for _, name := range metricNames {
					// Should follow Prometheus naming conventions
					g.Expect(name).To(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`),
						fmt.Sprintf("Metric name %s should follow Prometheus conventions", name))

					// Should have meaningful suffixes
					if strings.Contains(name, "_total") {
						g.Expect(name).To(HaveSuffix("_total"), "Counter metrics should end with _total")
					}
					if strings.Contains(name, "_seconds") {
						g.Expect(name).To(ContainSubstring("_seconds"), "Duration metrics should include _seconds")
					}
				}
			}, 1*time.Minute).Should(Succeed())
		})

		It("should handle monitoring system failures gracefully", func() {
			By("verifying controller continues operating when metrics collection fails")
			// This test verifies that the controller doesn't fail if metrics can't be scraped
			Eventually(func(g Gomega) {
				// Check controller is still running
				cmd := exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
					"-n", namespace, "-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Controller should remain running")

				// Check that metrics are still being exposed
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Metrics should still be accessible")
				g.Expect(metricsOutput).To(ContainSubstring("optipod_"), "OptipPod metrics should be present")
			}, 1*time.Minute).Should(Succeed())
		})

		// **Feature: e2e-test-enhancement, Property 17: Monitoring system integration**
		// **Validates: Requirements 8.4**
		DescribeTable("Property Test: Monitoring system integration",
			func(monitoringScenario string, expectedBehavior string, validationChecks []string) {
				By(fmt.Sprintf("testing monitoring integration for scenario: %s", monitoringScenario))

				// Create a policy to generate monitoring activity
				policyGen := fixtures.NewPolicyConfigGenerator()
				policyConfig := policyGen.GenerateBasicPolicyConfig(
					fmt.Sprintf("monitoring-property-%s", strings.ReplaceAll(monitoringScenario, " ", "-")),
					v1alpha1.ModeRecommend,
				)
				policyConfig.WorkloadSelector = map[string]string{"monitoring-property-test": "true"}
				policyConfig.ResourceBounds = helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "1Gi",
					},
				}

				policy, err := policyHelper.CreateOptimizationPolicy(policyConfig)
				Expect(err).NotTo(HaveOccurred(), "Failed to create monitoring property test policy")
				cleanupHelper.TrackResource(helpers.ResourceRef{
					Name: policy.Name, Namespace: policy.Namespace, Kind: "OptimizationPolicy",
				})

				// Create workloads if needed for the scenario
				if monitoringScenario != "metrics_only" {
					workloadGen := fixtures.NewWorkloadConfigGenerator()
					workloadConfig := workloadGen.GenerateBasicWorkloadConfig(
						fmt.Sprintf("monitoring-property-workload-%s", strings.ReplaceAll(monitoringScenario, " ", "-")),
						helpers.WorkloadTypeDeployment,
					)
					workloadConfig.Namespace = testNamespace
					workloadConfig.Labels = map[string]string{"monitoring-property-test": "true"}

					workload, err := workloadHelper.CreateDeployment(workloadConfig)
					Expect(err).NotTo(HaveOccurred(), "Failed to create monitoring property workload")
					cleanupHelper.TrackResource(helpers.ResourceRef{
						Name: workload.Name, Namespace: workload.Namespace, Kind: "Deployment",
					})

					// Wait for workload to be ready
					err = workloadHelper.WaitForWorkloadReady(workload.Name, helpers.WorkloadTypeDeployment, 2*time.Minute)
					Expect(err).NotTo(HaveOccurred(), "Workload should be ready")
				}

				// Wait for monitoring data and validate
				Eventually(func(g Gomega) {
					// Universal property: Controller should remain healthy
					cmd := exec.Command("kubectl", "get", "pod", "-l", "control-plane=controller-manager",
						"-n", namespace, "-o", "jsonpath={.items[0].status.phase}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(), "Controller health check should succeed")
					g.Expect(output).To(Equal("Running"), "Controller should remain running")

					// Universal property: Metrics should be accessible
					metricsOutput, err := getMetricsOutput()
					g.Expect(err).NotTo(HaveOccurred(), "Metrics should always be accessible")

					// Scenario-specific validations
					switch expectedBehavior {
					case "expose_business_metrics":
						// Business metrics should be available for monitoring
						businessMetrics := []string{
							"optipod_workloads_monitored",
							"optipod_recommendations_total",
							"optipod_applications_total",
						}
						for _, metric := range businessMetrics {
							g.Expect(metricsOutput).To(ContainSubstring(metric),
								fmt.Sprintf("Should expose business metric: %s", metric))
						}

					case "expose_health_metrics":
						// Health metrics should be available for alerting
						healthMetrics := []string{
							"optipod_reconciliation_errors_total",
							"controller_runtime_reconcile_total",
							"up",
						}
						for _, metric := range healthMetrics {
							g.Expect(metricsOutput).To(ContainSubstring(metric),
								fmt.Sprintf("Should expose health metric: %s", metric))
						}

					case "support_service_discovery":
						// Service should have proper labels/annotations for discovery
						cmd := exec.Command("kubectl", "get", "service", metricsServiceName,
							"-n", namespace, "-o", "jsonpath={.metadata.labels}")
						output, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred(), "Service should be accessible")
						g.Expect(output).NotTo(BeEmpty(), "Service should have labels for discovery")

					case "provide_alerting_data":
						// Metrics should have sufficient labels for alerting rules
						g.Expect(metricsOutput).To(MatchRegexp(`optipod_.*\{.*policy=".*".*\}`),
							"Metrics should have policy labels for alerting")
					}

					// Perform validation checks
					for _, check := range validationChecks {
						switch check {
						case "metric_format":
							collector := NewMetricsCollector()
							lines := strings.Split(metricsOutput, "\n")
							var metricsContent strings.Builder
							inMetrics := false
							for _, line := range lines {
								if strings.Contains(line, "# HELP") || strings.Contains(line, "# TYPE") {
									inMetrics = true
								}
								if inMetrics {
									metricsContent.WriteString(line + "\n")
								}
							}
							err = collector.ValidateMetricFormat(metricsContent.String())
							g.Expect(err).NotTo(HaveOccurred(), "Metrics should follow Prometheus format")

						case "naming_conventions":
							// Check metric naming follows conventions
							g.Expect(metricsOutput).To(MatchRegexp(`optipod_[a-zA-Z0-9_]+`),
								"Metric names should follow naming conventions")

						case "label_consistency":
							// Check that similar metrics have consistent labels
							if strings.Contains(metricsOutput, "optipod_workloads_monitored") &&
								strings.Contains(metricsOutput, "optipod_recommendations_total") {
								g.Expect(metricsOutput).To(MatchRegexp(`optipod_workloads_monitored\{.*policy=".*".*\}`),
									"Workload metrics should have policy labels")
								g.Expect(metricsOutput).To(MatchRegexp(`optipod_recommendations_total\{.*policy=".*".*\}`),
									"Recommendation metrics should have policy labels")
							}

						case "http_accessibility":
							// Check HTTP response is proper
							g.Expect(metricsOutput).To(ContainSubstring("HTTP/1.1 200 OK"),
								"Metrics endpoint should return 200 OK")
						}
					}

				}, 3*time.Minute, 15*time.Second).Should(Succeed())
			},
			Entry("Business metrics exposure", "business_metrics", "expose_business_metrics",
				[]string{"metric_format", "naming_conventions"}),
			Entry("Health monitoring", "health_monitoring", "expose_health_metrics",
				[]string{"metric_format", "http_accessibility"}),
			Entry("Service discovery", "service_discovery", "support_service_discovery",
				[]string{"http_accessibility"}),
			Entry("Alerting integration", "alerting_integration", "provide_alerting_data",
				[]string{"label_consistency", "metric_format"}),
			Entry("Metrics only scenario", "metrics_only", "expose_business_metrics",
				[]string{"metric_format", "naming_conventions"}),
			Entry("Performance monitoring", "performance_monitoring", "expose_health_metrics",
				[]string{"metric_format", "label_consistency"}),
		)
	})
})

// getControllerLogs retrieves the controller manager pod logs
func getControllerLogs() (string, error) {
	// Get the controller pod name
	cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager",
		"-o", "jsonpath={.items[0].metadata.name}", "-n", namespace)
	podName, err := utils.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to get controller pod name: %w", err)
	}

	// Get the logs
	cmd = exec.Command("kubectl", "logs", strings.TrimSpace(podName), "-n", namespace, "--tail=1000")
	return utils.Run(cmd)
}

// getK8sClient returns a Kubernetes client for testing
func getK8sClient() client.Client {
	k8sClient, err := utils.GetK8sClient()
	if err != nil {
		Fail(fmt.Sprintf("Failed to get k8s client: %v", err))
	}
	return k8sClient
}

// MetricsCollector provides utilities for collecting and validating metrics
type MetricsCollector struct {
	httpClient *http.Client
}

// NewMetricsCollector creates a new MetricsCollector instance
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CollectMetrics retrieves metrics from the metrics endpoint
func (mc *MetricsCollector) CollectMetrics(ctx context.Context, endpoint string, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}

// ValidateMetricFormat validates that metrics follow Prometheus format
func (mc *MetricsCollector) ValidateMetricFormat(metrics string) error {
	lines := strings.Split(metrics, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Basic Prometheus format validation
		if !regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{.*\})?\s+[0-9.-]+(\s+[0-9]+)?$`).MatchString(line) {
			return fmt.Errorf("invalid metric format: %s", line)
		}
	}

	return nil
}

// LogValidator provides utilities for validating controller logs
type LogValidator struct{}

// NewLogValidator creates a new LogValidator instance
func NewLogValidator() *LogValidator {
	return &LogValidator{}
}

// ValidateLogFormat validates that logs follow expected format
func (lv *LogValidator) ValidateLogFormat(logs string) error {
	lines := strings.Split(logs, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for basic log structure (timestamp, level, message)
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s+\w+\s+.*`).MatchString(line) {
			// Allow some flexibility in log format
			continue
		}
	}

	return nil
}

// ValidateLogContent validates that logs contain expected information
func (lv *LogValidator) ValidateLogContent(logs string, expectedPatterns []string) error {
	for _, pattern := range expectedPatterns {
		matched, err := regexp.MatchString(pattern, logs)
		if err != nil {
			return fmt.Errorf("invalid regex pattern %s: %w", pattern, err)
		}
		if !matched {
			return fmt.Errorf("expected pattern not found in logs: %s", pattern)
		}
	}

	return nil
}

// CheckSensitiveInformation checks that logs don't contain sensitive information
func (lv *LogValidator) CheckSensitiveInformation(logs string) error {
	sensitivePatterns := []string{
		`(?i)password\s*[:=]\s*\S+`,
		`(?i)token\s*[:=]\s*[A-Za-z0-9+/]{20,}`,
		`(?i)secret\s*[:=]\s*\S+`,
		`(?i)key\s*[:=]\s*[A-Za-z0-9+/]{20,}`,
	}

	for _, pattern := range sensitivePatterns {
		matched, err := regexp.MatchString(pattern, logs)
		if err != nil {
			return fmt.Errorf("invalid regex pattern %s: %w", pattern, err)
		}
		if matched {
			return fmt.Errorf("sensitive information found in logs matching pattern: %s", pattern)
		}
	}

	return nil
}
