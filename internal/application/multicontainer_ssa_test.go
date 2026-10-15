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

package application

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

// multiContainerExpectedRequests maps each container of the three-container
// fixture to its expected CPU/memory request strings.
var multiContainerExpectedRequests = map[string][2]string{
	"app":     {"300m", "512Mi"},
	"worker":  {"150m", "256Mi"},
	"sidecar": {"10m", "16Mi"},
}

// threeContainerDeployment builds a three-container Deployment: app and worker
// receive recommendations, the sidecar is untouched by the apply set.
func threeContainerDeployment(namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "three-container-deployment",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "multi-ssa"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "multi-ssa"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Name:  "worker",
							Image: "busybox:1.36",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
						{
							Name:  "sidecar",
							Image: "envoy:1.30",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

// multiContainerApplySet builds the production apply input carrying only the
// two recommended containers' resources.
func multiContainerApplySet() []ContainerRecommendation {
	return []ContainerRecommendation{
		{
			Container: "app",
			Recommendation: &recommendation.Recommendation{
				CPU:    resource.MustParse("300m"),
				Memory: resource.MustParse("512Mi"),
			},
		},
		{
			Container: "worker",
			Recommendation: &recommendation.Recommendation{
				CPU:    resource.MustParse("150m"),
				Memory: resource.MustParse("256Mi"),
			},
		},
	}
}

// multiContainerSSAPolicy returns an explicit strategy: ssa Auto policy.
func multiContainerSSAPolicy() *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "multicontainer-ssa-policy"},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy:           stringPtr(string(optipodv1alpha1.StrategySSA)),
				UpdateRequestsOnly: true,
			},
		},
	}
}

// fetchMultiContainerDeployment reads the live deployment back through the API.
func fetchMultiContainerDeployment(t *testing.T, k8sClient client.Client, namespace, name string) *appsv1.Deployment {
	t.Helper()

	fetched := &appsv1.Deployment{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, fetched); err != nil {
		t.Fatalf("Failed to fetch deployment: %v", err)
	}
	return fetched
}

// fetchMultiContainerWorkload converts the live deployment into the production
// application.Workload input.
func fetchMultiContainerWorkload(t *testing.T, k8sClient client.Client, namespace, name string) *Workload {
	t.Helper()

	fetched := fetchMultiContainerDeployment(t, k8sClient, namespace, name)
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(fetched)
	if err != nil {
		t.Fatalf("Failed to convert deployment to unstructured: %v", err)
	}
	return &Workload{
		Kind:      "Deployment",
		Namespace: namespace,
		Name:      name,
		Object:    &unstructured.Unstructured{Object: unstructuredObj},
	}
}

// assertContainerRequests verifies each container's CPU/memory requests.
func assertContainerRequests(t *testing.T, dep *appsv1.Deployment, expected map[string][2]string) {
	t.Helper()

	requests := make(map[string][2]string)
	for _, c := range dep.Spec.Template.Spec.Containers {
		requests[c.Name] = [2]string{c.Resources.Requests.Cpu().String(), c.Resources.Requests.Memory().String()}
	}
	for container, want := range expected {
		if got := requests[container]; got != want {
			t.Errorf("container %s requests = %v, want %v", container, got, want)
		}
	}
}

// assertUnrelatedFieldsUnchanged verifies images, template labels and replicas.
func assertUnrelatedFieldsUnchanged(t *testing.T, dep *appsv1.Deployment) {
	t.Helper()

	wantImages := []string{"nginx:1.21", "busybox:1.36", "envoy:1.30"}
	for i, want := range wantImages {
		if got := dep.Spec.Template.Spec.Containers[i].Image; got != want {
			t.Errorf("container image %d = %s, want %s", i, got, want)
		}
	}
	if dep.Spec.Template.Labels["app"] != "multi-ssa" {
		t.Errorf("template labels were modified: %v", dep.Spec.Template.Labels)
	}
	if *dep.Spec.Replicas != 2 {
		t.Errorf("replicas were modified: %d", *dep.Spec.Replicas)
	}
}

// optipodApplyFields returns the raw FieldsV1 of the optipod Apply managed
// fields entry, failing when the owner entry is missing.
func optipodApplyFields(t *testing.T, dep *appsv1.Deployment) string {
	t.Helper()

	for _, mf := range dep.ManagedFields {
		if mf.Manager == FieldManagerName && mf.Operation == metav1.ManagedFieldsOperationApply {
			return mf.FieldsV1.String()
		}
	}
	t.Fatalf("Expected managedFields entry for manager %q with Apply operation", FieldManagerName)
	return ""
}

// assertApplyOwnership verifies the optipod apply owner covers exactly the
// required containers and never the forbidden ones.
func assertApplyOwnership(t *testing.T, fields string, required, forbidden []string) {
	t.Helper()

	for _, container := range required {
		if !strings.Contains(fields, `k:{\"name\":\"`+container+`\"}`) {
			t.Errorf("optipod apply ownership does not cover container %s: %s", container, fields)
		}
	}
	for _, container := range forbidden {
		if strings.Contains(fields, `k:{\"name\":\"`+container+`\"}`) {
			t.Errorf("optipod apply ownership must not cover container %s: %s", container, fields)
		}
	}
}

// assertSteadyState verifies a repeat apply caused no further mutation.
func assertSteadyState(t *testing.T, dep *appsv1.Deployment, generation int64, resourceVersion string) {
	t.Helper()

	if dep.Generation != generation {
		t.Errorf("repeat apply bumped template generation from %d to %d (rollout churn)", generation, dep.Generation)
	}
	if dep.ResourceVersion != resourceVersion {
		t.Errorf("repeat apply changed resourceVersion from %s to %s (further object mutation)", resourceVersion, dep.ResourceVersion)
	}
}

// TestMultiContainerSSAApplyIsCompleteAndConvergent proves the multi-container
// SSA convergence fix (VAL-FIX-001) against a real API server: one apply must
// carry every container's recommended resources so containers no longer prune
// each other, an untouched container and unrelated template fields stay intact,
// and a repeated identical apply reaches a steady state with no further
// template mutation under the same applied-field owner.
func TestMultiContainerSSAApplyIsCompleteAndConvergent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Set up envtest environment
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Skipf("Failed to start test environment: %v", err)
		return
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("Failed to stop test environment: %v", err)
		}
	}()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}
	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create discovery client: %v", err)
	}

	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "multicontainer-ssa-apply"},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		if err := k8sClient.Delete(ctx, ns); err != nil {
			t.Logf("Failed to delete namespace: %v", err)
		}
	}()

	deployment := threeContainerDeployment(ns.Name)
	if err := k8sClient.Create(ctx, deployment); err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}

	engine := NewEngine(k8sClient, dynamicClient, discoveryClient, false)
	policy := multiContainerSSAPolicy()
	recs := multiContainerApplySet()

	// First apply: ONE server-side apply must carry both containers' resources.
	result, err := engine.Apply(ctx, fetchMultiContainerWorkload(t, k8sClient, ns.Name, deployment.Name), recs, policy)
	if err != nil {
		t.Fatalf("First Apply failed: %v", err)
	}
	if result.Method != "ServerSideApply" || !result.FieldOwnership {
		t.Fatalf("Expected SSA method with field ownership, got %+v", result)
	}

	afterFirst := fetchMultiContainerDeployment(t, k8sClient, ns.Name, deployment.Name)

	// Both recommended containers retain their requests; the untouched
	// sidecar and unrelated template fields are unchanged.
	assertContainerRequests(t, afterFirst, multiContainerExpectedRequests)
	assertUnrelatedFieldsUnchanged(t, afterFirst)

	// The optipod field manager owns both containers' applied leaves and not
	// the untouched sidecar.
	assertApplyOwnership(t, optipodApplyFields(t, afterFirst),
		[]string{"app", "worker"}, []string{"sidecar"})

	// Second identical apply must converge: no error, no template mutation, no
	// rollout churn, and the same applied-field owner.
	if _, err := engine.Apply(ctx, fetchMultiContainerWorkload(t, k8sClient, ns.Name, deployment.Name), recs, policy); err != nil {
		t.Fatalf("Second Apply failed: %v", err)
	}

	afterSecond := fetchMultiContainerDeployment(t, k8sClient, ns.Name, deployment.Name)
	assertContainerRequests(t, afterSecond, multiContainerExpectedRequests)
	assertUnrelatedFieldsUnchanged(t, afterSecond)
	assertSteadyState(t, afterSecond, afterFirst.Generation, afterFirst.ResourceVersion)
	assertApplyOwnership(t, optipodApplyFields(t, afterSecond),
		[]string{"app", "worker"}, []string{"sidecar"})
}
