package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCoverageValidatorBasic tests basic coverage validator functionality
func TestCoverageValidatorBasic(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "coverage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to clean up temp dir: %v", err)
		}
	}()

	validator := &CoverageValidator{
		RequirementsFile: filepath.Join(tempDir, "requirements.md"),
		DesignFile:       filepath.Join(tempDir, "design.md"),
		TestDirectory:    tempDir,
	}

	t.Run("ParseRequirements", func(t *testing.T) {
		// Create a sample requirements file
		requirementsContent := `# Requirements Document

## Requirements

### Requirement 1
1. WHEN a user creates a policy THEN the system SHALL validate the configuration
2. WHEN a policy is invalid THEN the system SHALL reject it with an error message
`
		err := os.WriteFile(validator.RequirementsFile, []byte(requirementsContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write requirements file: %v", err)
		}

		requirements, err := validator.parseRequirements()
		if err != nil {
			t.Fatalf("Failed to parse requirements: %v", err)
		}

		if len(requirements) != 2 {
			t.Errorf("Expected 2 requirements, got %d", len(requirements))
		}
	})

	t.Run("ExtractRequirementID", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"1.1 WHEN a user creates a policy", "1.1"},
			{"2.3 WHEN workloads are processed", "2.3"},
			{"10 WHEN something happens", "10"},
			{"WHEN no number", ""},
		}

		for _, tc := range testCases {
			result := validator.extractRequirementID(tc.input)
			if result != tc.expected {
				t.Errorf("extractRequirementID(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		}
	})

	t.Run("ScanTestFiles", func(t *testing.T) {
		// Create some test files
		testFiles := []string{
			"policy_test.go",
			"workload_test.go",
		}

		for _, file := range testFiles {
			err := os.WriteFile(filepath.Join(tempDir, file), []byte("package test"), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file %s: %v", file, err)
			}
		}

		foundFiles, err := validator.scanTestFiles()
		if err != nil {
			t.Fatalf("Failed to scan test files: %v", err)
		}

		if len(foundFiles) != 2 {
			t.Errorf("Expected 2 test files, got %d", len(foundFiles))
		}
	})
}
