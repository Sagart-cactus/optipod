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
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusProvider implements MetricsProvider using Prometheus.
type PrometheusProvider struct {
	client v1.API
}

// NewPrometheusProvider creates a new PrometheusProvider.
func NewPrometheusProvider(prometheusURL string) (*PrometheusProvider, error) {
	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	return &PrometheusProvider{
		client: v1.NewAPI(client),
	}, nil
}

// GetContainerMetrics queries Prometheus for container CPU and memory usage
// over the rolling window and computes percentiles.
func (p *PrometheusProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*ContainerMetrics, error) {
	// Query CPU usage
	cpuQuery := fmt.Sprintf(
		`rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container="%s"}[%s])`,
		namespace, podName, containerName, formatDuration(window),
	)

	cpuSamples, err := p.queryRange(ctx, cpuQuery, window)
	if err != nil {
		return nil, fmt.Errorf("failed to query CPU metrics: %w", err)
	}

	// Convert CPU samples from cores to millicores
	cpuMillicores := make([]int64, len(cpuSamples))
	for i, v := range cpuSamples {
		cpuMillicores[i] = int64(v * 1000)
	}

	// Query memory usage
	memoryQuery := fmt.Sprintf(
		`container_memory_working_set_bytes{namespace="%s",pod="%s",container="%s"}`,
		namespace, podName, containerName,
	)

	memorySamples, err := p.queryRange(ctx, memoryQuery, window)
	if err != nil {
		return nil, fmt.Errorf("failed to query memory metrics: %w", err)
	}

	// Convert memory samples to int64 bytes
	memoryBytes := make([]int64, len(memorySamples))
	for i, v := range memorySamples {
		memoryBytes[i] = int64(v)
	}

	// Compute percentiles
	cpuMetrics := computePercentiles(cpuMillicores, true)
	memoryMetrics := computePercentiles(memoryBytes, false)

	return &ContainerMetrics{
		CPU:    cpuMetrics,
		Memory: memoryMetrics,
	}, nil
}

// HealthCheck verifies that Prometheus is accessible.
func (p *PrometheusProvider) HealthCheck(ctx context.Context) error {
	// Query Prometheus build info as a health check
	_, err := p.client.Buildinfo(ctx)
	if err != nil {
		return fmt.Errorf("prometheus health check failed: %w", err)
	}
	return nil
}

// queryRange executes a range query and returns the sample values.
func (p *PrometheusProvider) queryRange(ctx context.Context, query string, window time.Duration) ([]float64, error) {
	end := time.Now()
	start := end.Add(-window)

	// Use 30-second step for reasonable granularity
	step := 30 * time.Second

	result, warnings, err := p.client.QueryRange(ctx, query, v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if len(warnings) > 0 {
		// Log warnings but don't fail
		fmt.Printf("Prometheus query warnings: %v\n", warnings)
	}

	// Extract values from the result
	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	if len(matrix) == 0 {
		return nil, fmt.Errorf("no data returned from Prometheus")
	}

	// Collect all sample values from the first series
	// (there should only be one series for a specific container)
	samples := make([]float64, 0, len(matrix[0].Values))
	for _, sample := range matrix[0].Values {
		samples = append(samples, float64(sample.Value))
	}

	if len(samples) == 0 {
		return nil, fmt.Errorf("no samples in result")
	}

	return samples, nil
}

// formatDuration formats a duration for use in PromQL queries.
func formatDuration(d time.Duration) string {
	// Convert to seconds, minutes, hours, or days as appropriate
	seconds := int(d.Seconds())

	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}

	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	}

	days := hours / 24
	return fmt.Sprintf("%dd", days)
}
