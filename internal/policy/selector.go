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

package policy

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
)

// workloadTypeMatches checks if a workload type matches the policy's workload type filter
func workloadTypeMatches(filter *optipodv1alpha1.WorkloadTypeFilter, workloadKind string) bool {
	if filter == nil {
		return true // No filter = all types match (backward compatibility)
	}

	workloadType := optipodv1alpha1.WorkloadType(workloadKind)
	activeTypes := optipodv1alpha1.GetActiveWorkloadTypes(filter)
	return activeTypes.Contains(workloadType)
}

// PolicySelector handles selection of the best policy for a workload when multiple policies match
type PolicySelector struct {
	client client.Client
}

// NewPolicySelector creates a new policy selector
func NewPolicySelector(c client.Client) *PolicySelector {
	return &PolicySelector{
		client: c,
	}
}

// PolicyMatch represents a policy that matches a workload along with its weight
type PolicyMatch struct {
	Policy *optipodv1alpha1.OptimizationPolicy
	Weight int32
}

// SelectBestPolicy finds the best policy for a workload based on weights
// Returns the policy with the highest weight, or an error if no policies match
func (ps *PolicySelector) SelectBestPolicy(ctx context.Context, workload *discovery.Workload) (*optipodv1alpha1.OptimizationPolicy, error) {
	log := logf.FromContext(ctx)

	// Get all policies
	policyList := &optipodv1alpha1.OptimizationPolicyList{}
	if err := ps.client.List(ctx, policyList); err != nil {
		return nil, fmt.Errorf("failed to list optimization policies: %w", err)
	}

	// Find matching policies
	var matches []PolicyMatch
	for _, policy := range policyList.Items {
		// Skip disabled policies
		if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
			continue
		}

		// Check if policy matches workload
		if ps.policyMatchesWorkload(ctx, &policy, workload) {
			matches = append(matches, PolicyMatch{
				Policy: &policy,
				Weight: policy.GetWeight(),
			})
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no matching policies found for workload %s/%s", workload.Namespace, workload.Name)
	}

	// Sort by weight (highest first), then by name for deterministic ordering
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Weight != matches[j].Weight {
			return matches[i].Weight > matches[j].Weight
		}
		// If weights are equal, sort by name for deterministic behavior
		return matches[i].Policy.Name < matches[j].Policy.Name
	})

	selectedPolicy := matches[0].Policy

	// Log policy selection details
	if len(matches) > 1 {
		log.Info("Multiple policies match workload, selected highest weight",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"selectedPolicy", selectedPolicy.Name,
			"selectedWeight", selectedPolicy.GetWeight(),
			"totalMatches", len(matches))

		// Log other matching policies for visibility
		for i, match := range matches[1:] {
			if i < 3 { // Limit to first 3 alternatives to avoid log spam
				log.V(1).Info("Alternative policy",
					"policy", match.Policy.Name,
					"weight", match.Weight)
			}
		}
	} else {
		log.V(1).Info("Single policy matches workload",
			"workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
			"policy", selectedPolicy.Name,
			"weight", selectedPolicy.GetWeight())
	}

	return selectedPolicy, nil
}

// policyMatchesWorkload checks if a policy's selectors match a workload
func (ps *PolicySelector) policyMatchesWorkload(ctx context.Context, policy *optipodv1alpha1.OptimizationPolicy, workload *discovery.Workload) bool {
	// Check workload type filter first (new)
	if !workloadTypeMatches(policy.Spec.Selector.WorkloadTypes, workload.Kind) {
		return false
	}

	// Use the same logic as discovery.DiscoverWorkloads but for a single workload
	// This ensures consistency with the existing workload discovery logic

	// Check namespace selector
	if policy.Spec.Selector.NamespaceSelector != nil {
		// Get the namespace to check its labels
		namespace := &corev1.Namespace{}
		if err := ps.client.Get(ctx, client.ObjectKey{Name: workload.Namespace}, namespace); err != nil {
			// If we can't get the namespace, assume it doesn't match
			return false
		}

		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.NamespaceSelector)
		if err != nil {
			return false
		}

		if !selector.Matches(labels.Set(namespace.Labels)) {
			return false
		}
	}

	// Check namespace allow/deny lists
	if policy.Spec.Selector.Namespaces != nil {
		// Check deny list first (takes precedence)
		for _, denied := range policy.Spec.Selector.Namespaces.Deny {
			if workload.Namespace == denied {
				return false
			}
		}

		// Check allow list if specified
		if len(policy.Spec.Selector.Namespaces.Allow) > 0 {
			allowed := false
			for _, allowed_ns := range policy.Spec.Selector.Namespaces.Allow {
				if workload.Namespace == allowed_ns {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}

	// Check workload selector
	if policy.Spec.Selector.WorkloadSelector != nil {
		workloadLabels := ps.getWorkloadLabels(workload)

		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.WorkloadSelector)
		if err != nil {
			return false
		}

		if !selector.Matches(labels.Set(workloadLabels)) {
			return false
		}
	}

	return true
}

// getWorkloadLabels extracts labels from a workload object
func (ps *PolicySelector) getWorkloadLabels(workload *discovery.Workload) map[string]string {
	switch obj := workload.Object.(type) {
	case *appsv1.Deployment:
		return obj.Labels
	case *appsv1.StatefulSet:
		return obj.Labels
	case *appsv1.DaemonSet:
		return obj.Labels
	default:
		return nil
	}
}

// GetMatchingPolicies returns all policies that match a workload, sorted by weight
func (ps *PolicySelector) GetMatchingPolicies(ctx context.Context, workload *discovery.Workload) ([]PolicyMatch, error) {
	// Get all policies
	policyList := &optipodv1alpha1.OptimizationPolicyList{}
	if err := ps.client.List(ctx, policyList); err != nil {
		return nil, fmt.Errorf("failed to list optimization policies: %w", err)
	}

	// Find matching policies
	var matches []PolicyMatch
	for _, policy := range policyList.Items {
		// Skip disabled policies
		if policy.Spec.Mode == optipodv1alpha1.ModeDisabled {
			continue
		}

		// Check if policy matches workload
		if ps.policyMatchesWorkload(ctx, &policy, workload) {
			matches = append(matches, PolicyMatch{
				Policy: &policy,
				Weight: policy.GetWeight(),
			})
		}
	}

	// Sort by weight (highest first), then by name for deterministic ordering
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Weight != matches[j].Weight {
			return matches[i].Weight > matches[j].Weight
		}
		return matches[i].Policy.Name < matches[j].Policy.Name
	})

	return matches, nil
}
