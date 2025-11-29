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

package discovery

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// Workload represents a Kubernetes resource that manages pods
type Workload struct {
	Kind      string
	Namespace string
	Name      string
	Labels    map[string]string
	Object    client.Object
}

// DiscoverWorkloads discovers workloads matching the policy selectors
// It queries Deployments, StatefulSets, and DaemonSets matching label selectors,
// filters by namespace selectors, and applies allow/deny namespace lists with deny precedence.
func DiscoverWorkloads(ctx context.Context, c client.Client, policy *optipodv1alpha1.OptimizationPolicy) ([]Workload, error) {
	var allWorkloads []Workload

	// Get all namespaces that match the policy
	namespaces, err := getMatchingNamespaces(ctx, c, policy)
	if err != nil {
		return nil, err
	}

	// For each namespace, discover workloads
	for _, ns := range namespaces {
		// Discover Deployments
		deployments, err := discoverDeployments(ctx, c, ns, policy)
		if err != nil {
			return nil, err
		}
		allWorkloads = append(allWorkloads, deployments...)

		// Discover StatefulSets
		statefulSets, err := discoverStatefulSets(ctx, c, ns, policy)
		if err != nil {
			return nil, err
		}
		allWorkloads = append(allWorkloads, statefulSets...)

		// Discover DaemonSets
		daemonSets, err := discoverDaemonSets(ctx, c, ns, policy)
		if err != nil {
			return nil, err
		}
		allWorkloads = append(allWorkloads, daemonSets...)
	}

	return allWorkloads, nil
}

// getMatchingNamespaces returns namespaces that match the policy selectors
func getMatchingNamespaces(ctx context.Context, c client.Client, policy *optipodv1alpha1.OptimizationPolicy) ([]string, error) {
	// List all namespaces
	namespaceList := &corev1.NamespaceList{}
	if err := c.List(ctx, namespaceList); err != nil {
		return nil, err
	}

	var matchingNamespaces []string

	for _, ns := range namespaceList.Items {
		// Check if namespace matches the selector
		if namespaceMatches(ns, policy) {
			matchingNamespaces = append(matchingNamespaces, ns.Name)
		}
	}

	return matchingNamespaces, nil
}

// namespaceMatches checks if a namespace matches the policy selectors
func namespaceMatches(ns corev1.Namespace, policy *optipodv1alpha1.OptimizationPolicy) bool {
	// Apply deny list first (takes precedence)
	if policy.Spec.Selector.Namespaces != nil {
		for _, denied := range policy.Spec.Selector.Namespaces.Deny {
			if ns.Name == denied {
				return false
			}
		}

		// Apply allow list
		if len(policy.Spec.Selector.Namespaces.Allow) > 0 {
			allowed := false
			for _, allowedNs := range policy.Spec.Selector.Namespaces.Allow {
				if ns.Name == allowedNs {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}

	// Check namespace label selector
	if policy.Spec.Selector.NamespaceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.NamespaceSelector)
		if err != nil {
			return false
		}
		if !selector.Matches(labels.Set(ns.Labels)) {
			return false
		}
	}

	return true
}

// discoverDeployments discovers Deployments in a namespace matching the workload selector
func discoverDeployments(ctx context.Context, c client.Client, namespace string, policy *optipodv1alpha1.OptimizationPolicy) ([]Workload, error) {
	deploymentList := &appsv1.DeploymentList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	// Apply workload label selector if specified
	if policy.Spec.Selector.WorkloadSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.WorkloadSelector)
		if err != nil {
			return nil, err
		}
		listOpts.LabelSelector = selector
	}

	if err := c.List(ctx, deploymentList, listOpts); err != nil {
		return nil, err
	}

	var workloads []Workload //nolint:prealloc // Size unknown
	for _, deployment := range deploymentList.Items {
		workloads = append(workloads, Workload{
			Kind:      "Deployment",
			Namespace: deployment.Namespace,
			Name:      deployment.Name,
			Labels:    deployment.Labels,
			Object:    &deployment,
		})
	}

	return workloads, nil
}

// discoverStatefulSets discovers StatefulSets in a namespace matching the workload selector
func discoverStatefulSets(ctx context.Context, c client.Client, namespace string, policy *optipodv1alpha1.OptimizationPolicy) ([]Workload, error) {
	statefulSetList := &appsv1.StatefulSetList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	// Apply workload label selector if specified
	if policy.Spec.Selector.WorkloadSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.WorkloadSelector)
		if err != nil {
			return nil, err
		}
		listOpts.LabelSelector = selector
	}

	if err := c.List(ctx, statefulSetList, listOpts); err != nil {
		return nil, err
	}

	var workloads []Workload //nolint:prealloc // Size unknown
	for _, statefulSet := range statefulSetList.Items {
		workloads = append(workloads, Workload{
			Kind:      "StatefulSet",
			Namespace: statefulSet.Namespace,
			Name:      statefulSet.Name,
			Labels:    statefulSet.Labels,
			Object:    &statefulSet,
		})
	}

	return workloads, nil
}

// discoverDaemonSets discovers DaemonSets in a namespace matching the workload selector
func discoverDaemonSets(ctx context.Context, c client.Client, namespace string, policy *optipodv1alpha1.OptimizationPolicy) ([]Workload, error) {
	daemonSetList := &appsv1.DaemonSetList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	// Apply workload label selector if specified
	if policy.Spec.Selector.WorkloadSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector.WorkloadSelector)
		if err != nil {
			return nil, err
		}
		listOpts.LabelSelector = selector
	}

	if err := c.List(ctx, daemonSetList, listOpts); err != nil {
		return nil, err
	}

	var workloads []Workload //nolint:prealloc // Size unknown
	for _, daemonSet := range daemonSetList.Items {
		workloads = append(workloads, Workload{
			Kind:      "DaemonSet",
			Namespace: daemonSet.Namespace,
			Name:      daemonSet.Name,
			Labels:    daemonSet.Labels,
			Object:    &daemonSet,
		})
	}

	return workloads, nil
}
