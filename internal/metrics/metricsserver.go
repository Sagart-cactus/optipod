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
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// MetricsServerProvider implements MetricsProvider using Kubernetes metrics-server.
type MetricsServerProvider struct {
	clientset        kubernetes.Interface
	metricsClientset metricsclientset.Interface
	maxSamples       int           // Maximum number of samples to collect
	sampleInterval   time.Duration // Interval between samples
}

// NewMetricsServerProvider creates a new MetricsServerProvider with default settings.
// Default: 10 samples with 15-second intervals (suitable for production).
func NewMetricsServerProvider(clientset kubernetes.Interface, metricsClientset metricsclientset.Interface) *MetricsServerProvider {
	return &MetricsServerProvider{
		clientset:        clientset,
		metricsClientset: metricsClientset,
		maxSamples:       10,               // Default: 10 samples for production
		sampleInterval:   15 * time.Second, // Match metrics-server scrape interval
	}
}

// NewMetricsServerProviderWithConfig creates a new MetricsServerProvider with custom configuration.
// This allows tests to use fewer samples for faster execution.
func NewMetricsServerProviderWithConfig(clientset kubernetes.Interface, metricsClientset metricsclientset.Interface, maxSamples int, sampleInterval time.Duration) *MetricsServerProvider {
	if maxSamples < 1 {
		maxSamples = 1
	}
	if sampleInterval < 1*time.Second {
		sampleInterval = 1 * time.Second
	}
	return &MetricsServerProvider{
		clientset:        clientset,
		metricsClientset: metricsClientset,
		maxSamples:       maxSamples,
		sampleInterval:   sampleInterval,
	}
}

// GetContainerMetrics collects metrics from metrics-server and computes percentiles.
// Since metrics-server provides point-in-time metrics, we collect multiple samples
// over a short period to build a time series for percentile computation.
// Note: We collect a configurable number of samples rather than sampling over the
// entire rolling window, as that would be impractical (e.g., 1 hour would take 1 hour).
func (m *MetricsServerProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*ContainerMetrics, error) {
	// Calculate number of samples based on window, but cap at configured maxSamples
	// This provides enough data for percentile computation without excessive wait time
	numSamples := int(window / m.sampleInterval)
	if numSamples < 1 {
		numSamples = 1
	}
	if numSamples > m.maxSamples {
		numSamples = m.maxSamples
	}

	cpuSamples := make([]int64, 0, numSamples)
	memorySamples := make([]int64, 0, numSamples)

	// Collect samples
	for i := 0; i < numSamples; i++ {
		podMetrics, err := m.metricsClientset.MetricsV1beta1().PodMetricses(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get pod metrics: %w", err)
		}

		// Find the container metrics
		var containerMetrics *metricsv1beta1.ContainerMetrics
		for i := range podMetrics.Containers {
			if podMetrics.Containers[i].Name == containerName {
				containerMetrics = &podMetrics.Containers[i]
				break
			}
		}

		if containerMetrics == nil {
			return nil, fmt.Errorf("container %s not found in pod %s/%s metrics", containerName, namespace, podName)
		}

		// Extract CPU and memory usage
		cpuUsage := containerMetrics.Usage.Cpu().MilliValue()
		memoryUsage := containerMetrics.Usage.Memory().Value()

		cpuSamples = append(cpuSamples, cpuUsage)
		memorySamples = append(memorySamples, memoryUsage)

		// Wait before next sample (except for the last iteration)
		if i < numSamples-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(m.sampleInterval):
			}
		}
	}

	// Compute percentiles
	cpuMetrics := computePercentiles(cpuSamples, true)        // CPU in millicores
	memoryMetrics := computePercentiles(memorySamples, false) // Memory in bytes

	return &ContainerMetrics{
		CPU:    cpuMetrics,
		Memory: memoryMetrics,
	}, nil
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
