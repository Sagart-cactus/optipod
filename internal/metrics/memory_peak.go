package metrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// MemoryPeakProvider returns one observed maximum per nonempty bucket inside
// the eligible lifetime. Buckets bound response size without skipping scrapes.
type MemoryPeakProvider interface {
	MemoryPeakSamples(context.Context, string, string, string, time.Time, time.Time, time.Duration) ([]Point, error)
}

func (p *PrometheusProvider) MemoryPeakSamples(ctx context.Context, ns, pod, container string, start, end time.Time, window time.Duration) ([]Point, error) {
	src := &api.MetricSource{Name: workingSetMetric}
	return p.gaugePeaks(ctx, src, ns, pod, container, start, end, window)
}

func (s *configuredSeries) MemoryPeakSamples(ctx context.Context, ns, pod, container string, start, end time.Time, window time.Duration) ([]Point, error) {
	if s.config.JVM != nil {
		// Heap and non-heap observations must be combined at matching timestamps.
		return s.GetResourceSamples(ctx, ns, pod, container, window, false)
	}
	if src := s.source(false); src != nil {
		return s.base.gaugePeaks(ctx, src, ns, pod, container, start, end, window)
	}
	return s.base.MemoryPeakSamples(ctx, ns, pod, container, start, end, window)
}

func (p *PrometheusProvider) gaugePeaks(ctx context.Context, src *api.MetricSource, ns, pod, container string, start, end time.Time, window time.Duration) ([]Point, error) {
	selector, err := metricSelector(src, ns, pod, container)
	if err != nil {
		return nil, err
	}
	// Prometheus timestamps have millisecond precision and range selectors
	// exclude the left edge. Partition the eligible inclusive interval exactly.
	first := start.UnixMilli()
	if start.After(time.UnixMilli(first)) {
		first++
	}
	last := end.UnixMilli()
	if first > last {
		return nil, ErrNoData
	}
	step := p.SampleInterval(window)
	width := step.Milliseconds()
	size := last - first + 1
	var points []Point
	read := func(from, to, millis int64) error {
		query := fmt.Sprintf("max_over_time(%s[%dms])", selector, millis)
		if src.Multiplier != nil {
			query = fmt.Sprintf("(%s) * %g", query, *src.Multiplier)
		}
		value, warnings, err := p.queryRange(ctx, query, v1.Range{Start: time.UnixMilli(from), End: time.UnixMilli(to), Step: step})
		bucket, err := matrixPoints(value, warnings, err, false)
		if errors.Is(err, ErrNoData) {
			return nil
		}
		points = append(points, bucket...)
		return err
	}
	fullEnd := first + size/width*width - 1
	if size >= width {
		if err := read(first+width-1, fullEnd, width); err != nil {
			return nil, err
		}
	}
	if fullEnd < last {
		if err := read(last, last, last-fullEnd); err != nil {
			return nil, err
		}
	}
	if len(points) == 0 {
		return nil, ErrNoData
	}
	return points, nil
}
