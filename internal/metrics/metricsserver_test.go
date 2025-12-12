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
	"time"
)

// TestSampleCalculation verifies that the number of samples is capped appropriately
func TestSampleCalculation(t *testing.T) {
	tests := []struct {
		name           string
		window         time.Duration
		maxSamples     int
		expectedMax    int
		sampleInterval time.Duration
	}{
		{
			name:           "Production: 1 hour window should be capped at 10 samples",
			window:         1 * time.Hour,
			maxSamples:     10,
			expectedMax:    10,
			sampleInterval: 15 * time.Second,
		},
		{
			name:           "E2E: 1 hour window should be capped at 3 samples",
			window:         1 * time.Hour,
			maxSamples:     3,
			expectedMax:    3,
			sampleInterval: 15 * time.Second,
		},
		{
			name:           "E2E: 5 minute window should give 3 samples",
			window:         5 * time.Minute,
			maxSamples:     3,
			expectedMax:    3,
			sampleInterval: 15 * time.Second,
		},
		{
			name:           "E2E: 30 second window should give 2 samples",
			window:         30 * time.Second,
			maxSamples:     3,
			expectedMax:    2,
			sampleInterval: 15 * time.Second,
		},
		{
			name:           "E2E: 10 second window should give 1 sample",
			window:         10 * time.Second,
			maxSamples:     3,
			expectedMax:    1,
			sampleInterval: 15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the calculation logic from GetContainerMetrics
			numSamples := int(tt.window / tt.sampleInterval)
			if numSamples < 1 {
				numSamples = 1
			}
			if numSamples > tt.maxSamples {
				numSamples = tt.maxSamples
			}

			if numSamples != tt.expectedMax {
				t.Errorf("Expected %d samples, got %d", tt.expectedMax, numSamples)
			}

			// Verify total collection time is reasonable
			totalTime := time.Duration(numSamples-1) * tt.sampleInterval
			maxTime := 3 * time.Minute // Allow up to 3 minutes for production (10 samples)
			if totalTime > maxTime {
				t.Errorf("Total collection time %v exceeds %v", totalTime, maxTime)
			}
		})
	}
}

// TestMaxCollectionTime verifies that metrics collection times are reasonable
func TestMaxCollectionTime(t *testing.T) {
	tests := []struct {
		name           string
		maxSamples     int
		sampleInterval time.Duration
		maxAllowed     time.Duration
	}{
		{
			name:           "E2E configuration (3 samples, 15s interval)",
			maxSamples:     3,
			sampleInterval: 15 * time.Second,
			maxAllowed:     1 * time.Minute,
		},
		{
			name:           "Production configuration (10 samples, 15s interval)",
			maxSamples:     10,
			sampleInterval: 15 * time.Second,
			maxAllowed:     3 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Maximum time is (maxSamples - 1) * sampleInterval
			// because we don't wait after the last sample
			maxTime := time.Duration(tt.maxSamples-1) * tt.sampleInterval

			if maxTime > tt.maxAllowed {
				t.Errorf("Maximum collection time %v exceeds allowed %v", maxTime, tt.maxAllowed)
			}

			t.Logf("Maximum collection time: %v (acceptable, limit: %v)", maxTime, tt.maxAllowed)
		})
	}
}

// TestPercentileComputation verifies that percentiles can be computed with small sample sizes
func TestPercentileComputation(t *testing.T) {
	tests := []struct {
		name    string
		samples []int64
		p       int
		want    int64
	}{
		{
			name:    "3 samples - P50",
			samples: []int64{100, 200, 300},
			p:       50,
			want:    200,
		},
		{
			name:    "3 samples - P90",
			samples: []int64{100, 200, 300},
			p:       90,
			want:    280, // Linear interpolation: 200 + 0.8 * (300-200)
		},
		{
			name:    "1 sample",
			samples: []int64{100},
			p:       90,
			want:    100,
		},
		{
			name:    "2 samples - P50",
			samples: []int64{100, 200},
			p:       50,
			want:    150, // Linear interpolation: 100 + 0.5 * (200-100)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.samples, tt.p)
			if got != tt.want {
				t.Errorf("percentile(%v, %d) = %d, want %d", tt.samples, tt.p, got, tt.want)
			}
		})
	}
}
