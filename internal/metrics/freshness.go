package metrics

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ObservationClock distinguishes a query's evaluation time from the source
// scrape time. Prometheus can carry a sample forward within its lookback.
type ObservationClock interface {
	SourceTimestamp(context.Context, string, string, string, time.Time, bool) (time.Time, error)
}

func (p *PrometheusProvider) SourceTimestamp(ctx context.Context, ns, pod, container string, at time.Time, cpu bool) (time.Time, error) {
	metric := workingSetMetric
	if cpu {
		metric = "container_cpu_usage_seconds_total"
	}
	query := fmt.Sprintf(`timestamp(%s{namespace=%q,pod=%q,container=%q})`, metric, ns, pod, container)
	value, warnings, err := p.client.QueryRange(ctx, query, v1.Range{Start: at, End: at, Step: time.Second})
	if err != nil {
		return time.Time{}, err
	}
	if len(warnings) > 0 {
		return time.Time{}, fmt.Errorf("incomplete source timestamp result: %v", warnings)
	}
	matrix, ok := value.(model.Matrix)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid source timestamp result")
	}
	var newest time.Time
	for _, series := range matrix {
		for _, point := range series.Values {
			v := float64(point.Value)
			if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > float64(at.Unix()+1) {
				return time.Time{}, fmt.Errorf("invalid source timestamp")
			}
			t := time.Unix(0, int64(v*1e9))
			if t.After(newest) {
				newest = t
			}
		}
	}
	if newest.IsZero() {
		return newest, ErrNoData
	}
	return newest, nil
}
