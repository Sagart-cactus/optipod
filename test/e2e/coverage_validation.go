package e2e

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CoverageValidator validates test coverage and completeness
type CoverageValidator struct {
	RequirementsFile string
	DesignFile       string
	TestDirectory    string
}

// RequirementCoverage represents coverage information for a requirement
type RequirementCoverage struct {
	ID          string
	Description string
	TestFiles   []string
	Properties  []string
	Covered     bool
}

// PropertyCoverage represents coverage information for a correctness property
type PropertyCoverage struct {
	ID          string
	Description string
	TestFile    string
	TestName    string
	Implemented bool
}

// TestCoverageReport contains comprehensive coverage analysis
type TestCoverageReport struct {
	Requirements    []RequirementCoverage
	Properties      []PropertyCoverage
	TestFiles       []string
	CoveragePercent float64
	MissingCoverage []string
	Recommendations []string
}

// ValidateTestCoverage performs comprehensive test coverage analysis
func (cv *CoverageValidator) ValidateTestCoverage() (*TestCoverageReport, error) {
	report := &TestCoverageReport{
		Requirements:    []RequirementCoverage{},
		Properties:      []PropertyCoverage{},
		TestFiles:       []string{},
		MissingCoverage: []string{},
		Recommendations: []string{},
	}

	// Parse requirements from requirements.md
	requirements, err := cv.parseRequirements()
	if err != nil {
		return nil, fmt.Errorf("failed to parse requirements: %w", err)
	}

	// Parse properties from design.md
	properties, err := cv.parseProperties()
	if err != nil {
		return nil, fmt.Errorf("failed to parse properties: %w", err)
	}

	// Scan test files
	testFiles, err := cv.scanTestFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan test files: %w", err)
	}

	report.TestFiles = testFiles

	// Analyze requirement coverage
	for _, req := range requirements {
		coverage := cv.analyzeRequirementCoverage(req, testFiles)
		report.Requirements = append(report.Requirements, coverage)
	}

	// Analyze property coverage
	for _, prop := range properties {
		coverage := cv.analyzePropertyCoverage(prop, testFiles)
		report.Properties = append(report.Properties, coverage)
	}

	// Calculate overall coverage
	report.CoveragePercent = cv.calculateCoveragePercent(report)

	// Identify missing coverage
	report.MissingCoverage = cv.identifyMissingCoverage(report)

	// Generate recommendations
	report.Recommendations = cv.generateRecommendations(report)

	return report, nil
}

// parseRequirements extracts requirements from requirements.md
func (cv *CoverageValidator) parseRequirements() ([]string, error) {
	content, err := os.ReadFile(cv.RequirementsFile)
	if err != nil {
		return nil, err
	}

	// Extract acceptance criteria using regex
	criteriaRegex := regexp.MustCompile(`(?m)^\d+\.\s+WHEN.*?THE.*?SHALL.*?$`)
	matches := criteriaRegex.FindAllString(string(content), -1)

	requirements := make([]string, 0)
	for _, match := range matches {
		requirements = append(requirements, strings.TrimSpace(match))
	}

	return requirements, nil
}

// parseProperties extracts correctness properties from design.md
func (cv *CoverageValidator) parseProperties() ([]string, error) {
	content, err := os.ReadFile(cv.DesignFile)
	if err != nil {
		return nil, err
	}

	// Extract properties using regex
	propertyRegex := regexp.MustCompile(`Property \d+:.*?\n\*For any\*.*?\n\*\*Validates:.*?\*\*`)
	matches := propertyRegex.FindAllString(string(content), -1)

	properties := make([]string, 0)
	for _, match := range matches {
		properties = append(properties, strings.TrimSpace(match))
	}

	return properties, nil
}

// scanTestFiles finds all test files in the test directory
func (cv *CoverageValidator) scanTestFiles() ([]string, error) {
	var testFiles []string

	err := filepath.Walk(cv.TestDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(path, "_test.go") {
			testFiles = append(testFiles, path)
		}

		return nil
	})

	return testFiles, err
}

// analyzeRequirementCoverage checks if a requirement is covered by tests
func (cv *CoverageValidator) analyzeRequirementCoverage(requirement string, testFiles []string) RequirementCoverage {
	coverage := RequirementCoverage{
		ID:          cv.extractRequirementID(requirement),
		Description: requirement,
		TestFiles:   []string{},
		Properties:  []string{},
		Covered:     false,
	}

	// Search for requirement references in test files
	for _, testFile := range testFiles {
		if cv.testFileReferencesRequirement(testFile, coverage.ID) {
			coverage.TestFiles = append(coverage.TestFiles, testFile)
			coverage.Covered = true
		}
	}

	return coverage
}

// analyzePropertyCoverage checks if a property is implemented by tests
func (cv *CoverageValidator) analyzePropertyCoverage(property string, testFiles []string) PropertyCoverage {
	coverage := PropertyCoverage{
		ID:          cv.extractPropertyID(property),
		Description: property,
		Implemented: false,
	}

	// Search for property implementations in test files
	for _, testFile := range testFiles {
		if testName := cv.findPropertyTest(testFile, coverage.ID); testName != "" {
			coverage.TestFile = testFile
			coverage.TestName = testName
			coverage.Implemented = true
			break
		}
	}

	return coverage
}

// testFileReferencesRequirement checks if a test file references a requirement
func (cv *CoverageValidator) testFileReferencesRequirement(testFile, requirementID string) bool {
	content, err := os.ReadFile(testFile)
	if err != nil {
		return false
	}

	// Look for requirement references in comments or test names
	requirementPattern := fmt.Sprintf(`(?i)(requirement|req).*?%s`, regexp.QuoteMeta(requirementID))
	matched, _ := regexp.MatchString(requirementPattern, string(content))
	return matched
}

// findPropertyTest finds a test that implements a specific property
func (cv *CoverageValidator) findPropertyTest(testFile, propertyID string) string {
	content, err := os.ReadFile(testFile)
	if err != nil {
		return ""
	}

	// Look for property references in test comments - use word boundaries for exact match
	propertyPattern := fmt.Sprintf(`(?i)property\s+%s\b`, regexp.QuoteMeta(propertyID))
	if matched, _ := regexp.MatchString(propertyPattern, string(content)); matched {
		// Extract test function name
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, testFile, content, parser.ParseComments)
		if err != nil {
			return ""
		}

		for _, decl := range node.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if strings.HasPrefix(fn.Name.Name, "Test") {
					// Check if this function contains the property reference
					start := fset.Position(fn.Pos()).Offset
					end := fset.Position(fn.End()).Offset
					if start < len(content) && end <= len(content) {
						fnContent := string(content[start:end])
						if matched, _ := regexp.MatchString(propertyPattern, fnContent); matched {
							// Check if it's marked as not implemented
							notImplementedPattern := `(?i)(not implemented|todo|placeholder|not.*yet)`
							if matched, _ := regexp.MatchString(notImplementedPattern, fnContent); matched {
								return "" // Not actually implemented
							}
							return fn.Name.Name
						}
					}
				}
			}
		}
	}

	return ""
}

// extractRequirementID extracts requirement ID from requirement text
func (cv *CoverageValidator) extractRequirementID(requirement string) string {
	// Extract number from beginning of requirement
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(requirement)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractPropertyID extracts property ID from property text
func (cv *CoverageValidator) extractPropertyID(property string) string {
	// Extract property number
	re := regexp.MustCompile(`Property (\d+):`)
	matches := re.FindStringSubmatch(property)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// calculateCoveragePercent calculates overall coverage percentage
func (cv *CoverageValidator) calculateCoveragePercent(report *TestCoverageReport) float64 {
	totalItems := len(report.Requirements) + len(report.Properties)
	if totalItems == 0 {
		return 0
	}

	coveredItems := 0
	for _, req := range report.Requirements {
		if req.Covered {
			coveredItems++
		}
	}
	for _, prop := range report.Properties {
		if prop.Implemented {
			coveredItems++
		}
	}

	return float64(coveredItems) / float64(totalItems) * 100
}

// identifyMissingCoverage identifies gaps in test coverage
func (cv *CoverageValidator) identifyMissingCoverage(report *TestCoverageReport) []string {
	var missing []string

	for _, req := range report.Requirements {
		if !req.Covered {
			missing = append(missing, fmt.Sprintf("Requirement %s: %s", req.ID, req.Description))
		}
	}

	for _, prop := range report.Properties {
		if !prop.Implemented {
			missing = append(missing, fmt.Sprintf("Property %s: %s", prop.ID, prop.Description))
		}
	}

	return missing
}

// generateRecommendations provides recommendations for improving coverage
func (cv *CoverageValidator) generateRecommendations(report *TestCoverageReport) []string {
	var recommendations []string

	if report.CoveragePercent < 80 {
		recommendations = append(recommendations,
			"Overall test coverage is below 80%. Consider adding more comprehensive tests.")
	}

	uncoveredReqs := 0
	for _, req := range report.Requirements {
		if !req.Covered {
			uncoveredReqs++
		}
	}

	if uncoveredReqs > 0 {
		recommendations = append(recommendations, fmt.Sprintf(
			"%d requirements lack test coverage. Add tests that reference these requirements.",
			uncoveredReqs))
	}

	unimplementedProps := 0
	for _, prop := range report.Properties {
		if !prop.Implemented {
			unimplementedProps++
		}
	}

	if unimplementedProps > 0 {
		recommendations = append(recommendations, fmt.Sprintf(
			"%d correctness properties are not implemented as tests. Add property-based tests for these.",
			unimplementedProps))
	}

	if len(report.TestFiles) < 8 {
		recommendations = append(recommendations,
			"Consider organizing tests into more focused test files for better maintainability.")
	}

	return recommendations
}

// PrintReport prints a formatted coverage report
func (report *TestCoverageReport) PrintReport() {
	fmt.Printf("=== E2E Test Coverage Report ===\n\n")
	fmt.Printf("Overall Coverage: %.1f%%\n\n", report.CoveragePercent)

	fmt.Printf("Requirements Coverage (%d total):\n", len(report.Requirements))
	for _, req := range report.Requirements {
		status := "❌"
		if req.Covered {
			status = "✅"
		}
		fmt.Printf("  %s Requirement %s: %d test files\n", status, req.ID, len(req.TestFiles))
	}

	fmt.Printf("\nProperties Coverage (%d total):\n", len(report.Properties))
	for _, prop := range report.Properties {
		status := "❌"
		if prop.Implemented {
			status = "✅"
		}
		fmt.Printf("  %s Property %s: %s\n", status, prop.ID, prop.TestFile)
	}

	if len(report.MissingCoverage) > 0 {
		fmt.Printf("\nMissing Coverage:\n")
		for _, missing := range report.MissingCoverage {
			fmt.Printf("  - %s\n", missing)
		}
	}

	if len(report.Recommendations) > 0 {
		fmt.Printf("\nRecommendations:\n")
		for _, rec := range report.Recommendations {
			fmt.Printf("  - %s\n", rec)
		}
	}

	fmt.Printf("\nTest Files (%d total):\n", len(report.TestFiles))
	for _, file := range report.TestFiles {
		fmt.Printf("  - %s\n", file)
	}
}
