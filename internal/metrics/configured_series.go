package metrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type configuredSeries struct {
	base   *PrometheusProvider
	config api.MetricsConfig
}

func ConfigureSeries(base SeriesProvider, cfg api.MetricsConfig) (SeriesProvider, error) {
	hasSource := cfg.JVM != nil || (cfg.CPU != nil && cfg.CPU.Source != nil) || (cfg.Memory != nil && cfg.Memory.Source != nil)
	if !hasSource {
		return base, nil
	}
	prom, ok := base.(*PrometheusProvider)
	if !ok {
		return nil, fmt.Errorf("configured metric source requires Prometheus")
	}
	return &configuredSeries{base: prom, config: cfg}, nil
}
func (s *configuredSeries) SampleInterval(window time.Duration) time.Duration {
	return s.base.SampleInterval(window)
}
func (s *configuredSeries) source(cpu bool) *api.MetricSource {
	cfg := s.config.Memory
	if cpu {
		cfg = s.config.CPU
	}
	if cfg == nil {
		return nil
	}
	return cfg.Source
}
func metricSelector(src *api.MetricSource, ns, pod, container string) (string, error) {
	if err := api.ValidateMetricSource(src); err != nil {
		return "", err
	}
	labels := []string{fmt.Sprintf("namespace=%q", ns), fmt.Sprintf("pod=%q", pod), fmt.Sprintf("container=%q", container)}
	keys := []string{}
	for key := range src.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		labels = append(labels, fmt.Sprintf("%s=%q", key, src.Labels[key]))
	}
	return src.Name + "{" + strings.Join(labels, ",") + "}", nil
}
func (s *configuredSeries) GetResourceSamples(ctx context.Context, ns, pod, container string, window time.Duration, cpu bool) ([]Point, error) {
	end := time.Now()
	if !cpu && s.config.JVM != nil {
		j := s.config.JVM
		series := [][]Point{}
		for _, src := range []*api.MetricSource{&j.HeapUsed, &j.HeapMax, &j.NonHeapUsed} {
			points, err := s.readGauge(ctx, src, ns, pod, container, window, end, false)
			if err != nil {
				return nil, err
			}
			series = append(series, points)
		}
		rss, err := s.readGauge(ctx, &api.MetricSource{Name: workingSetMetric}, ns, pod, container, window, end, false)
		if err != nil {
			return nil, err
		}
		series = append(series, rss)
		maps := []map[int64]int64{}
		for _, points := range series {
			m := map[int64]int64{}
			for _, p := range points {
				m[p.Time.UnixNano()] = p.Value
			}
			maps = append(maps, m)
		}
		target := .7
		if j.TargetHeapUtilization != nil {
			target = *j.TargetHeapUtilization
		}
		result := []Point{}
		for _, point := range series[0] {
			stamp := point.Time.UnixNano()
			maximum, ok := maps[1][stamp]
			nonheap, ok2 := maps[2][stamp]
			working, ok3 := maps[3][stamp]
			if !ok || !ok2 || !ok3 {
				continue
			}
			if maximum <= 0 {
				return nil, fmt.Errorf("JVM maximum heap must be positive")
			}
			bytes := math.Max(float64(point.Value)/target, float64(maximum)) + float64(nonheap) + float64(j.NativeReserve.Value())
			bytes = math.Max(bytes, float64(working))
			if math.IsNaN(bytes) || math.IsInf(bytes, 0) || bytes >= float64(math.MaxInt64) {
				return nil, fmt.Errorf("invalid JVM memory calculation")
			}
			result = append(result, Point{Time: point.Time, Value: int64(math.Ceil(bytes))})
		}
		if len(result) == 0 {
			return nil, ErrNoData
		}
		return result, nil
	}
	if src := s.source(cpu); src != nil {
		return s.readGauge(ctx, src, ns, pod, container, window, end, cpu)
	}
	return s.base.GetResourceSamples(ctx, ns, pod, container, window, cpu)
}
func (s *configuredSeries) readGauge(ctx context.Context, src *api.MetricSource, ns, pod, container string, window time.Duration, end time.Time, cpu bool) ([]Point, error) {
	query, err := metricSelector(src, ns, pod, container)
	if err != nil {
		return nil, err
	}
	value, warnings, _, err := s.base.client.QueryRange(ctx, query, v1.Range{Start: end.Add(-window), End: end, Step: s.SampleInterval(window)})
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		return nil, fmt.Errorf("incomplete custom metric result: %v", warnings)
	}
	matrix, ok := value.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("custom metric result must be a matrix")
	}
	multiplier := 1.0
	if src.Multiplier != nil {
		multiplier = *src.Multiplier
	}
	if cpu {
		multiplier *= 1000
	}
	values := map[model.Time]int64{}
	for _, series := range matrix {
		for _, sample := range series.Values {
			v := float64(sample.Value) * multiplier
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= float64(math.MaxInt64) {
				return nil, fmt.Errorf("invalid custom metric value")
			}
			n := int64(math.Ceil(v))
			if old, ok := values[sample.Timestamp]; !ok || n > old {
				values[sample.Timestamp] = n
			}
		}
	}
	if len(values) == 0 {
		return nil, ErrNoData
	}
	points := []Point{}
	for stamp, value := range values {
		points = append(points, Point{Time: stamp.Time(), Value: value})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
	return points, nil
}
func (s *configuredSeries) SourceTimestamp(ctx context.Context, ns, pod, container string, at time.Time, cpu bool) (time.Time, error) {
	sources := []*api.MetricSource{}
	if !cpu && s.config.JVM != nil {
		j := s.config.JVM
		sources = []*api.MetricSource{&j.HeapUsed, &j.HeapMax, &j.NonHeapUsed, {Name: workingSetMetric}}
	} else if src := s.source(cpu); src != nil {
		sources = append(sources, src)
	} else {
		return s.base.SourceTimestamp(ctx, ns, pod, container, at, cpu)
	}
	var oldest time.Time
	for _, src := range sources {
		selector, err := metricSelector(src, ns, pod, container)
		if err != nil {
			return time.Time{}, err
		}
		value, warnings, _, err := s.base.client.QueryRange(ctx, "timestamp("+selector+")", v1.Range{Start: at, End: at, Step: time.Second})
		if err != nil {
			return time.Time{}, err
		}
		if len(warnings) > 0 {
			return time.Time{}, fmt.Errorf("incomplete custom source timestamp")
		}
		matrix, ok := value.(model.Matrix)
		if !ok || len(matrix) == 0 {
			return time.Time{}, ErrNoData
		}
		found := false
		for _, series := range matrix {
			for _, point := range series.Values {
				v := float64(point.Value)
				if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > float64(at.Unix()+1) {
					return time.Time{}, fmt.Errorf("invalid custom source timestamp")
				}
				stamp := time.Unix(0, int64(v*1e9))
				if oldest.IsZero() || stamp.Before(oldest) {
					oldest = stamp
				}
				found = true
			}
		}
		if !found {
			return time.Time{}, ErrNoData
		}
	}
	return oldest, nil
}
