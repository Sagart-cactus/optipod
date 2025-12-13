//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestIntegrationComponents tests the integration components without requiring a full e2e environment
// TestIntegrationComponents is included in the main e2e test suite
// All tests in this file are automatically discovered by Ginkgo

var _ = Describe("Coverage Validator Unit Tests", func() {
	var (
		validator *CoverageValidator
		tempDir   string
	)

	BeforeEach(func() {
		// Create temporary directory for test files
		var err error
		tempDir, err = os.MkdirTemp("", "coverage-test-*")
		Expect(err).NotTo(HaveOccurred())

		validator = &CoverageValidator{
			RequirementsFile: filepath.Join(tempDir, "requirements.md"),
			DesignFile:       filepath.Join(tempDir, "design.md"),
			TestDirectory:    tempDir,
		}
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("Requirements Parsing", func() {
		It("should parse requirements from markdown file", func() {
			// Create a sample requirements file
			requirementsContent := `# Requirements Document

## Requirements

### Requirement 1
1. WHEN a user creates a policy THEN the system SHALL validate the configuration
2. WHEN a policy is invalid THEN the system SHALL reject it with an error message

### Requirement 2
1. WHEN a workload is processed THEN the system SHALL generate recommendations
2. WHEN recommendations are applied THEN the system SHALL update the workload
`
			err := os.WriteFile(validator.RequirementsFile, []byte(requirementsContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			requirements, err := validator.parseRequirements()
			Expect(err).NotTo(HaveOccurred())
			Expect(requirements).To(HaveLen(4))
			Expect(requirements[0]).To(ContainSubstring("WHEN a user creates a policy"))
			Expect(requirements[1]).To(ContainSubstring("WHEN a policy is invalid"))
		})

		It("should handle empty requirements file", func() {
			err := os.WriteFile(validator.RequirementsFile, []byte("# Empty Requirements"), 0644)
			Expect(err).NotTo(HaveOccurred())

			requirements, err := validator.parseRequirements()
			Expect(err).NotTo(HaveOccurred())
			Expect(requirements).To(BeEmpty())
		})

		It("should handle missing requirements file", func() {
			requirements, err := validator.parseRequirements()
			Expect(err).To(HaveOccurred())
			Expect(requirements).To(BeNil())
		})
	})

	Context("Properties Parsing", func() {
		It("should parse properties from design file", func() {
			designContent := `# Design Document

## Correctness Properties

Property 1: Policy mode behavior consistency
*For any* optimization policy configuration and workload, the policy mode should consistently determine whether recommendations are generated
**Validates: Requirements 1.1, 1.2, 1.3**

Property 2: Resource bounds enforcement
*For any* optimization policy with resource bounds and any workload, recommendations should always respect the configured limits
**Validates: Requirements 2.2, 2.3, 2.4**
`
			err := os.WriteFile(validator.DesignFile, []byte(designContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			properties, err := validator.parseProperties()
			Expect(err).NotTo(HaveOccurred())
			Expect(properties).To(HaveLen(2))
			Expect(properties[0]).To(ContainSubstring("Property 1: Policy mode behavior consistency"))
			Expect(properties[1]).To(ContainSubstring("Property 2: Resource bounds enforcement"))
		})

		It("should handle design file without properties", func() {
			designContent := `# Design Document

## Overview
This is a design document without properties.
`
			err := os.WriteFile(validator.DesignFile, []byte(designContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			properties, err := validator.parseProperties()
			Expect(err).NotTo(HaveOccurred())
			Expect(properties).To(BeEmpty())
		})
	})

	Context("Test File Scanning", func() {
		It("should find test files in directory", func() {
			// Create some test files
			testFiles := []string{
				"policy_test.go",
				"workload_test.go",
				"helper_test.go",
			}

			for _, file := range testFiles {
				err := os.WriteFile(filepath.Join(tempDir, file), []byte("package test"), 0644)
				Expect(err).NotTo(HaveOccurred())
			}

			// Create a non-test file
			err := os.WriteFile(filepath.Join(tempDir, "helper.go"), []byte("package helper"), 0644)
			Expect(err).NotTo(HaveOccurred())

			foundFiles, err := validator.scanTestFiles()
			Expect(err).NotTo(HaveOccurred())
			Expect(foundFiles).To(HaveLen(3))

			for _, file := range testFiles {
				expectedPath := filepath.Join(tempDir, file)
				Expect(foundFiles).To(ContainElement(expectedPath))
			}
		})

		It("should handle directory with no test files", func() {
			foundFiles, err := validator.scanTestFiles()
			Expect(err).NotTo(HaveOccurred())
			Expect(foundFiles).To(BeEmpty())
		})
	})

	Context("Requirement Coverage Analysis", func() {
		It("should analyze requirement coverage correctly", func() {
			// Create a test file that references requirements
			testContent := `package test

// This test validates requirement 1.1
func TestPolicyCreation(t *testing.T) {
	// Test implementation
}

// This test validates requirement 2.3
func TestResourceBounds(t *testing.T) {
	// Test implementation
}
`
			testFile := filepath.Join(tempDir, "policy_test.go")
			err := os.WriteFile(testFile, []byte(testContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			testFiles := []string{testFile}

			coverage := validator.analyzeRequirementCoverage("1.1 WHEN a user creates a policy", testFiles)
			Expect(coverage.ID).To(Equal("1.1"))
			Expect(coverage.Covered).To(BeTrue())
			Expect(coverage.TestFiles).To(ContainElement(testFile))

			coverage = validator.analyzeRequirementCoverage("3.1 WHEN something else happens", testFiles)
			Expect(coverage.ID).To(Equal("3.1"))
			Expect(coverage.Covered).To(BeFalse())
			Expect(coverage.TestFiles).To(BeEmpty())
		})
	})

	Context("Property Coverage Analysis", func() {
		It("should analyze property coverage correctly", func() {
			// Create a test file with property references
			testContent := `package test

// **Feature: test-feature, Property 1: Policy mode behavior consistency**
func TestPolicyModeProperty(t *testing.T) {
	// Property 1 test implementation
}

// **Feature: test-feature, Property 2: Resource bounds enforcement**  
func TestResourceBoundsProperty(t *testing.T) {
	// Property 2 test implementation - not implemented yet
}
`
			testFile := filepath.Join(tempDir, "property_test.go")
			err := os.WriteFile(testFile, []byte(testContent), 0644)
			Expect(err).NotTo(HaveOccurred())

			testFiles := []string{testFile}

			coverage := validator.analyzePropertyCoverage("Property 1: Policy mode behavior consistency", testFiles)
			Expect(coverage.ID).To(Equal("1"))
			Expect(coverage.Implemented).To(BeTrue())
			Expect(coverage.TestFile).To(Equal(testFile))

			coverage = validator.analyzePropertyCoverage("Property 2: Resource bounds enforcement", testFiles)
			Expect(coverage.ID).To(Equal("2"))
			Expect(coverage.Implemented).To(BeFalse())
			Expect(coverage.TestFile).To(BeEmpty())
		})
	})

	Context("Coverage Calculation", func() {
		It("should calculate coverage percentage correctly", func() {
			report := &TestCoverageReport{
				Requirements: []RequirementCoverage{
					{ID: "1.1", Covered: true},
					{ID: "1.2", Covered: true},
					{ID: "2.1", Covered: false},
					{ID: "2.2", Covered: true},
				},
				Properties: []PropertyCoverage{
					{ID: "1", Implemented: true},
					{ID: "2", Implemented: false},
				},
			}

			percentage := validator.calculateCoveragePercent(report)
			// 3 covered requirements + 1 implemented property = 4 out of 6 total = 66.67%
			Expect(percentage).To(BeNumerically("~", 66.67, 0.1))
		})

		It("should handle empty report", func() {
			report := &TestCoverageReport{
				Requirements: []RequirementCoverage{},
				Properties:   []PropertyCoverage{},
			}

			percentage := validator.calculateCoveragePercent(report)
			Expect(percentage).To(Equal(0.0))
		})
	})

	Context("Missing Coverage Identification", func() {
		It("should identify missing coverage correctly", func() {
			report := &TestCoverageReport{
				Requirements: []RequirementCoverage{
					{ID: "1.1", Description: "Policy creation", Covered: true},
					{ID: "1.2", Description: "Policy validation", Covered: false},
				},
				Properties: []PropertyCoverage{
					{ID: "1", Description: "Property 1: Consistency", Implemented: true},
					{ID: "2", Description: "Property 2: Bounds", Implemented: false},
				},
			}

			missing := validator.identifyMissingCoverage(report)
			Expect(missing).To(HaveLen(2))
			Expect(missing[0]).To(ContainSubstring("Requirement 1.2"))
			Expect(missing[1]).To(ContainSubstring("Property 2"))
		})
	})

	Context("Recommendations Generation", func() {
		It("should generate appropriate recommendations", func() {
			report := &TestCoverageReport{
				Requirements: []RequirementCoverage{
					{Covered: true}, {Covered: false}, {Covered: false},
				},
				Properties: []PropertyCoverage{
					{Implemented: true}, {Implemented: false},
				},
				TestFiles: []string{"test1.go", "test2.go"},
			}
			report.CoveragePercent = validator.calculateCoveragePercent(report)

			recommendations := validator.generateRecommendations(report)
			Expect(recommendations).NotTo(BeEmpty())

			// Should recommend improving coverage since it's below 80%
			recommendationText := fmt.Sprintf("%v", recommendations)
			Expect(recommendationText).To(ContainSubstring("coverage"))
		})
	})
})

var _ = Describe("Test Suite Validator Unit Tests", func() {
	var (
		validator  *TestSuiteValidator
		fakeClient client.Client
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		corev1.AddToScheme(scheme)

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		validator = NewTestSuiteValidator(fakeClient, "test-namespace")
	})

	Context("Cluster Connectivity Validation", func() {
		It("should validate healthy cluster connectivity", func() {
			// Create some nodes in the fake client
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			err := fakeClient.Create(context.Background(), node)
			Expect(err).NotTo(HaveOccurred())

			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err = validator.validateClusterConnectivity(context.Background(), result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ComponentHealth["cluster-connectivity"]).To(Equal(HealthStatusHealthy))
		})

		It("should detect cluster with no nodes", func() {
			result := &ValidationResult{
				ComponentHealth: make(map[string]HealthStatus),
				Issues:          []ValidationIssue{},
			}

			err := validator.validateClusterConnectivity(context.Background(), result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ComponentHealth["cluster-connectivity"]).To(Equal(HealthStatusDegraded))
			Expect(result.Issues).To(HaveLen(1))
			Expect(result.Issues[0].Severity).To(Equal(IssueSeverityMajor))
		})
	})

	Context("Overall Health Calculation", func() {
		It("should calculate healthy status", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusHealthy,
					"component2": HealthStatusHealthy,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityMinor},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusHealthy))
		})

		It("should calculate degraded status", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusDegraded,
					"component2": HealthStatusDegraded,
					"component3": HealthStatusDegraded,
					"component4": HealthStatusDegraded,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityMajor},
					{Severity: IssueSeverityMajor},
					{Severity: IssueSeverityMajor},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusDegraded))
		})

		It("should calculate unhealthy status", func() {
			result := &ValidationResult{
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusUnhealthy,
					"component2": HealthStatusUnhealthy,
					"component3": HealthStatusUnhealthy,
				},
				Issues: []ValidationIssue{
					{Severity: IssueSeverityCritical},
				},
			}

			health := validator.calculateOverallHealth(result)
			Expect(health).To(Equal(HealthStatusUnhealthy))
		})
	})

	Context("Recommendations Generation", func() {
		It("should generate recommendations for unhealthy suite", func() {
			result := &ValidationResult{
				OverallHealth: HealthStatusUnhealthy,
				ComponentHealth: map[string]HealthStatus{
					"test-structure": HealthStatusUnhealthy,
				},
				QualityMetrics: QualityMetrics{
					TestCoverage:     0.6,
					PropertyCoverage: 0.5,
					PerformanceScore: 0.6,
				},
			}

			validator.generateRecommendations(result)
			Expect(result.Recommendations).NotTo(BeEmpty())

			recommendationText := fmt.Sprintf("%v", result.Recommendations)
			Expect(recommendationText).To(ContainSubstring("critical"))
		})

		It("should generate recommendations for healthy suite", func() {
			result := &ValidationResult{
				OverallHealth: HealthStatusHealthy,
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusHealthy,
				},
				QualityMetrics: QualityMetrics{
					TestCoverage:     0.9,
					PropertyCoverage: 0.8,
					PerformanceScore: 0.8,
				},
			}

			validator.generateRecommendations(result)
			Expect(result.Recommendations).NotTo(BeEmpty())

			recommendationText := fmt.Sprintf("%v", result.Recommendations)
			Expect(recommendationText).To(ContainSubstring("healthy"))
		})
	})
})

var _ = Describe("Diagnostic Collector Unit Tests", func() {
	var (
		collector  *DiagnosticCollector
		fakeClient client.Client
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		corev1.AddToScheme(scheme)

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		collector = NewDiagnosticCollector(fakeClient, "test-namespace")
	})

	Context("Controller Logs Collection", func() {
		It("should collect simulated controller logs", func() {
			logs, err := collector.collectControllerLogs(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(logs).NotTo(BeEmpty())

			// Should have logs with different levels
			hasInfo := false
			hasError := false
			for _, log := range logs {
				if log.Level == "INFO" {
					hasInfo = true
				}
				if log.Level == "ERROR" {
					hasError = true
				}
			}
			Expect(hasInfo).To(BeTrue())
			Expect(hasError).To(BeTrue())
		})
	})

	Context("Resource States Collection", func() {
		It("should collect resource states from namespace", func() {
			// Create some test resources
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			err := fakeClient.Create(context.Background(), pod)
			Expect(err).NotTo(HaveOccurred())

			states, err := collector.collectResourceStates(context.Background())
			Expect(err).NotTo(HaveOccurred())

			// Should have collected the pod state
			podKey := "Pod/test-pod"
			Expect(states).To(HaveKey(podKey))

			podState := states[podKey].(map[string]interface{})
			Expect(podState).To(HaveKey("phase"))
			Expect(podState["phase"]).To(Equal(corev1.PodRunning))
		})

		It("should handle empty namespace", func() {
			states, err := collector.collectResourceStates(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(states).NotTo(BeNil())
			// Should be empty but not nil
			Expect(len(states)).To(Equal(0))
		})
	})

	Context("Events Collection", func() {
		It("should collect Kubernetes events", func() {
			// Create a test event
			event := &corev1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-event",
					Namespace: "test-namespace",
				},
				Type:    "Warning",
				Reason:  "TestReason",
				Message: "Test event message",
				InvolvedObject: corev1.ObjectReference{
					Kind: "Pod",
					Name: "test-pod",
				},
				FirstTimestamp: metav1.Time{Time: time.Now()},
			}
			err := fakeClient.Create(context.Background(), event)
			Expect(err).NotTo(HaveOccurred())

			events, err := collector.collectEvents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1))

			collectedEvent := events[0]
			Expect(collectedEvent.Type).To(Equal("Warning"))
			Expect(collectedEvent.Reason).To(Equal("TestReason"))
			Expect(collectedEvent.Message).To(Equal("Test event message"))
			Expect(collectedEvent.Object).To(Equal("Pod/test-pod"))
		})
	})

	Context("Diagnostic Collection Integration", func() {
		It("should collect comprehensive diagnostics", func() {
			// Create some test resources
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}
			err := fakeClient.Create(context.Background(), pod)
			Expect(err).NotTo(HaveOccurred())

			diagnostics, err := collector.CollectDiagnostics(context.Background(), FailureTypeWorkloadProcessing)
			Expect(err).NotTo(HaveOccurred())
			Expect(diagnostics).NotTo(BeNil())

			// Validate diagnostic structure
			Expect(diagnostics.Timestamp).NotTo(BeZero())
			Expect(diagnostics.Namespace).To(Equal("test-namespace"))
			Expect(diagnostics.FailureType).To(Equal(FailureTypeWorkloadProcessing))
			Expect(diagnostics.ControllerLogs).NotTo(BeEmpty())
			Expect(diagnostics.ResourceStates).NotTo(BeNil())
			Expect(diagnostics.Events).NotTo(BeNil())
			// Events may be empty in test environment, but should be initialized
			Expect(diagnostics.ArtifactPaths).NotTo(BeEmpty())
			Expect(diagnostics.ErrorDetails).NotTo(BeEmpty())
		})
	})

	Context("Artifact Generation", func() {
		It("should generate diagnostic artifacts", func() {
			diagnosticInfo := &DiagnosticInfo{
				Timestamp:   time.Now(),
				Namespace:   "test-namespace",
				FailureType: FailureTypeWorkloadProcessing,
				ControllerLogs: []LogEntry{
					{
						Timestamp: time.Now(),
						Level:     "ERROR",
						Message:   "Test error message",
					},
				},
				ResourceStates: map[string]interface{}{
					"Pod/test-pod": map[string]interface{}{
						"phase": "Running",
					},
				},
				Events: []EventInfo{
					{
						Type:      "Warning",
						Reason:    "TestReason",
						Message:   "Test event",
						Object:    "Pod/test-pod",
						Timestamp: time.Now(),
					},
				},
			}

			artifacts, err := collector.generateArtifacts(context.Background(), diagnosticInfo)
			Expect(err).NotTo(HaveOccurred())
			Expect(artifacts).To(HaveLen(3)) // logs, states, events

			// Verify artifacts exist
			for _, artifact := range artifacts {
				_, err := os.Stat(artifact)
				Expect(err).NotTo(HaveOccurred())
			}

			// Cleanup artifacts
			for _, artifact := range artifacts {
				os.Remove(artifact)
			}
		})
	})
})

var _ = Describe("Validation Result Reporting Unit Tests", func() {
	Context("Report Generation", func() {
		It("should generate reports without panicking", func() {
			result := &ValidationResult{
				Timestamp:     time.Now(),
				OverallHealth: HealthStatusHealthy,
				ComponentHealth: map[string]HealthStatus{
					"component1": HealthStatusHealthy,
					"component2": HealthStatusDegraded,
				},
				QualityMetrics: QualityMetrics{
					TestCoverage:         0.85,
					PropertyCoverage:     0.70,
					HelperUtilization:    0.90,
					CleanupEffectiveness: 0.95,
					ExecutionStability:   0.80,
					PerformanceScore:     0.75,
				},
				Issues: []ValidationIssue{
					{
						Severity:    IssueSeverityMinor,
						Component:   "component2",
						Description: "Minor issue",
						Impact:      "Low impact",
						Suggestion:  "Fix suggestion",
					},
				},
				Recommendations: []string{
					"Recommendation 1",
					"Recommendation 2",
				},
			}

			// This should not panic
			Expect(func() {
				result.PrintValidationReport()
			}).NotTo(Panic())
		})

		It("should handle empty validation result", func() {
			result := &ValidationResult{
				Timestamp:       time.Now(),
				OverallHealth:   HealthStatusHealthy,
				ComponentHealth: make(map[string]HealthStatus),
				QualityMetrics:  QualityMetrics{},
				Issues:          []ValidationIssue{},
				Recommendations: []string{},
			}

			// This should not panic even with empty data
			Expect(func() {
				result.PrintValidationReport()
			}).NotTo(Panic())
		})
	})
})
