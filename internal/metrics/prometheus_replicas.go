package metrics

import (
	"context"
	"fmt"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/errgroup"
)

// Keep configuration order for deterministic tie breaks. Every scraper must
// answer successfully before a partial result can influence a recommendation.
func readReplicas[T any](ctx context.Context, clients []v1.API, read func(context.Context, v1.API) (T, error)) ([]T, error) {
	if len(clients) == 1 {
		v, err := read(ctx, clients[0])
		return []T{v}, err
	}
	g, ctx := errgroup.WithContext(ctx)
	out := make([]T, len(clients))
	for i, c := range clients {
		g.Go(func() error {
			value, err := read(ctx, c)
			if err != nil {
				return fmt.Errorf("prometheus replica %d: %w", i+1, err)
			}
			out[i] = value
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// All scrapers evaluate the same grid. The existing range readers retain one
// observation per timestamp, including an observed peak from either scraper.
func (p *PrometheusProvider) queryRange(ctx context.Context, q string, r v1.Range) (model.Value, v1.Warnings, error) {
	if len(p.clients) == 1 {
		v, w, _, err := p.clients[0].QueryRange(ctx, q, r)
		return v, w, err
	}
	results, err := readReplicas(ctx, p.clients, func(ctx context.Context, c v1.API) (model.Matrix, error) {
		v, w, _, err := c.QueryRange(ctx, q, r)
		if err != nil {
			return nil, err
		}
		if len(w) > 0 {
			return nil, fmt.Errorf("incomplete Prometheus result: %v", w)
		}
		m, ok := v.(model.Matrix)
		if !ok {
			return nil, fmt.Errorf("unexpected result type: %T", v)
		}
		return m, nil
	})
	if err != nil {
		return nil, nil, err
	}
	var m model.Matrix
	for _, part := range results {
		m = append(m, part...)
	}
	return m, nil, nil
}
