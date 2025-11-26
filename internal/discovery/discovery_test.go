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
				namespaceNames = append(namespaceNames, nsName)
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
				case "Deployment":
					deploymentCount++
				case "StatefulSet":
					statefulSetCount++
				case "DaemonSet":
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
