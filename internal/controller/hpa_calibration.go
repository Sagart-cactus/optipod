package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/metrics"
)

const hpaCalibrationAnnotation = "optipod.io/hpa-cpu-calibration"

func (wp *WorkloadProcessor) addHPAHistory(ctx context.Context, w *discovery.Workload, container string, p *api.OptimizationPolicy, r *coexist.Restrictions, m *metrics.ContainerMetrics) error {
	if !p.ControlsResource("cpu") || r == nil || w.Object == nil {
		return nil
	}
	enabled := false
	for _, name := range strings.Split(w.Object.GetAnnotations()[hpaCalibrationAnnotation], ",") {
		if strings.TrimSpace(name) == container {
			enabled = true
			break
		}
	}
	if !enabled {
		return nil
	}
	if len(r.HPAs) != 1 {
		return fmt.Errorf("HPA CPU calibration requires exactly one targeting HPA (found %d)", len(r.HPAs))
	}
	provider, ok := wp.metricsProvider.(metrics.HPAHistoryProvider)
	if !ok {
		return fmt.Errorf("HPA CPU calibration requires Prometheus HPA replica history")
	}
	hpa := r.HPAs[0]
	expected, err := discovery.GVR(w.Kind)
	if err != nil {
		return err
	}
	actual, err := schema.ParseGroupVersion(hpa.Spec.ScaleTargetRef.APIVersion)
	if err != nil || expected.Group != actual.Group {
		return fmt.Errorf("HPA CPU calibration: %s targets a different API group", hpa.Name)
	}
	low := int32(1)
	if hpa.Spec.MinReplicas != nil {
		low = max(1, *hpa.Spec.MinReplicas)
	}
	budget := 30 * time.Second
	if raw, ok := w.Object.GetAnnotations()["optipod.io/hpa-cpu-delay-budget"]; ok {
		budget, err = time.ParseDuration(raw)
		if err != nil || budget < 0 {
			return fmt.Errorf("invalid HPA CPU delay budget %q: expected a nonnegative duration", raw)
		}
	}
	start := m.CPU.FirstSample
	if start.Before(hpa.CreationTimestamp.Time) {
		start = hpa.CreationTimestamp.Time
	}
	window := p.Spec.MetricsConfig.ResourceSettings(true).RollingWindow.Duration
	h, err := provider.HPAHistory(ctx, w.Namespace, hpa.Name, low, hpa.Spec.MaxReplicas, start, m.CPU.LastSample, window)
	if err != nil {
		return fmt.Errorf("HPA CPU calibration for %s: %w", hpa.Name, err)
	}
	h.DelayBudget = budget
	template, err := discovery.Template(w.Object)
	if err != nil {
		return err
	}
	h.ScaleOutTargetMilli = soleCPUScaleTarget(hpa.Spec.Metrics, container, &template.Spec)
	m.HPAHistory = h
	return nil
}

// A pod-wide target with multiple contributors cannot be attributed to one
// container. An external or secondary metric can independently trigger growth.
func soleCPUScaleTarget(specs []autoscalingv2.MetricSpec, container string, pod *corev1.PodSpec) int64 {
	if len(specs) != 1 {
		return 0
	}
	m := specs[0]
	var target autoscalingv2.MetricTarget
	switch {
	case m.Type == autoscalingv2.ContainerResourceMetricSourceType && m.ContainerResource != nil && m.ContainerResource.Name == corev1.ResourceCPU && m.ContainerResource.Container == container:
		target = m.ContainerResource.Target
	case m.Type == autoscalingv2.ResourceMetricSourceType && m.Resource != nil && m.Resource.Name == corev1.ResourceCPU:
		slots := discovery.ContainerSlots(pod)
		if len(slots) != 1 || slots[0].Container.Name != container {
			return 0
		}
		target = m.Resource.Target
	default:
		return 0
	}
	if target.Type != autoscalingv2.AverageValueMetricType || target.AverageValue == nil {
		return 0
	}
	return target.AverageValue.MilliValue()
}
