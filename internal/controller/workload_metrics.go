package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const metricsPodsAnnotation = "optipod.io/metrics-pods"

// Stored on the owning workload, so rolling revisions and operator restarts
// preserve the exact pod names whose historical Prometheus series belong here.
// Owner UID checks exclude unrelated pods with overlapping selectors.
type observedPod struct {
	Containers   []string             `json:"containers,omitempty"`
	ExecutionUID types.UID            `json:"executionUID,omitempty"`
	Succeeded    bool                 `json:"succeeded,omitempty"`
	Name         string               `json:"name"`
	UID          types.UID            `json:"uid"`
	Created      time.Time            `json:"created"`
	Started      map[string]time.Time `json:"started,omitempty"`
	Ended        *time.Time           `json:"ended,omitempty"`
	Active       bool                 `json:"-"`
}

func (wp *WorkloadProcessor) metricPods(ctx context.Context, w *discovery.Workload, policy *api.OptimizationPolicy) ([]observedPod, error) {
	var pods []observedPod
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool { return apierrors.IsConflict(err) || apierrors.IsInvalid(err) }, func() error {
		var err error
		pods, err = wp.observeMetricPods(ctx, w, policy)
		return err
	})
	return pods, err
}

func (wp *WorkloadProcessor) observeMetricPods(ctx context.Context, w *discovery.Workload, policy *api.OptimizationPolicy) ([]observedPod, error) {
	obj := w.Object.DeepCopyObject().(client.Object)
	if err := wp.client.Get(ctx, client.ObjectKeyFromObject(w.Object), obj); err != nil {
		return nil, err
	}
	if obj.GetUID() != w.Object.GetUID() {
		return nil, fmt.Errorf("workload identity changed")
	}
	// Historical observations from Recommend cannot authorize automatic sizing
	// beneath a parent controller whose resource ownership is external.
	if policy.Spec.Mode == api.ModeAuto && metav1.GetControllerOf(obj) != nil {
		return nil, fmt.Errorf("automatic pod metrics require a top-level workload")
	}
	old := obj.GetAnnotations()[metricsPodsAnnotation]
	history, err := decodePodJournal(old)
	if err != nil {
		return nil, fmt.Errorf("invalid pod history: %w", err)
	}
	byUID := map[types.UID]observedPod{}
	for _, p := range history {
		byUID[p.UID] = p
	}
	pods := &corev1.PodList{}
	if err := wp.client.List(ctx, pods, client.InNamespace(w.Namespace)); err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !wp.observesPod(ctx, w, policy, pod) {
			continue
		}
		p := observePod(pod, byUID[pod.UID])
		byUID[p.UID] = p
	}
	retention := 30 * 24 * time.Hour
	if discovery.IsBatch(w.Kind) {
		retention, _ = policy.BatchSettings()
	}
	history = nil
	for _, p := range byUID {
		if !p.Active && p.Ended == nil {
			t := now
			p.Ended = &t
		}
		if p.Ended != nil && p.Ended.Before(now.Add(-retention)) {
			continue
		}
		history = append(history, p)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("no owned pods observed")
	}
	sort.Slice(history, func(i, j int) bool { return history[i].UID < history[j].UID })
	data, err := encodePodJournal(history, obj.GetAnnotations())
	if err != nil {
		return nil, err
	}
	if data != old {
		if err := wp.client.Patch(ctx, obj, podHistoryPatch(obj, data)); err != nil {
			return nil, fmt.Errorf("persist pod history: %w", err)
		}
	}
	return history, nil
}

func (wp *WorkloadProcessor) collectWorkloadMetrics(ctx context.Context, w *discovery.Workload, container string, p *api.OptimizationPolicy) (*metrics.ContainerMetrics, error) {
	series, rich := wp.metricsProvider.(metrics.SeriesProvider)
	if !rich || wp.client == nil {
		for _, cpu := range []bool{true, false} {
			if p.Spec.MetricsConfig.ResourceSettings(cpu).ReplicaAggregation == api.ReplicaAggregationMeanPercentile {
				return nil, fmt.Errorf("mean replica percentiles require per-pod metric series")
			}
		}
		return wp.collectLegacyMetrics(ctx, w, container, p)
	}
	series, err := metrics.ConfigureSeries(series, p.Spec.MetricsConfig)
	if err != nil {
		return nil, err
	}
	pods, err := wp.metricPods(ctx, w, p)
	if err != nil {
		return nil, err
	}
	batchAge, minExecutions := p.BatchSettings()
	if discovery.IsBatch(w.Kind) {
		executions := map[types.UID]bool{}
		for _, pod := range pods {
			if pod.Succeeded && pod.Ended != nil && time.Since(*pod.Ended) <= batchAge {
				executions[pod.ExecutionUID] = true
			}
		}
		if len(executions) < minExecutions {
			return nil, fmt.Errorf("insufficient completed executions: have %d need %d", len(executions), minExecutions)
		}
	}
	if registrar, ok := wp.metricsProvider.(metrics.SamplingTargetRegistrar); ok {
		for _, pod := range pods {
			if pod.Active && pod.Ended == nil {
				registrar.RegisterTarget(metrics.TargetKey{Namespace: w.Namespace, WorkloadKind: w.Kind, WorkloadName: w.Name, Container: container, PodName: pod.Name}, pod.Name)
			}
		}
	}
	result := &metrics.ContainerMetrics{}
	for _, cpu := range []bool{true, false} {
		if (cpu && !p.ControlsResource("cpu")) || (!cpu && !p.ControlsResource("memory")) {
			continue
		}
		stats, validUntil, err := collectResourceMetrics(ctx, series, w, container, p, pods, cpu, batchAge)
		if err != nil {
			return nil, err
		}
		capMetricsValidity(result, validUntil)

		if cpu {
			result.CPU = stats
		} else {
			result.Memory = stats
		}
	}
	return result, nil
}

func capMetricsValidity(result *metrics.ContainerMetrics, until time.Time) {
	if result.ValidUntil.IsZero() || until.Before(result.ValidUntil) {
		result.ValidUntil = until
	}
}

func observePod(pod *corev1.Pod, previous observedPod) observedPod {
	p := observedPod{Name: pod.Name, UID: pod.UID, Created: pod.CreationTimestamp.Time, Active: true, Started: map[string]time.Time{}, Succeeded: pod.Status.Phase == corev1.PodSucceeded}
	if owner := metav1.GetControllerOf(pod); owner != nil && owner.Kind == "Job" {
		p.ExecutionUID = owner.UID
	}
	for _, slot := range discovery.ContainerSlots(&pod.Spec) {
		p.Containers = append(p.Containers, slot.Container.Name)
	}
	if previous.UID != "" {
		p.Started = previous.Started
		if p.Started == nil {
			p.Started = map[string]time.Time{}
		}
	}
	for _, s := range append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...) {
		if s.State.Running != nil {
			p.Started[s.Name] = s.State.Running.StartedAt.Time
		}
		if s.State.Terminated != nil {
			p.Started[s.Name] = s.State.Terminated.StartedAt.Time
			t := s.State.Terminated.FinishedAt.Time
			if (pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) && (p.Ended == nil || t.After(*p.Ended)) {
				p.Ended = &t
			}
		}
	}
	if pod.DeletionTimestamp != nil {
		t := pod.DeletionTimestamp.Time
		p.Ended = &t
	}
	return p
}

func (wp *WorkloadProcessor) collectLegacyMetrics(ctx context.Context, w *discovery.Workload, container string, p *api.OptimizationPolicy) (*metrics.ContainerMetrics, error) {
	pod, err := wp.getFirstPodName(w)
	if err != nil {
		return nil, err
	}
	cpuWindow := p.Spec.MetricsConfig.ResourceSettings(true).RollingWindow.Duration
	memWindow := p.Spec.MetricsConfig.ResourceSettings(false).RollingWindow.Duration
	result, err := wp.metricsProvider.GetContainerMetrics(ctx, w.Namespace, pod, container, cpuWindow)
	if err == nil && cpuWindow != memWindow {
		var m *metrics.ContainerMetrics
		m, err = wp.metricsProvider.GetContainerMetrics(ctx, w.Namespace, pod, container, memWindow)
		if err == nil {
			result.Memory = m.Memory
		}
	}
	return result, err
}

func collectResourceMetrics(ctx context.Context, series metrics.SeriesProvider, w *discovery.Workload, container string, p *api.OptimizationPolicy, pods []observedPod, cpu bool, batchAge time.Duration) (metrics.ResourceMetrics, time.Time, error) {
	validity := &metrics.ContainerMetrics{}
	settings := p.Spec.MetricsConfig.ResourceSettings(cpu)
	window := settings.RollingWindow.Duration
	step := series.SampleInterval(window)
	now := time.Now()
	cutoff := now.Add(-window)
	maxAge := p.Spec.MetricsConfig.MaxSampleAge.Duration
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}
	historyMaxAge := maxAge
	if discovery.IsBatch(w.Kind) {
		historyMaxAge = batchAge
	}
	points := []metrics.Point{}
	var replicas [][]metrics.Point
	var newestSource time.Time
	expected, covered := 0, 0
	for _, pod := range pods {
		observation, err := collectReplicaMetrics(ctx, series, w, container, p, pod, cpu,
			metricWindow{window: window, step: step, now: now, cutoff: cutoff, maxAge: maxAge, batchAge: batchAge})
		if err != nil {
			return metrics.ResourceMetrics{}, time.Time{}, err
		}
		if observation == nil {
			continue
		}
		expected += observation.expected
		covered += observation.covered
		points = append(points, observation.points...)
		if settings.ReplicaAggregation == api.ReplicaAggregationMeanPercentile && len(observation.points) > 0 {
			replicas = append(replicas, observation.points)
		}
		if observation.newestSource.After(newestSource) {
			newestSource = observation.newestSource
		}
		if !observation.validity.ValidUntil.IsZero() {
			capMetricsValidity(validity, observation.validity.ValidUntil)
		}
	}

	stats := metrics.Statistics(points, cpu)
	stats.MeanReplicaPercentiles(replicas, cpu)
	if _, ok := series.(metrics.ObservationClock); !ok {
		newestSource = stats.LastSample
	}
	if expected > 0 {
		stats.CoveragePercent = math.Min(100, 100*float64(covered)/float64(expected))
	}
	minimum := 1
	if p.Spec.MetricsConfig.MinSamples != nil {
		minimum = int(*p.Spec.MetricsConfig.MinSamples)
	}
	if stats.Samples < minimum {
		if discovery.IsBatch(w.Kind) {
			resource := "memory"
			if cpu {
				resource = "CPU"
			}
			return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("insufficient samples for %s in eligible executions: have %d need %d", resource, stats.Samples, minimum)
		}
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("warming up or insufficient samples: have %d need %d", stats.Samples, minimum)
	}
	if newestSource.IsZero() || time.Since(newestSource) > historyMaxAge {
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("stale metrics: newest observation exceeds %s", historyMaxAge)
	}
	capMetricsValidity(validity, newestSource.Add(historyMaxAge))
	if stats.LastSample.Sub(stats.FirstSample) < p.Spec.MetricsConfig.MinimumHistory.Duration {
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("insufficient history: need %s", p.Spec.MetricsConfig.MinimumHistory.Duration)
	}
	if stats.CoveragePercent < float64(p.Spec.MetricsConfig.MinCoveragePercent) {
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("insufficient coverage: %.1f%% need %d%%", stats.CoveragePercent, p.Spec.MetricsConfig.MinCoveragePercent)
	}
	return stats, validity.ValidUntil, nil
}

type metricWindow struct {
	window, step, maxAge, batchAge time.Duration
	now, cutoff                    time.Time
}
type replicaObservations struct {
	points            []metrics.Point
	newestSource      time.Time
	expected, covered int
	validity          metrics.ContainerMetrics
}

func (pod observedPod) observationBounds(container string, startupIgnore time.Duration, scope metricWindow) (time.Time, time.Time) {
	start := pod.Created
	if t, ok := pod.Started[container]; ok {
		start = t
	}
	start = start.Add(startupIgnore)
	if start.Before(scope.cutoff) {
		start = scope.cutoff
	}
	end := scope.now
	if pod.Ended != nil && pod.Ended.Before(end) {
		end = *pod.Ended
	}
	return start, end
}

// Short batch executions use raw scrapes to retain their source clock. Continuous
// P100 memory uses bucket maxima and the provider's normal freshness checks.
func replicaSamples(ctx context.Context, series metrics.SeriesProvider, w *discovery.Workload, pod observedPod, container string, cpu bool, percentile string, start, end time.Time, scope metricWindow) ([]metrics.Point, bool, error) {
	raw, supportsRaw := series.(interface {
		GetExecutionSamples(context.Context, string, string, string, time.Time, time.Time, bool, string) ([]metrics.Point, error)
	})
	if supportsRaw && discovery.IsBatch(w.Kind) && pod.Ended != nil && end.Sub(start) <= 2*scope.step {
		points, err := raw.GetExecutionSamples(ctx, w.Namespace, pod.Name, container, start, end, cpu, percentile)
		return points, true, err
	}
	if peaks, ok := series.(metrics.MemoryPeakProvider); ok && !cpu && percentile == "P100" {
		points, err := peaks.MemoryPeakSamples(ctx, w.Namespace, pod.Name, container, start, end, scope.window)
		return points, false, err
	}
	points, err := series.GetResourceSamples(ctx, w.Namespace, pod.Name, container, scope.window, cpu)
	return points, false, err
}

func collectReplicaMetrics(ctx context.Context, series metrics.SeriesProvider, w *discovery.Workload, container string, p *api.OptimizationPolicy, pod observedPod, cpu bool, scope metricWindow) (*replicaObservations, error) {
	result := &replicaObservations{}
	if discovery.IsBatch(w.Kind) && pod.Ended != nil && time.Since(*pod.Ended) > scope.batchAge {
		return nil, nil
	}
	if len(pod.Containers) > 0 && !slices.Contains(pod.Containers, container) {
		return nil, nil
	}
	start, end := pod.observationBounds(container, p.Spec.MetricsConfig.StartupIgnore.Duration, scope)
	if !start.Before(end) {
		return nil, nil
	}
	result.expected += int(math.Ceil(end.Sub(start).Seconds() / scope.step.Seconds()))
	observations, useRaw, err := replicaSamples(ctx, series, w, pod, container, cpu,
		p.Spec.MetricsConfig.ResourceSettings(cpu).Percentile, start, end, scope)
	if err != nil {
		if errors.Is(err, metrics.ErrNoData) {
			if pod.Active && pod.Ended == nil && scope.now.Sub(start) > scope.maxAge {
				return nil, fmt.Errorf("missing metrics for active replica %s", pod.Name)
			}
			return result, nil
		}
		return nil, err
	}
	buckets := map[int64]bool{}
	if !useRaw {
		if err := checkObservationClock(ctx, series, result, w, pod, container, start, end, cpu, scope); err != nil {
			return nil, err
		}
	}
	var podNewest time.Time
	for _, point := range observations {
		if point.Time.Before(start) || (pod.Ended != nil && point.Time.After(end)) || point.Time.After(time.Now()) {
			continue
		}
		result.points = append(result.points, point)
		if point.Time.After(podNewest) {
			podNewest = point.Time
		}
		buckets[int64(point.Time.Sub(start)/scope.step)] = true
	}
	if _, ok := series.(metrics.ObservationClock); !ok && pod.Active && pod.Ended == nil {
		if podNewest.IsZero() || scope.now.Sub(podNewest) > scope.maxAge {
			return nil, fmt.Errorf("stale metrics for replica %s", pod.Name)
		}
		capMetricsValidity(&result.validity, podNewest.Add(scope.maxAge))
	}
	if useRaw {
		result.newestSource = podNewest
	}
	result.covered += len(buckets)
	return result, nil
}

func checkObservationClock(ctx context.Context, series metrics.SeriesProvider, result *replicaObservations, w *discovery.Workload, pod observedPod, container string, start, end time.Time, cpu bool, scope metricWindow) error {
	if clock, ok := series.(interface {
		ExecutionSourceTimestamp(context.Context, string, string, string, time.Time, time.Time, bool) (time.Time, error)
	}); ok && pod.Ended != nil && discovery.IsBatch(w.Kind) {
		stamp, err := clock.ExecutionSourceTimestamp(ctx, w.Namespace, pod.Name, container, start, end, cpu)
		if errors.Is(err, metrics.ErrNoData) {
			return nil
		}
		result.newestSource = stamp
		return err
	}

	if clock, ok := series.(metrics.ObservationClock); ok {
		stamp, err := clock.SourceTimestamp(ctx, w.Namespace, pod.Name, container, end, cpu)
		if err != nil && !errors.Is(err, metrics.ErrNoData) {
			return err
		}
		if err == nil && stamp.After(result.newestSource) {
			result.newestSource = stamp
		}
		if pod.Active && pod.Ended == nil && (err != nil || scope.now.Sub(stamp) > scope.maxAge) {
			return fmt.Errorf("stale metrics for replica %s: source scrape exceeds %s", pod.Name, scope.maxAge)
		}
		if err == nil && pod.Active && pod.Ended == nil {
			capMetricsValidity(&result.validity, stamp.Add(scope.maxAge))
		}
	}
	return nil
}
