package recommendation

import (
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPredictiveBoundsAndWarming(t *testing.T) {
	n, cap := int32(3), int32(25)
	factor := 1.0
	p := &api.OptimizationPolicy{Spec: api.OptimizationPolicySpec{MetricsConfig: api.MetricsConfig{SafetyFactor: &factor, PredictiveCPU: &api.PredictiveCPUConfig{Horizon: metav1.Duration{Duration: 30 * time.Second}, TrainingWindow: metav1.Duration{Duration: time.Minute}, MinimumHistory: metav1.Duration{Duration: 10 * time.Second}, MinSamples: &n, MaxIncreasePercent: &cap}}, ResourceBounds: api.ResourceBounds{CPU: api.ResourceBound{Min: resource.MustParse("50m"), Max: resource.MustParse("1")}}}}
	now := time.Now()
	points := []metrics.Point{{Time: now.Add(-20 * time.Second), Value: 100}, {Time: now.Add(-10 * time.Second), Value: 200}, {Time: now, Value: 300}}
	base := resource.MustParse("300m")
	q, _ := predictiveCPU(points, base, p)
	if q.MilliValue() != 375 {
		t.Fatalf("unbounded forecast: %s", q.String())
	}
	q, _ = predictiveCPU(points[:1], base, p)
	if q.Cmp(base) != 0 {
		t.Fatal("short history changed baseline")
	}
	points[0].Value = 500
	q, _ = predictiveCPU(points, base, p)
	if q.Cmp(base) != 0 {
		t.Fatal("declining trend reduced baseline")
	}
	points[0].Value = 100
	p.Spec.ResourceBounds.CPU.Max = resource.MustParse("350m")
	q, _ = predictiveCPU(points, base, p)
	if q.MilliValue() != 350 {
		t.Fatal("forecast exceeds resource bound")
	}
}
