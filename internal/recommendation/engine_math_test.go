/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package recommendation

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// These tables drive the production recommendation engine directly and assert
// exact CPU/memory outputs (VAL-FAST-005): percentile selection, safety-factor
// arithmetic, below/within/above bounds clamping, legacy absent optional
// settings and zero usage. Values are chosen so every multiplication lands on
// an exact byte/millicore boundary, keeping the assertions exact (see the
// library gotcha about canonicalizing quantities like "4" for 4000m; these
// tables assert MilliValue/Value as the authoritative comparison).

const (
	mem128Mi = 128 * 1024 * 1024
	mem250Mi = 250 * 1024 * 1024
	mem400Mi = 400 * 1024 * 1024
	mem100Mi = 100 * 1024 * 1024
	mem300Mi = 300 * 1024 * 1024
	mem120Mi = 120 * 1024 * 1024
	mem512Mi = 512 * 1024 * 1024
)

// mathMetrics returns CPU percentiles 100m/250m/500m and memory percentiles
// 128Mi/250Mi/400Mi.
func mathMetrics() *metrics.ContainerMetrics {
	return &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P50:     *resource.NewMilliQuantity(100, resource.DecimalSI),
			P90:     *resource.NewMilliQuantity(250, resource.DecimalSI),
			P99:     *resource.NewMilliQuantity(500, resource.DecimalSI),
			Samples: 50,
		},
		Memory: metrics.ResourceMetrics{
			P50:     *resource.NewQuantity(mem128Mi, resource.BinarySI),
			P90:     *resource.NewQuantity(mem250Mi, resource.BinarySI),
			P99:     *resource.NewQuantity(mem400Mi, resource.BinarySI),
			Samples: 50,
		},
	}
}

// mathPolicy builds a policy with the given percentile, safety factor and very
// wide bounds so clamping never interferes.
func mathPolicy(percentile string, safetyFactor *float64) *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider:     "prometheus",
				Percentile:   percentile,
				SafetyFactor: safetyFactor,
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("0m"),
					Max: resource.MustParse("8000m"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("0Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
		},
	}
}

func TestComputeRecommendationExactPercentileAndSafetyArithmetic(t *testing.T) {
	one := 1.0
	onePointFive := 1.5

	tests := []struct {
		name            string
		percentile      string
		safetyFactor    *float64
		wantCPUMilli    int64
		wantMemoryBytes int64
		wantExplanation string
	}{
		{
			name:            "P50 selection with safety factor 1.0",
			percentile:      "P50",
			safetyFactor:    &one,
			wantCPUMilli:    100,
			wantMemoryBytes: mem128Mi,
			wantExplanation: "Computed from P50 percentile",
		},
		{
			name:            "P90 selection with safety factor 1.0",
			percentile:      "P90",
			safetyFactor:    &one,
			wantCPUMilli:    250,
			wantMemoryBytes: mem250Mi,
			wantExplanation: "Computed from P90 percentile",
		},
		{
			name:            "P99 selection with safety factor 1.0",
			percentile:      "P99",
			safetyFactor:    &one,
			wantCPUMilli:    500,
			wantMemoryBytes: mem400Mi,
			wantExplanation: "Computed from P99 percentile",
		},
		{
			name:            "P90 with safety factor 1.5 multiplies CPU in millicores",
			percentile:      "P90",
			safetyFactor:    &onePointFive,
			wantCPUMilli:    375,       // 250m * 1.5
			wantMemoryBytes: 393216000, // 250Mi * 1.5 = 375Mi exactly
			wantExplanation: "Computed from P90 percentile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine()
			rec, err := engine.ComputeRecommendation(mathMetrics(), mathPolicy(tt.percentile, tt.safetyFactor))
			if err != nil {
				t.Fatalf("ComputeRecommendation returned an error: %v", err)
			}

			if got := rec.CPU.MilliValue(); got != tt.wantCPUMilli {
				t.Errorf("CPU recommendation = %d milli, want %d milli", got, tt.wantCPUMilli)
			}
			if got := rec.Memory.Value(); got != tt.wantMemoryBytes {
				t.Errorf("memory recommendation = %d bytes, want %d bytes", got, tt.wantMemoryBytes)
			}
			if rec.CPU.Format != resource.DecimalSI {
				t.Errorf("CPU format = %v, want DecimalSI", rec.CPU.Format)
			}
			if rec.Memory.Format != resource.BinarySI {
				t.Errorf("memory format = %v, want BinarySI", rec.Memory.Format)
			}
			if !strings.Contains(rec.Explanation, tt.wantExplanation) {
				t.Errorf("explanation %q does not mention %q", rec.Explanation, tt.wantExplanation)
			}
		})
	}
}

func TestComputeRecommendationClampsBelowWithinAndAboveBounds(t *testing.T) {
	one := 1.0
	onePointTwo := 1.2

	tests := []struct {
		name string
		// inputs
		cpuP90Milli    int64
		memoryP90Bytes int64
		safetyFactor   *float64
		cpuMinMilli    int64
		cpuMaxMilli    int64
		memMinBytes    int64
		memMaxBytes    int64
		// expected outputs
		wantCPUMilli    int64
		wantMemoryBytes int64
	}{
		{
			name:            "below-minimum usage clamps up to the minimum for both resources",
			cpuP90Milli:     100,
			memoryP90Bytes:  mem100Mi,
			safetyFactor:    &one,
			cpuMinMilli:     200,
			cpuMaxMilli:     2000,
			memMinBytes:     mem250Mi,
			memMaxBytes:     4 * mem250Mi,
			wantCPUMilli:    200,
			wantMemoryBytes: mem250Mi,
		},
		{
			name:            "within-bounds usage passes through with safety factor applied",
			cpuP90Milli:     500,
			memoryP90Bytes:  mem300Mi,
			safetyFactor:    &onePointTwo,
			cpuMinMilli:     200,
			cpuMaxMilli:     2000,
			memMinBytes:     mem250Mi,
			memMaxBytes:     mem512Mi * 2,
			wantCPUMilli:    600,       // 500m * 1.2
			wantMemoryBytes: 377487360, // 300Mi * 1.2 = 360Mi exactly
		},
		{
			name:            "above-maximum usage clamps down to the maximum for both resources",
			cpuP90Milli:     900,
			memoryP90Bytes:  800 * 1024 * 1024,
			safetyFactor:    &onePointTwo,
			cpuMinMilli:     100,
			cpuMaxMilli:     500,
			memMinBytes:     mem120Mi,
			memMaxBytes:     mem512Mi,
			wantCPUMilli:    500,
			wantMemoryBytes: mem512Mi,
		},
		{
			name:            "CPU clamps to max while memory stays within bounds in the same computation",
			cpuP90Milli:     900,
			memoryP90Bytes:  mem100Mi,
			safetyFactor:    &onePointTwo,
			cpuMinMilli:     100,
			cpuMaxMilli:     500,
			memMinBytes:     64 * 1024 * 1024,
			memMaxBytes:     mem512Mi,
			wantCPUMilli:    500,      // 900m * 1.2 = 1080m clamped to 500m
			wantMemoryBytes: mem120Mi, // 100Mi * 1.2 = 120Mi within bounds
		},
		{
			name:            "below-minimum CPU and above-maximum memory clamp independently",
			cpuP90Milli:     50,
			memoryP90Bytes:  4 * 1024 * 1024 * 1024,
			safetyFactor:    &one,
			cpuMinMilli:     200,
			cpuMaxMilli:     2000,
			memMinBytes:     mem128Mi,
			memMaxBytes:     mem512Mi,
			wantCPUMilli:    200,
			wantMemoryBytes: mem512Mi,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine()
			containerMetrics := &metrics.ContainerMetrics{
				CPU: metrics.ResourceMetrics{
					P50: *resource.NewMilliQuantity(tt.cpuP90Milli, resource.DecimalSI),
					P90: *resource.NewMilliQuantity(tt.cpuP90Milli, resource.DecimalSI),
					P99: *resource.NewMilliQuantity(tt.cpuP90Milli, resource.DecimalSI),
				},
				Memory: metrics.ResourceMetrics{
					P50: *resource.NewQuantity(tt.memoryP90Bytes, resource.BinarySI),
					P90: *resource.NewQuantity(tt.memoryP90Bytes, resource.BinarySI),
					P99: *resource.NewQuantity(tt.memoryP90Bytes, resource.BinarySI),
				},
			}
			policy := mathPolicy("P90", tt.safetyFactor)
			policy.Spec.ResourceBounds = optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: *resource.NewMilliQuantity(tt.cpuMinMilli, resource.DecimalSI),
					Max: *resource.NewMilliQuantity(tt.cpuMaxMilli, resource.DecimalSI),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: *resource.NewQuantity(tt.memMinBytes, resource.BinarySI),
					Max: *resource.NewQuantity(tt.memMaxBytes, resource.BinarySI),
				},
			}

			rec, err := engine.ComputeRecommendation(containerMetrics, policy)
			if err != nil {
				t.Fatalf("ComputeRecommendation returned an error: %v", err)
			}
			if got := rec.CPU.MilliValue(); got != tt.wantCPUMilli {
				t.Errorf("CPU recommendation = %d milli, want %d milli", got, tt.wantCPUMilli)
			}
			if got := rec.Memory.Value(); got != tt.wantMemoryBytes {
				t.Errorf("memory recommendation = %d bytes, want %d bytes", got, tt.wantMemoryBytes)
			}
		})
	}
}

// TestComputeRecommendationLegacyAbsentOptionalSettings pins the production
// defaults for policies omitting the newer optional metrics settings (legacy
// compatibility, VAL-FAST-005): an absent safety factor falls back to 1.2 and an
// absent percentile falls back to P90, preserving prior safe behavior.
func TestComputeRecommendationLegacyAbsentOptionalSettings(t *testing.T) {
	engine := NewEngine()

	t.Run("absent safety factor defaults to 1.2", func(t *testing.T) {
		rec, err := engine.ComputeRecommendation(mathMetrics(), mathPolicy("P90", nil))
		if err != nil {
			t.Fatalf("ComputeRecommendation returned an error: %v", err)
		}
		// 250m * 1.2 = 300m; 250Mi * 1.2 = 300Mi (both exact).
		if got := rec.CPU.MilliValue(); got != 300 {
			t.Errorf("CPU recommendation = %d milli, want 300 milli (P90 250m * default 1.2)", got)
		}
		if got := rec.Memory.Value(); got != mem300Mi {
			t.Errorf("memory recommendation = %d bytes, want %d (P90 250Mi * default 1.2)", got, int64(mem300Mi))
		}
		if !strings.Contains(rec.Explanation, "safety factor 1.20") {
			t.Errorf("explanation %q does not record the default safety factor", rec.Explanation)
		}
	})

	t.Run("absent percentile defaults to P90", func(t *testing.T) {
		one := 1.0
		rec, err := engine.ComputeRecommendation(mathMetrics(), mathPolicy("", &one))
		if err != nil {
			t.Fatalf("ComputeRecommendation returned an error: %v", err)
		}
		if got := rec.CPU.MilliValue(); got != 250 {
			t.Errorf("CPU recommendation = %d milli, want 250 milli (P90 default)", got)
		}
		if got := rec.Memory.Value(); got != mem250Mi {
			t.Errorf("memory recommendation = %d bytes, want %d (P90 default)", got, int64(mem250Mi))
		}
		if !strings.Contains(rec.Explanation, "Computed from P90 percentile") {
			t.Errorf("explanation %q does not report the P90 default", rec.Explanation)
		}
	})

	t.Run("zero usage with nonzero minimum bounds clamps to the minimum", func(t *testing.T) {
		zeroMetrics := &metrics.ContainerMetrics{
			CPU: metrics.ResourceMetrics{
				P50: *resource.NewMilliQuantity(0, resource.DecimalSI),
				P90: *resource.NewMilliQuantity(0, resource.DecimalSI),
				P99: *resource.NewMilliQuantity(0, resource.DecimalSI),
			},
			Memory: metrics.ResourceMetrics{
				P50: *resource.NewQuantity(0, resource.BinarySI),
				P90: *resource.NewQuantity(0, resource.BinarySI),
				P99: *resource.NewQuantity(0, resource.BinarySI),
			},
		}
		policy := mathPolicy("P90", nil)
		policy.Spec.ResourceBounds.CPU.Min = resource.MustParse("10m")
		policy.Spec.ResourceBounds.Memory.Min = resource.MustParse("64Mi")

		rec, err := engine.ComputeRecommendation(zeroMetrics, policy)
		if err != nil {
			t.Fatalf("ComputeRecommendation returned an error: %v", err)
		}
		if got := rec.CPU.MilliValue(); got != 10 {
			t.Errorf("CPU recommendation = %d milli, want 10 milli (clamped to min)", got)
		}
		if got := rec.Memory.Value(); got != 64*1024*1024 {
			t.Errorf("memory recommendation = %d bytes, want 64Mi (clamped to min)", got)
		}
	})

	t.Run("zero usage with zero minimum bounds stays zero", func(t *testing.T) {
		zeroMetrics := &metrics.ContainerMetrics{
			CPU: metrics.ResourceMetrics{
				P50: *resource.NewMilliQuantity(0, resource.DecimalSI),
				P90: *resource.NewMilliQuantity(0, resource.DecimalSI),
				P99: *resource.NewMilliQuantity(0, resource.DecimalSI),
			},
			Memory: metrics.ResourceMetrics{
				P50: *resource.NewQuantity(0, resource.BinarySI),
				P90: *resource.NewQuantity(0, resource.BinarySI),
				P99: *resource.NewQuantity(0, resource.BinarySI),
			},
		}
		rec, err := engine.ComputeRecommendation(zeroMetrics, mathPolicy("P90", nil))
		if err != nil {
			t.Fatalf("ComputeRecommendation returned an error: %v", err)
		}
		if !rec.CPU.IsZero() {
			t.Errorf("CPU recommendation = %s, want zero", rec.CPU.String())
		}
		if !rec.Memory.IsZero() {
			t.Errorf("memory recommendation = %s, want zero", rec.Memory.String())
		}
	})
}

func TestComputeRecommendationRejectsNilInputs(t *testing.T) {
	engine := NewEngine()

	if _, err := engine.ComputeRecommendation(nil, mathPolicy("P90", nil)); err == nil {
		t.Error("expected an error for nil container metrics")
	}
	if _, err := engine.ComputeRecommendation(mathMetrics(), nil); err == nil {
		t.Error("expected an error for a nil policy")
	}
}
