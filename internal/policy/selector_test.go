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
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
)

// Feature: workload-type-selector, Property 3: Policy Matcher Include Validation
func TestPolicyMatcherIncludeValidation(t *testing.T) {
	// **Validates: Requirements 1.5**

	scheme := runtime.NewScheme()
	_ = optipodv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	// Test cases for include validation
	testCases := []struct {
		name         string
		includeTypes []optipodv1alpha1.WorkloadType
		workloadKind string
		shouldMatch  bool
	}{
		{
			name:         "Include Deployment - Deployment workload should match",
			includeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
			workloadKind: "Deployment",
			shouldMatch:  true,
		},
		{
			name:         "Include Deployment - StatefulSet workload should not match",
			includeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
			workloadKind: "StatefulSet",
			shouldMatch:  false,
		},
		{
			name:         "Include StatefulSet - StatefulSet workload should match",
			includeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeStatefulSet},
			workloadKind: "StatefulSet",
			shouldMatch:  true,
		},
		{
			name:         "Include DaemonSet - DaemonSet workload should match",
			includeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDaemonSet},
			workloadKind: "DaemonSet",
			shouldMatch:  true,
		},
		{
			name: "Include multiple types - Deployment should match",
			includeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "Deployment",
			shouldMatch:  true,
		},
		{
			name: "Include multiple types - StatefulSet should match",
			includeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "StatefulSet",
			shouldMatch:  true,
		},
		{
			name: "Include multiple types - DaemonSet should not match",
			includeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "DaemonSet",
			shouldMatch:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			// Create fake client with the namespace
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(namespace).
				Build()

			// Create policy with include filter
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: tc.includeTypes,
						},
					},
				},
			}

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      tc.workloadKind,
				Namespace: "test-namespace",
				Name:      "test-workload",
				Labels:    map[string]string{},
			}

			// Create policy selector
			ps := NewPolicySelector(fakeClient)

			// Test the policy matching
			ctx := context.Background()
			matches := ps.policyMatchesWorkload(ctx, policy, workload)

			if matches != tc.shouldMatch {
				t.Errorf("Expected match result %v, got %v for workload kind %s with include types %v",
					tc.shouldMatch, matches, tc.workloadKind, tc.includeTypes)
			}
		})
	}
}

// Feature: workload-type-selector, Property 5: Policy Matcher Exclude Validation
func TestPolicyMatcherExcludeValidation(t *testing.T) {
	// **Validates: Requirements 2.5**

	scheme := runtime.NewScheme()
	_ = optipodv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	// Test cases for exclude validation
	testCases := []struct {
		name         string
		excludeTypes []optipodv1alpha1.WorkloadType
		workloadKind string
		shouldMatch  bool
	}{
		{
			name:         "Exclude Deployment - Deployment workload should not match",
			excludeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
			workloadKind: "Deployment",
			shouldMatch:  false,
		},
		{
			name:         "Exclude Deployment - StatefulSet workload should match",
			excludeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
			workloadKind: "StatefulSet",
			shouldMatch:  true,
		},
		{
			name:         "Exclude StatefulSet - StatefulSet workload should not match",
			excludeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeStatefulSet},
			workloadKind: "StatefulSet",
			shouldMatch:  false,
		},
		{
			name:         "Exclude DaemonSet - DaemonSet workload should not match",
			excludeTypes: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDaemonSet},
			workloadKind: "DaemonSet",
			shouldMatch:  false,
		},
		{
			name: "Exclude multiple types - Deployment should not match",
			excludeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "Deployment",
			shouldMatch:  false,
		},
		{
			name: "Exclude multiple types - StatefulSet should not match",
			excludeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "StatefulSet",
			shouldMatch:  false,
		},
		{
			name: "Exclude multiple types - DaemonSet should match",
			excludeTypes: []optipodv1alpha1.WorkloadType{
				optipodv1alpha1.WorkloadTypeDeployment,
				optipodv1alpha1.WorkloadTypeStatefulSet,
			},
			workloadKind: "DaemonSet",
			shouldMatch:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			// Create fake client with the namespace
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(namespace).
				Build()

			// Create policy with exclude filter
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Exclude: tc.excludeTypes,
						},
					},
				},
			}

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      tc.workloadKind,
				Namespace: "test-namespace",
				Name:      "test-workload",
				Labels:    map[string]string{},
			}

			// Create policy selector
			ps := NewPolicySelector(fakeClient)

			// Test the policy matching
			ctx := context.Background()
			matches := ps.policyMatchesWorkload(ctx, policy, workload)

			if matches != tc.shouldMatch {
				t.Errorf("Expected match result %v, got %v for workload kind %s with exclude types %v",
					tc.shouldMatch, matches, tc.workloadKind, tc.excludeTypes)
			}
		})
	}
}

// Feature: workload-type-selector, Property 13: Multiple Policy Independence
func TestProperty13_MultiplePolicyIndependence(t *testing.T) {
	// **Validates: Requirements 7.1**

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("multiple policies with different workloadTypes filters evaluate constraints independently", prop.ForAll(
		func(workloadKind string, policy1Include []optipodv1alpha1.WorkloadType, policy1Exclude []optipodv1alpha1.WorkloadType,
			policy2Include []optipodv1alpha1.WorkloadType, policy2Exclude []optipodv1alpha1.WorkloadType,
			policy1Weight int32, policy2Weight int32) bool {

			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create a test namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			// Create policies with different workload type filters
			policy1 := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "policy1",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Weight: &policy1Weight,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: policy1Include,
							Exclude: policy1Exclude,
						},
					},
				},
			}

			policy2 := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "policy2",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Weight: &policy2Weight,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: policy2Include,
							Exclude: policy2Exclude,
						},
					},
				},
			}

			// Create fake client with namespace and policies
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(namespace, policy1, policy2).
				Build()

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      workloadKind,
				Namespace: "test-namespace",
				Name:      "test-workload",
				Labels:    map[string]string{},
			}

			// Create policy selector
			ps := NewPolicySelector(fakeClient)
			ctx := context.Background()

			// Test that each policy's constraints are evaluated independently
			policy1Matches := ps.policyMatchesWorkload(ctx, policy1, workload)
			policy2Matches := ps.policyMatchesWorkload(ctx, policy2, workload)

			// Verify that policy1's match result is based only on its own constraints
			expectedPolicy1Match := workloadTypeMatches(policy1.Spec.Selector.WorkloadTypes, workloadKind)
			if policy1Matches != expectedPolicy1Match {
				return false
			}

			// Verify that policy2's match result is based only on its own constraints
			expectedPolicy2Match := workloadTypeMatches(policy2.Spec.Selector.WorkloadTypes, workloadKind)
			if policy2Matches != expectedPolicy2Match {
				return false
			}

			// Test SelectBestPolicy to ensure it considers only applicable policies
			bestPolicy, err := ps.SelectBestPolicy(ctx, workload)

			// If no policies match, should get an error
			if !policy1Matches && !policy2Matches {
				return err != nil
			}

			// If only one policy matches, it should be selected
			if policy1Matches && !policy2Matches {
				return err == nil && bestPolicy.Name == "policy1"
			}
			if !policy1Matches && policy2Matches {
				return err == nil && bestPolicy.Name == "policy2"
			}

			// If both policies match, highest weight should be selected
			if policy1Matches && policy2Matches {
				if policy1Weight > policy2Weight {
					return err == nil && bestPolicy.Name == "policy1"
				} else if policy2Weight > policy1Weight {
					return err == nil && bestPolicy.Name == "policy2"
				} else {
					// Equal weights - should select by name (policy1 < policy2)
					return err == nil && bestPolicy.Name == "policy1"
				}
			}

			return true
		},
		gen.OneConstOf("Deployment", "StatefulSet", "DaemonSet"),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.Int32Range(1, 100),
		gen.Int32Range(1, 100),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 14: Weight-Based Selection with Type Filtering
func TestProperty14_WeightBasedSelectionWithTypeFiltering(t *testing.T) {
	// **Validates: Requirements 7.2, 7.3**

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("among policies with compatible workload type filters, highest weight policy is selected", prop.ForAll(
		func(workloadKind string, includeTypes []optipodv1alpha1.WorkloadType, excludeTypes []optipodv1alpha1.WorkloadType,
			weight1 int32, weight2 int32, weight3 int32) bool {

			// Ensure weights are different for deterministic testing
			if weight1 == weight2 || weight2 == weight3 || weight1 == weight3 {
				return true // Skip this test case
			}

			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create a test namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			// Create three policies with the same workload type filter but different weights
			policy1 := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "policy1",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Weight: &weight1,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: includeTypes,
							Exclude: excludeTypes,
						},
					},
				},
			}

			policy2 := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "policy2",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Weight: &weight2,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: includeTypes,
							Exclude: excludeTypes,
						},
					},
				},
			}

			policy3 := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "policy3",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Weight: &weight3,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: includeTypes,
							Exclude: excludeTypes,
						},
					},
				},
			}

			// Create fake client with namespace and policies
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(namespace, policy1, policy2, policy3).
				Build()

			// Create a test workload
			workload := &discovery.Workload{
				Kind:      workloadKind,
				Namespace: "test-namespace",
				Name:      "test-workload",
				Labels:    map[string]string{},
			}

			// Create policy selector
			ps := NewPolicySelector(fakeClient)
			ctx := context.Background()

			// Check if the workload type matches the filter
			workloadTypeFilter := &optipodv1alpha1.WorkloadTypeFilter{
				Include: includeTypes,
				Exclude: excludeTypes,
			}
			shouldMatch := workloadTypeMatches(workloadTypeFilter, workloadKind)

			// Test SelectBestPolicy
			bestPolicy, err := ps.SelectBestPolicy(ctx, workload)

			// If workload type doesn't match the filter, no policy should be selected
			if !shouldMatch {
				return err != nil
			}

			// If workload type matches, the highest weight policy should be selected
			if err != nil {
				return false
			}

			// Determine which policy should have the highest weight
			maxWeight := weight1
			expectedPolicyName := "policy1"

			if weight2 > maxWeight {
				maxWeight = weight2
				expectedPolicyName = "policy2"
			}

			if weight3 > maxWeight {
				maxWeight = weight3
				expectedPolicyName = "policy3"
			}

			// Verify the correct policy was selected
			return bestPolicy.Name == expectedPolicyName && bestPolicy.GetWeight() == maxWeight
		},
		gen.OneConstOf("Deployment", "StatefulSet", "DaemonSet"),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.SliceOf(gen.OneConstOf(optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet, optipodv1alpha1.WorkloadTypeDaemonSet)),
		gen.Int32Range(1, 100),
		gen.Int32Range(101, 200),
		gen.Int32Range(201, 300),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
