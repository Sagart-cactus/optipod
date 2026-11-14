package recommendation

import (
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIndependentResourceSettings(t *testing.T) {
	cpuMargin, memoryMargin := 1.1, 1.5
	p := &api.OptimizationPolicy{Spec: api.OptimizationPolicySpec{
		MetricsConfig: api.MetricsConfig{
			CPU:    &api.ResourceMetricsConfig{Percentile: "P95", SafetyFactor: &cpuMargin, RollingWindow: metav1.Duration{Duration: time.Hour}},
			Memory: &api.ResourceMetricsConfig{Percentile: "P100", SafetyFactor: &memoryMargin, RollingWindow: metav1.Duration{Duration: 7 * 24 * time.Hour}},
		},
		ResourceBounds: api.ResourceBounds{CPU: api.ResourceBound{Min: resource.MustParse("10m"), Max: resource.MustParse("4")}, Memory: api.ResourceBound{Min: resource.MustParse("1Mi"), Max: resource.MustParse("8Gi")}},
	}}
	r, err := NewEngine().ComputeRecommendation(&metrics.ContainerMetrics{
		CPU:    metrics.ResourceMetrics{P95: resource.MustParse("400m"), P90: resource.MustParse("100m")},
		Memory: metrics.ResourceMetrics{P100: resource.MustParse("200Mi"), P90: resource.MustParse("100Mi")},
	}, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.CPU.MilliValue() != 440 || r.Memory.Value() != 300*1024*1024 {
		t.Fatalf("wrong independent settings: %+v", r)
	}
}
