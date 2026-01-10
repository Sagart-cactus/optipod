package simulators

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/controller"
	"github.com/optipod/optipod/internal/policy"
	"github.com/optipod/optipod/test/clusterless/harness"
)

// ControllerSimulator wraps the real reconciler for testability
type ControllerSimulator struct {
	reconciler *controller.OptimizationPolicyReconciler
	harness    *harness.TestHarness
}

// NewControllerSimulator creates a new controller simulator
func NewControllerSimulator(h *harness.TestHarness) *ControllerSimulator {
	// Create real reconciler using fake clients
	reconciler := &controller.OptimizationPolicyReconciler{
		Client:         h.Client,
		Scheme:         h.Scheme,
		EventRecorder:  h.EventRecorder,
		PolicySelector: policy.NewPolicySelector(h.Client),
		// WorkloadProcessor will be created in tests as needed
	}

	return &ControllerSimulator{
		reconciler: reconciler,
		harness:    h,
	}
}

// ReconcilePolicy triggers reconciliation for a specific policy
func (s *ControllerSimulator) ReconcilePolicy(
	ctx context.Context,
	pol *optipodv1alpha1.OptimizationPolicy,
) (ctrl.Result, error) {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pol.Name,
			Namespace: pol.Namespace,
		},
	}

	return s.reconciler.Reconcile(ctx, req)
}

// ReconcilePolicyByName triggers reconciliation by policy name
func (s *ControllerSimulator) ReconcilePolicyByName(
	ctx context.Context,
	name, namespace string,
) (ctrl.Result, error) {
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	return s.reconciler.Reconcile(ctx, req)
}

// VerifyPolicyStatus checks that policy status conditions match expectations
func (s *ControllerSimulator) VerifyPolicyStatus(
	ctx context.Context,
	policyName, namespace string,
	expectedConditions map[string]metav1.ConditionStatus,
) error {
	pol := &optipodv1alpha1.OptimizationPolicy{}
	key := types.NamespacedName{Name: policyName, Namespace: namespace}

	if err := s.harness.Client.Get(ctx, key, pol); err != nil {
		return fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
	}

	for condType, expectedStatus := range expectedConditions {
		found := false
		for _, cond := range pol.Status.Conditions {
			if cond.Type == condType {
				found = true
				if cond.Status != expectedStatus {
					return fmt.Errorf("condition %s: expected %s, got %s (reason: %s, message: %s)",
						condType, expectedStatus, cond.Status, cond.Reason, cond.Message)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("condition %s not found in policy status", condType)
		}
	}

	return nil
}

// GetPolicyStatus returns the current policy status
func (s *ControllerSimulator) GetPolicyStatus(
	ctx context.Context,
	policyName, namespace string,
) (*optipodv1alpha1.OptimizationPolicyStatus, error) {
	pol := &optipodv1alpha1.OptimizationPolicy{}
	key := types.NamespacedName{Name: policyName, Namespace: namespace}

	if err := s.harness.Client.Get(ctx, key, pol); err != nil {
		return nil, fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
	}

	return &pol.Status, nil
}

// VerifyPolicyCondition checks a specific condition
func (s *ControllerSimulator) VerifyPolicyCondition(
	ctx context.Context,
	policyName, namespace string,
	conditionType string,
	expectedStatus metav1.ConditionStatus,
	expectedReason string,
) error {
	pol := &optipodv1alpha1.OptimizationPolicy{}
	key := types.NamespacedName{Name: policyName, Namespace: namespace}

	if err := s.harness.Client.Get(ctx, key, pol); err != nil {
		return fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
	}

	for _, cond := range pol.Status.Conditions {
		if cond.Type == conditionType {
			if cond.Status != expectedStatus {
				return fmt.Errorf("condition %s status: expected %s, got %s",
					conditionType, expectedStatus, cond.Status)
			}
			if expectedReason != "" && cond.Reason != expectedReason {
				return fmt.Errorf("condition %s reason: expected %s, got %s",
					conditionType, expectedReason, cond.Reason)
			}
			return nil
		}
	}

	return fmt.Errorf("condition %s not found", conditionType)
}

// GetPolicyCondition returns a specific condition
func (s *ControllerSimulator) GetPolicyCondition(
	ctx context.Context,
	policyName, namespace string,
	conditionType string,
) (*metav1.Condition, error) {
	pol := &optipodv1alpha1.OptimizationPolicy{}
	key := types.NamespacedName{Name: policyName, Namespace: namespace}

	if err := s.harness.Client.Get(ctx, key, pol); err != nil {
		return nil, fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
	}

	for _, cond := range pol.Status.Conditions {
		if cond.Type == conditionType {
			return &cond, nil
		}
	}

	return nil, fmt.Errorf("condition %s not found", conditionType)
}

// VerifyPolicyMetrics checks that policy metrics are updated correctly
func (s *ControllerSimulator) VerifyPolicyMetrics(
	ctx context.Context,
	policyName, namespace string,
	expectedWorkloadCount int,
	expectedRecommendationCount int,
) error {
	pol := &optipodv1alpha1.OptimizationPolicy{}
	key := types.NamespacedName{Name: policyName, Namespace: namespace}

	if err := s.harness.Client.Get(ctx, key, pol); err != nil {
		return fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
	}

	if pol.Status.WorkloadsDiscovered != expectedWorkloadCount {
		return fmt.Errorf("workload count: expected %d, got %d",
			expectedWorkloadCount, pol.Status.WorkloadsDiscovered)
	}

	if pol.Status.WorkloadsProcessed != expectedRecommendationCount {
		return fmt.Errorf("processed count: expected %d, got %d",
			expectedRecommendationCount, pol.Status.WorkloadsProcessed)
	}

	return nil
}

// SimulateReconcileLoop runs multiple reconciliation cycles
func (s *ControllerSimulator) SimulateReconcileLoop(
	ctx context.Context,
	pol *optipodv1alpha1.OptimizationPolicy,
	cycles int,
) error {
	for i := 0; i < cycles; i++ {
		result, err := s.ReconcilePolicy(ctx, pol)
		if err != nil {
			return fmt.Errorf("reconcile cycle %d failed: %w", i+1, err)
		}

		// If requeue is requested, we would normally wait, but in tests we continue immediately.
		if result.RequeueAfter > 0 {
			continue
		}
	}
	return nil
}

// GetReconciler returns the underlying reconciler for advanced testing
func (s *ControllerSimulator) GetReconciler() *controller.OptimizationPolicyReconciler {
	return s.reconciler
}

// SetWorkloadProcessor sets a custom workload processor for testing
func (s *ControllerSimulator) SetWorkloadProcessor(processor interface{}) {
	// This would set the workload processor on the reconciler
	// Implementation depends on the actual reconciler structure
}

// SetPolicySelector sets a custom policy selector for testing
func (s *ControllerSimulator) SetPolicySelector(selector *policy.PolicySelector) {
	s.reconciler.PolicySelector = selector
}
