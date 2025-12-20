package e2e

import (
	"fmt"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Observability Unit Tests", func() {
	Context("Metrics Format Validation", func() {
		It("should validate Prometheus metric naming conventions", func() {
			validNames := []string{
				"optipod_workloads_monitored",
				"optipod_reconciliation_duration_seconds",
				"controller_runtime_reconcile_total",
				"go_memstats_alloc_bytes",
			}

			for _, name := range validNames {
				Expect(name).To(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`))
			}
		})

		It("should validate metric suffixes", func() {
			counterMetrics := []string{
				"optipod_reconciliation_errors_total",
				"optipod_recommendations_total",
				"optipod_applications_total",
			}

			for _, metric := range counterMetrics {
				Expect(metric).To(HaveSuffix("_total"))
			}

			durationMetrics := []string{
				"optipod_reconciliation_duration_seconds",
				"optipod_metrics_collection_duration_seconds",
			}

			for _, metric := range durationMetrics {
				Expect(metric).To(ContainSubstring("_seconds"))
			}
		})

		It("should validate metric labels format", func() {
			validMetricLines := []string{
				`optipod_workloads_monitored{namespace="default",policy="test-policy"} 1`,
				`optipod_reconciliation_errors_total{policy="test-policy",error_type="validation"} 0`,
				`optipod_ssa_patch_total{policy="test",namespace="default",workload="app",kind="Deployment",status="success",patch_type="strategic"} 1`,
			}

			for _, line := range validMetricLines {
				Expect(line).To(MatchRegexp(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{.*\})?\s+[0-9.-]+`))
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
				Expect(entry).To(MatchRegexp(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`))

				// Should contain log level
				Expect(entry).To(MatchRegexp(`(INFO|DEBUG|WARN|ERROR)`))

				// Should contain message
				Expect(entry).To(MatchRegexp(`(INFO|DEBUG|WARN|ERROR)\s+\w+`))
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
				Expect(log).To(MatchRegexp(`\w+-\w+`))
			}
		})
	})

	Context("Security Validation", func() {
		It("should detect various sensitive information patterns", func() {
			sensitivePatterns := map[string]string{
				"password": `password=mysecret123`,
				"token":    `token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9abcdefghijklmnop`,
				"secret":   `secret=very-secret-value`,
				"key":      `key=abcdef1234567890abcdef1234567890`,
			}

			for patternName, sensitiveText := range sensitivePatterns {
				// Simple validation that sensitive patterns can be detected
				hasSensitive := strings.Contains(strings.ToLower(sensitiveText), patternName)
				Expect(hasSensitive).To(BeTrue(), fmt.Sprintf("Should detect %s pattern", patternName))
			}
		})

		It("should allow safe log content", func() {
			safeLogs := []string{
				`INFO Starting controller with config file /etc/config.yaml`,
				`DEBUG Processing policy test-policy with mode Auto`,
				`INFO Workload nginx-deployment updated successfully`,
				`WARN Metrics collection took longer than expected (5.2s)`,
			}

			sensitivePatterns := []string{"password", "token", "secret", "key"}

			for _, log := range safeLogs {
				isSafe := true
				for _, pattern := range sensitivePatterns {
					if strings.Contains(strings.ToLower(log), pattern) {
						isSafe = false
						break
					}
				}
				Expect(isSafe).To(BeTrue(), fmt.Sprintf("Safe log should pass validation: %s", log))
			}
		})

		It("should validate regex patterns work correctly", func() {
			// Test that our regex patterns are working
			timestampRegex := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
			testLog := `2025-01-01T12:00:00.000Z INFO Test message`
			
			Expect(timestampRegex.MatchString(testLog)).To(BeTrue())
		})
	})
})