package application

import (
	"context"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/horizontal"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (e *Engine) ReconcileHorizontal(ctx context.Context, w *discovery.Workload, p *api.OptimizationPolicy) error {
	return horizontal.Reconcile(ctx, e.client, w, p, e.dryRun)
}
func (e *Engine) CleanupHorizontal(ctx context.Context, key client.ObjectKey) error {
	return horizontal.Cleanup(ctx, e.client, key, e.dryRun)
}
