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

package webhook

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// createTwoContainerDeployment builds a two-container Deployment used as the
// recommendation-annotation store target.
func createTwoContainerDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Name:  "worker",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

// getStoredAnnotations reads the workload's annotations back through the API client.
func getStoredAnnotations(t *testing.T, k8sClient client.Client, namespace, name string) map[string]string {
	t.Helper()

	fetched := &appsv1.Deployment{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, fetched); err != nil {
		t.Fatalf("failed to get workload: %v", err)
	}
	return fetched.GetAnnotations()
}

// TestStoreRecommendationsKeepsEveryContainerKeys proves the recommendation
// store is container-scoped: storing one container's recommendations must never
// clear another container's previously stored keys. The production apply path
// drives StoreRecommendations once per container, so after two consecutive
// store operations both containers' keys must persist (VAL-FIX-002).
func TestStoreRecommendationsKeepsEveryContainerKeys(t *testing.T) {
	deployment := createTwoContainerDeployment("store-two-containers", "default")
	k8sClient := createFakeClientWithWorkloads(nil, []client.Object{deployment})
	annotationManager := NewAnnotationManager(k8sClient)
	ctx := context.Background()

	appCPU := resource.MustParse("250m")
	appMemory := resource.MustParse("256Mi")
	appCPULimit := resource.MustParse("375m")
	appMemoryLimit := resource.MustParse("333Mi")
	workerCPU := resource.MustParse("500m")
	workerMemory := resource.MustParse("512Mi")
	workerCPULimit := resource.MustParse("750m")
	workerMemoryLimit := resource.MustParse("666Mi")

	// First store operation: container "app" (as the per-container apply loop does).
	err := annotationManager.StoreRecommendations(ctx, deployment, []ResourceRecommendation{
		{
			ContainerName: "app",
			CPU:           &appCPU,
			Memory:        &appMemory,
			CPULimit:      &appCPULimit,
			MemoryLimit:   &appMemoryLimit,
		},
	})
	if err != nil {
		t.Fatalf("first StoreRecommendations failed: %v", err)
	}

	// Second consecutive store operation: container "worker".
	err = annotationManager.StoreRecommendations(ctx, deployment, []ResourceRecommendation{
		{
			ContainerName: "worker",
			CPU:           &workerCPU,
			Memory:        &workerMemory,
			CPULimit:      &workerCPULimit,
			MemoryLimit:   &workerMemoryLimit,
		},
	})
	if err != nil {
		t.Fatalf("second StoreRecommendations failed: %v", err)
	}

	annotations := getStoredAnnotations(t, k8sClient, "default", "store-two-containers")

	expected := map[string]string{
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app":       "250m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app":    "256Mi",
		optipodv1alpha1.AnnotationCPULimitPrefix + ".app":         "375m",
		optipodv1alpha1.AnnotationMemoryLimitPrefix + ".app":      "333Mi",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".worker":    "500m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".worker": "512Mi",
		optipodv1alpha1.AnnotationCPULimitPrefix + ".worker":      "750m",
		optipodv1alpha1.AnnotationMemoryLimitPrefix + ".worker":   "666Mi",
		optipodv1alpha1.AnnotationWebhookEnabled:                  "true",
		optipodv1alpha1.AnnotationStrategy:                        string(optipodv1alpha1.StrategyWebhook),
	}

	for key, want := range expected {
		got, ok := annotations[key]
		if !ok {
			t.Errorf("missing annotation %s after storing both containers (VAL-FIX-002: a later container must not clear an earlier container's keys)", key)
			continue
		}
		if got != want {
			t.Errorf("annotation %s = %q, want %q", key, got, want)
		}
	}
}

// TestStoreRecommendationsRewritesScopedContainer proves the container-scoped
// clear still replaces a container's own stale keys while siblings survive.
func TestStoreRecommendationsRewritesScopedContainer(t *testing.T) {
	deployment := createTwoContainerDeployment("store-rewrite", "default")
	k8sClient := createFakeClientWithWorkloads(nil, []client.Object{deployment})
	annotationManager := NewAnnotationManager(k8sClient)
	ctx := context.Background()

	appCPU := resource.MustParse("250m")
	appMemory := resource.MustParse("256Mi")
	workerCPU := resource.MustParse("500m")
	workerMemory := resource.MustParse("512Mi")

	if err := annotationManager.StoreRecommendations(ctx, deployment, []ResourceRecommendation{
		{ContainerName: "app", CPU: &appCPU, Memory: &appMemory},
		{ContainerName: "worker", CPU: &workerCPU, Memory: &workerMemory},
	}); err != nil {
		t.Fatalf("initial StoreRecommendations failed: %v", err)
	}

	// Store an updated recommendation for "app" only: "app" keys are replaced,
	// "worker" keys are untouched.
	updatedCPU := resource.MustParse("300m")
	updatedMemory := resource.MustParse("384Mi")
	if err := annotationManager.StoreRecommendations(ctx, deployment, []ResourceRecommendation{
		{ContainerName: "app", CPU: &updatedCPU, Memory: &updatedMemory},
	}); err != nil {
		t.Fatalf("updated StoreRecommendations failed: %v", err)
	}

	annotations := getStoredAnnotations(t, k8sClient, "default", "store-rewrite")

	expected := map[string]string{
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app":       "300m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app":    "384Mi",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".worker":    "500m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".worker": "512Mi",
	}
	for key, want := range expected {
		if got := annotations[key]; got != want {
			t.Errorf("annotation %s = %q, want %q", key, got, want)
		}
	}
}

// TestMutatorPatchesEveryRecommendedContainer proves the production pod
// mutator generates admission patches covering every container with stored
// recommendations, not only the last stored one (VAL-FIX-002).
func TestMutatorPatchesEveryRecommendedContainer(t *testing.T) {
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mutate-two-containers",
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{
					Allow: []string{"default"},
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy: stringPtr(string(optipodv1alpha1.StrategyWebhook)),
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "two-container-pod",
			Namespace: "default",
			Annotations: map[string]string{
				optipodv1alpha1.AnnotationWebhookEnabled:                  "true",
				optipodv1alpha1.AnnotationStrategy:                        string(optipodv1alpha1.StrategyWebhook),
				optipodv1alpha1.AnnotationCPURequestPrefix + ".app":       "250m",
				optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app":    "256Mi",
				optipodv1alpha1.AnnotationCPURequestPrefix + ".worker":    "500m",
				optipodv1alpha1.AnnotationMemoryRequestPrefix + ".worker": "512Mi",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
				{
					Name:  "worker",
					Image: "busybox:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
		},
	}

	k8sClient := createFakeClient(policy)
	mutator := NewMutator(k8sClient, createTestEventRecorder())

	response := mutator.MutatePod(&AdmissionRequest{Pod: pod, DryRun: false})

	if !response.Allowed {
		t.Fatalf("pod admission was not allowed: %s", response.Message)
	}

	// The mutator emits one JSON-patch leaf per recommended resource per container.
	expectedPatches := map[string]string{
		"/spec/containers/0/resources/requests/cpu":    "250m",
		"/spec/containers/0/resources/requests/memory": "256Mi",
		"/spec/containers/1/resources/requests/cpu":    "500m",
		"/spec/containers/1/resources/requests/memory": "512Mi",
	}

	gotPatches := make(map[string]interface{})
	for _, patch := range response.Patches {
		gotPatches[patch.Path] = patch.Value
	}

	for path, want := range expectedPatches {
		got, ok := gotPatches[path]
		if !ok {
			t.Errorf("mutator patch for %s is missing; admission must cover every recommended container (got patches: %v)", path, gotPatches)
			continue
		}
		if fmt.Sprint(got) != want {
			t.Errorf("patch %s = %v, want %q", path, got, want)
		}
	}
}

// TestClearRecommendationsStillRemovesEverything proves the explicit
// ClearRecommendations API still removes every container's keys.
func TestClearRecommendationsStillRemovesEverything(t *testing.T) {
	deployment := createTwoContainerDeployment("store-clear", "default")
	k8sClient := createFakeClientWithWorkloads(nil, []client.Object{deployment})
	annotationManager := NewAnnotationManager(k8sClient)
	ctx := context.Background()

	appCPU := resource.MustParse("250m")
	appMemory := resource.MustParse("256Mi")
	workerCPU := resource.MustParse("500m")
	workerMemory := resource.MustParse("512Mi")

	if err := annotationManager.StoreRecommendations(ctx, deployment, []ResourceRecommendation{
		{ContainerName: "app", CPU: &appCPU, Memory: &appMemory},
		{ContainerName: "worker", CPU: &workerCPU, Memory: &workerMemory},
	}); err != nil {
		t.Fatalf("StoreRecommendations failed: %v", err)
	}

	if err := annotationManager.ClearRecommendations(ctx, deployment); err != nil {
		t.Fatalf("ClearRecommendations failed: %v", err)
	}

	annotations := getStoredAnnotations(t, k8sClient, "default", "store-clear")
	for key := range annotations {
		containerName, _, err := annotationManager.parseAnnotationKey(key)
		if err == nil && containerName != "" {
			t.Errorf("ClearRecommendations left recommendation annotation %s behind", key)
		}
	}
	if _, exists := annotations[optipodv1alpha1.AnnotationWebhookEnabled]; exists {
		t.Errorf("ClearRecommendations left the webhook-enabled annotation behind")
	}
	if _, exists := annotations[optipodv1alpha1.AnnotationStrategy]; exists {
		t.Errorf("ClearRecommendations left the strategy annotation behind")
	}
}
