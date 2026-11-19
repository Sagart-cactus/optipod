package metrics

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// HPAHistory contains observed scale-in pressure at each Prometheus evaluation.
// A factor of one preserves demand while a scale-out is pending.
type HPAHistory struct {
	First   time.Time
	Step    time.Duration
	Factors map[int64]float64
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
	labels := fmt.Sprintf(`{namespace=%q,horizontalpodautoscaler=%q}`, namespace, name)
	// Evaluate the two gauges together on each individual scraper. Either
	// desired or current can be zero during scale-to-zero or initialization.
	current := "max(kube_horizontalpodautoscaler_status_current_replicas" + labels + ")"
	desired := "max(kube_horizontalpodautoscaler_status_desired_replicas" + labels + ")"
	query := fmt.Sprintf("clamp_min((%s > 0) / clamp((%s > 0), %d, %d), 1)", current, desired, minReplicas, maxReplicas)
	value, warnings, err := p.queryRange(ctx, query, v1.Range{Start: first, End: last, Step: step})
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
	h := &HPAHistory{First: first, Step: step, Factors: map[int64]float64{}}
	for _, series := range matrix {
		for _, sample := range series.Values {
			f := float64(sample.Value)
			if math.IsNaN(f) || math.IsInf(f, 0) || f < 1 {
				return nil, fmt.Errorf("invalid HPA replica ratio %v", f)
			}
			t := sample.Timestamp.Time().Unix()
			h.Factors[t] = max(f, h.Factors[t])
		}
	}
	if len(h.Factors) == 0 {
		return nil, ErrNoData
	}
	return h, nil
}
