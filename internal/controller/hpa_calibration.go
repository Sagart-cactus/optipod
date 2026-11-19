package controller

import (
	"context"
	"fmt"
	"strings"

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
	start := m.CPU.FirstSample
	if start.Before(hpa.CreationTimestamp.Time) {
		start = hpa.CreationTimestamp.Time
	}
	window := p.Spec.MetricsConfig.ResourceSettings(true).RollingWindow.Duration
	h, err := provider.HPAHistory(ctx, w.Namespace, hpa.Name, low, hpa.Spec.MaxReplicas, start, m.CPU.LastSample, window)
	if err != nil {
		return fmt.Errorf("HPA CPU calibration for %s: %w", hpa.Name, err)
	}
	m.HPAHistory = h
	return nil
}
