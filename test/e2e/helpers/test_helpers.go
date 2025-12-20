package helpers

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/onsi/gomega"
	"github.com/optipod/optipod/test/utils"
)

// PolicyHelper provides utilities for managing OptimizationPolicies in tests
type PolicyHelper struct{}

// NewPolicyHelper creates a new PolicyHelper instance
func NewPolicyHelper() *PolicyHelper {
	return &PolicyHelper{}
}

// CreatePolicyFromFile applies a policy from a YAML file
func (p *PolicyHelper) CreatePolicyFromFile(filePath string) error {
	cmd := exec.Command("kubectl", "apply", "-f", filePath, "--validate=false")
	_, err := utils.Run(cmd)
	return err
}

// DeletePolicyFromFile deletes a policy from a YAML file
func (p *PolicyHelper) DeletePolicyFromFile(filePath string) error {
	cmd := exec.Command("kubectl", "delete", "-f", filePath, "--ignore-not-found=true")
	_, err := utils.Run(cmd)
	return err
}

// WaitForPolicyReady waits for a policy to be in Ready state
func (p *PolicyHelper) WaitForPolicyReady(policyName, namespace string, timeout time.Duration) error {
	gomega.Eventually(func() string {
		cmd := exec.Command("kubectl", "get", "optimizationpolicy", policyName, "-n", namespace,
			"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
		output, _ := utils.Run(cmd)
		return strings.TrimSpace(output)
	}, timeout, 5*time.Second).Should(gomega.Equal("True"))
	return nil
}

// WorkloadHelper provides utilities for managing workloads in tests
type WorkloadHelper struct{}

// NewWorkloadHelper creates a new WorkloadHelper instance
func NewWorkloadHelper() *WorkloadHelper {
	return &WorkloadHelper{}
}

// CreateWorkloadFromFile applies a workload from a YAML file
func (w *WorkloadHelper) CreateWorkloadFromFile(filePath string) error {
	cmd := exec.Command("kubectl", "apply", "-f", filePath, "--validate=false")
	_, err := utils.Run(cmd)
	return err
}

// DeleteWorkloadFromFile deletes a workload from a YAML file
func (w *WorkloadHelper) DeleteWorkloadFromFile(filePath string) error {
	cmd := exec.Command("kubectl", "delete", "-f", filePath, "--ignore-not-found=true")
	_, err := utils.Run(cmd)
	return err
}

// WaitForWorkloadReady waits for a deployment to be available
func (w *WorkloadHelper) WaitForWorkloadReady(workloadName, namespace string, timeout time.Duration) error {
	gomega.Eventually(func() error {
		cmd := exec.Command("kubectl", "wait", "--for=condition=available", "--timeout=60s",
			fmt.Sprintf("deployment/%s", workloadName), "-n", namespace)
		_, err := utils.Run(cmd)
		return err
	}, timeout, 5*time.Second).Should(gomega.Succeed())
	return nil
}

// GetWorkloadAnnotations retrieves OptipPod annotations from a workload
func (w *WorkloadHelper) GetWorkloadAnnotations(workloadName, namespace string) (map[string]string, error) {
	cmd := exec.Command("kubectl", "get", "deployment", workloadName, "-n", namespace,
		"-o", "jsonpath={.metadata.annotations}")
	output, err := utils.Run(cmd)
	if err != nil {
		return nil, err
	}

	// Simple parsing - in a real implementation you'd use JSON parsing
	annotations := make(map[string]string)
	if strings.Contains(output, "optipod.io/managed") {
		annotations["optipod.io/managed"] = "true"
	}

	return annotations, nil
}

// ValidationHelper provides utilities for validating test results
type ValidationHelper struct{}

// NewValidationHelper creates a new ValidationHelper instance
func NewValidationHelper() *ValidationHelper {
	return &ValidationHelper{}
}

// CheckOptipodLogs searches OptipPod controller logs for specific patterns
func (v *ValidationHelper) CheckOptipodLogs(pattern string, since time.Duration) (bool, error) {
	sinceFlag := fmt.Sprintf("--since=%s", since.String())
	cmd := exec.Command("kubectl", "logs", "-n", "optipod-system", "deployment/optipod-controller-manager", sinceFlag)
	output, err := utils.Run(cmd)
	if err != nil {
		return false, err
	}

	return strings.Contains(output, pattern), nil
}

// ValidateMetricsEndpoint checks if the metrics endpoint is accessible
func (v *ValidationHelper) ValidateMetricsEndpoint() error {
	// Create a curl pod to test metrics endpoint
	curlPod := `
apiVersion: v1
kind: Pod
metadata:
  name: curl-metrics-test
  namespace: optipod-system
spec:
  restartPolicy: Never
  containers:
  - name: curl
    image: curlimages/curl:latest
    command: ["curl", "-s", "http://optipod-controller-manager-metrics-service:8080/metrics"]
`

	// Apply curl pod
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(curlPod)
	_, err := utils.Run(cmd)
	if err != nil {
		return err
	}

	// Wait for pod to complete and check logs
	gomega.Eventually(func() string {
		cmd := exec.Command("kubectl", "get", "pod", "curl-metrics-test", "-n", "optipod-system",
			"-o", "jsonpath={.status.phase}")
		output, _ := utils.Run(cmd)
		return output
	}, 1*time.Minute, 5*time.Second).Should(gomega.Equal("Succeeded"))

	// Get logs to check metrics content
	cmd = exec.Command("kubectl", "logs", "curl-metrics-test", "-n", "optipod-system")
	output, err := utils.Run(cmd)
	if err != nil {
		return err
	}

	// Cleanup
	cmd = exec.Command("kubectl", "delete", "pod", "curl-metrics-test", "-n", "optipod-system", "--ignore-not-found=true")
	_, _ = utils.Run(cmd) // Ignore cleanup errors

	// Validate metrics content
	if !strings.Contains(output, "# HELP") {
		return fmt.Errorf("metrics endpoint did not return Prometheus format")
	}

	return nil
}

// CleanupHelper provides utilities for cleaning up test resources
type CleanupHelper struct{}

// NewCleanupHelper creates a new CleanupHelper instance
func NewCleanupHelper() *CleanupHelper {
	return &CleanupHelper{}
}

// CleanupAllPolicies removes all OptimizationPolicies from optipod-system namespace
func (c *CleanupHelper) CleanupAllPolicies() error {
	cmd := exec.Command("kubectl", "delete", "optimizationpolicy", "--all", "-n", "optipod-system",
		"--ignore-not-found=true")
	_, err := utils.Run(cmd)
	return err
}

// CleanupAllWorkloads removes all deployments from default namespace
func (c *CleanupHelper) CleanupAllWorkloads() error {
	cmd := exec.Command("kubectl", "delete", "deployment", "--all", "-n", "default", "--ignore-not-found=true")
	_, err := utils.Run(cmd)
	return err
}

// CleanupTestWorkloads removes test workloads from specified namespace
func (c *CleanupHelper) CleanupTestWorkloads(namespace string) error {
	// Delete specific test workloads
	testWorkloads := []string{
		"policy-mode-auto-test",
		"policy-mode-recommend-test",
		"policy-mode-disabled-test",
		"bounds-within-test",
		"bounds-below-min-test",
		"bounds-above-max-test",
		"boundary-limits-test",
		"invalid-bounds-test",
	}

	for _, workload := range testWorkloads {
		cmd := exec.Command("kubectl", "delete", "deployment", workload, "-n", namespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd) // Ignore cleanup errors
	}

	return nil
}

// CleanupNamespace removes a namespace and all its resources
func (c *CleanupHelper) CleanupNamespace(namespace string) error {
	cmd := exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found=true")
	_, err := utils.Run(cmd)
	return err
}
