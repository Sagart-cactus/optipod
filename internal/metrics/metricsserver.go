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
	"context"
	"fmt"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// MetricsServerProvider implements MetricsProvider using Kubernetes metrics-server.
type MetricsServerProvider struct {
	metricsClientset  metricsclientset.Interface
	store             *timeSeriesStore
	sampler           *MetricsServerSampler
	defaultMinSamples int
	interval          time.Duration
	maxSamples        int
}

// SamplingConfig controls metrics-server sampling behavior.
type SamplingConfig struct {
	Interval   time.Duration
	MaxSamples int
	MinSamples int
	TargetTTL  time.Duration
}

// NewMetricsServerProvider creates a new MetricsServerProvider with default settings.
func NewMetricsServerProvider(metricsClientset metricsclientset.Interface) *MetricsServerProvider {
	return NewMetricsServerProviderWithConfig(metricsClientset, SamplingConfig{})
}

// NewMetricsServerProviderWithConfig creates a new MetricsServerProvider with custom configuration.
func NewMetricsServerProviderWithConfig(metricsClientset metricsclientset.Interface, config SamplingConfig) *MetricsServerProvider {
	interval := config.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	maxSamples := config.MaxSamples
	if maxSamples < 1 {
		maxSamples = 2880 // 24h @ 30s
	}

	minSamples := config.MinSamples
	if minSamples < 1 {
		minSamples = 10
	}

	store := newTimeSeriesStore(maxSamples)
	sampler := NewMetricsServerSampler(metricsClientset, store, interval, config.TargetTTL)

	return &MetricsServerProvider{
		metricsClientset:  metricsClientset,
		store:             store,
		sampler:           sampler,
		defaultMinSamples: minSamples,
		interval:          interval,
		maxSamples:        maxSamples,
	}
}

// GetContainerMetrics reads cached metrics samples and computes percentiles.
func (m *MetricsServerProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*ContainerMetrics, error) {
	if window <= 0 {
		window = time.Hour
	}

	key, ok := m.store.resolveTargetKey(namespace, podName, containerName)
	if !ok {
		return nil, &ErrInsufficientSamples{Have: 0, Need: m.defaultMinSamples}
	}

	cutoff := time.Now().Add(-window)
	samples := m.store.samplesSince(key, cutoff)
	if len(samples) == 0 {
		return nil, &ErrInsufficientSamples{Have: 0, Need: m.defaultMinSamples}
	}

	cpuSamples := make([]int64, 0, len(samples))
	memorySamples := make([]int64, 0, len(samples))

	for _, sample := range samples {
		cpuSamples = append(cpuSamples, sample.CPUMilli)
		memorySamples = append(memorySamples, sample.MemoryByte)
	}

	// Compute percentiles
	cpuMetrics := computePercentiles(cpuSamples, true)        // CPU in millicores
	memoryMetrics := computePercentiles(memorySamples, false) // Memory in bytes

	return &ContainerMetrics{
		CPU:    cpuMetrics,
		Memory: memoryMetrics,
	}, nil
}

// DefaultMinSamplesRequired returns the operator-wide default minimum samples required.
func (m *MetricsServerProvider) DefaultMinSamplesRequired() int {
	return m.defaultMinSamples
}

// RegisterTarget updates sampling for a workload container.
func (m *MetricsServerProvider) RegisterTarget(key TargetKey, podName string) {
	if m.sampler == nil {
		return
	}
	m.sampler.RegisterTarget(key, podName)
}

// Sampler returns the background sampler so it can be registered with the manager.
func (m *MetricsServerProvider) Sampler() *MetricsServerSampler {
	return m.sampler
}

// HealthCheck verifies that metrics-server is accessible.
func (m *MetricsServerProvider) HealthCheck(ctx context.Context) error {
	// Try to list node metrics as a health check
	_, err := m.metricsClientset.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("metrics-server health check failed: %w", err)
	}
	return nil
}

// computePercentiles calculates P50, P90, and P99 from a slice of samples.
// If isMillicore is true, values are treated as millicores; otherwise as bytes.
func computePercentiles(samples []int64, isMillicore bool) ResourceMetrics {
	if len(samples) == 0 {
		return ResourceMetrics{
			P50:     resource.Quantity{},
			P90:     resource.Quantity{},
			P99:     resource.Quantity{},
			Samples: 0,
		}
	}

	// Sort samples for percentile computation
	sorted := make([]int64, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p50 := percentile(sorted, 50)
	p90 := percentile(sorted, 90)
	p99 := percentile(sorted, 99)

	var p50Qty, p90Qty, p99Qty resource.Quantity
	if isMillicore {
		p50Qty = *resource.NewMilliQuantity(p50, resource.DecimalSI)
		p90Qty = *resource.NewMilliQuantity(p90, resource.DecimalSI)
		p99Qty = *resource.NewMilliQuantity(p99, resource.DecimalSI)
	} else {
		p50Qty = *resource.NewQuantity(p50, resource.BinarySI)
		p90Qty = *resource.NewQuantity(p90, resource.BinarySI)
		p99Qty = *resource.NewQuantity(p99, resource.BinarySI)
	}

	return ResourceMetrics{
		P50:     p50Qty,
		P90:     p90Qty,
		P99:     p99Qty,
		Samples: len(samples),
	}
}

// percentile computes the nth percentile from a sorted slice.
// Uses linear interpolation between values when the index is not an integer.
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	// Calculate the index for the percentile
	// Using the "nearest rank" method
	rank := float64(p) / 100.0 * float64(len(sorted)-1)
	lowerIndex := int(rank)
	upperIndex := lowerIndex + 1

	if upperIndex >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation
	fraction := rank - float64(lowerIndex)
	lower := float64(sorted[lowerIndex])
	upper := float64(sorted[upperIndex])

	return int64(lower + fraction*(upper-lower))
}
