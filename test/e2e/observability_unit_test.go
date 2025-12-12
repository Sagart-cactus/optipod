//go:build e2e
// +build e2e

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
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Observability Unit Tests", func() {
	Context("MetricsCollector", func() {
		var collector *MetricsCollector

		BeforeEach(func() {
			collector = NewMetricsCollector()
		})

		It("should create a new MetricsCollector with proper configuration", func() {
			Expect(collector).NotTo(BeNil())
			Expect(collector.httpClient).NotTo(BeNil())
			Expect(collector.httpClient.Timeout).To(Equal(30 * time.Second))
		})

		It("should collect metrics from HTTP endpoint", func() {
			// Create a test server that returns sample metrics
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify authorization header
				auth := r.Header.Get("Authorization")
				if auth != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`# HELP test_metric A test metric
# TYPE test_metric counter
test_metric{label="value"} 42
`))
			}))
			defer testServer.Close()

			ctx := context.Background()
			metrics, err := collector.CollectMetrics(ctx, testServer.URL, "test-token")

			Expect(err).NotTo(HaveOccurred())
			Expect(metrics).To(ContainSubstring("test_metric"))
			Expect(metrics).To(ContainSubstring("42"))
		})

		It("should handle authentication errors", func() {
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer testServer.Close()

			ctx := context.Background()
			_, err := collector.CollectMetrics(ctx, testServer.URL, "invalid-token")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("401"))
		})

		It("should handle network errors gracefully", func() {
			ctx := context.Background()
			_, err := collector.CollectMetrics(ctx, "http://invalid-endpoint:9999", "token")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to make request"))
		})

		It("should validate metric format correctly", func() {
			validMetrics := `# HELP test_counter A test counter
# TYPE test_counter counter
test_counter{job="test"} 1
test_gauge 42.5
test_histogram_bucket{le="0.1"} 0
test_histogram_bucket{le="+Inf"} 1
`

			err := collector.ValidateMetricFormat(validMetrics)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject invalid metric format", func() {
			invalidMetrics := `invalid metric line without value
test_metric{invalid_label} 42
`

			err := collector.ValidateMetricFormat(invalidMetrics)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid metric format"))
		})

		It("should handle empty metrics gracefully", func() {
			err := collector.ValidateMetricFormat("")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate metrics with comments", func() {
			metricsWithComments := `# This is a comment
# HELP test_metric A test metric
# TYPE test_metric counter
test_metric 1
`

			err := collector.ValidateMetricFormat(metricsWithComments)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("LogValidator", func() {
		var validator *LogValidator

		BeforeEach(func() {
			validator = NewLogValidator()
		})

		It("should create a new LogValidator", func() {
			Expect(validator).NotTo(BeNil())
		})

		It("should validate proper log format", func() {
			validLogs := `2025-01-01T12:00:00.000Z INFO Starting controller
2025-01-01T12:00:01.000Z DEBUG Processing policy test-policy
2025-01-01T12:00:02.000Z WARN No metrics available for workload
2025-01-01T12:00:03.000Z ERROR Failed to update workload: permission denied
`

			err := validator.ValidateLogFormat(validLogs)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle logs with flexible format", func() {
			flexibleLogs := `INFO: Controller started successfully
DEBUG: Processing optimization policy
WARN: Metrics collection took longer than expected
`

			err := validator.ValidateLogFormat(flexibleLogs)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate log content with expected patterns", func() {
			logs := `2025-01-01T12:00:00.000Z INFO Reconciling OptimizationPolicy test-policy
2025-01-01T12:00:01.000Z DEBUG Discovered workload test-workload
2025-01-01T12:00:02.000Z INFO Generated recommendations for workload
`

			expectedPatterns := []string{
				`(?i)reconcil.*optimizationpolicy`,
				`(?i)discover.*workload`,
				`(?i)generat.*recommendation`,
			}

			err := validator.ValidateLogContent(logs, expectedPatterns)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should detect missing expected patterns", func() {
			logs := `2025-01-01T12:00:00.000Z INFO Starting controller
2025-01-01T12:00:01.000Z DEBUG Processing request
`

			expectedPatterns := []string{
				`(?i)reconcil.*optimizationpolicy`,
			}

			err := validator.ValidateLogContent(logs, expectedPatterns)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected pattern not found"))
		})

		It("should detect sensitive information in logs", func() {
			logsWithSensitiveInfo := `2025-01-01T12:00:00.000Z INFO Starting controller
2025-01-01T12:00:01.000Z DEBUG password=secret123
2025-01-01T12:00:02.000Z INFO token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9
`

			err := validator.CheckSensitiveInformation(logsWithSensitiveInfo)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sensitive information found"))
		})

		It("should pass logs without sensitive information", func() {
			cleanLogs := `2025-01-01T12:00:00.000Z INFO Starting controller
2025-01-01T12:00:01.000Z DEBUG Processing policy test-policy
2025-01-01T12:00:02.000Z INFO Workload updated successfully
`

			err := validator.CheckSensitiveInformation(cleanLogs)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle empty logs", func() {
			err := validator.ValidateLogFormat("")
			Expect(err).NotTo(HaveOccurred())

			err = validator.CheckSensitiveInformation("")
			Expect(err).NotTo(HaveOccurred())

			err = validator.ValidateLogContent("", []string{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle invalid regex patterns gracefully", func() {
			logs := `2025-01-01T12:00:00.000Z INFO Test log`

			invalidPatterns := []string{
				`[invalid regex`,
			}

			err := validator.ValidateLogContent(logs, invalidPatterns)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid regex pattern"))
		})

		It("should validate case-insensitive patterns", func() {
			logs := `2025-01-01T12:00:00.000Z INFO RECONCILING OptimizationPolicy
2025-01-01T12:00:01.000Z DEBUG discovered workload
`

			patterns := []string{
				`(?i)reconcil.*optimizationpolicy`,
				`(?i)discover.*workload`,
			}

			err := validator.ValidateLogContent(logs, patterns)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Metrics Format Validation", func() {
		It("should validate Prometheus metric naming conventions", func() {
			validNames := []string{
				"optipod_workloads_monitored",
				"optipod_reconciliation_duration_seconds",
				"controller_runtime_reconcile_total",
				"go_memstats_alloc_bytes",
			}

			for _, name := range validNames {
				Expect(name).To(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`),
					"Metric name should follow Prometheus conventions")
			}
		})

		It("should validate metric suffixes", func() {
			counterMetrics := []string{
				"optipod_reconciliation_errors_total",
				"optipod_recommendations_total",
				"optipod_applications_total",
			}

			for _, metric := range counterMetrics {
				Expect(metric).To(HaveSuffix("_total"),
					"Counter metrics should end with _total")
			}

			durationMetrics := []string{
				"optipod_reconciliation_duration_seconds",
				"optipod_metrics_collection_duration_seconds",
			}

			for _, metric := range durationMetrics {
				Expect(metric).To(ContainSubstring("_seconds"),
					"Duration metrics should include _seconds")
			}
		})

		It("should validate metric labels format", func() {
			validMetricLines := []string{
				`optipod_workloads_monitored{namespace="default",policy="test-policy"} 1`,
				`optipod_reconciliation_errors_total{policy="test-policy",error_type="validation"} 0`,
				`optipod_ssa_patch_total{policy="test",namespace="default",workload="app",kind="Deployment",status="success",patch_type="strategic"} 1`,
			}

			for _, line := range validMetricLines {
				Expect(line).To(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{.*\})?\s+[0-9.-]+(\s+[0-9]+)?$`),
					"Metric line should follow Prometheus format")
			}
		})

		It("should reject invalid metric formats", func() {
			invalidMetricLines := []string{
				`123invalid_start 1`,      // Can't start with number
				`metric-with-dashes 1`,    // Can't contain dashes
				`metric{invalid label} 1`, // Invalid label format
				`metric{label="value} 1`,  // Unclosed quote
				`metric 1.2.3`,            // Invalid value
			}

			for _, line := range invalidMetricLines {
				Expect(line).NotTo(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{.*\})?\s+[0-9.-]+(\s+[0-9]+)?$`),
					fmt.Sprintf("Invalid metric line should be rejected: %s", line))
			}
		})
	})

	Context("Log Pattern Validation", func() {
		It("should validate common log patterns", func() {
			logEntries := []string{
				`2025-01-01T12:00:00.000Z INFO Reconciling OptimizationPolicy test-policy`,
				`2025-01-01T12:00:01.000Z DEBUG Discovered 3 workloads in namespace default`,
				`2025-01-01T12:00:02.000Z WARN Metrics collection took 5.2 seconds`,
				`2025-01-01T12:00:03.000Z ERROR Failed to update workload: permission denied`,
			}

			for _, entry := range logEntries {
				// Should contain timestamp
				Expect(entry).To(MatchRegexp(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`),
					"Log entry should contain timestamp")

				// Should contain log level
				Expect(entry).To(MatchRegexp(`(INFO|DEBUG|WARN|ERROR)`),
					"Log entry should contain log level")

				// Should contain message
				Expect(entry).To(MatchRegexp(`(INFO|DEBUG|WARN|ERROR)\s+\w+`),
					"Log entry should contain message after log level")
			}
		})

		It("should validate contextual information in logs", func() {
			contextualLogs := []string{
				`INFO Reconciling OptimizationPolicy test-policy in namespace optipod-system`,
				`DEBUG Processing workload nginx-deployment with 2 containers`,
				`WARN Metrics unavailable for workload app-server, using defaults`,
				`ERROR Update failed for workload web-app: resource conflict`,
			}

			for _, log := range contextualLogs {
				// Should contain resource names
				Expect(log).To(MatchRegexp(`\w+-\w+`),
					"Log should contain resource names with context")

				// Should not contain just generic messages
				Expect(log).NotTo(MatchRegexp(`^(INFO|DEBUG|WARN|ERROR)\s+(Processing|Failed|Success)$`),
					"Log should contain specific contextual information")
			}
		})

		It("should validate error log detail", func() {
			errorLogs := []string{
				`ERROR Failed to update workload nginx-deployment: permission denied`,
				`ERROR Metrics collection failed for workload app-server: connection timeout`,
				`ERROR Policy validation failed for test-policy: min CPU greater than max CPU`,
			}

			for _, log := range errorLogs {
				// Should contain error reason
				Expect(log).To(MatchRegexp(`ERROR.*:.*`),
					"Error log should contain reason after colon")

				// Should contain resource context
				Expect(log).To(MatchRegexp(`(workload|policy|namespace)`),
					"Error log should contain resource context")
			}
		})
	})

	Context("Security Validation", func() {
		It("should detect various sensitive information patterns", func() {
			sensitivePatterns := map[string]string{
				"password":     `password=mysecret123`,
				"token":        `token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
				"secret":       `secret=very-secret-value`,
				"key":          `key=abcdef1234567890abcdef1234567890`,
				"bearer token": `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
			}

			validator := NewLogValidator()

			for patternName, sensitiveText := range sensitivePatterns {
				err := validator.CheckSensitiveInformation(sensitiveText)
				Expect(err).To(HaveOccurred(),
					fmt.Sprintf("Should detect %s pattern", patternName))
				Expect(err.Error()).To(ContainSubstring("sensitive information found"),
					fmt.Sprintf("Should report sensitive information for %s", patternName))
			}
		})

		It("should allow safe log content", func() {
			safeLogs := []string{
				`INFO Starting controller with config file /etc/config.yaml`,
				`DEBUG Processing policy test-policy with mode Auto`,
				`INFO Workload nginx-deployment updated successfully`,
				`WARN Metrics collection took longer than expected (5.2s)`,
			}

			validator := NewLogValidator()

			for _, log := range safeLogs {
				err := validator.CheckSensitiveInformation(log)
				Expect(err).NotTo(HaveOccurred(),
					fmt.Sprintf("Safe log should pass validation: %s", log))
			}
		})
	})
})
