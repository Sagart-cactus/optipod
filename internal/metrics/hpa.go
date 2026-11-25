package metrics

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ReplicaCounts pairs observations from the same scraper at one evaluation.
type ReplicaCounts struct {
	Current, Serving int32
}

// HPAHistory contains the observed fleet and conservative serving capacity.
type HPAHistory struct {
	First       time.Time
	Step        time.Duration
	Counts      map[int64]ReplicaCounts
	DelayBudget time.Duration
	// Positive when the selected container alone determines the absolute CPU scaling signal.
	ScaleOutTargetMilli int64
}

type HPAHistoryProvider interface {
	HPAHistory(ctx context.Context, namespace, name string, minReplicas, maxReplicas int32, start, end time.Time, window time.Duration) (*HPAHistory, error)
}

func (p *PrometheusProvider) HPAHistory(ctx context.Context, namespace, name string, minReplicas, maxReplicas int32, start, end time.Time, window time.Duration) (*HPAHistory, error) {
	step := p.SampleInterval(window)
	// Never include a measurement from a previous HPA with the same name.
	first := start.Truncate(step)
	if first.Before(start) {
		first = first.Add(step)
	}
	last := end.Truncate(step)
	if first.After(last) {
		return nil, ErrNoData
	}
	query := fmt.Sprintf(`max by (__name__) ({__name__=~"kube_horizontalpodautoscaler_status_(current|desired)_replicas",namespace=%q,horizontalpodautoscaler=%q})`, namespace, name)
	grid := v1.Range{Start: first, End: last, Step: step}
	replicas, err := readReplicas(ctx, p.clients, func(ctx context.Context, client v1.API) (map[int64]ReplicaCounts, error) {
		value, warnings, _, err := client.QueryRange(ctx, query, grid)
		if err != nil {
			return nil, err
		}
		if len(warnings) != 0 {
			return nil, fmt.Errorf("incomplete HPA history: %v", warnings)
		}
		matrix, ok := value.(model.Matrix)
		if !ok {
			return nil, fmt.Errorf("unexpected HPA history type %T", value)
		}
		return pairedCounts(matrix, minReplicas, maxReplicas)
	})
	if err != nil {
		return nil, err
	}
	h := &HPAHistory{First: first, Step: step, Counts: map[int64]ReplicaCounts{}}
	for _, counts := range replicas {
		for t, c := range counts {
			old := h.Counts[t]
			// Select one coherent pair, favoring the higher scale-in pressure.
			left, right := int64(c.Current)*int64(old.Serving), int64(old.Current)*int64(c.Serving)
			if left > right || (left == right && c.Current > old.Current) {
				h.Counts[t] = c
			}
		}
	}
	if len(h.Counts) == 0 {
		return nil, ErrNoData
	}
	return h, nil
}

func pairedCounts(matrix model.Matrix, low, high int32) (map[int64]ReplicaCounts, error) {
	counts := map[int64]ReplicaCounts{}
	for _, series := range matrix {
		for _, sample := range series.Values {
			n := float64(sample.Value)
			if n < 0 || n > math.MaxInt32 || math.Trunc(n) != n {
				return nil, fmt.Errorf("invalid HPA replica count %v", n)
			}
			t := sample.Timestamp.Time().Unix()
			pair := counts[t]
			switch series.Metric[model.MetricNameLabel] {
			case "kube_horizontalpodautoscaler_status_current_replicas":
				pair.Current = int32(n)
			case "kube_horizontalpodautoscaler_status_desired_replicas":
				if n > 0 {
					pair.Serving = max(low, min(high, int32(n)))
				}
			}
			counts[t] = pair
		}
	}
	for t, c := range counts {
		if c.Current == 0 || c.Serving == 0 {
			delete(counts, t)
			continue
		}
		c.Serving = min(c.Current, c.Serving)
		counts[t] = c
	}
	return counts, nil
}
