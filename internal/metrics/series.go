package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var ErrNoData = errors.New("no metric samples")

// Point is one observation, retaining time for freshness, coverage and trends.
// CPU is expressed in millicores and memory in bytes.
type Point struct {
	Time  time.Time
	Value int64
}

type SeriesProvider interface {
	GetResourceSamples(context.Context, string, string, string, time.Duration, bool) ([]Point, error)
	SampleInterval(time.Duration) time.Duration
}

// Prometheus limits range queries to 11,000 points per series. Preserve the
// short rate interval independently of the historical query's resolution.
func (p *PrometheusProvider) SampleInterval(window time.Duration) time.Duration {
	steps := math.Ceil(window.Seconds() / 10000 / 30)
	if steps < 1 {
		steps = 1
	}
	return time.Duration(steps) * 30 * time.Second
}

func (p *PrometheusProvider) GetResourceSamples(ctx context.Context, ns, pod, container string, window time.Duration, cpu bool) ([]Point, error) {
	query := fmt.Sprintf(`container_memory_working_set_bytes{namespace=%q,pod=%q,container=%q}`, ns, pod, container)
	if cpu {
		query = fmt.Sprintf(`rate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container=%q}[1m])`, ns, pod, container)
	}
	end := time.Now()
	result, warnings, err := p.client.QueryRange(ctx, query, v1.Range{Start: end.Add(-window), End: end, Step: p.SampleInterval(window)})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	// Partial/omitted series can underestimate demand. Do not treat a warned
	// result as a complete observation suitable for automatic reductions.
	if len(warnings) > 0 {
		return nil, fmt.Errorf("incomplete Prometheus result: %v", warnings)
	}
	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}
	// Duplicate scrape jobs must not give one replica more statistical weight.
	// Retain the conservative maximum for each timestamp of this pod/container.
	values := map[model.Time]int64{}
	for _, series := range matrix {
		for _, sample := range series.Values {
			v := float64(sample.Value)
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return nil, fmt.Errorf("invalid metric sample: %v", v)
			}
			if cpu {
				v *= 1000
			}
			if v >= float64(math.MaxInt64) {
				return nil, fmt.Errorf("metric sample overflows resource quantity")
			}
			n := int64(math.Ceil(v))
			if old, exists := values[sample.Timestamp]; !exists || n > old {
				values[sample.Timestamp] = n
			}
		}
	}
	if len(values) == 0 {
		return nil, ErrNoData
	}
	points := make([]Point, 0, len(values))
	for stamp, value := range values {
		points = append(points, Point{Time: stamp.Time(), Value: value})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
	return points, nil
}

func Statistics(points []Point, cpu bool) ResourceMetrics {
	values := make([]int64, len(points))
	var first, last time.Time
	for i, p := range points {
		values[i] = p.Value
		if first.IsZero() || p.Time.Before(first) {
			first = p.Time
		}
		if p.Time.After(last) {
			last = p.Time
		}
	}
	r := computePercentiles(values, cpu)
	r.FirstSample, r.LastSample, r.Points = first, last, points
	return r
}

func (m *MetricsServerProvider) SampleInterval(_ time.Duration) time.Duration { return m.interval }

func (m *MetricsServerProvider) GetResourceSamples(_ context.Context, namespace, pod, container string, window time.Duration, cpu bool) ([]Point, error) {
	if window > time.Duration(m.maxSamples)*m.interval {
		return nil, fmt.Errorf("metrics-server window %s exceeds configured sample retention %s; increase capacity or use Prometheus", window, time.Duration(m.maxSamples)*m.interval)
	}
	key, ok := m.store.resolveTargetKey(namespace, pod, container)
	if !ok {
		return nil, ErrNoData
	}
	samples := m.store.samplesSince(key, time.Now().Add(-window))
	points := make([]Point, 0, len(samples))
	for _, sample := range samples {
		if sample.PodName != "" && sample.PodName != pod {
			continue
		}
		v := sample.MemoryByte
		if cpu {
			v = sample.CPUMilli
		}
		points = append(points, Point{Time: sample.Timestamp, Value: v})
	}
	if len(points) == 0 {
		return nil, ErrNoData
	}
	return points, nil
}
