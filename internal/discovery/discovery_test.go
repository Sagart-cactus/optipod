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
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

const (
	// Test workload type constants
	testDeploymentKind  = "Deployment"
	testStatefulSetKind = "StatefulSet"
	testDaemonSetKind   = "DaemonSet"
)

// Feature: k8s-workload-rightsizing, Property 16: Workload discovery
// For any OptimizationPolicy with label selectors, the system should automatically discover
// all matching Deployments, StatefulSets, and DaemonSets in the specified namespaces.
// Validates: Requirements 6.3, 6.4, 6.5
func TestProperty16_WorkloadDiscovery(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("discovers all matching workload types", prop.ForAll(
		func(numNamespaces int) bool {
			if numNamespaces <= 0 {
				numNamespaces = 1
			}

			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespaces
			var objects []client.Object
			var namespaceNames []string
			for i := 0; i < numNamespaces; i++ {
				nsName := "test-ns-" + string(rune('a'+i))
				namespaceNames = append(namespaceNames, nsName) //nolint:staticcheck // Result is used
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName,
					},
				}
				objects = append(objects, ns)

				// Create one of each workload type in each namespace
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deployment",
						Namespace: nsName,
						Labels: map[string]string{
							"app": "test-app",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, deployment)

				statefulSet := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-statefulset",
						Namespace: nsName,
						Labels: map[string]string{
							"app": "test-app",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, statefulSet)

				daemonSet := &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-daemonset",
						Namespace: nsName,
						Labels: map[string]string{
							"app": "test-app",
						},
					},
					Spec: appsv1.DaemonSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, daemonSet)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy that selects workloads by label
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test-app",
							},
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should discover 3 workloads per namespace (Deployment, StatefulSet, DaemonSet)
			expectedCount := numNamespaces * 3
			if len(workloads) != expectedCount {
				return false
			}

			// Verify we have one of each type per namespace
			deploymentCount := 0
			statefulSetCount := 0
			daemonSetCount := 0

			for _, w := range workloads {
				switch w.Kind {
				case testDeploymentKind:
					deploymentCount++
				case testStatefulSetKind:
					statefulSetCount++
				case testDaemonSetKind:
					daemonSetCount++
				}
			}

			return deploymentCount == numNamespaces &&
				statefulSetCount == numNamespaces &&
				daemonSetCount == numNamespaces
		},
		gen.IntRange(1, 5),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 27: Multi-tenant scoping
// For any policy in a multi-tenant cluster, the system should correctly scope workloads
// using namespace selectors, workload label selectors, and optional allow/deny lists,
// with deny-lists taking precedence.
// Validates: Requirements 12.1, 12.2, 12.3, 12.4, 12.5
func TestProperty27_MultiTenantScoping(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("deny list takes precedence over allow list", prop.ForAll(
		func(seed int) bool {
			// Generate two different namespace names
			allowedNs := "allowed-ns-" + string(rune('a'+(seed%26)))
			deniedNs := "denied-ns-" + string(rune('a'+((seed+1)%26)))

			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create two namespaces
			allowedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: allowedNs,
				},
			}
			deniedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: deniedNs,
				},
			}

			// Create a deployment in each namespace
			allowedDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: allowedNs,
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}

			deniedDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: deniedNs,
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allowedNamespace, deniedNamespace, allowedDeployment, deniedDeployment).
				Build()

			// Create policy with both allow and deny lists
			// The denied namespace is in both lists, so it should be excluded
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Namespaces: &optipodv1alpha1.NamespaceFilter{
							Allow: []string{allowedNs, deniedNs},
							Deny:  []string{deniedNs},
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should only discover workload from allowed namespace
			if len(workloads) != 1 {
				return false
			}

			// Verify it's from the allowed namespace
			return workloads[0].Namespace == allowedNs
		},
		gen.Int(),
	))

	properties.Property("allow list filters namespaces", prop.ForAll(
		func(seed int) bool {
			// Generate two different namespace names
			allowedNs := "allowed-ns-" + string(rune('a'+(seed%26)))
			otherNs := "other-ns-" + string(rune('a'+((seed+1)%26)))

			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create two namespaces
			allowedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: allowedNs,
				},
			}
			otherNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherNs,
				},
			}

			// Create a deployment in each namespace
			allowedDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: allowedNs,
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}

			otherDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: otherNs,
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allowedNamespace, otherNamespace, allowedDeployment, otherDeployment).
				Build()

			// Create policy with only allow list
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Namespaces: &optipodv1alpha1.NamespaceFilter{
							Allow: []string{allowedNs},
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should only discover workload from allowed namespace
			if len(workloads) != 1 {
				return false
			}

			// Verify it's from the allowed namespace
			return workloads[0].Namespace == allowedNs
		},
		gen.Int(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 1: Include Filter Behavior
// For any OptimizationPolicy with workloadTypes.include specified, the Discovery_Engine should only discover workloads whose types are in the include list
// Validates: Requirements 1.1, 1.2, 1.3
func TestProperty1_IncludeFilterBehavior(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("include filter only discovers specified workload types", prop.ForAll(
		func(includeDeployment, includeStatefulSet, includeDaemonSet bool) bool {
			// Skip test case where no types are included (would result in empty include list)
			if !includeDeployment && !includeStatefulSet && !includeDaemonSet {
				return true
			}

			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create one of each workload type
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, statefulSet)

			daemonSet := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-daemonset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, daemonSet)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Build include list based on parameters
			var includeTypes []optipodv1alpha1.WorkloadType
			if includeDeployment {
				includeTypes = append(includeTypes, optipodv1alpha1.WorkloadTypeDeployment)
			}
			if includeStatefulSet {
				includeTypes = append(includeTypes, optipodv1alpha1.WorkloadTypeStatefulSet)
			}
			if includeDaemonSet {
				includeTypes = append(includeTypes, optipodv1alpha1.WorkloadTypeDaemonSet)
			}

			// Create policy with include filter
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: includeTypes,
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Count discovered workload types
			deploymentCount := 0
			statefulSetCount := 0
			daemonSetCount := 0

			for _, w := range workloads {
				switch w.Kind {
				case testDeploymentKind:
					deploymentCount++
				case testStatefulSetKind:
					statefulSetCount++
				case testDaemonSetKind:
					daemonSetCount++
				}
			}

			// Verify only included types are discovered
			expectedDeploymentCount := 0
			if includeDeployment {
				expectedDeploymentCount = 1
			}
			expectedStatefulSetCount := 0
			if includeStatefulSet {
				expectedStatefulSetCount = 1
			}
			expectedDaemonSetCount := 0
			if includeDaemonSet {
				expectedDaemonSetCount = 1
			}

			return deploymentCount == expectedDeploymentCount &&
				statefulSetCount == expectedStatefulSetCount &&
				daemonSetCount == expectedDaemonSetCount
		},
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 4: Exclude Filter Behavior
// For any OptimizationPolicy with workloadTypes.exclude specified, the Discovery_Engine should not discover workloads whose types are in the exclude list
// Validates: Requirements 2.1, 2.2, 2.3
func TestProperty4_ExcludeFilterBehavior(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("exclude filter prevents discovery of specified workload types", prop.ForAll(
		func(excludeDeployment, excludeStatefulSet, excludeDaemonSet bool) bool {
			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create one of each workload type
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, statefulSet)

			daemonSet := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-daemonset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, daemonSet)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Build exclude list based on parameters
			var excludeTypes []optipodv1alpha1.WorkloadType
			if excludeDeployment {
				excludeTypes = append(excludeTypes, optipodv1alpha1.WorkloadTypeDeployment)
			}
			if excludeStatefulSet {
				excludeTypes = append(excludeTypes, optipodv1alpha1.WorkloadTypeStatefulSet)
			}
			if excludeDaemonSet {
				excludeTypes = append(excludeTypes, optipodv1alpha1.WorkloadTypeDaemonSet)
			}

			// Create policy with exclude filter
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Exclude: excludeTypes,
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Count discovered workload types
			deploymentCount := 0
			statefulSetCount := 0
			daemonSetCount := 0

			for _, w := range workloads {
				switch w.Kind {
				case testDeploymentKind:
					deploymentCount++
				case testStatefulSetKind:
					statefulSetCount++
				case testDaemonSetKind:
					daemonSetCount++
				}
			}

			// Verify excluded types are not discovered
			expectedDeploymentCount := 1
			if excludeDeployment {
				expectedDeploymentCount = 0
			}
			expectedStatefulSetCount := 1
			if excludeStatefulSet {
				expectedStatefulSetCount = 0
			}
			expectedDaemonSetCount := 1
			if excludeDaemonSet {
				expectedDaemonSetCount = 0
			}

			return deploymentCount == expectedDeploymentCount &&
				statefulSetCount == expectedStatefulSetCount &&
				daemonSetCount == expectedDaemonSetCount
		},
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 2: Backward Compatibility for Missing Filters
// For any OptimizationPolicy without workloadTypes specified, the Discovery_Engine should discover all supported workload types (Deployment, StatefulSet, DaemonSet)
// Validates: Requirements 1.4, 5.1
func TestProperty2_BackwardCompatibilityForMissingFilters(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("missing workloadTypes filter discovers all workload types", prop.ForAll(
		func(numNamespaces int) bool {
			if numNamespaces <= 0 {
				numNamespaces = 1
			}
			if numNamespaces > 3 {
				numNamespaces = 3 // Limit to avoid excessive test time
			}

			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			var objects []client.Object
			var namespaceNames []string

			// Create namespaces and workloads
			for i := 0; i < numNamespaces; i++ {
				nsName := "test-ns-" + string(rune('a'+i))
				namespaceNames = append(namespaceNames, nsName) //nolint:staticcheck // Result is used

				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: nsName,
					},
				}
				objects = append(objects, namespace)

				// Create one of each workload type in each namespace
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deployment",
						Namespace: nsName,
						Labels:    map[string]string{"app": "test"},
					},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, deployment)

				statefulSet := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-statefulset",
						Namespace: nsName,
						Labels:    map[string]string{"app": "test"},
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, statefulSet)

				daemonSet := &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-daemonset",
						Namespace: nsName,
						Labels:    map[string]string{"app": "test"},
					},
					Spec: appsv1.DaemonSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "test", Image: "test:latest"},
								},
							},
						},
					},
				}
				objects = append(objects, daemonSet)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy WITHOUT workloadTypes filter (backward compatibility)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						// WorkloadTypes is nil - this is the key test condition
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should discover all 3 workload types per namespace
			expectedCount := numNamespaces * 3
			if len(workloads) != expectedCount {
				return false
			}

			// Count discovered workload types
			deploymentCount := 0
			statefulSetCount := 0
			daemonSetCount := 0

			for _, w := range workloads {
				switch w.Kind {
				case testDeploymentKind:
					deploymentCount++
				case testStatefulSetKind:
					statefulSetCount++
				case testDaemonSetKind:
					daemonSetCount++
				}
			}

			// Verify all types are discovered (backward compatibility)
			return deploymentCount == numNamespaces &&
				statefulSetCount == numNamespaces &&
				daemonSetCount == numNamespaces
		},
		gen.IntRange(1, 3),
	))

	properties.Property("empty workloadTypes filter discovers all workload types", prop.ForAll(
		func(seed int) bool {
			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create one of each workload type
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, statefulSet)

			daemonSet := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-daemonset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, daemonSet)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy with empty workloadTypes filter (backward compatibility)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							// Both Include and Exclude are empty - should behave like no filter
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should discover all 3 workload types
			if len(workloads) != 3 {
				return false
			}

			// Count discovered workload types
			deploymentCount := 0
			statefulSetCount := 0
			daemonSetCount := 0

			for _, w := range workloads {
				switch w.Kind {
				case testDeploymentKind:
					deploymentCount++
				case testStatefulSetKind:
					statefulSetCount++
				case testDaemonSetKind:
					daemonSetCount++
				}
			}

			// Verify all types are discovered (backward compatibility)
			return deploymentCount == 1 &&
				statefulSetCount == 1 &&
				daemonSetCount == 1
		},
		gen.Int(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: workload-type-selector, Property 7: Empty Result Set Handling
// For any OptimizationPolicy where workloadTypes filtering results in no valid workload types, the Discovery_Engine should return an empty result set
// Validates: Requirements 3.3
func TestProperty7_EmptyResultSetHandling(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("filtering that results in no valid workload types returns empty result set", prop.ForAll(
		func(seed int) bool {
			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create one of each workload type
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, statefulSet)

			daemonSet := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-daemonset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, daemonSet)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy that excludes all workload types (results in empty set)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Exclude: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeDeployment,
								optipodv1alpha1.WorkloadTypeStatefulSet,
								optipodv1alpha1.WorkloadTypeDaemonSet,
							},
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should return empty result set
			return len(workloads) == 0
		},
		gen.Int(),
	))

	properties.Property("include filter with empty list defaults to all workload types", prop.ForAll(
		func(seed int) bool {
			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create one of each workload type
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, statefulSet)

			daemonSet := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-daemonset",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, daemonSet)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy with empty include list (should default to all types)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: []optipodv1alpha1.WorkloadType{}, // Empty include list
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should return all workload types when include list is empty (defaults to all)
			return len(workloads) == 3
		},
		gen.Int(),
	))

	properties.Property("conflicting include/exclude filters return empty result set", prop.ForAll(
		func(seed int) bool {
			// Create a fake client with scheme
			scheme := runtime.NewScheme()
			_ = optipodv1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			// Create namespace
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			var objects []client.Object
			objects = append(objects, namespace)

			// Create a deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"app": "test"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test", Image: "test:latest"},
							},
						},
					},
				},
			}
			objects = append(objects, deployment)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create policy where include and exclude contain the same type (exclude takes precedence)
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						WorkloadTypes: &optipodv1alpha1.WorkloadTypeFilter{
							Include: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeDeployment,
							},
							Exclude: []optipodv1alpha1.WorkloadType{
								optipodv1alpha1.WorkloadTypeDeployment, // Same type in both lists
							},
						},
					},
				},
			}

			// Discover workloads
			workloads, err := DiscoverWorkloads(context.Background(), fakeClient, policy)
			if err != nil {
				return false
			}

			// Should return empty result set (exclude takes precedence)
			return len(workloads) == 0
		},
		gen.Int(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
