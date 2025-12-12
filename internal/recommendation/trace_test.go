package recommendation

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// TestTraceMemoryValues traces the memory value through each step
func TestTraceMemoryValues(t *testing.T) {
	t.Log("=== STEP 1: Parse metrics-server values ===")

	// Metrics-server returns: "73964Ki"
	memFromMetricsServer := resource.MustParse("73964Ki")
	t.Logf("Memory from metrics-server: %s", memFromMetricsServer.String())
	t.Logf("  Value (bytes): %d", memFromMetricsServer.Value())
	t.Logf("  MilliValue: %d", memFromMetricsServer.MilliValue())
	t.Logf("  Format: %v", memFromMetricsServer.Format)

	t.Log("\n=== STEP 2: Convert to int64 bytes (what our code does) ===")

	// Our code calls .Value() to get bytes
	memBytes := memFromMetricsServer.Value()
	t.Logf("Memory as int64 bytes: %d", memBytes)

	t.Log("\n=== STEP 3: Create resource.Quantity with BinarySI ===")

	// Our computePercentiles creates a new quantity
	memQuantity := resource.NewQuantity(memBytes, resource.BinarySI)
	t.Logf("Memory quantity: %s", memQuantity.String())
	t.Logf("  Value (bytes): %d", memQuantity.Value())
	t.Logf("  MilliValue: %d", memQuantity.MilliValue())
	t.Logf("  Format: %v", memQuantity.Format)

	t.Log("\n=== STEP 4: Multiply by safety factor (1.2) ===")

	value := memQuantity.Value()
	result := int64(float64(value) * 1.2)
	t.Logf("After multiplication: %d bytes", result)

	memWithSafety := resource.NewQuantity(result, memQuantity.Format)
	t.Logf("Memory with safety: %s", memWithSafety.String())
	t.Logf("  Value (bytes): %d", memWithSafety.Value())
	t.Logf("  MilliValue: %d", memWithSafety.MilliValue())
	t.Logf("  Format: %v", memWithSafety.Format)

	t.Log("\n=== STEP 5: Clamp to bounds ===")

	minMem := resource.MustParse("64Mi")
	maxMem := resource.MustParse("2Gi")

	t.Logf("Min bound: %s (value=%d)", minMem.String(), minMem.Value())
	t.Logf("Max bound: %s (value=%d)", maxMem.String(), maxMem.Value())

	var clamped resource.Quantity
	if memWithSafety.Cmp(minMem) < 0 {
		clamped = minMem.DeepCopy()
		t.Log("Clamped to MIN")
	} else if memWithSafety.Cmp(maxMem) > 0 {
		clamped = maxMem.DeepCopy()
		t.Log("Clamped to MAX")
	} else {
		clamped = memWithSafety.DeepCopy()
		t.Log("No clamping needed")
	}

	t.Logf("Clamped memory: %s", clamped.String())
	t.Logf("  Value (bytes): %d", clamped.Value())
	t.Logf("  MilliValue: %d", clamped.MilliValue())
	t.Logf("  Format: %v", clamped.Format)

	t.Log("\n=== STEP 6: JSON serialization ===")

	type TestRec struct {
		Memory *resource.Quantity `json:"memory,omitempty"`
	}

	testRec := TestRec{
		Memory: &clamped,
	}

	jsonBytes, _ := json.Marshal(testRec)
	t.Logf("JSON: %s", string(jsonBytes))

	t.Log("\n=== STEP 7: Full engine test ===")

	engine := NewEngine()

	containerMetrics := &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P90:     *resource.NewMilliQuantity(2, resource.DecimalSI),
			Samples: 3,
		},
		Memory: metrics.ResourceMetrics{
			P90:     *memQuantity,
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
					Min: *resource.NewMilliQuantity(50, resource.DecimalSI),
					Max: *resource.NewQuantity(2, resource.DecimalSI),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: minMem,
					Max: maxMem,
				},
			},
		},
	}

	rec, err := engine.ComputeRecommendation(containerMetrics, policy)
	if err != nil {
		t.Fatalf("ComputeRecommendation failed: %v", err)
	}

	t.Logf("Final recommendation memory: %s", rec.Memory.String())
	t.Logf("  Value (bytes): %d", rec.Memory.Value())
	t.Logf("  MilliValue: %d", rec.Memory.MilliValue())
	t.Logf("  Format: %v", rec.Memory.Format)

	finalJSON, _ := json.Marshal(struct {
		Memory *resource.Quantity `json:"memory"`
	}{Memory: &rec.Memory})
	t.Logf("Final JSON: %s", string(finalJSON))
}
