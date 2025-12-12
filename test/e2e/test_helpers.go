//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createTestNamespace creates a test namespace with proper labels
func createTestNamespace(ctx context.Context, client client.Client, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"e2e-test": "true",
				"test-run": fmt.Sprintf("%d", metav1.Now().Unix()),
			},
		},
	}

	return client.Create(ctx, ns)
}

// deleteTestNamespace deletes a test namespace
func deleteTestNamespace(ctx context.Context, client client.Client, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return client.Delete(ctx, ns)
}

// isCRDAvailable checks if the OptimizationPolicy CRD is available in the cluster
func isCRDAvailable(ctx context.Context, client client.Client) bool {
	// Try to list OptimizationPolicies to see if the CRD exists
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "optipod.optipod.io",
		Version: "v1alpha1",
		Kind:    "OptimizationPolicy",
	})

	err := client.List(ctx, list)
	// If we get a "no matches for kind" error, the CRD doesn't exist
	if err != nil && strings.Contains(err.Error(), "no matches for kind") {
		return false
	}
	return true
}
