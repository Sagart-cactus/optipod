package helpers

import (
	"context"
	"fmt"
	"time"

	"github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicyHelper provides utilities for managing OptimizationPolicy resources in tests
type PolicyHelper struct {
	client    client.Client
	namespace string
}

// NewPolicyHelper creates a new PolicyHelper instance
func NewPolicyHelper(k8sClient client.Client, namespace string) *PolicyHelper {
	return &PolicyHelper{
		client:    k8sClient,
		namespace: namespace,
	}
}

// PolicyConfig defines configuration for creating OptimizationPolicy resources
type PolicyConfig struct {
	Name                   string
	Mode                   v1alpha1.PolicyMode
	NamespaceSelector      map[string]string
	WorkloadSelector       map[string]string
	ResourceBounds         ResourceBounds
	MetricsConfig          MetricsConfig
	UpdateStrategy         UpdateStrategy
	ReconciliationInterval *metav1.Duration
}

// ResourceBounds defines CPU and memory bounds
type ResourceBounds struct {
	CPU    ResourceBound
	Memory ResourceBound
}

// ResourceBound defines min/max limits for a resource
type ResourceBound struct {
	Min string
	Max string
}

// MetricsConfig defines metrics collection configuration
type MetricsConfig struct {
	Provider      string
	RollingWindow string
	Percentile    string
	SafetyFactor  float64
}

// UpdateStrategy defines how workloads should be updated
type UpdateStrategy struct {
	AllowInPlaceResize bool
	AllowRecreate      bool
	UpdateRequestsOnly bool
}

// CreateOptimizationPolicy creates an OptimizationPolicy with the specified configuration
func (h *PolicyHelper) CreateOptimizationPolicy(config PolicyConfig) (*v1alpha1.OptimizationPolicy, error) {
	policy := &v1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: h.namespace,
		},
		Spec: v1alpha1.OptimizationPolicySpec{
			Mode: config.Mode,
			Selector: v1alpha1.WorkloadSelector{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: config.NamespaceSelector,
				},
				WorkloadSelector: &metav1.LabelSelector{
					MatchLabels: config.WorkloadSelector,
				},
			},
			MetricsConfig: v1alpha1.MetricsConfig{
				Provider:      config.MetricsConfig.Provider,
				RollingWindow: parseDuration(config.MetricsConfig.RollingWindow),
				Percentile:    config.MetricsConfig.Percentile,
				SafetyFactor:  &config.MetricsConfig.SafetyFactor,
			},
			UpdateStrategy: v1alpha1.UpdateStrategy{
				AllowInPlaceResize: config.UpdateStrategy.AllowInPlaceResize,
				AllowRecreate:      config.UpdateStrategy.AllowRecreate,
				UpdateRequestsOnly: config.UpdateStrategy.UpdateRequestsOnly,
			},
		},
	}

	// Set resource bounds - ResourceBounds is not a pointer, so we set it directly
	policy.Spec.ResourceBounds = v1alpha1.ResourceBounds{
		CPU: v1alpha1.ResourceBound{
			Min: resource.MustParse(config.ResourceBounds.CPU.Min),
			Max: resource.MustParse(config.ResourceBounds.CPU.Max),
		},
		Memory: v1alpha1.ResourceBound{
			Min: resource.MustParse(config.ResourceBounds.Memory.Min),
			Max: resource.MustParse(config.ResourceBounds.Memory.Max),
		},
	}

	// Set reconciliation interval if provided
	if config.ReconciliationInterval != nil {
		policy.Spec.ReconciliationInterval = *config.ReconciliationInterval
	}

	err := h.client.Create(context.TODO(), policy)
	if err != nil {
		return nil, fmt.Errorf("failed to create OptimizationPolicy %s: %w", config.Name, err)
	}

	return policy, nil
}

// WaitForPolicyReady waits for the OptimizationPolicy to reach Ready condition
func (h *PolicyHelper) WaitForPolicyReady(policyName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 5*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			policy := &v1alpha1.OptimizationPolicy{}
			err := h.client.Get(context.TODO(), types.NamespacedName{
				Name:      policyName,
				Namespace: h.namespace,
			}, policy)
			if err != nil {
				return false, err
			}

			// Check if Ready condition is True
			for _, condition := range policy.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		})
}

// ValidatePolicyBehavior validates that the policy behaves according to its mode
func (h *PolicyHelper) ValidatePolicyBehavior(policyName string, expectedMode v1alpha1.PolicyMode) error {
	policy := &v1alpha1.OptimizationPolicy{}
	err := h.client.Get(context.TODO(), types.NamespacedName{
		Name:      policyName,
		Namespace: h.namespace,
	}, policy)
	if err != nil {
		return fmt.Errorf("failed to get policy %s: %w", policyName, err)
	}

	if policy.Spec.Mode != expectedMode {
		return fmt.Errorf("policy mode mismatch: expected %s, got %s", expectedMode, policy.Spec.Mode)
	}

	// Additional validation based on mode
	switch expectedMode {
	case v1alpha1.ModeAuto:
		// In Auto mode, we expect workloads to be processed
		// For now, we just check that the policy is in the correct mode
		// Individual workload status validation will be done in the tests
		break
	case v1alpha1.ModeRecommend:
		// In Recommend mode, we expect recommendations but no updates
		// For now, we just check that the policy is in the correct mode
		// Individual workload status validation will be done in the tests
		break
	case v1alpha1.ModeDisabled:
		// In Disabled mode, we expect no workload processing
		// We can check that WorkloadsDiscovered and WorkloadsProcessed are 0
		if policy.Status.WorkloadsDiscovered > 0 || policy.Status.WorkloadsProcessed > 0 {
			return fmt.Errorf("disabled mode policy should not process any workloads, but found %d discovered and %d processed",
				policy.Status.WorkloadsDiscovered, policy.Status.WorkloadsProcessed)
		}
	}

	return nil
}

// GetPolicy retrieves an OptimizationPolicy by name
func (h *PolicyHelper) GetPolicy(policyName string) (*v1alpha1.OptimizationPolicy, error) {
	policy := &v1alpha1.OptimizationPolicy{}
	err := h.client.Get(context.TODO(), types.NamespacedName{
		Name:      policyName,
		Namespace: h.namespace,
	}, policy)
	return policy, err
}

// DeletePolicy deletes an OptimizationPolicy by name
func (h *PolicyHelper) DeletePolicy(policyName string) error {
	policy := &v1alpha1.OptimizationPolicy{}
	err := h.client.Get(context.TODO(), types.NamespacedName{
		Name:      policyName,
		Namespace: h.namespace,
	}, policy)
	if err != nil {
		return err
	}

	return h.client.Delete(context.TODO(), policy)
}

// parseDuration parses a duration string and returns a metav1.Duration
func parseDuration(durationStr string) metav1.Duration {
	if durationStr == "" {
		return metav1.Duration{Duration: time.Hour} // Default to 1h
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return metav1.Duration{Duration: time.Hour} // Default to 1h on error
	}

	return metav1.Duration{Duration: duration}
}
