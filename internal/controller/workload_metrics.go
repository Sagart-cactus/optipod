package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

func (wp *WorkloadProcessor) metricPods(ctx context.Context, w *discovery.Workload) ([]observedPod, error) {
	obj := w.Object.DeepCopyObject().(client.Object)
	if err := wp.client.Get(ctx, client.ObjectKeyFromObject(w.Object), obj); err != nil {
		return nil, err
	}
	if obj.GetUID() != w.Object.GetUID() {
		return nil, fmt.Errorf("workload identity changed")
	}
	var history []observedPod
	old := obj.GetAnnotations()[metricsPodsAnnotation]
	if old != "" {
		if err := json.Unmarshal([]byte(old), &history); err != nil {
			return nil, fmt.Errorf("invalid pod history: %w", err)
		}
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
		owner, err := discovery.ResolvePodWorkload(ctx, wp.client, pod)
		if err != nil || owner.Object.GetUID() != w.Object.GetUID() {
			continue
		}
		p := observePod(pod, byUID[pod.UID])
		byUID[p.UID] = p
	}
	history = nil
	for _, p := range byUID {
		if !p.Active && p.Ended == nil {
			t := now
			p.Ended = &t
		}
		if p.Ended != nil && p.Ended.Before(now.Add(-30*24*time.Hour)) {
			continue
		}
		history = append(history, p)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("no owned pods observed")
	}
	// Bound metadata; refuse rather than silently losing historical replicas.
	if len(history) > 512 {
		return nil, fmt.Errorf("pod history exceeds 512 entries; archive or shorten workload lifetime before optimizing")
	}
	sort.Slice(history, func(i, j int) bool { return history[i].UID < history[j].UID })
	data, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	if string(data) != old {
		patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": obj.GetUID(), "annotations": map[string]string{metricsPodsAnnotation: string(data)}}})
		if err := wp.client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch)); err != nil {
			return nil, fmt.Errorf("persist pod history: %w", err)
		}
	}
	return history, nil
}

func (wp *WorkloadProcessor) collectWorkloadMetrics(ctx context.Context, w *discovery.Workload, container string, p *api.OptimizationPolicy) (*metrics.ContainerMetrics, error) {
	series, rich := wp.metricsProvider.(metrics.SeriesProvider)
	if !rich || wp.client == nil {
		return wp.collectLegacyMetrics(ctx, w, container, p)
	}
	series, err := metrics.ConfigureSeries(series, p.Spec.MetricsConfig)
	if err != nil {
		return nil, err
	}
	pods, err := wp.metricPods(ctx, w)
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
	window := p.Spec.MetricsConfig.ResourceSettings(cpu).RollingWindow.Duration
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
		if observation.newestSource.After(newestSource) {
			newestSource = observation.newestSource
		}
		if !observation.validity.ValidUntil.IsZero() {
			capMetricsValidity(validity, observation.validity.ValidUntil)
		}
	}

	stats := metrics.Statistics(points, cpu)
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
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("warming up or insufficient samples: have %d need %d", stats.Samples, minimum)
	}
	if newestSource.IsZero() || time.Since(newestSource) > historyMaxAge {
		return metrics.ResourceMetrics{}, time.Time{}, fmt.Errorf("stale metrics: newest observation exceeds %s", maxAge)
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

func collectReplicaMetrics(ctx context.Context, series metrics.SeriesProvider, w *discovery.Workload, container string, p *api.OptimizationPolicy, pod observedPod, cpu bool, scope metricWindow) (*replicaObservations, error) {
	result := &replicaObservations{}
	if discovery.IsBatch(w.Kind) && pod.Ended != nil && time.Since(*pod.Ended) > scope.batchAge {
		return nil, nil
	}
	if len(pod.Containers) > 0 {
		present := false
		for _, name := range pod.Containers {
			if name == container {
				present = true
				break
			}
		}
		if !present {
			return nil, nil
		}
	}
	start := pod.Created
	if t, ok := pod.Started[container]; ok {
		start = t
	}
	start = start.Add(p.Spec.MetricsConfig.StartupIgnore.Duration)
	if start.Before(scope.cutoff) {
		start = scope.cutoff
	}
	end := scope.now
	if pod.Ended != nil && pod.Ended.Before(end) {
		end = *pod.Ended
	}
	if !start.Before(end) {
		return nil, nil
	}
	result.expected += int(math.Ceil(end.Sub(start).Seconds() / scope.step.Seconds()))
	observations, err := series.GetResourceSamples(ctx, w.Namespace, pod.Name, container, scope.window, cpu)
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
	if err := checkObservationClock(ctx, series, result, w.Namespace, pod, container, end, cpu, scope); err != nil {
		return nil, err
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
	result.covered += len(buckets)
	return result, nil
}

func checkObservationClock(ctx context.Context, series metrics.SeriesProvider, result *replicaObservations, namespace string, pod observedPod, container string, end time.Time, cpu bool, scope metricWindow) error {
	if clock, ok := series.(metrics.ObservationClock); ok {
		stamp, err := clock.SourceTimestamp(ctx, namespace, pod.Name, container, end, cpu)
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
