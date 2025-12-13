package helpers

import (
	"context"
	"fmt"
	"time"

	"github.com/optipod/optipod/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CleanupHelper provides utilities for cleaning up test resources
type CleanupHelper struct {
	client    client.Client
	resources []ResourceRef
}

// NewCleanupHelper creates a new CleanupHelper instance
func NewCleanupHelper(k8sClient client.Client) *CleanupHelper {
	return &CleanupHelper{
		client:    k8sClient,
		resources: make([]ResourceRef, 0),
	}
}

// ResourceRef represents a reference to a Kubernetes resource
type ResourceRef struct {
	Name      string
	Namespace string
	Kind      string
	Object    client.Object
}

// TrackResource adds a resource to the cleanup list
func (h *CleanupHelper) TrackResource(resource ResourceRef) {
	h.resources = append(h.resources, resource)
}

// TrackPolicy adds an OptimizationPolicy to the cleanup list
func (h *CleanupHelper) TrackPolicy(name, namespace string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: namespace,
		Kind:      "OptimizationPolicy",
		Object:    &v1alpha1.OptimizationPolicy{},
	})
}

// TrackDeployment adds a Deployment to the cleanup list
func (h *CleanupHelper) TrackDeployment(name, namespace string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: namespace,
		Kind:      "Deployment",
		Object:    &appsv1.Deployment{},
	})
}

// TrackStatefulSet adds a StatefulSet to the cleanup list
func (h *CleanupHelper) TrackStatefulSet(name, namespace string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: namespace,
		Kind:      "StatefulSet",
		Object:    &appsv1.StatefulSet{},
	})
}

// TrackDaemonSet adds a DaemonSet to the cleanup list
func (h *CleanupHelper) TrackDaemonSet(name, namespace string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: namespace,
		Kind:      "DaemonSet",
		Object:    &appsv1.DaemonSet{},
	})
}

// TrackNamespace adds a Namespace to the cleanup list
func (h *CleanupHelper) TrackNamespace(name string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: "",
		Kind:      "Namespace",
		Object:    &corev1.Namespace{},
	})
}

// TrackServiceAccount adds a ServiceAccount to the cleanup list
func (h *CleanupHelper) TrackServiceAccount(name, namespace string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: namespace,
		Kind:      "ServiceAccount",
		Object:    &corev1.ServiceAccount{},
	})
}

// TrackClusterRole adds a ClusterRole to the cleanup list
func (h *CleanupHelper) TrackClusterRole(name string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: "",
		Kind:      "ClusterRole",
		Object:    &rbacv1.ClusterRole{},
	})
}

// TrackClusterRoleBinding adds a ClusterRoleBinding to the cleanup list
func (h *CleanupHelper) TrackClusterRoleBinding(name string) {
	h.TrackResource(ResourceRef{
		Name:      name,
		Namespace: "",
		Kind:      "ClusterRoleBinding",
		Object:    &rbacv1.ClusterRoleBinding{},
	})
}

// CleanupAll removes all tracked resources
func (h *CleanupHelper) CleanupAll() error {
	var errs []error

	// Clean up resources in reverse order (LIFO)
	for i := len(h.resources) - 1; i >= 0; i-- {
		resource := h.resources[i]
		if err := h.deleteResource(resource); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s %s/%s: %w",
				resource.Kind, resource.Namespace, resource.Name, err))
		}
	}

	// Clear the resources list
	h.resources = make([]ResourceRef, 0)

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	return nil
}

// CleanupNamespace removes all resources in a specific namespace
func (h *CleanupHelper) CleanupNamespace(namespace string) error {
	// Delete OptimizationPolicies
	policies := &v1alpha1.OptimizationPolicyList{}
	if err := h.client.List(context.TODO(), policies, client.InNamespace(namespace)); err == nil {
		for _, policy := range policies.Items {
			if err := h.client.Delete(context.TODO(), &policy); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete policy %s: %w", policy.Name, err)
			}
		}
	}

	// Delete Deployments
	deployments := &appsv1.DeploymentList{}
	if err := h.client.List(context.TODO(), deployments, client.InNamespace(namespace)); err == nil {
		for _, deployment := range deployments.Items {
			if err := h.client.Delete(context.TODO(), &deployment); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete deployment %s: %w", deployment.Name, err)
			}
		}
	}

	// Delete StatefulSets
	statefulSets := &appsv1.StatefulSetList{}
	if err := h.client.List(context.TODO(), statefulSets, client.InNamespace(namespace)); err == nil {
		for _, statefulSet := range statefulSets.Items {
			if err := h.client.Delete(context.TODO(), &statefulSet); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete statefulset %s: %w", statefulSet.Name, err)
			}
		}
	}

	// Delete DaemonSets
	daemonSets := &appsv1.DaemonSetList{}
	if err := h.client.List(context.TODO(), daemonSets, client.InNamespace(namespace)); err == nil {
		for _, daemonSet := range daemonSets.Items {
			if err := h.client.Delete(context.TODO(), &daemonSet); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete daemonset %s: %w", daemonSet.Name, err)
			}
		}
	}

	// Wait for resources to be deleted
	return h.waitForNamespaceCleanup(namespace)
}

// deleteResource deletes a single resource
func (h *CleanupHelper) deleteResource(resource ResourceRef) error {
	// Get the resource first to check if it exists
	namespacedName := types.NamespacedName{
		Name:      resource.Name,
		Namespace: resource.Namespace,
	}

	// Create the appropriate object based on Kind if Object is not provided
	var obj client.Object
	if resource.Object != nil {
		obj = resource.Object.DeepCopyObject().(client.Object)
	} else {
		obj = h.createObjectByKind(resource.Kind)
		if obj == nil {
			return fmt.Errorf("unsupported resource kind: %s", resource.Kind)
		}
	}

	err := h.client.Get(context.TODO(), namespacedName, obj)
	if errors.IsNotFound(err) {
		// Resource already deleted
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// Delete the resource
	err = h.client.Delete(context.TODO(), obj)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	// Wait for deletion to complete for certain resource types
	switch resource.Kind {
	case "Namespace":
		return h.waitForNamespaceDeletion(resource.Name)
	case "Deployment", "StatefulSet", "DaemonSet":
		return h.waitForWorkloadDeletion(resource.Name, resource.Namespace, resource.Kind)
	}

	return nil
}

// createObjectByKind creates an empty object of the specified kind
func (h *CleanupHelper) createObjectByKind(kind string) client.Object {
	switch kind {
	case "Deployment":
		return &appsv1.Deployment{}
	case "StatefulSet":
		return &appsv1.StatefulSet{}
	case "DaemonSet":
		return &appsv1.DaemonSet{}
	case "Pod":
		return &corev1.Pod{}
	case "Service":
		return &corev1.Service{}
	case "ConfigMap":
		return &corev1.ConfigMap{}
	case "Secret":
		return &corev1.Secret{}
	case "Namespace":
		return &corev1.Namespace{}
	case "OptimizationPolicy":
		return &v1alpha1.OptimizationPolicy{}
	default:
		return nil
	}
}

// waitForNamespaceDeletion waits for a namespace to be fully deleted
func (h *CleanupHelper) waitForNamespaceDeletion(namespaceName string) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 2*time.Second, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			namespace := &corev1.Namespace{}
			err := h.client.Get(context.TODO(), types.NamespacedName{Name: namespaceName}, namespace)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return false, nil
		})
}

// waitForWorkloadDeletion waits for a workload to be fully deleted
func (h *CleanupHelper) waitForWorkloadDeletion(name, namespace, kind string) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 2*time.Second, 1*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			var obj client.Object

			switch kind {
			case "Deployment":
				obj = &appsv1.Deployment{}
			case "StatefulSet":
				obj = &appsv1.StatefulSet{}
			case "DaemonSet":
				obj = &appsv1.DaemonSet{}
			default:
				return true, nil // Skip waiting for unknown types
			}

			err := h.client.Get(context.TODO(), types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, obj)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return false, nil
		})
}

// waitForNamespaceCleanup waits for all resources in a namespace to be cleaned up
func (h *CleanupHelper) waitForNamespaceCleanup(namespace string) error {
	return wait.PollUntilContextTimeout(
		context.TODO(), 5*time.Second, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			// Check if any OptimizationPolicies remain
			policies := &v1alpha1.OptimizationPolicyList{}
			if err := h.client.List(context.TODO(), policies, client.InNamespace(namespace)); err == nil {
				if len(policies.Items) > 0 {
					return false, nil
				}
			}

			// Check if any Deployments remain
			deployments := &appsv1.DeploymentList{}
			if err := h.client.List(context.TODO(), deployments, client.InNamespace(namespace)); err == nil {
				if len(deployments.Items) > 0 {
					return false, nil
				}
			}

			// Check if any StatefulSets remain
			statefulSets := &appsv1.StatefulSetList{}
			if err := h.client.List(context.TODO(), statefulSets, client.InNamespace(namespace)); err == nil {
				if len(statefulSets.Items) > 0 {
					return false, nil
				}
			}

			// Check if any DaemonSets remain
			daemonSets := &appsv1.DaemonSetList{}
			if err := h.client.List(context.TODO(), daemonSets, client.InNamespace(namespace)); err == nil {
				if len(daemonSets.Items) > 0 {
					return false, nil
				}
			}

			return true, nil
		})
}

// ForceCleanupAll forcefully removes all tracked resources without waiting
func (h *CleanupHelper) ForceCleanupAll() error {
	var errs []error

	for i := len(h.resources) - 1; i >= 0; i-- {
		resource := h.resources[i]

		// Get the resource
		namespacedName := types.NamespacedName{
			Name:      resource.Name,
			Namespace: resource.Namespace,
		}

		obj := resource.Object.DeepCopyObject().(client.Object)
		err := h.client.Get(context.TODO(), namespacedName, obj)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get %s %s/%s: %w",
				resource.Kind, resource.Namespace, resource.Name, err))
			continue
		}

		// Remove finalizers if present
		if obj.GetFinalizers() != nil {
			obj.SetFinalizers([]string{})
			if err := h.client.Update(context.TODO(), obj); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove finalizers from %s %s/%s: %w",
					resource.Kind, resource.Namespace, resource.Name, err))
			}
		}

		// Force delete
		if err := h.client.Delete(context.TODO(), obj, client.GracePeriodSeconds(0)); err != nil && !errors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to force delete %s %s/%s: %w",
				resource.Kind, resource.Namespace, resource.Name, err))
		}
	}

	// Clear the resources list
	h.resources = make([]ResourceRef, 0)

	if len(errs) > 0 {
		return fmt.Errorf("force cleanup errors: %v", errs)
	}

	return nil
}

// GetTrackedResources returns a copy of the currently tracked resources
func (h *CleanupHelper) GetTrackedResources() []ResourceRef {
	resources := make([]ResourceRef, len(h.resources))
	copy(resources, h.resources)
	return resources
}

// ClearTrackedResources clears the list of tracked resources without deleting them
func (h *CleanupHelper) ClearTrackedResources() {
	h.resources = make([]ResourceRef, 0)
}

// ValidateCleanupCompleteness validates that all tracked resources have been cleaned up
func (h *CleanupHelper) ValidateCleanupCompleteness() error {
	var remainingResources []string

	for _, resource := range h.resources {
		namespacedName := types.NamespacedName{
			Name:      resource.Name,
			Namespace: resource.Namespace,
		}

		obj := resource.Object.DeepCopyObject().(client.Object)
		err := h.client.Get(context.TODO(), namespacedName, obj)
		if err == nil {
			// Resource still exists
			remainingResources = append(remainingResources,
				fmt.Sprintf("%s %s/%s", resource.Kind, resource.Namespace, resource.Name))
		} else if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check resource %s %s/%s: %w",
				resource.Kind, resource.Namespace, resource.Name, err)
		}
	}

	if len(remainingResources) > 0 {
		return fmt.Errorf("cleanup incomplete, remaining resources: %v", remainingResources)
	}

	return nil
}
