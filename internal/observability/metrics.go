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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// WorkloadsMonitored tracks the number of workloads currently monitored by OptiPod
	WorkloadsMonitored = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_monitored",
			Help: "Number of workloads currently monitored by OptiPod",
		},
		[]string{"namespace", "policy"},
	)

	// WorkloadsUpdated tracks the number of workloads updated in the last reconciliation cycle
	WorkloadsUpdated = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_updated",
			Help: "Number of workloads updated in the last reconciliation cycle",
		},
		[]string{"namespace", "policy"},
	)

	// WorkloadsSkipped tracks the number of workloads skipped with reasons
	WorkloadsSkipped = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "optipod_workloads_skipped",
			Help: "Number of workloads skipped in the last reconciliation cycle",
		},
		[]string{"namespace", "policy", "reason"},
	)

	// ReconciliationDuration tracks the duration of reconciliation cycles
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_reconciliation_duration_seconds",
			Help:    "Duration of reconciliation cycles in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy"},
	)

	// MetricsCollectionDuration tracks the duration of metrics collection operations
	MetricsCollectionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "optipod_metrics_collection_duration_seconds",
			Help:    "Duration of metrics collection operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider"},
	)

	// ReconciliationErrors tracks errors during reconciliation
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"policy", "error_type"},
	)

	// RecommendationsTotal tracks the total number of recommendations generated
	RecommendationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_recommendations_total",
			Help: "Total number of recommendations generated",
		},
		[]string{"policy"},
	)

	// ApplicationsTotal tracks the total number of applications (updates) performed
	ApplicationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "optipod_applications_total",
			Help: "Total number of resource updates applied",
		},
		[]string{"policy", "method"},
	)
)

func init() {
	// Register metrics automatically when the package is imported
	// This ensures metrics work in both production and test environments
	RegisterMetrics()
}

// RegisterMetrics registers all OptiPod metrics with the controller-runtime metrics registry
func RegisterMetrics() {
	// Use Register instead of MustRegister to avoid panics if metrics are already registered
	metrics.Registry.Register(WorkloadsMonitored)
	metrics.Registry.Register(WorkloadsUpdated)
	metrics.Registry.Register(WorkloadsSkipped)
	metrics.Registry.Register(ReconciliationDuration)
	metrics.Registry.Register(MetricsCollectionDuration)
	metrics.Registry.Register(ReconciliationErrors)
	metrics.Registry.Register(RecommendationsTotal)
	metrics.Registry.Register(ApplicationsTotal)
}
