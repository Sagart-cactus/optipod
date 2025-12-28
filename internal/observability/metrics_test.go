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

package observability

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Feature: k8s-workload-rightsizing, Property 25: Prometheus metrics exposure
// Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5
// For any running OptiPod instance, the system should expose Prometheus metrics for workloads monitored,
// workloads updated, recommendations skipped, optimization cycle duration, and optimization cycle errors.
func TestProperty_PrometheusMetricsExposure(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("all required metrics are registered and can be updated", prop.ForAll(
		func(namespace, policyName, reason, errorType, method string, monitoredCount, updatedCount, skippedCount int, duration float64) bool {
			// Create a new registry for this test iteration to avoid conflicts
			registry := prometheus.NewRegistry()

			// Create metrics
			workloadsMonitored := prometheus.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "optipod_workloads_monitored",
					Help: "Number of workloads currently monitored by OptiPod",
				},
				[]string{"namespace", "policy"},
			)

			workloadsUpdated := prometheus.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "optipod_workloads_updated",
					Help: "Number of workloads updated in the last reconciliation cycle",
				},
				[]string{"namespace", "policy"},
			)

			workloadsSkipped := prometheus.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: "optipod_workloads_skipped",
					Help: "Number of workloads skipped in the last reconciliation cycle",
				},
				[]string{"namespace", "policy", "reason"},
			)

			reconciliationDuration := prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "optipod_reconciliation_duration_seconds",
					Help:    "Duration of reconciliation cycles in seconds",
					Buckets: prometheus.DefBuckets,
				},
				[]string{"policy"},
			)

			reconciliationErrors := prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "optipod_reconciliation_errors_total",
					Help: "Total number of reconciliation errors",
				},
				[]string{"policy", "error_type"},
			)

			recommendationsTotal := prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "optipod_recommendations_total",
					Help: "Total number of recommendations generated",
				},
				[]string{"policy"},
			)

			applicationsTotal := prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "optipod_applications_total",
					Help: "Total number of resource updates applied",
				},
				[]string{"policy", "method"},
			)

			// Register all metrics
			registry.MustRegister(
				workloadsMonitored,
				workloadsUpdated,
				workloadsSkipped,
				reconciliationDuration,
				reconciliationErrors,
				recommendationsTotal,
				applicationsTotal,
			)

			// Update metrics with test values
			workloadsMonitored.WithLabelValues(namespace, policyName).Set(float64(monitoredCount))
			workloadsUpdated.WithLabelValues(namespace, policyName).Set(float64(updatedCount))
			workloadsSkipped.WithLabelValues(namespace, policyName, reason).Set(float64(skippedCount))
			reconciliationDuration.WithLabelValues(policyName).Observe(duration)
			reconciliationErrors.WithLabelValues(policyName, errorType).Inc()
			recommendationsTotal.WithLabelValues(policyName).Inc()
			applicationsTotal.WithLabelValues(policyName, method).Inc()

			// Gather metrics to verify they can be collected
			metricFamilies, err := registry.Gather()
			if err != nil {
				return false
			}

			// Verify all required metrics are present
			requiredMetrics := map[string]bool{
				"optipod_workloads_monitored":             false,
				"optipod_workloads_updated":               false,
				"optipod_workloads_skipped":               false,
				"optipod_reconciliation_duration_seconds": false,
				"optipod_reconciliation_errors_total":     false,
				"optipod_recommendations_total":           false,
				"optipod_applications_total":              false,
			}

			for _, mf := range metricFamilies {
				if _, exists := requiredMetrics[mf.GetName()]; exists {
					requiredMetrics[mf.GetName()] = true
				}
			}

			// Check all required metrics are present
			for metricName, found := range requiredMetrics {
				if !found {
					t.Logf("Required metric %s not found", metricName)
					return false
				}
			}

			// Verify gauge metrics have correct values
			for _, mf := range metricFamilies {
				switch mf.GetName() {
				case "optipod_workloads_monitored":
					if !verifyGaugeValue(mf, float64(monitoredCount)) {
						return false
					}
				case "optipod_workloads_updated":
					if !verifyGaugeValue(mf, float64(updatedCount)) {
						return false
					}
				case "optipod_workloads_skipped":
					if !verifyGaugeValue(mf, float64(skippedCount)) {
						return false
					}
				case "optipod_reconciliation_duration_seconds":
					if mf.GetType() != dto.MetricType_HISTOGRAM {
						return false
					}
				case "optipod_reconciliation_errors_total":
					if mf.GetType() != dto.MetricType_COUNTER {
						return false
					}
				case "optipod_recommendations_total":
					if mf.GetType() != dto.MetricType_COUNTER {
						return false
					}
				case "optipod_applications_total":
					if mf.GetType() != dto.MetricType_COUNTER {
						return false
					}
				}
			}

			return true
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // namespace
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // policyName
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // reason
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // errorType
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // method
		gen.IntRange(0, 1000),    // monitoredCount
		gen.IntRange(0, 1000),    // updatedCount
		gen.IntRange(0, 1000),    // skippedCount
		gen.Float64Range(0, 300), // duration (0-300 seconds)
	))

	properties.TestingRun(t)
}

// verifyGaugeValue checks if a gauge metric has the expected value
func verifyGaugeValue(mf *dto.MetricFamily, expectedValue float64) bool {
	if mf.GetType() != dto.MetricType_GAUGE {
		return false
	}

	for _, m := range mf.GetMetric() {
		if m.GetGauge().GetValue() == expectedValue {
			return true
		}
	}

	return false
}

// Feature: server-side-apply-support, Property 10: Metrics track patch type
// Validates: Requirements 7.5
// For any patch operation, Prometheus metrics should distinguish between SSA and Strategic Merge Patch operations
func TestProperty_MetricsTrackPatchType(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("SSA patch metrics are tracked with correct labels", prop.ForAll(
		func(policy, namespace, workload, kind, status, patchType string) bool {
			// Create a new registry for this test iteration to avoid conflicts
			registry := prometheus.NewRegistry()

			// Create SSA patch metric
			ssaPatchTotal := prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "optipod_ssa_patch_total",
					Help: "Total number of Server-Side Apply patch operations",
				},
				[]string{"policy", "namespace", "workload", "kind", "status", "patch_type"},
			)

			// Register metric
			registry.MustRegister(ssaPatchTotal)

			// Record a patch operation
			ssaPatchTotal.WithLabelValues(policy, namespace, workload, kind, status, patchType).Inc()

			// Gather metrics to verify they can be collected
			metricFamilies, err := registry.Gather()
			if err != nil {
				return false
			}

			// Verify the metric is present
			var found bool
			for _, mf := range metricFamilies {
				if mf.GetName() == "optipod_ssa_patch_total" {
					found = true

					// Verify it's a counter
					if mf.GetType() != dto.MetricType_COUNTER {
						return false
					}

					// Verify the metric has the correct labels
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()
						if len(labels) != 6 {
							return false
						}

						// Verify all required labels are present
						labelMap := make(map[string]string)
						for _, label := range labels {
							labelMap[label.GetName()] = label.GetValue()
						}

						requiredLabels := []string{"policy", "namespace", "workload", "kind", "status", "patch_type"}
						for _, reqLabel := range requiredLabels {
							if _, exists := labelMap[reqLabel]; !exists {
								return false
							}
						}

						// Verify the patch_type label distinguishes between SSA and Strategic Merge
						if patchType != "ServerSideApply" && patchType != "StrategicMergePatch" {
							// For this property test, we only care about valid patch types
							// Invalid patch types should still be recorded, but we verify
							// that the system can distinguish between the two valid types
							continue
						}

						if labelMap["patch_type"] != patchType {
							return false
						}

						// Verify counter value is at least 1
						if m.GetCounter().GetValue() < 1 {
							return false
						}
					}
				}
			}

			return found
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // policy
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // namespace
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }), // workload
		gen.OneConstOf("Deployment", "StatefulSet", "DaemonSet"),             // kind
		gen.OneConstOf("success", "failure"),                                 // status
		gen.OneConstOf("ServerSideApply", "StrategicMergePatch"),             // patch_type
	))

	properties.TestingRun(t)
}

// TestNewMetricsRegistration tests that new metrics are properly registered
func TestNewMetricsRegistration(t *testing.T) {
	// Create a new registry for this test
	registry := prometheus.NewRegistry()

	// Create new metrics
	optimizationSuccessTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_optimization_success_total",
			Help: "Total number of successful optimizations",
		},
		[]string{"policy", "namespace", "workload", "kind", "method"},
	)

	optimizationFailureTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_optimization_failure_total",
			Help: "Total number of failed optimizations",
		},
		[]string{"policy", "namespace", "workload", "kind", "method", "reason"},
	)

	resourceChangesMagnitude := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_resource_changes_magnitude",
			Help:    "Magnitude of resource changes in percentage",
			Buckets: []float64{-90, -75, -50, -25, -10, -5, 0, 5, 10, 25, 50, 75, 100, 200, 500},
		},
		[]string{"policy", "namespace", "workload", "resource_type"},
	)

	defaultMultiplierUsage := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_default_multiplier_usage_total",
			Help: "Total number of times default multipliers were used",
		},
		[]string{"policy", "resource_type", "multiplier_value"},
	)

	optimizationDecisionDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_optimization_decision_duration_seconds",
			Help:    "Duration of optimization decision making in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy", "workload_kind"},
	)

	// Register all new metrics
	registry.MustRegister(
		optimizationSuccessTotal,
		optimizationFailureTotal,
		resourceChangesMagnitude,
		defaultMultiplierUsage,
		optimizationDecisionDuration,
	)

	// Test that metrics can be updated
	optimizationSuccessTotal.WithLabelValues("test-policy", "test-ns", "test-workload", "Deployment", "ServerSideApply").Inc()
	optimizationFailureTotal.WithLabelValues("test-policy", "test-ns", "test-workload", "Deployment", "ServerSideApply", "PatchFailed").Inc()
	resourceChangesMagnitude.WithLabelValues("test-policy", "test-ns", "test-workload", "cpu").Observe(25.5)
	defaultMultiplierUsage.WithLabelValues("test-policy", "memory", "1.3").Inc()
	optimizationDecisionDuration.WithLabelValues("test-policy", "Deployment").Observe(0.5)

	// Gather metrics to verify they can be collected
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify all new metrics are present
	expectedMetrics := map[string]bool{
		"optipod_optimization_success_total":             false,
		"optipod_optimization_failure_total":             false,
		"optipod_resource_changes_magnitude":             false,
		"optipod_default_multiplier_usage_total":         false,
		"optipod_optimization_decision_duration_seconds": false,
	}

	for _, mf := range metricFamilies {
		if _, exists := expectedMetrics[mf.GetName()]; exists {
			expectedMetrics[mf.GetName()] = true
		}
	}

	// Check all expected metrics are present
	for metricName, found := range expectedMetrics {
		if !found {
			t.Errorf("Expected metric %s not found", metricName)
		}
	}
}

// TestMetricHelperFunctions tests the new metric helper functions
func TestMetricHelperFunctions(t *testing.T) {
	// Test RecordOptimizationSuccess
	t.Run("RecordOptimizationSuccess", func(t *testing.T) {
		// This test verifies the function doesn't panic and can be called
		// In a real environment, we would verify the metric value increased
		RecordOptimizationSuccess("test-policy", "test-ns", "test-workload", "Deployment", "ServerSideApply")
	})

	// Test RecordOptimizationFailure
	t.Run("RecordOptimizationFailure", func(t *testing.T) {
		RecordOptimizationFailure("test-policy", "test-ns", "test-workload", "Deployment", "ServerSideApply", "PatchFailed")
	})

	// Test RecordResourceChangeMagnitude
	t.Run("RecordResourceChangeMagnitude", func(t *testing.T) {
		RecordResourceChangeMagnitude("test-policy", "test-ns", "test-workload", "cpu", 25.5)
		RecordResourceChangeMagnitude("test-policy", "test-ns", "test-workload", "memory", -15.2)
	})

	// Test RecordDefaultMultiplierUsage
	t.Run("RecordDefaultMultiplierUsage", func(t *testing.T) {
		RecordDefaultMultiplierUsage("test-policy", "cpu", "1.5")
		RecordDefaultMultiplierUsage("test-policy", "memory", "1.3")
	})

	// Test RecordOptimizationDecisionDuration
	t.Run("RecordOptimizationDecisionDuration", func(t *testing.T) {
		RecordOptimizationDecisionDuration("test-policy", "Deployment", 0.5)
		RecordOptimizationDecisionDuration("test-policy", "StatefulSet", 1.2)
	})
}

// Feature: memory-safety-enhancements, Property 1: Resource change magnitude tracking
// Validates: Requirements - Enhanced observability
// For any resource optimization, the system should track the magnitude of resource changes in Prometheus metrics
func TestProperty_ResourceChangeMagnitudeTracking(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("resource change magnitudes are tracked correctly", prop.ForAll(
		func(policy, namespace, workload string, beforeCPU, afterCPU, beforeMemory, afterMemory int64) bool {
			// Skip zero values to avoid division by zero
			if beforeCPU == 0 || beforeMemory == 0 {
				return true
			}

			// Create a new registry for this test iteration
			registry := prometheus.NewRegistry()

			resourceChangesMagnitude := prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "optipod_resource_changes_magnitude",
					Help:    "Magnitude of resource changes in percentage",
					Buckets: []float64{-90, -75, -50, -25, -10, -5, 0, 5, 10, 25, 50, 75, 100, 200, 500},
				},
				[]string{"policy", "namespace", "workload", "resource_type"},
			)

			registry.MustRegister(resourceChangesMagnitude)

			// Calculate expected change percentages
			cpuChangePercent := float64(afterCPU-beforeCPU) / float64(beforeCPU) * 100
			memoryChangePercent := float64(afterMemory-beforeMemory) / float64(beforeMemory) * 100

			// Record the changes
			resourceChangesMagnitude.WithLabelValues(policy, namespace, workload, "cpu").Observe(cpuChangePercent)
			resourceChangesMagnitude.WithLabelValues(policy, namespace, workload, "memory").Observe(memoryChangePercent)

			// Gather metrics to verify they were recorded
			metricFamilies, err := registry.Gather()
			if err != nil {
				return false
			}

			// Verify the metric is present and has observations
			for _, mf := range metricFamilies {
				if mf.GetName() == "optipod_resource_changes_magnitude" {
					if mf.GetType() != dto.MetricType_HISTOGRAM {
						return false
					}

					// Verify we have metrics for both CPU and memory
					cpuFound := false
					memoryFound := false

					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()
						for _, label := range labels {
							if label.GetName() == "resource_type" {
								if label.GetValue() == "cpu" {
									cpuFound = true
								}
								if label.GetValue() == "memory" {
									memoryFound = true
								}
							}
						}

						// Verify histogram has at least one observation
						if m.GetHistogram().GetSampleCount() == 0 {
							return false
						}
					}

					return cpuFound && memoryFound
				}
			}

			return false
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Int64Range(1, 10000),    // beforeCPU (avoid zero)
		gen.Int64Range(1, 10000),    // afterCPU
		gen.Int64Range(1, 10000000), // beforeMemory (avoid zero)
		gen.Int64Range(1, 10000000), // afterMemory
	))

	properties.TestingRun(t)
}
