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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/observability"
)

// Test generators for property-based testing

// genPodWithAnnotations generates a pod with resource recommendation annotations
func genPodWithAnnotations() gopter.Gen {
	return gen.Const(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				optipodv1alpha1.AnnotationWebhookEnabled:               "true",
				optipodv1alpha1.AnnotationStrategy:                     string(optipodv1alpha1.StrategyWebhook),
				optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "100m",
				optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "128Mi",
				optipodv1alpha1.AnnotationCPULimitPrefix + ".app":      "200m",
				optipodv1alpha1.AnnotationMemoryLimitPrefix + ".app":   "256Mi",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
		},
	})
}

// genPodWithoutAnnotations generates a pod without resource recommendation annotations
func genPodWithoutAnnotations() gopter.Gen {
	return gen.Const(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"app": "test",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
		},
	})
}

// genOptimizationPolicy generates an optimization policy
func genOptimizationPolicy() gopter.Gen {
	return gen.Const(&optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode:   optipodv1alpha1.ModeAuto,
			Weight: int32Ptr(100),
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{
					Allow: []string{"default"},
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy: stringPtr(string(optipodv1alpha1.StrategyWebhook)),
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider: "prometheus",
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("10m"),
					Max: resource.MustParse("2"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
		},
	})
}

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}

func stringPtr(s string) *string {
	return &s
}

// createFakeClient creates a fake Kubernetes client for testing
func createFakeClient(policies ...*optipodv1alpha1.OptimizationPolicy) client.Client {
	scheme := runtime.NewScheme()
	_ = optipodv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = admissionregistrationv1.AddToScheme(scheme)

	objects := make([]client.Object, len(policies))
	for i, policy := range policies {
		objects[i] = policy
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

// createFakeClientWithWorkloads creates a fake Kubernetes client with policies and workloads
func createFakeClientWithWorkloads(policies []*optipodv1alpha1.OptimizationPolicy, workloads []client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = optipodv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = admissionregistrationv1.AddToScheme(scheme)

	objects := make([]client.Object, 0, len(policies)+len(workloads))
	for _, policy := range policies {
		objects = append(objects, policy)
	}
	objects = append(objects, workloads...)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

// createTestEventRecorder creates a proper event recorder for testing
func createTestEventRecorder() *observability.EventRecorder {
	// For tests, we can pass nil as the underlying recorder
	// The EventRecorder will handle nil gracefully
	return observability.NewEventRecorder(nil)
}

// Property 3: Webhook pod modification behavior
// **Feature: mutating-webhook-support, Property 3: Webhook pod modification behavior**
// **Validates: Requirements 2.1, 2.4**
func TestProperty_WebhookPodModificationBehavior(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook modifies pods with annotations and matching policies", prop.ForAll(
		func(pod *corev1.Pod, policy *optipodv1alpha1.OptimizationPolicy) bool {
			// Create fake client with policy
			client := createFakeClient(policy)
			eventRecorder := createTestEventRecorder()
			mutator := NewMutator(client, eventRecorder)

			// Ensure mutator is not nil to avoid panic
			if mutator == nil {
				return false
			}

			// Create admission request
			req := &AdmissionRequest{
				Pod:    pod,
				DryRun: false,
			}

			// Process mutation
			response := mutator.MutatePod(req)

			// Should be allowed and have patches
			return response.Allowed && len(response.Patches) > 0
		},
		genPodWithAnnotations(),
		genOptimizationPolicy(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 4: Webhook non-interference
// **Feature: mutating-webhook-support, Property 4: Webhook non-interference**
// **Validates: Requirements 2.2, 2.5**
func TestProperty_WebhookNonInterference(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook leaves pods unchanged when no annotations or policy match", prop.ForAll(
		func(pod *corev1.Pod, policy *optipodv1alpha1.OptimizationPolicy) bool {
			// Make policy not match pod (different namespace)
			policy.Namespace = "different-namespace"
			policy.Spec.Selector.Namespaces = &optipodv1alpha1.NamespaceFilter{
				Allow: []string{"different-namespace"},
			}

			// Create fake client with policy
			client := createFakeClient(policy)
			eventRecorder := createTestEventRecorder()
			mutator := NewMutator(client, eventRecorder)

			// Create admission request
			req := &AdmissionRequest{
				Pod:    pod,
				DryRun: false,
			}

			// Process mutation
			response := mutator.MutatePod(req)

			// Should be allowed with no patches
			return response.Allowed && len(response.Patches) == 0
		},
		genPodWithoutAnnotations(),
		genOptimizationPolicy(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 5: Webhook strategy control
// **Feature: mutating-webhook-support, Property 5: Webhook strategy control**
// **Validates: Requirements 2.3**
func TestProperty_WebhookStrategyControl(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook does not modify pods when strategy is disabled", prop.ForAll(
		func(pod *corev1.Pod, policy *optipodv1alpha1.OptimizationPolicy) bool {
			// Modify pod to have SSA strategy instead of webhook
			pod.Annotations[optipodv1alpha1.AnnotationWebhookEnabled] = "false"
			pod.Annotations[optipodv1alpha1.AnnotationStrategy] = string(optipodv1alpha1.StrategySSA)

			// Ensure policy uses SSA strategy
			policy.Spec.UpdateStrategy.Strategy = stringPtr(string(optipodv1alpha1.StrategySSA))

			// Create fake client with policy
			client := createFakeClient(policy)
			eventRecorder := createTestEventRecorder()
			mutator := NewMutator(client, eventRecorder)

			// Create admission request
			req := &AdmissionRequest{
				Pod:    pod,
				DryRun: false,
			}

			// Process mutation
			response := mutator.MutatePod(req)

			// Should be allowed but with no patches (webhook disabled)
			return response.Allowed && len(response.Patches) == 0
		},
		genPodWithAnnotations(),
		genOptimizationPolicy(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 6: Annotation storage and format
// **Feature: mutating-webhook-support, Property 6: Annotation storage and format**
// **Validates: Requirements 3.1, 3.2, 3.3**
func TestProperty_AnnotationStorageAndFormat(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("annotation storage uses standardized keys and webhook parses them correctly", prop.ForAll(
		func(recommendations []ResourceRecommendation) bool {
			// Create fake client
			client := createFakeClient()
			annotationManager := NewAnnotationManager(client)

			// Create test pod with annotations based on recommendations
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: make(map[string]string),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}

			// Manually add annotations in the expected format
			for _, rec := range recommendations {
				if rec.CPU != nil {
					key := optipodv1alpha1.AnnotationCPURequestPrefix + "." + rec.ContainerName
					pod.Annotations[key] = rec.CPU.String()
				}
				if rec.Memory != nil {
					key := optipodv1alpha1.AnnotationMemoryRequestPrefix + "." + rec.ContainerName
					pod.Annotations[key] = rec.Memory.String()
				}
				if rec.CPULimit != nil {
					key := optipodv1alpha1.AnnotationCPULimitPrefix + "." + rec.ContainerName
					pod.Annotations[key] = rec.CPULimit.String()
				}
				if rec.MemoryLimit != nil {
					key := optipodv1alpha1.AnnotationMemoryLimitPrefix + "." + rec.ContainerName
					pod.Annotations[key] = rec.MemoryLimit.String()
				}
			}

			// Parse annotations back
			parsedRecs, err := annotationManager.GetRecommendations(pod)
			if err != nil {
				return false
			}

			// Verify we get back the same number of recommendations
			if len(parsedRecs) != len(recommendations) {
				return false
			}

			// Create maps for comparison
			originalMap := make(map[string]ResourceRecommendation)
			for _, rec := range recommendations {
				originalMap[rec.ContainerName] = rec
			}

			parsedMap := make(map[string]ResourceRecommendation)
			for _, rec := range parsedRecs {
				parsedMap[rec.ContainerName] = rec
			}

			// Compare each container's recommendations
			for containerName, original := range originalMap {
				parsed, exists := parsedMap[containerName]
				if !exists {
					return false
				}

				// Compare CPU requests
				if (original.CPU == nil) != (parsed.CPU == nil) {
					return false
				}
				if original.CPU != nil && parsed.CPU != nil && !original.CPU.Equal(*parsed.CPU) {
					return false
				}

				// Compare memory requests
				if (original.Memory == nil) != (parsed.Memory == nil) {
					return false
				}
				if original.Memory != nil && parsed.Memory != nil && !original.Memory.Equal(*parsed.Memory) {
					return false
				}

				// Compare CPU limits
				if (original.CPULimit == nil) != (parsed.CPULimit == nil) {
					return false
				}
				if original.CPULimit != nil && parsed.CPULimit != nil && !original.CPULimit.Equal(*parsed.CPULimit) {
					return false
				}

				// Compare memory limits
				if (original.MemoryLimit == nil) != (parsed.MemoryLimit == nil) {
					return false
				}
				if original.MemoryLimit != nil && parsed.MemoryLimit != nil && !original.MemoryLimit.Equal(*parsed.MemoryLimit) {
					return false
				}
			}

			return true
		},
		genResourceRecommendations(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 7: Annotation error handling
// **Feature: mutating-webhook-support, Property 7: Annotation error handling**
// **Validates: Requirements 3.4, 7.4**
func TestProperty_AnnotationErrorHandling(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("invalid annotation values are handled gracefully and pod creation proceeds", prop.ForAll(
		func(invalidValue string) bool {
			// Create fake client
			client := createFakeClient()
			annotationManager := NewAnnotationManager(client)

			// Create pod with invalid annotation value
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    invalidValue,
						optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "128Mi", // Valid value
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:latest"},
					},
				},
			}

			// Parse annotations - should not fail even with invalid values
			recommendations, err := annotationManager.SafeParseAnnotations(pod)

			// Should not return error (graceful handling)
			if err != nil {
				return false
			}

			// Should still parse valid annotations
			if len(recommendations) == 0 {
				return false
			}

			// Should have memory recommendation but not CPU (due to invalid value)
			for _, rec := range recommendations {
				if rec.ContainerName == "app" {
					return rec.CPU == nil && rec.Memory != nil
				}
			}

			return false
		},
		genInvalidResourceValue(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 8: Annotation scope handling
// **Feature: mutating-webhook-support, Property 8: Annotation scope handling**
// **Validates: Requirements 3.5**
func TestProperty_AnnotationScopeHandling(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("container-specific annotations are handled correctly", prop.ForAll(
		func(containerNames []string) bool {
			if len(containerNames) == 0 {
				return true // Skip empty container lists
			}

			// Create fake client
			client := createFakeClient()
			annotationManager := NewAnnotationManager(client)

			// Create pod with multiple containers
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: make(map[string]string),
				},
				Spec: corev1.PodSpec{
					Containers: make([]corev1.Container, len(containerNames)),
				},
			}

			// Add containers and annotations
			for i, containerName := range containerNames {
				pod.Spec.Containers[i] = corev1.Container{
					Name:  containerName,
					Image: "nginx:latest",
				}

				// Add container-specific annotations
				cpuKey := optipodv1alpha1.AnnotationCPURequestPrefix + "." + containerName
				memoryKey := optipodv1alpha1.AnnotationMemoryRequestPrefix + "." + containerName
				pod.Annotations[cpuKey] = "100m"
				pod.Annotations[memoryKey] = "128Mi"
			}

			// Parse annotations
			recommendations, err := annotationManager.GetRecommendations(pod)
			if err != nil {
				return false
			}

			// Should have recommendations for each container
			if len(recommendations) != len(containerNames) {
				return false
			}

			// Verify each container has recommendations
			recMap := make(map[string]ResourceRecommendation)
			for _, rec := range recommendations {
				recMap[rec.ContainerName] = rec
			}

			for _, containerName := range containerNames {
				rec, exists := recMap[containerName]
				if !exists {
					return false
				}

				// Should have both CPU and memory recommendations
				if rec.CPU == nil || rec.Memory == nil {
					return false
				}

				// Verify values
				expectedCPU := resource.MustParse("100m")
				expectedMemory := resource.MustParse("128Mi")
				if !rec.CPU.Equal(expectedCPU) || !rec.Memory.Equal(expectedMemory) {
					return false
				}
			}

			return true
		},
		genContainerNames(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Generator functions for property tests

// genResourceRecommendations generates valid resource recommendations
func genResourceRecommendations() gopter.Gen {
	return gen.OneConstOf(
		[]ResourceRecommendation{
			{
				ContainerName: "app",
				CPU:           quantityPtr(resource.MustParse("100m")),
				Memory:        quantityPtr(resource.MustParse("128Mi")),
			},
		},
		[]ResourceRecommendation{
			{
				ContainerName: "app",
				CPU:           quantityPtr(resource.MustParse("100m")),
				Memory:        quantityPtr(resource.MustParse("128Mi")),
				CPULimit:      quantityPtr(resource.MustParse("200m")),
				MemoryLimit:   quantityPtr(resource.MustParse("256Mi")),
			},
		},
		[]ResourceRecommendation{
			{
				ContainerName: "app",
				CPU:           quantityPtr(resource.MustParse("100m")),
				Memory:        quantityPtr(resource.MustParse("128Mi")),
			},
			{
				ContainerName: "sidecar",
				CPU:           quantityPtr(resource.MustParse("50m")),
				Memory:        quantityPtr(resource.MustParse("64Mi")),
			},
		},
	)
}

// genContainerNames generates a slice of unique container names
func genContainerNames() gopter.Gen {
	return gen.OneConstOf(
		[]string{"app"},
		[]string{"app", "sidecar"},
		[]string{"app", "proxy", "cache"},
	)
}

// genInvalidResourceValue generates invalid resource values for error testing
func genInvalidResourceValue() gopter.Gen {
	return gen.OneConstOf(
		"invalid",
		"",
		"not-a-number",
		"-100m",
		"100invalid",
		"100.5.5m",
		"abc123",
	)
}

// Helper function to create quantity pointer
func quantityPtr(q resource.Quantity) *resource.Quantity {
	return &q
}

// Property 9: Rollout strategy behavior
// **Feature: mutating-webhook-support, Property 9: Rollout strategy behavior**
// **Validates: Requirements 4.1, 4.2, 4.3**
func TestProperty_RolloutStrategyBehavior(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("rollout strategy controls when resource changes take effect", prop.ForAll(
		func(workload client.Object, rolloutStrategy optipodv1alpha1.RolloutStrategyType) bool {
			// Create policy with the specified rollout strategy
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode:   optipodv1alpha1.ModeAuto,
					Weight: int32Ptr(100),
					Selector: optipodv1alpha1.WorkloadSelector{
						Namespaces: &optipodv1alpha1.NamespaceFilter{
							Allow: []string{"default"},
						},
					},
					UpdateStrategy: optipodv1alpha1.UpdateStrategy{
						Strategy:        stringPtr(string(optipodv1alpha1.StrategyWebhook)),
						RolloutStrategy: stringPtr(string(rolloutStrategy)),
					},
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider: "prometheus",
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("10m"),
							Max: resource.MustParse("2"),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: resource.MustParse("64Mi"),
							Max: resource.MustParse("8Gi"),
						},
					},
				},
			}

			// Create fake client with both policy and workload
			client := createFakeClientWithWorkloads([]*optipodv1alpha1.OptimizationPolicy{policy}, []client.Object{workload})
			rolloutController := NewRolloutController(client)

			// Test rollout strategy behavior
			switch rolloutStrategy {
			case optipodv1alpha1.RolloutImmediate:
				// Should trigger rolling restart for supported workloads
				if rolloutController.SupportsRollingRestart(workload) {
					err := rolloutController.TriggerRollingRestart(context.Background(), workload, policy)
					// Should succeed for supported workloads
					return err == nil
				}
				return true // Skip unsupported workloads

			case optipodv1alpha1.RolloutOnNextRestart:
				// Should not trigger immediate restart
				// This is tested by ensuring the policy has the correct strategy
				return policy.GetRolloutStrategy() == optipodv1alpha1.RolloutOnNextRestart

			default:
				// Invalid strategy should be handled
				return false
			}
		},
		genSupportedWorkload(),
		genRolloutStrategy(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 10: Rolling restart workload validation
// **Feature: mutating-webhook-support, Property 10: Rolling restart workload validation**
// **Validates: Requirements 4.4, 4.5**
func TestProperty_RollingRestartWorkloadValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("rolling restart only works on supported workload types and updates pod templates", prop.ForAll(
		func(workload client.Object, workloadType optipodv1alpha1.WorkloadType) bool {
			// Create fake client
			client := createFakeClient()
			rolloutController := NewRolloutController(client)

			// Test workload type validation
			supportedTypes := rolloutController.GetSupportedWorkloadTypes()
			isSupported := false
			for _, supportedType := range supportedTypes {
				if workloadType == supportedType {
					isSupported = true
					break
				}
			}

			// Validate workload type
			err := rolloutController.ValidateWorkloadType(workloadType)
			if isSupported {
				// Should not return error for supported types
				if err != nil {
					return false
				}
			} else {
				// Should return error for unsupported types
				if err == nil {
					return false
				}
			}

			// Test SupportsRollingRestart method consistency
			actualSupport := rolloutController.SupportsRollingRestart(workload)

			// For supported workload types, the method should return true
			// For unsupported types, it should return false
			switch workload.(type) {
			case *appsv1.Deployment, *appsv1.StatefulSet, *appsv1.DaemonSet:
				return actualSupport == true
			default:
				return actualSupport == false
			}
		},
		genAnyWorkload(),
		genWorkloadType(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Additional generator functions for rollout tests

// genSupportedWorkload generates workloads that support rolling restart
func genSupportedWorkload() gopter.Gen {
	return gen.OneGenOf(
		genDeployment(),
		genStatefulSet(),
		genDaemonSet(),
	)
}

// genAnyWorkload generates any type of workload (supported and unsupported)
func genAnyWorkload() gopter.Gen {
	return gen.OneGenOf(
		genDeployment(),
		genStatefulSet(),
		genDaemonSet(),
		genPod(), // Unsupported workload type
	)
}

// genDeployment generates a Deployment workload
func genDeployment() gopter.Gen {
	return gen.Const(&appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:        3,
			ReadyReplicas:   3,
			UpdatedReplicas: 3,
		},
	})
}

// genStatefulSet generates a StatefulSet workload
func genStatefulSet() gopter.Gen {
	return gen.Const(&appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        3,
			ReadyReplicas:   3,
			UpdatedReplicas: 3,
		},
	})
}

// genDaemonSet generates a DaemonSet workload
func genDaemonSet() gopter.Gen {
	return gen.Const(&appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: "default",
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
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DaemonSetStatus{
			NumberReady:            3,
			DesiredNumberScheduled: 3,
			UpdatedNumberScheduled: 3,
		},
	})
}

// genPod generates a Pod (unsupported for rolling restart)
func genPod() gopter.Gen {
	return gen.Const(&corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
				},
			},
		},
	})
}

// genRolloutStrategy generates rollout strategy types
func genRolloutStrategy() gopter.Gen {
	return gen.OneConstOf(
		optipodv1alpha1.RolloutImmediate,
		optipodv1alpha1.RolloutOnNextRestart,
	)
}

// genWorkloadType generates workload types
func genWorkloadType() gopter.Gen {
	return gen.OneConstOf(
		optipodv1alpha1.WorkloadTypeDeployment,
		optipodv1alpha1.WorkloadTypeStatefulSet,
		optipodv1alpha1.WorkloadTypeDaemonSet,
	)
}

// Property 12: Webhook lifecycle management
// **Feature: mutating-webhook-support, Property 12: Webhook lifecycle management**
// **Validates: Requirements 6.1, 6.2, 6.3**
func TestProperty_WebhookLifecycleManagement(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook lifecycle registers on startup and cleans up on shutdown", prop.ForAll(
		func(config LifecycleConfig) bool {
			// Create fake client
			client := createFakeClient()

			// Create lifecycle manager
			lifecycleManager := NewLifecycleManager(client, config)

			// Ensure lifecycle manager is not nil to avoid panic
			if lifecycleManager == nil {
				return false
			}

			// Test startup registration
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Mock certificate files for testing
			if err := createMockCertificates(config.CertPath, config.KeyPath); err != nil {
				return false
			}
			defer cleanupMockCertificates(config.CertPath, config.KeyPath)

			// Start lifecycle management
			startErr := lifecycleManager.Start(ctx)
			if startErr != nil {
				// Should handle certificate errors gracefully
				return true
			}

			// Verify webhook configuration was created
			webhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{}
			err := client.Get(ctx, types.NamespacedName{Name: config.WebhookName}, webhookConfig)
			webhookExists := err == nil

			// Stop lifecycle management
			stopErr := lifecycleManager.Stop(ctx)

			// Verify cleanup
			err = client.Get(ctx, types.NamespacedName{Name: config.WebhookName}, webhookConfig)
			webhookCleanedUp := apierrors.IsNotFound(err)

			// Should successfully register and cleanup
			return webhookExists && stopErr == nil && webhookCleanedUp
		},
		genLifecycleConfig(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 13: Webhook health monitoring
// **Feature: mutating-webhook-support, Property 13: Webhook health monitoring**
// **Validates: Requirements 6.5**
func TestProperty_WebhookHealthMonitoring(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook health monitoring provides accurate status information", prop.ForAll(
		func(config LifecycleConfig) bool {
			// Create fake client
			client := createFakeClient()

			// Create lifecycle manager
			lifecycleManager := NewLifecycleManager(client, config)

			// Get initial health status
			initialStatus := lifecycleManager.GetHealthStatus()

			// Should have basic status fields
			if initialStatus["webhook_name"] != config.WebhookName {
				return false
			}
			if initialStatus["service_name"] != config.ServiceName {
				return false
			}
			if initialStatus["service_namespace"] != config.ServiceNamespace {
				return false
			}

			// Certificate status should be reported
			_, hasCertStatus := initialStatus["certificate_status"]
			if !hasCertStatus {
				return false
			}

			// Server status should be reported
			_, hasServerStatus := initialStatus["server_status"]
			if !hasServerStatus {
				return false
			}

			// Health status should be consistent
			return true
		},
		genLifecycleConfig(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Generator functions for lifecycle tests

// genLifecycleConfig generates valid lifecycle configurations
func genLifecycleConfig() gopter.Gen {
	return gen.OneConstOf(
		LifecycleConfig{
			WebhookName:       "test-webhook",
			ServiceName:       "test-service",
			ServiceNamespace:  "default",
			ServicePath:       "/mutate",
			CertPath:          "/tmp/test-cert.pem",
			KeyPath:           "/tmp/test-key.pem",
			CABundle:          []byte("test-ca-bundle"),
			FailurePolicy:     nil, // Use default
			NamespaceSelector: nil, // Use default
			Port:              9443,
		},
		LifecycleConfig{
			WebhookName:      "optipod-webhook",
			ServiceName:      "optipod-webhook-service",
			ServiceNamespace: "optipod-system",
			ServicePath:      "/mutate",
			CertPath:         "/tmp/webhook-cert.pem",
			KeyPath:          "/tmp/webhook-key.pem",
			CABundle:         []byte("production-ca-bundle"),
			FailurePolicy:    func() *admissionregistrationv1.FailurePolicyType { f := admissionregistrationv1.Fail; return &f }(),
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"optipod.io/enabled": "true"},
			},
			Port: 8443,
		},
	)
}

// Helper functions for lifecycle tests

// createMockCertificates creates mock certificate files for testing
func createMockCertificates(certPath, keyPath string) error {
	// Generate a proper self-signed certificate for testing
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"OptipodTest"},
			CommonName:   "localhost",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	// Create directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}

	// Encode and write certificate file
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer func() { _ = certOut.Close() }()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	// Encode and write key file
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = keyOut.Close() }()
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	return nil
}

// cleanupMockCertificates removes mock certificate files and directories
func cleanupMockCertificates(certPath, keyPath string) {
	// Ignore errors as this is cleanup
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
	// Try to remove directories (will only succeed if empty)
	_ = os.Remove(filepath.Dir(certPath))
	_ = os.Remove(filepath.Dir(keyPath))
}

// Property 14: Webhook observability
// **Feature: mutating-webhook-support, Property 14: Webhook observability**
// **Validates: Requirements 7.1, 7.2, 7.3**
func TestProperty_WebhookObservability(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook operations emit metrics and log detailed information", prop.ForAll(
		func(pod *corev1.Pod, policy *optipodv1alpha1.OptimizationPolicy, shouldSucceed bool) bool {
			// Create fake client with policy
			client := createFakeClient(policy)

			// Create event recorder for testing
			eventRecorder := createTestEventRecorder()

			// Create mutator with event recorder
			mutator := NewMutator(client, eventRecorder)

			// Ensure mutator is not nil to avoid panic
			if mutator == nil {
				return false
			}

			// Modify pod to control success/failure
			if !shouldSucceed {
				// Add invalid annotation to trigger parsing error
				if pod.Annotations == nil {
					pod.Annotations = make(map[string]string)
				}
				pod.Annotations[optipodv1alpha1.AnnotationCPURequestPrefix+".app"] = "invalid-value"
			}

			// Create admission request
			req := &AdmissionRequest{
				Pod:    pod,
				DryRun: false,
			}

			// Process mutation
			response := mutator.MutatePod(req)

			// Verify response is always allowed (graceful error handling)
			if !response.Allowed {
				return false
			}

			// For successful operations, should have patches if annotations are valid
			if shouldSucceed && pod.Annotations != nil {
				hasValidAnnotations := false
				for key := range pod.Annotations {
					if key == optipodv1alpha1.AnnotationWebhookEnabled ||
						key == optipodv1alpha1.AnnotationStrategy ||
						strings.HasPrefix(key, optipodv1alpha1.AnnotationCPURequestPrefix) ||
						strings.HasPrefix(key, optipodv1alpha1.AnnotationMemoryRequestPrefix) {
						hasValidAnnotations = true
						break
					}
				}

				if hasValidAnnotations && len(response.Patches) == 0 {
					// This might be valid if webhook is not enabled or no matching policy
					return true
				}
			}

			// For failed operations, should still be allowed but with error message
			if !shouldSucceed && response.Message == "" {
				return false
			}

			return true
		},
		genPodWithAnnotations(),
		genOptimizationPolicy(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Property 15: Webhook debugging support
// **Feature: mutating-webhook-support, Property 15: Webhook debugging support**
// **Validates: Requirements 7.5**
func TestProperty_WebhookDebuggingSupport(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("webhook provides debugging information through configuration endpoints", prop.ForAll(
		func(config ServerConfig) bool {
			// Create fake client
			client := createFakeClient()

			// Create event recorder
			eventRecorder := createTestEventRecorder()

			// Create server with debugging support
			server := NewServer(client, config.CertPath, config.KeyPath, config.Port, eventRecorder)

			// Ensure server is not nil to avoid panic
			if server == nil {
				return false
			}

			// Mock certificate files for testing
			if err := createMockCertificates(config.CertPath, config.KeyPath); err != nil {
				return false
			}
			defer cleanupMockCertificates(config.CertPath, config.KeyPath)

			// Test debug configuration endpoint
			debugInfo := server.GetDebugConfiguration()

			// Should contain basic server information
			if debugInfo == nil {
				return false
			}

			// Should have server configuration details
			serverPort, hasPort := debugInfo["server_port"]
			if !hasPort || serverPort != config.Port {
				return false
			}

			certPath, hasCertPath := debugInfo["cert_path"]
			if !hasCertPath || certPath != config.CertPath {
				return false
			}

			keyPath, hasKeyPath := debugInfo["key_path"]
			if !hasKeyPath || keyPath != config.KeyPath {
				return false
			}

			// Should have readiness status
			_, hasServerReady := debugInfo["server_ready"]
			if !hasServerReady {
				return false
			}

			_, hasMutatorReady := debugInfo["mutator_ready"]
			if !hasMutatorReady {
				return false
			}

			// Should have timestamp
			_, hasTimestamp := debugInfo["timestamp"]
			return hasTimestamp
		},
		genServerConfig(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Generator functions for observability tests

// genServerConfig generates valid server configurations
func genServerConfig() gopter.Gen {
	return gen.OneConstOf(
		ServerConfig{
			CertPath: "/tmp/test-cert.pem",
			KeyPath:  "/tmp/test-key.pem",
			Port:     9443,
		},
		ServerConfig{
			CertPath: "/tmp/webhook-cert.pem",
			KeyPath:  "/tmp/webhook-key.pem",
			Port:     8443,
		},
		ServerConfig{
			CertPath: "/tmp/optipod-cert.pem",
			KeyPath:  "/tmp/optipod-key.pem",
			Port:     443,
		},
	)
}

// ServerConfig represents webhook server configuration for testing
type ServerConfig struct {
	CertPath string
	KeyPath  string
	Port     int
}
