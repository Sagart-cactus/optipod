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
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// Feature: k8s-workload-rightsizing, Property 4: Bounds enforcement
// Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5
//
// Property: For any computed recommendation and policy with resource bounds,
// the final recommendation should never be less than the minimum bound or
// greater than the maximum bound for both CPU and memory.
func TestProperty_BoundsEnforcement(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("recommendations are always within bounds", prop.ForAll(
		func(cpuP50, cpuP90, cpuP99, memP50, memP90, memP99 int64,
			cpuMin, cpuMax, memMin, memMax int64,
			safetyFactor float64, percentile string) bool {

			// Ensure bounds are valid (min <= max)
			if cpuMin > cpuMax || memMin > memMax {
				return true // Skip invalid bounds
			}

			// Ensure safety factor is valid
			if safetyFactor < 1.0 {
				return true // Skip invalid safety factors
			}

			// Create container metrics
			containerMetrics := &metrics.ContainerMetrics{
				CPU: metrics.ResourceMetrics{
					P50:     *resource.NewMilliQuantity(cpuP50, resource.DecimalSI),
					P90:     *resource.NewMilliQuantity(cpuP90, resource.DecimalSI),
					P99:     *resource.NewMilliQuantity(cpuP99, resource.DecimalSI),
					Samples: 100,
				},
				Memory: metrics.ResourceMetrics{
					P50:     *resource.NewQuantity(memP50, resource.BinarySI),
					P90:     *resource.NewQuantity(memP90, resource.BinarySI),
					P99:     *resource.NewQuantity(memP99, resource.BinarySI),
					Samples: 100,
				},
			}

			// Create policy with bounds
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:      "prometheus",
						RollingWindow: metav1.Duration{},
						Percentile:    percentile,
						SafetyFactor:  &safetyFactor,
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: *resource.NewMilliQuantity(cpuMin, resource.DecimalSI),
							Max: *resource.NewMilliQuantity(cpuMax, resource.DecimalSI),
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: *resource.NewQuantity(memMin, resource.BinarySI),
							Max: *resource.NewQuantity(memMax, resource.BinarySI),
						},
					},
				},
			}

			// Compute recommendation
			engine := NewEngine()
			recommendation, err := engine.ComputeRecommendation(containerMetrics, policy)
			if err != nil {
				return false // Should not error with valid inputs
			}

			// Verify CPU is within bounds
			cpuValue := recommendation.CPU.MilliValue()
			if cpuValue < cpuMin || cpuValue > cpuMax {
				return false
			}

			// Verify Memory is within bounds
			memValue := recommendation.Memory.Value()
			if memValue < memMin || memValue > memMax {
				return false
			}

			return true
		},
		// CPU percentiles (in millicores)
		gen.Int64Range(0, 10000),
		gen.Int64Range(0, 10000),
		gen.Int64Range(0, 10000),
		// Memory percentiles (in bytes)
		gen.Int64Range(0, 10*1024*1024*1024), // 0-10GB
		gen.Int64Range(0, 10*1024*1024*1024),
		gen.Int64Range(0, 10*1024*1024*1024),
		// CPU bounds (in millicores)
		gen.Int64Range(0, 8000),
		gen.Int64Range(0, 8000),
		// Memory bounds (in bytes)
		gen.Int64Range(0, 8*1024*1024*1024), // 0-8GB
		gen.Int64Range(0, 8*1024*1024*1024),
		// Safety factor
		gen.Float64Range(1.0, 3.0),
		// Percentile selection
		gen.OneConstOf("P50", "P90", "P99", ""),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 14: Strategy application
// Validates: Requirements 5.6, 5.7, 5.8
//
// Property: For any recommendation computation, the system should derive CPU and memory
// requests using the strategy defined in the policy (e.g., percentile selection and safety factor).
// Specifically, before bounds clamping, recommendation = selected_percentile * safety_factor.
func TestProperty_SafetyFactorApplication(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("safety factor is correctly applied to selected percentile", prop.ForAll(
		func(cpuP50, cpuP90, cpuP99, memP50, memP90, memP99 int64,
			safetyFactor float64, percentile string) bool {

			// Ensure safety factor is valid
			if safetyFactor < 1.0 {
				return true // Skip invalid safety factors
			}

			// Ensure percentiles are non-negative
			if cpuP50 < 0 || cpuP90 < 0 || cpuP99 < 0 || memP50 < 0 || memP90 < 0 || memP99 < 0 {
				return true
			}

			// Create container metrics
			containerMetrics := &metrics.ContainerMetrics{
				CPU: metrics.ResourceMetrics{
					P50:     *resource.NewMilliQuantity(cpuP50, resource.DecimalSI),
					P90:     *resource.NewMilliQuantity(cpuP90, resource.DecimalSI),
					P99:     *resource.NewMilliQuantity(cpuP99, resource.DecimalSI),
					Samples: 100,
				},
				Memory: metrics.ResourceMetrics{
					P50:     *resource.NewQuantity(memP50, resource.BinarySI),
					P90:     *resource.NewQuantity(memP90, resource.BinarySI),
					P99:     *resource.NewQuantity(memP99, resource.BinarySI),
					Samples: 100,
				},
			}

			// Determine which percentile should be selected
			var expectedCPU, expectedMem int64
			switch percentile {
			case "P50":
				expectedCPU = cpuP50
				expectedMem = memP50
			case "P99":
				expectedCPU = cpuP99
				expectedMem = memP99
			case "P90", "":
				expectedCPU = cpuP90
				expectedMem = memP90
			default:
				expectedCPU = cpuP90
				expectedMem = memP90
			}

			// Apply safety factor to get expected values (before bounds)
			expectedCPUWithSafety := int64(float64(expectedCPU) * safetyFactor)
			expectedMemWithSafety := int64(float64(expectedMem) * safetyFactor)

			// Create policy with very wide bounds so clamping doesn't interfere
			policy := &optipodv1alpha1.OptimizationPolicy{
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					MetricsConfig: optipodv1alpha1.MetricsConfig{
						Provider:      "prometheus",
						RollingWindow: metav1.Duration{},
						Percentile:    percentile,
						SafetyFactor:  &safetyFactor,
					},
					ResourceBounds: optipodv1alpha1.ResourceBounds{
						CPU: optipodv1alpha1.ResourceBound{
							Min: *resource.NewMilliQuantity(0, resource.DecimalSI),
							Max: *resource.NewMilliQuantity(1000000, resource.DecimalSI), // Very high
						},
						Memory: optipodv1alpha1.ResourceBound{
							Min: *resource.NewQuantity(0, resource.BinarySI),
							Max: *resource.NewQuantity(100*1024*1024*1024*1024, resource.BinarySI), // Very high
						},
					},
				},
			}

			// Compute recommendation
			engine := NewEngine()
			recommendation, err := engine.ComputeRecommendation(containerMetrics, policy)
			if err != nil {
				return false // Should not error with valid inputs
			}

			// Verify CPU recommendation matches expected value (with safety factor applied)
			cpuValue := recommendation.CPU.MilliValue()
			// Allow small rounding differences
			cpuDiff := cpuValue - expectedCPUWithSafety
			if cpuDiff < -1 || cpuDiff > 1 {
				return false
			}

			// Verify Memory recommendation matches expected value (with safety factor applied)
			memValue := recommendation.Memory.Value()
			// Allow small rounding differences
			memDiff := memValue - expectedMemWithSafety
			if memDiff < -1 || memDiff > 1 {
				return false
			}

			return true
		},
		// CPU percentiles (in millicores)
		gen.Int64Range(0, 5000),
		gen.Int64Range(0, 5000),
		gen.Int64Range(0, 5000),
		// Memory percentiles (in bytes)
		gen.Int64Range(0, 5*1024*1024*1024), // 0-5GB
		gen.Int64Range(0, 5*1024*1024*1024),
		gen.Int64Range(0, 5*1024*1024*1024),
		// Safety factor
		gen.Float64Range(1.0, 2.0),
		// Percentile selection
		gen.OneConstOf("P50", "P90", "P99", ""),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
