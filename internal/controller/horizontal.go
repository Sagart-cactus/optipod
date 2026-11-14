package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (wp *WorkloadProcessor) CleanupHorizontal(ctx context.Context, key client.ObjectKey) error {
	if h, ok := wp.applicationEngine.(interface {
		CleanupHorizontal(context.Context, client.ObjectKey) error
	}); ok {
		return h.CleanupHorizontal(ctx, key)
	}
	return nil
}
