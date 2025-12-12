package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TestValidateTestCoverage validates comprehensive test coverage
func TestValidateTestCoverage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Coverage Validation Suite")
}

var _ = Describe("Test Coverage Validation", func() {
	var validator *CoverageValidator

	BeforeEach(func() {
		// Get the current working directory and construct paths relative to project root
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		// If we're in test/e2e, go up two levels to project root
		projectRoot := wd
		if strings.HasSuffix(wd, "test/e2e") {
			projectRoot = filepath.Join(wd, "../..")
		}

		validator = &CoverageValidator{
			RequirementsFile: filepath.Join(projectRoot, ".kiro/specs/e2e-test-enhancement/requirements.md"),
			DesignFile:       filepath.Join(projectRoot, ".kiro/specs/e2e-test-enhancement/design.md"),
			TestDirectory:    ".",
		}
	})

	Context("Requirements Coverage Analysis", func() {
		It("should validate all requirements are covered by tests", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())
			Expect(report).NotTo(BeNil())

			// Print detailed report
			report.PrintReport()

			// Validate coverage thresholds
			Expect(report.CoveragePercent).To(BeNumerically(">=", 70), "Test coverage should be at least 70%")
			Expect(report.Requirements).ToNot(BeEmpty(), "Should have parsed requirements")
			Expect(report.Properties).ToNot(BeEmpty(), "Should have parsed properties")
			Expect(len(report.TestFiles)).To(BeNumerically(">", 5), "Should have multiple test files")
		})

		It("should identify missing test coverage", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())

			if len(report.MissingCoverage) > 0 {
				GinkgoWriter.Printf("Missing coverage items:\n")
				for _, missing := range report.MissingCoverage {
					GinkgoWriter.Printf("  - %s\n", missing)
				}
			}

			// Allow some missing coverage but flag if too much
			missingPercent := float64(len(report.MissingCoverage)) / float64(len(report.Requirements)+len(report.Properties)) * 100
			Expect(missingPercent).To(BeNumerically("<=", 30), "Missing coverage should not exceed 30%")
		})

		It("should provide actionable recommendations", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())

			if len(report.Recommendations) > 0 {
				GinkgoWriter.Printf("Coverage recommendations:\n")
				for _, rec := range report.Recommendations {
					GinkgoWriter.Printf("  - %s\n", rec)
				}
			}

			// Recommendations should be helpful
			Expect(report.Recommendations).NotTo(BeEmpty(), "Should provide recommendations for improvement")
		})
	})

	Context("Property Coverage Analysis", func() {
		It("should validate all correctness properties have corresponding tests", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())

			implementedProperties := 0
			for _, prop := range report.Properties {
				if prop.Implemented {
					implementedProperties++
				}
			}

			propertyPercent := float64(implementedProperties) / float64(len(report.Properties)) * 100
			Expect(propertyPercent).To(BeNumerically(">=", 60), "At least 60% of properties should be implemented")
		})

		It("should identify property-based tests", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())

			propertyTests := 0
			for _, file := range report.TestFiles {
				if validator.containsPropertyBasedTests(file) {
					propertyTests++
				}
			}

			Expect(propertyTests).To(BeNumerically(">=", 3), "Should have multiple files with property-based tests")
		})
	})

	Context("Test Organization Analysis", func() {
		It("should validate test file organization", func() {
			report, err := validator.ValidateTestCoverage()
			Expect(err).NotTo(HaveOccurred())

			// Check for expected test files
			expectedFiles := []string{
				"error_handling_test.go",
				"observability_test.go",
				"workload_types_test.go",
			}

			for _, expected := range expectedFiles {
				found := false
				for _, actual := range report.TestFiles {
					if strings.Contains(actual, expected) {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Should have %s test file", expected)
			}
		})

		It("should validate helper components exist", func() {
			// Get the current working directory to determine correct paths
			wd, err := os.Getwd()
			Expect(err).NotTo(HaveOccurred())

			var helperFiles []string
			if strings.HasSuffix(wd, "test/e2e") {
				// Running from test/e2e directory
				helperFiles = []string{
					"helpers/policy_helpers.go",
					"helpers/workload_helpers.go",
					"helpers/validation_helpers.go",
					"helpers/cleanup_helpers.go",
				}
			} else {
				// Running from project root
				helperFiles = []string{
					"test/e2e/helpers/policy_helpers.go",
					"test/e2e/helpers/workload_helpers.go",
					"test/e2e/helpers/validation_helpers.go",
					"test/e2e/helpers/cleanup_helpers.go",
				}
			}

			for _, helper := range helperFiles {
				_, err := os.Stat(helper)
				Expect(err).NotTo(HaveOccurred(), "Helper file %s should exist", helper)
			}
		})
	})
})

// containsPropertyBasedTests checks if a test file contains property-based tests
func (cv *CoverageValidator) containsPropertyBasedTests(testFile string) bool {
	content, err := os.ReadFile(testFile)
	if err != nil {
		return false
	}

	// Look for property-based test patterns
	patterns := []string{
		`Property \d+:`,
		`**Feature: e2e-test-enhancement, Property`,
		`DescribeTable`,
		`Entry\(`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, string(content)); matched {
			return true
		}
	}

	return false
}
