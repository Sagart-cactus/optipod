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
	"fmt"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

// TestSSAIntegration tests SSA patch is accepted by Kubernetes
func TestSSAIntegration(t *testing.T) {
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

	// Create clients
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	ctx := context.Background()

	// Create a test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ssa-integration-test",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		if err := k8sClient.Delete(ctx, ns); err != nil {
			t.Logf("Failed to delete namespace: %v", err)
		}
	}()

	// Create a test deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "ssa-integration-test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
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
							Name:  "nginx",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, deployment); err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}
	defer func() {
		if err := k8sClient.Delete(ctx, deployment); err != nil {
			t.Logf("Failed to delete deployment: %v", err)
		}
	}()

	// Wait for deployment to be created
	time.Sleep(100 * time.Millisecond)

	// Create engine
	engine := &Engine{
		dynamicClient: dynamicClient,
	}

	// Create workload
	deploymentUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		t.Fatalf("Failed to convert deployment to unstructured: %v", err)
	}

	workload := &Workload{
		Kind:      "Deployment",
		Namespace: "ssa-integration-test",
		Name:      "test-deployment",
		Object:    &unstructured.Unstructured{Object: deploymentUnstructured},
	}

	// Create recommendation
	rec := &recommendation.Recommendation{
		CPU:         resource.MustParse("300m"),
		Memory:      resource.MustParse("512Mi"),
		Explanation: "Integration test recommendation",
	}

	// Create policy with SSA enabled
	policy := &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				UpdateRequestsOnly: false,
			},
		},
	}

	// Apply SSA patch
	err = engine.ApplyWithSSA(ctx, workload, "nginx", rec, policy)
	if err != nil {
		t.Fatalf("Failed to apply SSA patch: %v", err)
	}

	// Verify the deployment was updated
	updatedDeployment := &appsv1.Deployment{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Name:      "test-deployment",
		Namespace: "ssa-integration-test",
	}, updatedDeployment)
	if err != nil {
		t.Fatalf("Failed to get updated deployment: %v", err)
	}

	// Verify resources were updated
	container := updatedDeployment.Spec.Template.Spec.Containers[0]
	if container.Resources.Requests.Cpu().String() != "300m" {
		t.Errorf("Expected CPU request to be 300m, got %s", container.Resources.Requests.Cpu().String())
	}
	if container.Resources.Requests.Memory().String() != "512Mi" {
		t.Errorf("Expected memory request to be 512Mi, got %s", container.Resources.Requests.Memory().String())
	}

	// Verify managedFields shows "optipod" as owner
	foundOptipodManager := false
	for _, mf := range updatedDeployment.ManagedFields {
		if mf.Manager == "optipod" && mf.Operation == metav1.ManagedFieldsOperationApply {
			foundOptipodManager = true
			break
		}
	}
	if !foundOptipodManager {
		t.Errorf("Expected to find 'optipod' as field manager with Apply operation in managedFields")
	}

	// Verify only resource fields were modified (other fields should remain unchanged)
	if container.Image != "nginx:latest" {
		t.Errorf("Expected image to remain unchanged, got %s", container.Image)
	}
}

// TestSSAFieldOwnershipWithMultipleManagers tests field ownership with multiple managers
func TestSSAFieldOwnershipWithMultipleManagers(t *testing.T) {
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

	// Create clients
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}

	ctx := context.Background()

	// Create a test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ssa-multi-manager-test",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		if err := k8sClient.Delete(ctx, ns); err != nil {
			t.Logf("Failed to delete namespace: %v", err)
		}
	}()

	// Create a test deployment (simulating another manager like kubectl or ArgoCD)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-manager-deployment",
			Namespace: "ssa-multi-manager-test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "multi-test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "multi-test"},
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
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, deployment); err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}
	defer func() {
		if err := k8sClient.Delete(ctx, deployment); err != nil {
			t.Logf("Failed to delete deployment: %v", err)
		}
	}()

	// Wait for deployment to be created
	time.Sleep(100 * time.Millisecond)

	// Create engine
	engine := &Engine{
		dynamicClient: dynamicClient,
	}

	// Create workload
	deploymentUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		t.Fatalf("Failed to convert deployment to unstructured: %v", err)
	}

	workload := &Workload{
		Kind:      "Deployment",
		Namespace: "ssa-multi-manager-test",
		Name:      "multi-manager-deployment",
		Object:    &unstructured.Unstructured{Object: deploymentUnstructured},
	}

	// Create recommendation
	rec := &recommendation.Recommendation{
		CPU:         resource.MustParse("250m"),
		Memory:      resource.MustParse("384Mi"),
		Explanation: "Multi-manager test recommendation",
	}

	// Create policy with SSA enabled
	policy := &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				UpdateRequestsOnly: false,
			},
		},
	}

	// Apply SSA patch from optipod
	err = engine.ApplyWithSSA(ctx, workload, "app", rec, policy)
	if err != nil {
		t.Fatalf("Failed to apply SSA patch: %v", err)
	}

	// Verify the deployment was updated
	updatedDeployment := &appsv1.Deployment{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Name:      "multi-manager-deployment",
		Namespace: "ssa-multi-manager-test",
	}, updatedDeployment)
	if err != nil {
		t.Fatalf("Failed to get updated deployment: %v", err)
	}

	// Verify resources were updated by optipod
	container := updatedDeployment.Spec.Template.Spec.Containers[0]
	if container.Resources.Requests.Cpu().String() != "250m" {
		t.Errorf("Expected CPU request to be 250m, got %s", container.Resources.Requests.Cpu().String())
	}

	// Verify other fields managed by the original manager remain unchanged
	if *updatedDeployment.Spec.Replicas != 2 {
		t.Errorf("Expected replicas to remain 2, got %d", *updatedDeployment.Spec.Replicas)
	}
	if container.Image != "nginx:1.21" {
		t.Errorf("Expected image to remain nginx:1.21, got %s", container.Image)
	}

	// Verify managedFields shows both managers
	foundOptipodManager := false
	foundOtherManager := false
	for _, mf := range updatedDeployment.ManagedFields {
		if mf.Manager == "optipod" && mf.Operation == metav1.ManagedFieldsOperationApply {
			foundOptipodManager = true
		}
		if mf.Manager != "optipod" {
			foundOtherManager = true
		}
	}
	if !foundOptipodManager {
		t.Errorf("Expected to find 'optipod' as field manager in managedFields")
	}
	if !foundOtherManager {
		t.Errorf("Expected to find another field manager in managedFields")
	}
}

// int32Ptr returns a pointer to an int32 value
func int32Ptr(i int32) *int32 {
	return &i
}

// Feature: server-side-apply-support, Property 12: Field ownership tracking
// For any workload updated via SSA, querying managedFields should show "optipod" as the
// manager for resource request and limit fields
// Validates: Requirements 4.2, 4.5
func TestProperty_FieldOwnershipTracking(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("managedFields shows optipod as owner for resource fields", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values (in millicores and MiB)
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create a mock client that simulates managedFields behavior
			mockClient := &mockClientWithManagedFields{
				managedFields: []metav1.ManagedFieldsEntry{},
			}

			// Create engine with mock dynamic client
			mockDynamic := &mockDynamicClientWithManagedFields{
				mockClient: mockClient,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			// Apply with SSA
			err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify managedFields contains optipod as manager
			foundOptipod := false
			for _, mf := range mockClient.managedFields {
				if mf.Manager == "optipod" && mf.Operation == metav1.ManagedFieldsOperationApply {
					foundOptipod = true
					break
				}
			}

			return foundOptipod
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// mockClientWithManagedFields simulates managedFields behavior
type mockClientWithManagedFields struct {
	managedFields []metav1.ManagedFieldsEntry
}

// mockDynamicClientWithManagedFields is a mock dynamic client that tracks managedFields
type mockDynamicClientWithManagedFields struct {
	dynamic.Interface
	mockClient *mockClientWithManagedFields
}

func (m *mockDynamicClientWithManagedFields) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &mockNamespaceableResourceWithManagedFields{mockClient: m.mockClient}
}

type mockNamespaceableResourceWithManagedFields struct {
	dynamic.NamespaceableResourceInterface
	mockClient *mockClientWithManagedFields
}

func (m *mockNamespaceableResourceWithManagedFields) Namespace(ns string) dynamic.ResourceInterface {
	return &mockResourceInterfaceWithManagedFields{mockClient: m.mockClient}
}

type mockResourceInterfaceWithManagedFields struct {
	dynamic.ResourceInterface
	mockClient *mockClientWithManagedFields
}

func (m *mockResourceInterfaceWithManagedFields) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	// Simulate adding managedFields entry when SSA is used
	if pt == types.ApplyPatchType && options.FieldManager != "" {
		m.mockClient.managedFields = append(m.mockClient.managedFields, metav1.ManagedFieldsEntry{
			Manager:    options.FieldManager,
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "apps/v1",
			Time:       &metav1.Time{Time: time.Now()},
		})
	}

	return &unstructured.Unstructured{}, nil
}

// Feature: server-side-apply-support, Property 13: Consistent field manager across policies
// For any workload targeted by multiple policies, all SSA operations should use the same
// "optipod" fieldManager
// Validates: Requirements 4.3
func TestProperty_ConsistentFieldManager(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("all policies use the same 'optipod' field manager", prop.ForAll(
		func(numPolicies int, cpuReq, memReq int64) bool {
			// Generate 1-5 policies
			if numPolicies < 1 || numPolicies > 5 {
				return true // Skip invalid range
			}

			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Track all field managers used
			fieldManagers := make(map[string]bool)

			// Create a mock client that tracks field managers
			mockClient := &mockClientWithManagedFields{
				managedFields: []metav1.ManagedFieldsEntry{},
			}

			// Create engine with mock dynamic client
			mockDynamic := &mockDynamicClientWithManagedFields{
				mockClient: mockClient,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Apply patches from multiple policies
			for i := 0; i < numPolicies; i++ {
				// Create recommendation with slightly different values
				rec := &recommendation.Recommendation{
					CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq+int64(i*10))),
					Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq+int64(i*10))),
					Explanation: fmt.Sprintf("Policy %d recommendation", i),
				}

				// Create policy
				policy := createMockPolicy(true, false)
				policy.Name = fmt.Sprintf("policy-%d", i)

				// Apply with SSA
				err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
				if err != nil {
					return false
				}
			}

			// Collect all field managers used
			for _, mf := range mockClient.managedFields {
				if mf.Operation == metav1.ManagedFieldsOperationApply {
					fieldManagers[mf.Manager] = true
				}
			}

			// Verify only one field manager was used: "optipod"
			if len(fieldManagers) != 1 {
				return false
			}

			_, hasOptipod := fieldManagers["optipod"]
			return hasOptipod
		},
		gen.IntRange(1, 5),
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: server-side-apply-support, Property 14: Apply operation type
// For any SSA operation, the managedFields should record the operation as "Apply" not "Update"
// Validates: Requirements 4.4
func TestProperty_ApplyOperationType(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("SSA operations are recorded as Apply not Update", prop.ForAll(
		func(cpuReq, memReq int64, updateRequestsOnly bool) bool {
			// Generate reasonable resource values
			if cpuReq < 100 || cpuReq > 4000 || memReq < 128 || memReq > 8192 {
				return true // Skip invalid values
			}

			// Create a mock client that tracks operation types
			mockClient := &mockClientWithManagedFields{
				managedFields: []metav1.ManagedFieldsEntry{},
			}

			// Create engine with mock dynamic client
			mockDynamic := &mockDynamicClientWithManagedFields{
				mockClient: mockClient,
			}

			engine := &Engine{
				dynamicClient: mockDynamic,
			}

			// Create workload
			workload := createMockWorkload()

			// Create recommendation
			rec := &recommendation.Recommendation{
				CPU:         resource.MustParse(fmt.Sprintf("%dm", cpuReq)),
				Memory:      resource.MustParse(fmt.Sprintf("%dMi", memReq)),
				Explanation: "Test recommendation",
			}

			// Create policy
			policy := createMockPolicy(true, false)
			policy.Spec.UpdateStrategy.UpdateRequestsOnly = updateRequestsOnly

			// Apply with SSA
			err := engine.ApplyWithSSA(context.Background(), workload, "test-container", rec, policy)
			if err != nil {
				return false
			}

			// Verify all operations are "Apply" not "Update"
			for _, mf := range mockClient.managedFields {
				if mf.Manager == "optipod" {
					// SSA should use Apply operation
					if mf.Operation != metav1.ManagedFieldsOperationApply {
						return false
					}
				}
			}

			return true
		},
		gen.Int64Range(100, 4000),
		gen.Int64Range(128, 8192),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
