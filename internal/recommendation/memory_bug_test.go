package recommendation

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// TestMemoryRecommendationBug reproduces the fluentd memory bug
func TestMemoryRecommendationBug(t *testing.T) {
	engine := NewEngine()

	// Simulate the actual metrics from fluentd
	// P90 memory: 73847603 bytes (~70.4 Mi)
	containerMetrics := &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P90:     *resource.NewMilliQuantity(2, resource.DecimalSI), // 2m
			Samples: 3,
		},
		Memory: metrics.ResourceMetrics{
			P90:     *resource.NewQuantity(73847603, resource.BinarySI), // ~70.4 Mi
			Samples: 3,
		},
	}

	policy := &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Percentile: "P90",
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: *resource.NewMilliQuantity(50, resource.DecimalSI), // 50m
					Max: *resource.NewQuantity(2, resource.DecimalSI),       // 2 cores
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: *resource.NewQuantity(67108864, resource.BinarySI),   // 64Mi
					Max: *resource.NewQuantity(2147483648, resource.BinarySI), // 2Gi
				},
			},
		},
	}

	rec, err := engine.ComputeRecommendation(containerMetrics, policy)
	if err != nil {
		t.Fatalf("ComputeRecommendation failed: %v", err)
	}

	t.Logf("CPU recommendation: %s (value=%d, millivalue=%d, format=%v)",
		rec.CPU.String(), rec.CPU.Value(), rec.CPU.MilliValue(), rec.CPU.Format)
	t.Logf("Memory recommendation: %s (value=%d, millivalue=%d, format=%v)",
		rec.Memory.String(), rec.Memory.Value(), rec.Memory.MilliValue(), rec.Memory.Format)

	// Test JSON serialization
	type TestRec struct {
		CPU    *resource.Quantity `json:"cpu,omitempty"`
		Memory *resource.Quantity `json:"memory,omitempty"`
	}

	testRec := TestRec{
		CPU:    &rec.CPU,
		Memory: &rec.Memory,
	}

	jsonBytes, err := json.Marshal(testRec)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	t.Logf("JSON: %s", string(jsonBytes))

	// Expected: memory should be around 85Mi (73847603 * 1.2 = 88617123.6 bytes)
	// The JSON should show something like "85Mi" or "86Mi", NOT "88617123600m"

	memStr := rec.Memory.String()
	if len(memStr) > 0 && memStr[len(memStr)-1] == 'm' {
		t.Errorf("Memory recommendation has 'm' suffix (millicores), should have 'Mi' or 'Gi': %s", memStr)
	}

	// Check that memory value is reasonable (should be ~88MB in bytes)
	memValue := rec.Memory.Value()
	if memValue < 80000000 || memValue > 100000000 {
		t.Errorf("Memory value out of expected range: %d bytes (expected ~88MB)", memValue)
	}
}
