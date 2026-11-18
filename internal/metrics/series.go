package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/api/resource"
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
	result, warnings, err := p.queryRange(ctx, query, v1.Range{Start: end.Add(-window), End: end, Step: p.SampleInterval(window)})
	return matrixPoints(result, warnings, err, cpu)
}

// GetExecutionSamples reads actual scrapes for a short, finished container.
// A range query's evaluation grid can miss the entire execution.
func (p *PrometheusProvider) GetExecutionSamples(ctx context.Context, ns, pod, container string, start, end time.Time, cpu bool, target string) ([]Point, error) {
	metric := workingSetMetric
	if cpu {
		metric = "container_cpu_usage_seconds_total"
	}
	// The left edge of a PromQL range selector is open.
	query := fmt.Sprintf(`%s{namespace=%q,pod=%q,container=%q}[%dms]`, metric, ns, pod, container, end.Sub(start).Milliseconds()+1)
	traces, err := readReplicas(ctx, p.clients, func(ctx context.Context, c v1.API) ([]Point, error) {
		result, warnings, _, err := c.Query(ctx, query, end)
		points, err := executionPoints(result, warnings, err, start, end, cpu)
		if errors.Is(err, ErrNoData) {
			return nil, nil
		}
		return points, err
	})
	if err != nil {
		return nil, err
	}
	if len(traces) == 1 && len(traces[0]) > 0 {
		return traces[0], nil
	}
	// Keep one real execution trace. Joining staggered counters across scrapers
	// can invent rates, and counting duplicate scrapes inflates confidence.
	q, err := strconv.Atoi(strings.TrimPrefix(target, "P"))
	if err != nil || q < 0 || q > 100 {
		return nil, fmt.Errorf("invalid execution percentile %q", target)
	}
	var best []Point
	var bestValue int64
	for _, points := range traces {
		if len(points) == 0 {
			continue
		}
		samples := make([]int64, len(points))
		for i, point := range points {
			samples[i] = point.Value
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		value := percentile(samples, q)
		if len(best) == 0 || value > bestValue || (value == bestValue && len(points) > len(best)) {
			best = points
			bestValue = value
		}
	}
	if len(best) == 0 {
		return nil, ErrNoData
	}
	return best, nil
}

func executionPoints(result model.Value, warnings v1.Warnings, err error, start, end time.Time, cpu bool) ([]Point, error) {
	if err != nil || len(warnings) > 0 {
		return matrixPoints(result, warnings, err, cpu)
	}
	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("unexpected execution result type: %T", result)
	}
	for _, series := range matrix {
		var previous model.SamplePair
		havePrevious := false
		values := make([]model.SamplePair, 0, len(series.Values))
		for _, sample := range series.Values {
			if sample.Timestamp.Time().Before(start) || sample.Timestamp.Time().After(end) {
				continue
			}
			if math.IsNaN(float64(sample.Value)) || math.IsInf(float64(sample.Value), 0) || sample.Value < 0 {
				return nil, fmt.Errorf("invalid execution sample: %v", sample.Value)
			}
			if !cpu {
				values = append(values, sample)
				continue
			}
			// Never join different counters or estimate across a reset.
			if havePrevious && sample.Timestamp > previous.Timestamp && sample.Value >= previous.Value {
				seconds := sample.Timestamp.Sub(previous.Timestamp).Seconds()
				values = append(values, model.SamplePair{Timestamp: sample.Timestamp, Value: (sample.Value - previous.Value) / model.SampleValue(seconds)})
			}
			previous, havePrevious = sample, true
		}
		series.Values = values
	}
	return matrixPoints(matrix, nil, nil, cpu)
}

func matrixPoints(result model.Value, warnings v1.Warnings, err error, cpu bool) ([]Point, error) {
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

// MeanReplicaPercentiles gives every nonempty replica one vote. The pooled
// maximum, sample count, coverage and original points remain valid.
func (r *ResourceMetrics) MeanReplicaPercentiles(replicas [][]Point, cpu bool) {
	if len(replicas) < 2 {
		return
	}
	quantiles := [...]int{50, 80, 90, 95, 99}
	var sums, remainders [len(quantiles)]int64
	count := int64(len(replicas))
	for _, points := range replicas {
		values := make([]int64, len(points))
		for i, point := range points {
			values[i] = point.Value
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for i, q := range quantiles {
			value := percentile(values, q)
			// Split before adding to avoid overflowing a sum of large quantities.
			sums[i] += value / count
			remainders[i] += value % count
		}
	}
	for i, dst := range [...]*resource.Quantity{&r.P50, &r.P80, &r.P90, &r.P95, &r.P99} {
		*dst = sampleQuantity(sums[i]+remainders[i]/count, cpu)
	}
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
