//go:build e2e

package e2e

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
