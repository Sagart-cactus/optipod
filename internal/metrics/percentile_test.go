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

package metrics

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: k8s-workload-rightsizing, Property 13: Percentile computation
// Validates: Requirements 5.4, 5.5
//
// Property: For any collected usage metrics, the system should compute P50, P90, and P99
// percentiles for both CPU and memory, and P50 <= P90 <= P99 must hold.
func TestProperty_PercentileOrdering(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("P50 <= P90 <= P99 for all sample sets", prop.ForAll(
		func(samples []int64) bool {
			if len(samples) == 0 {
				return true // Empty samples are valid, skip
			}

			// Test with CPU (millicore format)
			cpuMetrics := computePercentiles(samples, true)
			
			// Verify ordering: P50 <= P90 <= P99
			p50Value := cpuMetrics.P50.MilliValue()
			p90Value := cpuMetrics.P90.MilliValue()
			p99Value := cpuMetrics.P99.MilliValue()

			if p50Value > p90Value {
				return false
			}
			if p90Value > p99Value {
				return false
			}

			// Test with Memory (byte format)
			memMetrics := computePercentiles(samples, false)
			
			p50Mem := memMetrics.P50.Value()
			p90Mem := memMetrics.P90.Value()
			p99Mem := memMetrics.P99.Value()

			if p50Mem > p90Mem {
				return false
			}
			if p90Mem > p99Mem {
				return false
			}

			// Verify sample count is correct
			if cpuMetrics.Samples != len(samples) {
				return false
			}
			if memMetrics.Samples != len(samples) {
				return false
			}

			return true
		},
		gen.SliceOf(gen.Int64Range(0, 10000000000)), // Generate slices of positive int64 values
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 13: Percentile computation
// Validates: Requirements 5.4, 5.5
//
// Property: For any non-empty sample set, all percentiles should be within the range
// of the minimum and maximum values in the sample set.
func TestProperty_PercentileBounds(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("percentiles are within min/max bounds", prop.ForAll(
		func(samples []int64) bool {
			if len(samples) == 0 {
				return true // Skip empty samples
			}

			// Find min and max
			min := samples[0]
			max := samples[0]
			for _, v := range samples {
				if v < min {
					min = v
				}
				if v > max {
					max = v
				}
			}

			metrics := computePercentiles(samples, true)
			
			p50 := metrics.P50.MilliValue()
			p90 := metrics.P90.MilliValue()
			p99 := metrics.P99.MilliValue()

			// All percentiles should be within [min, max]
			if p50 < min || p50 > max {
				return false
			}
			if p90 < min || p90 > max {
				return false
			}
			if p99 < min || p99 > max {
				return false
			}

			return true
		},
		gen.SliceOfN(10, gen.Int64Range(0, 10000000000)).SuchThat(func(v interface{}) bool {
			return len(v.([]int64)) > 0
		}),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 13: Percentile computation
// Validates: Requirements 5.4, 5.5
//
// Property: For a sample set with all identical values, all percentiles should equal that value.
func TestProperty_PercentileIdenticalValues(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("all percentiles equal for identical values", prop.ForAll(
		func(value int64, count int) bool {
			if count <= 0 {
				return true
			}

			// Create slice with identical values
			samples := make([]int64, count)
			for i := range samples {
				samples[i] = value
			}

			metrics := computePercentiles(samples, true)
			
			p50 := metrics.P50.MilliValue()
			p90 := metrics.P90.MilliValue()
			p99 := metrics.P99.MilliValue()

			// All percentiles should equal the value
			return p50 == value && p90 == value && p99 == value
		},
		gen.Int64Range(0, 10000000000),
		gen.IntRange(1, 100),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
