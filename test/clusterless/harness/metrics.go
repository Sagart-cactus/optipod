package harness

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MockMetricsProvider simulates metrics-server and Prometheus with deterministic data
type MockMetricsProvider struct {
	mu         sync.RWMutex
	timeSeries map[TargetKey][]MetricsSample
}

// TargetKey identifies a specific container
type TargetKey struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// MetricsSample represents a single metrics data point
type MetricsSample struct {
	Timestamp time.Time
	CPU       int64 // CPU in millicores
	Memory    int64 // Memory in bytes
}

// ContainerMetrics represents aggregated metrics for a container
type ContainerMetrics struct {
	CPU    ResourceMetrics
	Memory ResourceMetrics
}

// ResourceMetrics contains percentile data for a resource
type ResourceMetrics struct {
	P50 int64
	P90 int64
	P95 int64
	P99 int64
}

// NewMockMetricsProvider creates a new mock metrics provider
func NewMockMetricsProvider() *MockMetricsProvider {
	return &MockMetricsProvider{
		timeSeries: make(map[TargetKey][]MetricsSample),
	}
}

// InjectConstantLoad simulates steady workload with consistent resource usage
func (m *MockMetricsProvider) InjectConstantLoad(
	namespace, podName, containerName string,
	cpuMilli int64,
	memoryBytes int64,
	duration time.Duration,
	interval time.Duration,
) {
	key := TargetKey{namespace, podName, containerName}

	samples := []MetricsSample{}
	numSamples := int(duration / interval)

	for i := 0; i < numSamples; i++ {
		samples = append(samples, MetricsSample{
			Timestamp: time.Now().Add(-duration + time.Duration(i)*interval),
			CPU:       cpuMilli,
			Memory:    memoryBytes,
		})
	}

	m.mu.Lock()
	m.timeSeries[key] = samples
	m.mu.Unlock()
}

// InjectSpikeyLoad simulates variable workload with periodic spikes
func (m *MockMetricsProvider) InjectSpikeyLoad(
	namespace, podName, containerName string,
	baseCPU, spikeCPU int64,
	baseMem, spikeMem int64,
	duration time.Duration,
) {
	key := TargetKey{namespace, podName, containerName}

	samples := []MetricsSample{}
	interval := 1 * time.Minute
	numSamples := int(duration / interval)

	for i := 0; i < numSamples; i++ {
		// Spike every 10 samples (10% of the time)
		cpu := baseCPU
		mem := baseMem
		if i%10 == 0 {
			cpu = spikeCPU
			mem = spikeMem
		}

		samples = append(samples, MetricsSample{
			Timestamp: time.Now().Add(-duration + time.Duration(i)*interval),
			CPU:       cpu,
			Memory:    mem,
		})
	}

	m.mu.Lock()
	m.timeSeries[key] = samples
	m.mu.Unlock()
}

// InjectGradualIncrease simulates gradually increasing resource usage
func (m *MockMetricsProvider) InjectGradualIncrease(
	namespace, podName, containerName string,
	startCPU, endCPU int64,
	startMem, endMem int64,
	duration time.Duration,
) {
	key := TargetKey{namespace, podName, containerName}

	samples := []MetricsSample{}
	interval := 1 * time.Minute
	numSamples := int(duration / interval)

	cpuStep := (endCPU - startCPU) / int64(numSamples)
	memStep := (endMem - startMem) / int64(numSamples)

	for i := 0; i < numSamples; i++ {
		samples = append(samples, MetricsSample{
			Timestamp: time.Now().Add(-duration + time.Duration(i)*interval),
			CPU:       startCPU + int64(i)*cpuStep,
			Memory:    startMem + int64(i)*memStep,
		})
	}

	m.mu.Lock()
	m.timeSeries[key] = samples
	m.mu.Unlock()
}

// InjectCustomSamples allows injection of custom sample data
func (m *MockMetricsProvider) InjectCustomSamples(
	namespace, podName, containerName string,
	samples []MetricsSample,
) {
	key := TargetKey{namespace, podName, containerName}

	m.mu.Lock()
	m.timeSeries[key] = samples
	m.mu.Unlock()
}

// GetContainerMetrics retrieves metrics for a specific container
func (m *MockMetricsProvider) GetContainerMetrics(
	ctx context.Context,
	namespace, podName, containerName string,
	window time.Duration,
) (ContainerMetrics, error) {
	key := TargetKey{namespace, podName, containerName}

	m.mu.RLock()
	samples, exists := m.timeSeries[key]
	m.mu.RUnlock()

	if !exists {
		return ContainerMetrics{}, fmt.Errorf("no metrics found for container %s in pod %s/%s",
			containerName, namespace, podName)
	}

	if len(samples) == 0 {
		return ContainerMetrics{}, fmt.Errorf("no samples available for container %s in pod %s/%s",
			containerName, namespace, podName)
	}

	// Filter samples within the time window
	cutoff := time.Now().Add(-window)
	filteredSamples := []MetricsSample{}
	for _, sample := range samples {
		if sample.Timestamp.After(cutoff) {
			filteredSamples = append(filteredSamples, sample)
		}
	}

	if len(filteredSamples) == 0 {
		return ContainerMetrics{}, fmt.Errorf("no samples within time window for container %s in pod %s/%s",
			containerName, namespace, podName)
	}

	// Calculate percentiles
	cpuSamples := make([]int64, len(filteredSamples))
	memSamples := make([]int64, len(filteredSamples))
	for i, s := range filteredSamples {
		cpuSamples[i] = s.CPU
		memSamples[i] = s.Memory
	}

	return ContainerMetrics{
		CPU: ResourceMetrics{
			P50: percentile(cpuSamples, 50),
			P90: percentile(cpuSamples, 90),
			P95: percentile(cpuSamples, 95),
			P99: percentile(cpuSamples, 99),
		},
		Memory: ResourceMetrics{
			P50: percentile(memSamples, 50),
			P90: percentile(memSamples, 90),
			P95: percentile(memSamples, 95),
			P99: percentile(memSamples, 99),
		},
	}, nil
}

// ClearMetrics removes all stored metrics
func (m *MockMetricsProvider) ClearMetrics() {
	m.mu.Lock()
	m.timeSeries = make(map[TargetKey][]MetricsSample)
	m.mu.Unlock()
}

// HasMetrics checks if metrics exist for a container
func (m *MockMetricsProvider) HasMetrics(namespace, podName, containerName string) bool {
	key := TargetKey{namespace, podName, containerName}

	m.mu.RLock()
	_, exists := m.timeSeries[key]
	m.mu.RUnlock()

	return exists
}

// GetSampleCount returns the number of samples for a container
func (m *MockMetricsProvider) GetSampleCount(namespace, podName, containerName string) int {
	key := TargetKey{namespace, podName, containerName}

	m.mu.RLock()
	samples, exists := m.timeSeries[key]
	m.mu.RUnlock()

	if !exists {
		return 0
	}
	return len(samples)
}

// percentile calculates the nth percentile of a sorted slice
func percentile(data []int64, p int) int64 {
	if len(data) == 0 {
		return 0
	}

	// Sort the data
	sorted := make([]int64, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate percentile index
	index := float64(p) / 100.0 * float64(len(sorted)-1)

	// Handle edge cases
	if index <= 0 {
		return sorted[0]
	}
	if index >= float64(len(sorted)-1) {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation between two closest values
	lower := int(index)
	upper := lower + 1
	weight := index - float64(lower)

	return int64(float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight)
}
