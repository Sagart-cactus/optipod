//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ParallelConfig holds configuration for parallel test execution
type ParallelConfig struct {
	// MaxParallelNodes is the maximum number of parallel test nodes
	MaxParallelNodes int
	// NamespacePrefix is the prefix used for test namespaces to ensure isolation
	NamespacePrefix string
	// ResourceIsolation enables resource isolation between parallel tests
	ResourceIsolation bool
	// TimeoutMultiplier adjusts timeouts for parallel execution
	TimeoutMultiplier float64
}

// DefaultParallelConfig returns the default configuration for parallel execution
func DefaultParallelConfig() *ParallelConfig {
	maxNodes := 4
	if envNodes := os.Getenv("E2E_PARALLEL_NODES"); envNodes != "" {
		if parsed, err := strconv.Atoi(envNodes); err == nil && parsed > 0 {
			maxNodes = parsed
		}
	}

	timeoutMultiplier := 1.0
	if envTimeout := os.Getenv("E2E_TIMEOUT_MULTIPLIER"); envTimeout != "" {
		if parsed, err := strconv.ParseFloat(envTimeout, 64); err == nil && parsed > 0 {
			timeoutMultiplier = parsed
		}
	}

	return &ParallelConfig{
		MaxParallelNodes:  maxNodes,
		NamespacePrefix:   "e2e-parallel",
		ResourceIsolation: true,
		TimeoutMultiplier: timeoutMultiplier,
	}
}

// ParallelTestManager manages parallel test execution and resource isolation
type ParallelTestManager struct {
	config       *ParallelConfig
	client       client.Client
	namespaces   map[int]string
	namespaceMux sync.RWMutex
}

// NewParallelTestManager creates a new parallel test manager
func NewParallelTestManager(client client.Client, config *ParallelConfig) *ParallelTestManager {
	if config == nil {
		config = DefaultParallelConfig()
	}

	return &ParallelTestManager{
		config:     config,
		client:     client,
		namespaces: make(map[int]string),
	}
}

// GetIsolatedNamespace returns a namespace isolated for the current parallel node
func (ptm *ParallelTestManager) GetIsolatedNamespace(ctx context.Context) (string, error) {
	nodeID := GinkgoParallelProcess()

	ptm.namespaceMux.Lock()
	defer ptm.namespaceMux.Unlock()

	if namespace, exists := ptm.namespaces[nodeID]; exists {
		return namespace, nil
	}

	// Create isolated namespace for this parallel node
	namespace := fmt.Sprintf("%s-node-%d-%d", ptm.config.NamespacePrefix, nodeID, time.Now().Unix())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"e2e-test":      "true",
				"parallel-node": strconv.Itoa(nodeID),
				"test-run":      strconv.FormatInt(time.Now().Unix(), 10),
			},
		},
	}

	err := ptm.client.Create(ctx, ns)
	if err != nil {
		return "", fmt.Errorf("failed to create isolated namespace %s: %w", namespace, err)
	}

	ptm.namespaces[nodeID] = namespace
	return namespace, nil
}

// CleanupIsolatedNamespace cleans up the namespace for the current parallel node
func (ptm *ParallelTestManager) CleanupIsolatedNamespace(ctx context.Context) error {
	nodeID := GinkgoParallelProcess()

	ptm.namespaceMux.Lock()
	defer ptm.namespaceMux.Unlock()

	namespace, exists := ptm.namespaces[nodeID]
	if !exists {
		return nil // Nothing to cleanup
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	err := ptm.client.Delete(ctx, ns)
	if err != nil {
		return fmt.Errorf("failed to delete isolated namespace %s: %w", namespace, err)
	}

	delete(ptm.namespaces, nodeID)
	return nil
}

// GetAdjustedTimeout returns a timeout adjusted for parallel execution
func (ptm *ParallelTestManager) GetAdjustedTimeout(baseTimeout time.Duration) time.Duration {
	adjusted := time.Duration(float64(baseTimeout) * ptm.config.TimeoutMultiplier)

	// Add extra time for parallel execution overhead
	if GinkgoParallelProcess() > 1 {
		adjusted += time.Duration(float64(baseTimeout) * 0.2) // 20% overhead for parallel execution
	}

	return adjusted
}

// IsParallelExecution returns true if tests are running in parallel
func (ptm *ParallelTestManager) IsParallelExecution() bool {
	return GinkgoParallelProcess() > 1 || os.Getenv("GINKGO_PARALLEL") == "true"
}

// GetParallelNodeInfo returns information about the current parallel node
func (ptm *ParallelTestManager) GetParallelNodeInfo() (nodeID, totalNodes int) {
	// For now, return default values since GinkgoParallelTotal() is not available
	// In a real implementation, this would use Ginkgo's parallel execution info
	nodeID = GinkgoParallelProcess()
	totalNodes = ptm.config.MaxParallelNodes
	if nodeID == 0 {
		nodeID = 1
		totalNodes = 1
	}
	return nodeID, totalNodes
}

// WaitForParallelStability waits for parallel test stability
func (ptm *ParallelTestManager) WaitForParallelStability(ctx context.Context) error {
	if !ptm.IsParallelExecution() {
		return nil // No need to wait for stability in serial execution
	}

	// Wait a bit longer in parallel execution to avoid resource conflicts
	stabilityDelay := time.Duration(GinkgoParallelProcess()) * time.Second
	if stabilityDelay > 10*time.Second {
		stabilityDelay = 10 * time.Second
	}

	select {
	case <-time.After(stabilityDelay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ValidateParallelIsolation validates that parallel tests are properly isolated
func (ptm *ParallelTestManager) ValidateParallelIsolation(ctx context.Context) error {
	if !ptm.IsParallelExecution() {
		return nil // No isolation needed for serial execution
	}

	nodeID := GinkgoParallelProcess()

	// Check that our namespace exists and is isolated
	ptm.namespaceMux.RLock()
	namespace, exists := ptm.namespaces[nodeID]
	ptm.namespaceMux.RUnlock()

	if !exists {
		return fmt.Errorf("no isolated namespace found for parallel node %d", nodeID)
	}

	// Verify namespace exists and has correct labels
	ns := &corev1.Namespace{}
	key := types.NamespacedName{Name: namespace}
	err := ptm.client.Get(ctx, key, ns)
	if err != nil {
		return fmt.Errorf("failed to get isolated namespace %s: %w", namespace, err)
	}

	expectedNodeID := ns.Labels["parallel-node"]
	if expectedNodeID != strconv.Itoa(nodeID) {
		return fmt.Errorf("namespace %s has incorrect parallel-node label: expected %d, got %s",
			namespace, nodeID, expectedNodeID)
	}

	return nil
}

// Global parallel test manager instance
var parallelTestManager *ParallelTestManager

// InitializeParallelExecution initializes parallel test execution
func InitializeParallelExecution(client client.Client) {
	config := DefaultParallelConfig()
	parallelTestManager = NewParallelTestManager(client, config)

	// Log parallel execution info
	if parallelTestManager.IsParallelExecution() {
		nodeID, totalNodes := parallelTestManager.GetParallelNodeInfo()
		_, _ = fmt.Fprintf(GinkgoWriter, "Parallel execution enabled: Node %d of %d (Max nodes: %d)\n",
			nodeID, totalNodes, config.MaxParallelNodes)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Serial execution mode\n")
	}
}

// GetParallelTestManager returns the global parallel test manager
func GetParallelTestManager() *ParallelTestManager {
	return parallelTestManager
}

// Helper functions for common parallel test operations

// CreateIsolatedTestNamespace creates a test namespace isolated for parallel execution
func CreateIsolatedTestNamespace(ctx context.Context, client client.Client, baseName string) (string, error) {
	if parallelTestManager == nil {
		return "", fmt.Errorf("parallel test manager not initialized")
	}

	isolatedNS, err := parallelTestManager.GetIsolatedNamespace(ctx)
	if err != nil {
		return "", err
	}

	// If a base name is provided, create a sub-namespace concept using labels
	if baseName != "" {
		ns := &corev1.Namespace{}
		key := types.NamespacedName{Name: isolatedNS}
		err := client.Get(ctx, key, ns)
		if err != nil {
			return "", fmt.Errorf("failed to get isolated namespace: %w", err)
		}

		// Add base name as a label for identification
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["test-base-name"] = baseName

		err = client.Update(ctx, ns)
		if err != nil {
			return "", fmt.Errorf("failed to update namespace labels: %w", err)
		}
	}

	return isolatedNS, nil
}

// GetAdjustedTestTimeout returns a timeout adjusted for parallel execution
func GetAdjustedTestTimeout(baseTimeout time.Duration) time.Duration {
	if parallelTestManager == nil {
		return baseTimeout
	}
	return parallelTestManager.GetAdjustedTimeout(baseTimeout)
}

// WaitForTestStability waits for test stability in parallel execution
func WaitForTestStability(ctx context.Context) error {
	if parallelTestManager == nil {
		return nil
	}
	return parallelTestManager.WaitForParallelStability(ctx)
}

// ValidateTestIsolation validates that tests are properly isolated
func ValidateTestIsolation(ctx context.Context) error {
	if parallelTestManager == nil {
		return nil
	}
	return parallelTestManager.ValidateParallelIsolation(ctx)
}
