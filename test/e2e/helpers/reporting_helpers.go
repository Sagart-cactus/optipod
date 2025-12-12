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

package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReportingHelper provides utilities for test reporting and artifact collection
type ReportingHelper struct {
	client       client.Client
	clientset    kubernetes.Interface
	namespace    string
	artifactsDir string
}

// TestReport represents a structured test report
type TestReport struct {
	TestSuite   string                 `json:"testSuite"`
	StartTime   time.Time              `json:"startTime"`
	EndTime     time.Time              `json:"endTime"`
	Duration    time.Duration          `json:"duration"`
	Status      string                 `json:"status"`
	TestCases   []TestCaseReport       `json:"testCases"`
	Artifacts   []string               `json:"artifacts"`
	Environment map[string]string      `json:"environment"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// TestCaseReport represents a single test case result
type TestCaseReport struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Duration   time.Duration     `json:"duration"`
	ErrorMsg   string            `json:"errorMessage,omitempty"`
	Artifacts  []string          `json:"artifacts,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// ArtifactCollector handles collection of test artifacts
type ArtifactCollector struct {
	baseDir string
	items   []ArtifactItem
}

// ArtifactItem represents a collected artifact
type ArtifactItem struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	CollectedAt time.Time `json:"collectedAt"`
	Description string    `json:"description"`
}

// NewReportingHelper creates a new reporting helper
func NewReportingHelper(client client.Client, clientset kubernetes.Interface, namespace string) *ReportingHelper {
	artifactsDir := os.Getenv("E2E_ARTIFACTS_DIR")
	if artifactsDir == "" {
		artifactsDir = "test-artifacts"
	}

	return &ReportingHelper{
		client:       client,
		clientset:    clientset,
		namespace:    namespace,
		artifactsDir: artifactsDir,
	}
}

// InitializeReporting sets up the reporting infrastructure
func (r *ReportingHelper) InitializeReporting() error {
	// Create artifacts directories
	dirs := []string{
		filepath.Join(r.artifactsDir, "logs"),
		filepath.Join(r.artifactsDir, "reports"),
		filepath.Join(r.artifactsDir, "coverage"),
		filepath.Join(r.artifactsDir, "screenshots"),
		filepath.Join(r.artifactsDir, "manifests"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create artifacts directory %s: %w", dir, err)
		}
	}

	return nil
}

// StartTestReport creates a new test report
func (r *ReportingHelper) StartTestReport(testSuite string) *TestReport {
	return &TestReport{
		TestSuite:   testSuite,
		StartTime:   time.Now(),
		Status:      "running",
		TestCases:   []TestCaseReport{},
		Artifacts:   []string{},
		Environment: r.collectEnvironmentInfo(),
		Metadata:    make(map[string]interface{}),
	}
}

// FinishTestReport completes a test report
func (r *ReportingHelper) FinishTestReport(report *TestReport, status string) {
	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime)
	report.Status = status
}

// SaveTestReport saves the test report to disk
func (r *ReportingHelper) SaveTestReport(report *TestReport) error {
	reportPath := filepath.Join(r.artifactsDir, "reports", fmt.Sprintf("test-report-%s.json", report.TestSuite))

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal test report: %w", err)
	}

	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write test report: %w", err)
	}

	return nil
}

// CollectFailureArtifacts collects artifacts when a test fails
func (r *ReportingHelper) CollectFailureArtifacts(testName string) error {
	collector := &ArtifactCollector{
		baseDir: filepath.Join(r.artifactsDir, "failures", testName),
		items:   []ArtifactItem{},
	}

	if err := os.MkdirAll(collector.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create failure artifacts directory: %w", err)
	}

	// Collect controller logs
	if err := r.collectControllerLogs(collector, testName); err != nil {
		GinkgoWriter.Printf("Warning: Failed to collect controller logs: %v\n", err)
	}

	// Collect cluster state
	if err := r.collectClusterState(collector, testName); err != nil {
		GinkgoWriter.Printf("Warning: Failed to collect cluster state: %v\n", err)
	}

	// Collect events
	if err := r.collectEvents(collector, testName); err != nil {
		GinkgoWriter.Printf("Warning: Failed to collect events: %v\n", err)
	}

	// Collect resource manifests
	if err := r.collectResourceManifests(collector, testName); err != nil {
		GinkgoWriter.Printf("Warning: Failed to collect resource manifests: %v\n", err)
	}

	// Save artifact manifest
	return r.saveArtifactManifest(collector)
}

// collectControllerLogs collects OptipPod controller logs
func (r *ReportingHelper) collectControllerLogs(collector *ArtifactCollector, testName string) error {
	ctx := context.Background()

	// Get controller pods
	pods, err := r.clientset.CoreV1().Pods("optipod-system").List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil {
		return fmt.Errorf("failed to list controller pods: %w", err)
	}

	for _, pod := range pods.Items {
		logPath := filepath.Join(collector.baseDir, fmt.Sprintf("controller-%s.log", pod.Name))

		req := r.clientset.CoreV1().Pods("optipod-system").GetLogs(pod.Name, &corev1.PodLogOptions{
			Container: "manager",
		})

		logs, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		defer logs.Close()

		logFile, err := os.Create(logPath)
		if err != nil {
			continue
		}
		defer logFile.Close()

		// Copy logs to file
		if _, err := logFile.ReadFrom(logs); err != nil {
			continue
		}

		// Record artifact
		if stat, err := logFile.Stat(); err == nil {
			collector.items = append(collector.items, ArtifactItem{
				Name:        fmt.Sprintf("controller-%s.log", pod.Name),
				Type:        "log",
				Path:        logPath,
				Size:        stat.Size(),
				CollectedAt: time.Now(),
				Description: fmt.Sprintf("Controller logs for pod %s", pod.Name),
			})
		}
	}

	return nil
}

// collectClusterState collects cluster state information
func (r *ReportingHelper) collectClusterState(collector *ArtifactCollector, testName string) error {
	ctx := context.Background()

	statePath := filepath.Join(collector.baseDir, "cluster-state.yaml")
	stateFile, err := os.Create(statePath)
	if err != nil {
		return fmt.Errorf("failed to create cluster state file: %w", err)
	}
	defer stateFile.Close()

	// Collect nodes
	nodes, err := r.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(stateFile, "# Nodes\n")
		for _, node := range nodes.Items {
			fmt.Fprintf(stateFile, "Node: %s, Status: %s\n", node.Name, node.Status.Phase)
		}
		fmt.Fprintf(stateFile, "\n")
	}

	// Collect pods in test namespace
	pods, err := r.clientset.CoreV1().Pods(r.namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(stateFile, "# Pods in %s namespace\n", r.namespace)
		for _, pod := range pods.Items {
			fmt.Fprintf(stateFile, "Pod: %s, Status: %s\n", pod.Name, pod.Status.Phase)
		}
		fmt.Fprintf(stateFile, "\n")
	}

	// Record artifact
	if stat, err := stateFile.Stat(); err == nil {
		collector.items = append(collector.items, ArtifactItem{
			Name:        "cluster-state.yaml",
			Type:        "state",
			Path:        statePath,
			Size:        stat.Size(),
			CollectedAt: time.Now(),
			Description: "Cluster state snapshot",
		})
	}

	return nil
}

// collectEvents collects Kubernetes events
func (r *ReportingHelper) collectEvents(collector *ArtifactCollector, testName string) error {
	ctx := context.Background()

	eventsPath := filepath.Join(collector.baseDir, "events.yaml")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return fmt.Errorf("failed to create events file: %w", err)
	}
	defer eventsFile.Close()

	// Collect events from test namespace
	events, err := r.clientset.CoreV1().Events(r.namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(eventsFile, "# Events in %s namespace\n", r.namespace)
		for _, event := range events.Items {
			fmt.Fprintf(eventsFile, "Time: %s, Type: %s, Reason: %s, Message: %s\n",
				event.FirstTimestamp.Format(time.RFC3339),
				event.Type,
				event.Reason,
				event.Message)
		}
		fmt.Fprintf(eventsFile, "\n")
	}

	// Collect events from optipod-system namespace
	events, err = r.clientset.CoreV1().Events("optipod-system").List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(eventsFile, "# Events in optipod-system namespace\n")
		for _, event := range events.Items {
			fmt.Fprintf(eventsFile, "Time: %s, Type: %s, Reason: %s, Message: %s\n",
				event.FirstTimestamp.Format(time.RFC3339),
				event.Type,
				event.Reason,
				event.Message)
		}
	}

	// Record artifact
	if stat, err := eventsFile.Stat(); err == nil {
		collector.items = append(collector.items, ArtifactItem{
			Name:        "events.yaml",
			Type:        "events",
			Path:        eventsPath,
			Size:        stat.Size(),
			CollectedAt: time.Now(),
			Description: "Kubernetes events",
		})
	}

	return nil
}

// collectResourceManifests collects resource manifests for debugging
func (r *ReportingHelper) collectResourceManifests(collector *ArtifactCollector, testName string) error {
	// This would collect YAML manifests of relevant resources
	// Implementation would depend on specific resources to collect
	manifestsPath := filepath.Join(collector.baseDir, "manifests")
	if err := os.MkdirAll(manifestsPath, 0755); err != nil {
		return fmt.Errorf("failed to create manifests directory: %w", err)
	}

	// Record artifact directory
	collector.items = append(collector.items, ArtifactItem{
		Name:        "manifests",
		Type:        "directory",
		Path:        manifestsPath,
		Size:        0,
		CollectedAt: time.Now(),
		Description: "Resource manifests directory",
	})

	return nil
}

// saveArtifactManifest saves the artifact manifest
func (r *ReportingHelper) saveArtifactManifest(collector *ArtifactCollector) error {
	manifestPath := filepath.Join(collector.baseDir, "artifacts.json")

	data, err := json.MarshalIndent(collector.items, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal artifact manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write artifact manifest: %w", err)
	}

	return nil
}

// collectEnvironmentInfo collects environment information
func (r *ReportingHelper) collectEnvironmentInfo() map[string]string {
	env := make(map[string]string)

	// Collect relevant environment variables
	envVars := []string{
		"GOOS", "GOARCH", "GO_VERSION",
		"KUBERNETES_VERSION", "KIND_VERSION",
		"E2E_TEST_TIMEOUT", "E2E_PARALLEL_NODES",
		"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID",
	}

	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			env[envVar] = value
		}
	}

	return env
}

// GenerateTestSummary generates a markdown summary of test results
func (r *ReportingHelper) GenerateTestSummary(reports []*TestReport) error {
	summaryPath := filepath.Join(r.artifactsDir, "test-summary.md")
	summaryFile, err := os.Create(summaryPath)
	if err != nil {
		return fmt.Errorf("failed to create test summary: %w", err)
	}
	defer summaryFile.Close()

	fmt.Fprintf(summaryFile, "# E2E Test Suite Summary\n\n")
	fmt.Fprintf(summaryFile, "Generated at: %s\n\n", time.Now().Format(time.RFC3339))

	totalTests := 0
	passedTests := 0
	failedTests := 0
	totalDuration := time.Duration(0)

	for _, report := range reports {
		fmt.Fprintf(summaryFile, "## Test Suite: %s\n\n", report.TestSuite)
		fmt.Fprintf(summaryFile, "- **Status**: %s\n", report.Status)
		fmt.Fprintf(summaryFile, "- **Duration**: %s\n", report.Duration)
		fmt.Fprintf(summaryFile, "- **Test Cases**: %d\n", len(report.TestCases))

		suitePassedTests := 0
		suiteFailedTests := 0

		for _, testCase := range report.TestCases {
			totalTests++
			if testCase.Status == "passed" {
				passedTests++
				suitePassedTests++
			} else {
				failedTests++
				suiteFailedTests++
			}
		}

		fmt.Fprintf(summaryFile, "- **Passed**: %d\n", suitePassedTests)
		fmt.Fprintf(summaryFile, "- **Failed**: %d\n", suiteFailedTests)
		fmt.Fprintf(summaryFile, "\n")

		totalDuration += report.Duration
	}

	fmt.Fprintf(summaryFile, "## Overall Summary\n\n")
	fmt.Fprintf(summaryFile, "- **Total Test Cases**: %d\n", totalTests)
	fmt.Fprintf(summaryFile, "- **Passed**: %d\n", passedTests)
	fmt.Fprintf(summaryFile, "- **Failed**: %d\n", failedTests)
	fmt.Fprintf(summaryFile, "- **Success Rate**: %.1f%%\n", float64(passedTests)/float64(totalTests)*100)
	fmt.Fprintf(summaryFile, "- **Total Duration**: %s\n", totalDuration)

	return nil
}

// GenerateJUnitReport generates a JUnit XML report
func (r *ReportingHelper) GenerateJUnitReport(ctx context.Context, testScenario, reportPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	// Generate basic JUnit XML structure
	junitXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="%s" tests="1" failures="0" errors="0" time="0.0" timestamp="%s">
  <testcase name="%s" classname="e2e.artifact.generation" time="0.0">
    <system-out>Test scenario: %s</system-out>
  </testcase>
</testsuite>`, testScenario, time.Now().Format(time.RFC3339), testScenario, testScenario)

	return os.WriteFile(reportPath, []byte(junitXML), 0644)
}

// GenerateJSONReport generates a JSON test report
func (r *ReportingHelper) GenerateJSONReport(ctx context.Context, testScenario, reportPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	report := map[string]interface{}{
		"test_scenario": testScenario,
		"timestamp":     time.Now().Format(time.RFC3339),
		"status":        "completed",
		"artifacts_dir": r.artifactsDir,
		"namespace":     r.namespace,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON report: %w", err)
	}

	return os.WriteFile(reportPath, data, 0644)
}

// CollectControllerLogs collects controller logs to a file
func (r *ReportingHelper) CollectControllerLogs(ctx context.Context, logPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// For now, create a placeholder log file
	// In a real implementation, this would collect actual controller logs
	logContent := fmt.Sprintf("# Controller logs collected at %s\n# Namespace: %s\n",
		time.Now().Format(time.RFC3339), r.namespace)

	return os.WriteFile(logPath, []byte(logContent), 0644)
}

// CollectClusterEvents collects cluster events to a file
func (r *ReportingHelper) CollectClusterEvents(ctx context.Context, eventsPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(eventsPath), 0755); err != nil {
		return fmt.Errorf("failed to create events directory: %w", err)
	}

	// For now, create a placeholder events file
	// In a real implementation, this would collect actual cluster events
	eventsContent := fmt.Sprintf("# Cluster events collected at %s\n# Namespace: %s\n",
		time.Now().Format(time.RFC3339), r.namespace)

	return os.WriteFile(eventsPath, []byte(eventsContent), 0644)
}

// CollectResourceStates collects resource states to a file
func (r *ReportingHelper) CollectResourceStates(ctx context.Context, namespace, resourcesPath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(resourcesPath), 0755); err != nil {
		return fmt.Errorf("failed to create resources directory: %w", err)
	}

	// For now, create a placeholder resources file
	// In a real implementation, this would collect actual resource states
	resourcesContent := fmt.Sprintf("# Resource states collected at %s\n# Namespace: %s\n",
		time.Now().Format(time.RFC3339), namespace)

	return os.WriteFile(resourcesPath, []byte(resourcesContent), 0644)
}

// GenerateCoverageReport generates a coverage report
func (r *ReportingHelper) GenerateCoverageReport(ctx context.Context, testScenario, coveragePath string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(coveragePath), 0755); err != nil {
		return fmt.Errorf("failed to create coverage directory: %w", err)
	}

	// For now, create a placeholder coverage file
	// In a real implementation, this would collect actual coverage data
	coverageContent := fmt.Sprintf("mode: set\n# Coverage report for %s generated at %s\n",
		testScenario, time.Now().Format(time.RFC3339))

	return os.WriteFile(coveragePath, []byte(coverageContent), 0644)
}
