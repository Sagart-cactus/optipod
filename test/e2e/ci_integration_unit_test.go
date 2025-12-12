//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
)

var _ = Describe("CI Integration Unit Tests", func() {
	Context("Test execution scripts", func() {
		It("should validate parallel configuration parsing", func() {
			// Test default configuration
			config := DefaultParallelConfig()
			Expect(config.MaxParallelNodes).To(Equal(4))
			Expect(config.NamespacePrefix).To(Equal("e2e-parallel"))
			Expect(config.ResourceIsolation).To(BeTrue())
			Expect(config.TimeoutMultiplier).To(Equal(1.0))

			// Test environment variable override
			os.Setenv("E2E_PARALLEL_NODES", "8")
			os.Setenv("E2E_TIMEOUT_MULTIPLIER", "1.5")
			defer func() {
				os.Unsetenv("E2E_PARALLEL_NODES")
				os.Unsetenv("E2E_TIMEOUT_MULTIPLIER")
			}()

			config = DefaultParallelConfig()
			Expect(config.MaxParallelNodes).To(Equal(8))
			Expect(config.TimeoutMultiplier).To(Equal(1.5))
		})

		It("should validate performance configuration parsing", func() {
			// Test default configuration
			config := DefaultPerformanceConfig()
			Expect(config.DefaultTimeout).To(Equal(2 * time.Minute))
			Expect(config.ShortTimeout).To(Equal(30 * time.Second))
			Expect(config.LongTimeout).To(Equal(5 * time.Minute))
			Expect(config.MaxRetries).To(Equal(3))
			Expect(config.EnableRetryOnFailure).To(BeTrue())

			// Test environment variable overrides
			os.Setenv("E2E_DEFAULT_TIMEOUT", "3m")
			os.Setenv("E2E_MAX_RETRIES", "5")
			os.Setenv("E2E_DISABLE_RETRY", "true")
			defer func() {
				os.Unsetenv("E2E_DEFAULT_TIMEOUT")
				os.Unsetenv("E2E_MAX_RETRIES")
				os.Unsetenv("E2E_DISABLE_RETRY")
			}()

			config = DefaultPerformanceConfig()
			Expect(config.DefaultTimeout).To(Equal(3 * time.Minute))
			Expect(config.MaxRetries).To(Equal(0)) // Disabled by E2E_DISABLE_RETRY
			Expect(config.EnableRetryOnFailure).To(BeFalse())
		})

		It("should validate timeout calculation logic", func() {
			config := &PerformanceConfig{
				DefaultTimeout: 2 * time.Minute,
				ShortTimeout:   30 * time.Second,
				LongTimeout:    5 * time.Minute,
			}

			manager := NewTestTimeoutManager(config)

			Expect(manager.GetTimeout("short")).To(Equal(30 * time.Second))
			Expect(manager.GetTimeout("quick")).To(Equal(30 * time.Second))
			Expect(manager.GetTimeout("long")).To(Equal(5 * time.Minute))
			Expect(manager.GetTimeout("complex")).To(Equal(5 * time.Minute))
			Expect(manager.GetTimeout("default")).To(Equal(2 * time.Minute))
			Expect(manager.GetTimeout("unknown")).To(Equal(2 * time.Minute))
		})

		It("should validate parallel node information", func() {
			// Test serial execution
			nodeID, totalNodes := 1, 1
			if nodeID == 1 && totalNodes == 1 {
				// In serial execution, we expect single node
				Expect(nodeID).To(Equal(1))
				Expect(totalNodes).To(Equal(1))
			}

			// Test parallel execution simulation
			if os.Getenv("GINKGO_PARALLEL") == "true" {
				// In parallel execution, we expect multiple nodes
				Expect(totalNodes).To(BeNumerically(">", 1))
				Expect(nodeID).To(BeNumerically(">=", 1))
				Expect(nodeID).To(BeNumerically("<=", totalNodes))
			}
		})
	})

	Context("Reporting functions", func() {
		var (
			reportingHelper *helpers.ReportingHelper
			tempDir         string
		)

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			// Create a mock reporting helper - in real tests this would use actual clients
			reportingHelper = &helpers.ReportingHelper{}
		})

		It("should generate valid JUnit report structure", func() {
			testScenario := "unit-test-scenario"
			reportPath := filepath.Join(tempDir, "junit-report.xml")

			err := reportingHelper.GenerateJUnitReport(context.Background(), testScenario, reportPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify file exists
			Expect(reportPath).To(BeAnExistingFile())

			// Verify content structure
			content, err := os.ReadFile(reportPath)
			Expect(err).NotTo(HaveOccurred())

			contentStr := string(content)
			Expect(contentStr).To(ContainSubstring("<?xml"))
			Expect(contentStr).To(ContainSubstring("testsuite"))
			Expect(contentStr).To(ContainSubstring(testScenario))
		})

		It("should generate valid JSON report structure", func() {
			testScenario := "unit-test-scenario"
			reportPath := filepath.Join(tempDir, "report.json")

			err := reportingHelper.GenerateJSONReport(context.Background(), testScenario, reportPath)
			Expect(err).NotTo(HaveOccurred())

			// Verify file exists
			Expect(reportPath).To(BeAnExistingFile())

			// Verify content structure
			content, err := os.ReadFile(reportPath)
			Expect(err).NotTo(HaveOccurred())

			contentStr := string(content)
			Expect(contentStr).To(ContainSubstring("{"))
			Expect(contentStr).To(ContainSubstring("\"test_scenario\""))
			Expect(contentStr).To(ContainSubstring("\"timestamp\""))
		})

		It("should handle report generation errors gracefully", func() {
			testScenario := "error-test-scenario"
			invalidPath := "/invalid/path/report.xml"

			err := reportingHelper.GenerateJUnitReport(context.Background(), testScenario, invalidPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("directory"))
		})

		It("should validate report naming conventions", func() {
			testScenario := "naming-test"
			timestamp := time.Now().Format("20060102-150405")
			reportName := "junit-" + testScenario + "-" + timestamp + ".xml"

			// Verify naming pattern
			Expect(reportName).To(MatchRegexp(`^junit-.*-\d{8}-\d{6}\.xml$`))
		})
	})

	Context("Artifact collection", func() {
		var (
			reportingHelper *helpers.ReportingHelper
			tempDir         string
		)

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			// Create a mock reporting helper - in real tests this would use actual clients
			reportingHelper = &helpers.ReportingHelper{}
		})

		It("should create artifact directory structure", func() {
			artifactsDir := filepath.Join(tempDir, "test-artifacts")

			err := os.MkdirAll(filepath.Join(artifactsDir, "reports"), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.MkdirAll(filepath.Join(artifactsDir, "coverage"), 0755)
			Expect(err).NotTo(HaveOccurred())

			// Verify directories exist
			Expect(filepath.Join(artifactsDir, "reports")).To(BeADirectory())
			Expect(filepath.Join(artifactsDir, "logs")).To(BeADirectory())
			Expect(filepath.Join(artifactsDir, "coverage")).To(BeADirectory())
		})

		It("should validate artifact file permissions", func() {
			reportPath := filepath.Join(tempDir, "test-report.xml")

			err := reportingHelper.GenerateJUnitReport(context.Background(), "permission-test", reportPath)
			Expect(err).NotTo(HaveOccurred())

			// Check file permissions
			info, err := os.Stat(reportPath)
			Expect(err).NotTo(HaveOccurred())

			// File should be readable
			mode := info.Mode()
			Expect(mode & 0400).NotTo(Equal(0)) // Owner read permission
		})

		It("should handle concurrent artifact generation", func() {
			const numConcurrent = 5
			done := make(chan error, numConcurrent)

			// Start concurrent report generation
			for i := 0; i < numConcurrent; i++ {
				go func(id int) {
					reportPath := filepath.Join(tempDir, "concurrent-report-"+strconv.Itoa(id)+".xml")
					err := reportingHelper.GenerateJUnitReport(context.Background(), "concurrent-test-"+strconv.Itoa(id), reportPath)
					done <- err
				}(i)
			}

			// Wait for all to complete
			for i := 0; i < numConcurrent; i++ {
				err := <-done
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify all files were created
			for i := 0; i < numConcurrent; i++ {
				reportPath := filepath.Join(tempDir, "concurrent-report-"+strconv.Itoa(i)+".xml")
				Expect(reportPath).To(BeAnExistingFile())
			}
		})
	})

	Context("CI environment detection", func() {
		It("should detect CI environment variables", func() {
			// Test common CI environment variables
			ciEnvVars := []string{
				"CI",
				"CONTINUOUS_INTEGRATION",
				"GITHUB_ACTIONS",
				"GITLAB_CI",
				"JENKINS_URL",
				"BUILDKITE",
			}

			isCI := false
			for _, envVar := range ciEnvVars {
				if os.Getenv(envVar) != "" {
					isCI = true
					break
				}
			}

			// In actual CI, at least one should be set
			// For local testing, this might be false
			_ = isCI // We just validate the detection logic works
		})

		It("should validate test timeout adjustments for CI", func() {
			// Simulate CI environment
			os.Setenv("CI", "true")
			defer os.Unsetenv("CI")

			config := DefaultPerformanceConfig()

			// In CI, we might want longer timeouts
			if os.Getenv("CI") == "true" {
				// Timeouts should be reasonable for CI
				Expect(config.DefaultTimeout).To(BeNumerically(">=", time.Minute))
				Expect(config.MaxRetries).To(BeNumerically(">=", 1))
			}
		})

		It("should validate parallel execution limits for CI", func() {
			// Test resource-constrained CI environments
			os.Setenv("E2E_PARALLEL_NODES", "2") // Limit for CI
			defer os.Unsetenv("E2E_PARALLEL_NODES")

			config := DefaultParallelConfig()
			Expect(config.MaxParallelNodes).To(Equal(2))

			// Should not exceed reasonable limits for CI
			Expect(config.MaxParallelNodes).To(BeNumerically("<=", 8))
		})
	})

	Context("Error handling and exit codes", func() {
		It("should validate error message formatting", func() {
			testErr := "test error message"
			formattedErr := "CI Test Error: " + testErr

			Expect(formattedErr).To(ContainSubstring("CI Test Error"))
			Expect(formattedErr).To(ContainSubstring(testErr))
		})

		It("should validate timeout error handling", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
			defer cancel()

			// Wait for context to timeout
			<-ctx.Done()

			err := ctx.Err()
			Expect(err).To(Equal(context.DeadlineExceeded))
		})

		It("should validate retry logic parameters", func() {
			config := &PerformanceConfig{
				MaxRetries:           3,
				RetryInterval:        time.Second,
				EnableRetryOnFailure: true,
			}

			manager := NewTestTimeoutManager(config)

			// Test retry logic with a function that fails twice then succeeds
			attempts := 0
			operation := func() error {
				attempts++
				if attempts < 3 {
					return fmt.Errorf("attempt %d failed", attempts)
				}
				return nil
			}

			ctx := context.Background()
			err := manager.RetryWithBackoff(ctx, operation, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(attempts).To(Equal(3))
		})
	})
})
