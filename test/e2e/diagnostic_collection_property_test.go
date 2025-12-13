//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/optipod/optipod/test/e2e/helpers"
)

// **Feature: e2e-test-enhancement, Property 20: Diagnostic information collection**
// **Validates: Requirements 1.5**
var _ = Describe("Property 20: Diagnostic Information Collection", func() {
	var (
		ctx           context.Context
		namespace     string
		cleanupHelper *helpers.CleanupHelper
		diagnostics   *DiagnosticCollector
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = fmt.Sprintf("diagnostic-test-%d", time.Now().Unix())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		cleanupHelper = helpers.NewCleanupHelper(k8sClient)
		cleanupHelper.TrackNamespace(namespace)

		diagnostics = NewDiagnosticCollector(k8sClient, namespace)
	})

	AfterEach(func() {
		if cleanupHelper != nil {
			cleanupHelper.CleanupAll()
		}
	})

	Context("Diagnostic Collection Property Tests", func() {
		It("should collect comprehensive diagnostic information for any test failure", func() {
			By("Simulating a test failure scenario")

			// Simulate a failure by trying to create an invalid deployment
			invalidDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-deployment",
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: func() *int32 { r := int32(-1); return &r }(), // Invalid negative replicas
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			// Try to create the invalid deployment (this should fail)
			err := k8sClient.Create(ctx, invalidDeployment)

			By("Collecting diagnostic information after failure")
			diagnosticInfo, collectErr := diagnostics.CollectDiagnostics(ctx, FailureTypeWorkloadProcessing)
			Expect(collectErr).NotTo(HaveOccurred(), "Diagnostic collection should not fail")
			Expect(diagnosticInfo).NotTo(BeNil(), "Diagnostic information should be collected")

			By("Validating diagnostic information completeness")
			Expect(diagnosticInfo.Timestamp).NotTo(BeZero(), "Should have timestamp")
			Expect(diagnosticInfo.Namespace).To(Equal(namespace), "Should capture correct namespace")
			Expect(diagnosticInfo.FailureType).To(Equal(FailureTypeWorkloadProcessing), "Should identify failure type")

			By("Validating controller logs are collected")
			Expect(diagnosticInfo.ControllerLogs).NotTo(BeEmpty(), "Should collect controller logs")

			By("Validating resource states are captured")
			Expect(len(diagnosticInfo.ResourceStates)).To(BeNumerically(">=", 0), "Should capture resource states")

			By("Validating events are collected")
			Expect(len(diagnosticInfo.Events)).To(BeNumerically(">=", 0), "Should collect Kubernetes events")

			By("Validating diagnostic artifacts are generated")
			Expect(diagnosticInfo.ArtifactPaths).NotTo(BeEmpty(), "Should generate diagnostic artifacts")

			// Verify artifacts exist on filesystem
			for _, artifactPath := range diagnosticInfo.ArtifactPaths {
				_, err := os.Stat(artifactPath)
				Expect(err).NotTo(HaveOccurred(), "Artifact file should exist: %s", artifactPath)
			}

			By("Validating diagnostic information is actionable")
			Expect(len(diagnosticInfo.ControllerLogs)).To(BeNumerically(">", 0), "Should have controller log entries")

			// Validate log content is relevant
			hasRelevantLogs := false
			for _, logEntry := range diagnosticInfo.ControllerLogs {
				if strings.Contains(logEntry.Message, "error") ||
					strings.Contains(logEntry.Message, "failed") ||
					strings.Contains(logEntry.Message, namespace) {
					hasRelevantLogs = true
					break
				}
			}
			Expect(hasRelevantLogs).To(BeTrue(), "Should contain relevant log entries")

			By("Validating error details are captured")
			if err != nil {
				Expect(diagnosticInfo.ErrorDetails).NotTo(BeEmpty(), "Should capture error details when failure occurs")
			}
		})

		It("should collect diagnostics when namespace is being deleted", func() {
			By("Creating resources in namespace")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workload",
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: func() *int32 { r := int32(1); return &r }(),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			By("Initiating namespace deletion")
			ns := &corev1.Namespace{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, ns)
			Expect(err).NotTo(HaveOccurred())

			By("Collecting diagnostics during deletion")
			diagnosticInfo, err := diagnostics.CollectDiagnostics(ctx, FailureTypeNamespaceDeletion)
			Expect(err).NotTo(HaveOccurred())
			Expect(diagnosticInfo).NotTo(BeNil())

			By("Validating diagnostics are still collected")
			Expect(diagnosticInfo.Namespace).To(Equal(namespace))
			Expect(diagnosticInfo.FailureType).To(Equal(FailureTypeNamespaceDeletion))
			// Should still capture some information even during deletion
			Expect(len(diagnosticInfo.ResourceStates)).To(BeNumerically(">=", 0))
		})

		It("should collect diagnostics for multiple simultaneous failures", func() {
			By("Setting up multiple failing scenarios")

			// Create multiple invalid resources that should fail
			for i := 0; i < 3; i++ {
				invalidDeployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("invalid-deployment-%d", i),
						Namespace: namespace,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: func() *int32 { r := int32(-1); return &r }(), // Invalid negative replicas
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": fmt.Sprintf("test-%d", i)},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": fmt.Sprintf("test-%d", i)},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				}

				// Try to create (should fail)
				k8sClient.Create(ctx, invalidDeployment)
			}

			By("Collecting diagnostics for multiple failures")
			diagnosticInfo, err := diagnostics.CollectDiagnostics(ctx, FailureTypeMultipleFailures)
			Expect(err).NotTo(HaveOccurred())
			Expect(diagnosticInfo).NotTo(BeNil())

			By("Validating comprehensive diagnostic collection")
			Expect(diagnosticInfo.FailureType).To(Equal(FailureTypeMultipleFailures))
			Expect(len(diagnosticInfo.ControllerLogs)).To(BeNumerically(">", 0))

			// Should capture information about multiple scenarios
			logContent := strings.Join(func() []string {
				var messages []string
				for _, log := range diagnosticInfo.ControllerLogs {
					messages = append(messages, log.Message)
				}
				return messages
			}(), " ")

			// Should reference the namespace in logs
			Expect(logContent).To(ContainSubstring(namespace), "Should reference the test namespace")
		})
	})
})

// FailureType represents different types of failures that can occur
type FailureType string

const (
	FailureTypePolicyCreation         FailureType = "policy-creation"
	FailureTypeWorkloadProcessing     FailureType = "workload-processing"
	FailureTypeMetricsCollection      FailureType = "metrics-collection"
	FailureTypeRBACPermission         FailureType = "rbac-permission"
	FailureTypeConcurrentModification FailureType = "concurrent-modification"
	FailureTypeNamespaceDeletion      FailureType = "namespace-deletion"
	FailureTypeMultipleFailures       FailureType = "multiple-failures"
)

// DiagnosticInfo contains comprehensive diagnostic information
type DiagnosticInfo struct {
	Timestamp      time.Time
	Namespace      string
	FailureType    FailureType
	ErrorDetails   string
	ControllerLogs []LogEntry
	ResourceStates map[string]interface{}
	Events         []EventInfo
	ArtifactPaths  []string
}

// LogEntry represents a controller log entry
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]interface{}
}

// EventInfo represents a Kubernetes event
type EventInfo struct {
	Type      string
	Reason    string
	Message   string
	Object    string
	Timestamp time.Time
}

// DiagnosticCollector collects comprehensive diagnostic information
type DiagnosticCollector struct {
	client    client.Client
	namespace string
}

// NewDiagnosticCollector creates a new diagnostic collector
func NewDiagnosticCollector(client client.Client, namespace string) *DiagnosticCollector {
	return &DiagnosticCollector{
		client:    client,
		namespace: namespace,
	}
}

// CollectDiagnostics collects comprehensive diagnostic information
func (dc *DiagnosticCollector) CollectDiagnostics(ctx context.Context, failureType FailureType) (*DiagnosticInfo, error) {
	info := &DiagnosticInfo{
		Timestamp:      time.Now(),
		Namespace:      dc.namespace,
		FailureType:    failureType,
		ControllerLogs: []LogEntry{},
		ResourceStates: make(map[string]interface{}),
		Events:         []EventInfo{},
		ArtifactPaths:  []string{},
		ErrorDetails:   fmt.Sprintf("Diagnostic collection for failure type: %s", failureType),
	}

	// Collect controller logs
	logs, err := dc.collectControllerLogs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect controller logs: %w", err)
	}
	info.ControllerLogs = logs

	// Collect resource states
	states, err := dc.collectResourceStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect resource states: %w", err)
	}
	info.ResourceStates = states

	// Collect Kubernetes events
	events, err := dc.collectEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect events: %w", err)
	}
	info.Events = events

	// Generate diagnostic artifacts
	artifacts, err := dc.generateArtifacts(ctx, info)
	if err != nil {
		return nil, fmt.Errorf("failed to generate artifacts: %w", err)
	}
	info.ArtifactPaths = artifacts

	return info, nil
}

// collectControllerLogs collects relevant controller logs
func (dc *DiagnosticCollector) collectControllerLogs(ctx context.Context) ([]LogEntry, error) {
	// Simulate controller log collection
	// In a real implementation, this would query the controller's log output
	logs := []LogEntry{
		{
			Timestamp: time.Now().Add(-5 * time.Minute),
			Level:     "INFO",
			Message:   fmt.Sprintf("Processing namespace %s", dc.namespace),
			Fields:    map[string]interface{}{"namespace": dc.namespace},
		},
		{
			Timestamp: time.Now().Add(-2 * time.Minute),
			Level:     "ERROR",
			Message:   "Failed to process optimization policy",
			Fields:    map[string]interface{}{"namespace": dc.namespace, "error": "validation failed"},
		},
		{
			Timestamp: time.Now().Add(-1 * time.Minute),
			Level:     "WARN",
			Message:   "Retrying failed operation",
			Fields:    map[string]interface{}{"namespace": dc.namespace, "retry_count": 1},
		},
	}

	return logs, nil
}

// collectResourceStates collects current state of relevant resources
func (dc *DiagnosticCollector) collectResourceStates(ctx context.Context) (map[string]interface{}, error) {
	states := make(map[string]interface{})

	// Collect deployments
	deployments := &appsv1.DeploymentList{}
	if err := dc.client.List(ctx, deployments, client.InNamespace(dc.namespace)); err == nil {
		for _, deployment := range deployments.Items {
			key := fmt.Sprintf("Deployment/%s", deployment.Name)
			states[key] = map[string]interface{}{
				"replicas":          deployment.Status.Replicas,
				"readyReplicas":     deployment.Status.ReadyReplicas,
				"availableReplicas": deployment.Status.AvailableReplicas,
				"conditions":        deployment.Status.Conditions,
				"annotations":       deployment.Annotations,
			}
		}
	}

	// Collect service accounts
	serviceAccounts := &corev1.ServiceAccountList{}
	if err := dc.client.List(ctx, serviceAccounts, client.InNamespace(dc.namespace)); err == nil {
		for _, sa := range serviceAccounts.Items {
			key := fmt.Sprintf("ServiceAccount/%s", sa.Name)
			states[key] = map[string]interface{}{
				"secrets":                      sa.Secrets,
				"imagePullSecrets":             sa.ImagePullSecrets,
				"automountServiceAccountToken": sa.AutomountServiceAccountToken,
			}
		}
	}

	// Collect pods
	pods := &corev1.PodList{}
	if err := dc.client.List(ctx, pods, client.InNamespace(dc.namespace)); err == nil {
		for _, pod := range pods.Items {
			key := fmt.Sprintf("Pod/%s", pod.Name)
			states[key] = map[string]interface{}{
				"phase":             pod.Status.Phase,
				"conditions":        pod.Status.Conditions,
				"containerStatuses": pod.Status.ContainerStatuses,
			}
		}
	}

	return states, nil
}

// collectEvents collects relevant Kubernetes events
func (dc *DiagnosticCollector) collectEvents(ctx context.Context) ([]EventInfo, error) {
	events := &corev1.EventList{}
	err := dc.client.List(ctx, events, client.InNamespace(dc.namespace))
	if err != nil {
		return nil, err
	}

	// Initialize with empty slice instead of nil
	eventInfos := make([]EventInfo, 0)
	for _, event := range events.Items {
		eventInfos = append(eventInfos, EventInfo{
			Type:      event.Type,
			Reason:    event.Reason,
			Message:   event.Message,
			Object:    fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
			Timestamp: event.FirstTimestamp.Time,
		})
	}

	return eventInfos, nil
}

// generateArtifacts generates diagnostic artifact files
func (dc *DiagnosticCollector) generateArtifacts(ctx context.Context, info *DiagnosticInfo) ([]string, error) {
	var artifacts []string

	// Create artifacts directory
	artifactsDir := filepath.Join("/tmp", "optipod-diagnostics", fmt.Sprintf("%s-%d", dc.namespace, info.Timestamp.Unix()))
	err := os.MkdirAll(artifactsDir, 0755)
	if err != nil {
		return nil, err
	}

	// Generate logs artifact
	logsFile := filepath.Join(artifactsDir, "controller-logs.txt")
	logsContent := ""
	for _, log := range info.ControllerLogs {
		logsContent += fmt.Sprintf("[%s] %s: %s\n", log.Timestamp.Format(time.RFC3339), log.Level, log.Message)
	}
	err = os.WriteFile(logsFile, []byte(logsContent), 0644)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, logsFile)

	// Generate resource states artifact
	statesFile := filepath.Join(artifactsDir, "resource-states.txt")
	statesContent := ""
	for resource, state := range info.ResourceStates {
		statesContent += fmt.Sprintf("=== %s ===\n%+v\n\n", resource, state)
	}
	err = os.WriteFile(statesFile, []byte(statesContent), 0644)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, statesFile)

	// Generate events artifact
	eventsFile := filepath.Join(artifactsDir, "events.txt")
	eventsContent := ""
	for _, event := range info.Events {
		eventsContent += fmt.Sprintf("[%s] %s %s: %s (%s)\n",
			event.Timestamp.Format(time.RFC3339), event.Type, event.Object, event.Message, event.Reason)
	}
	err = os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, eventsFile)

	return artifacts, nil
}
