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
)

// PrometheusProvider implements MetricsProvider using Prometheus.
type PrometheusProvider struct {
	client v1.API
}

// NewPrometheusProvider creates a new PrometheusProvider with authentication support.
func NewPrometheusProvider(config PrometheusConfig) (*PrometheusProvider, error) {
	// Build HTTP client with auth and TLS
	httpClient, err := buildHTTPClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	client, err := api.NewClient(api.Config{
		Address:      config.URL,
		RoundTripper: httpClient.Transport,
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
	cpu, err := p.GetResourceSamples(ctx, namespace, podName, containerName, window, true)
	if err != nil {
		return nil, fmt.Errorf("failed to query CPU metrics: %w", err)
	}
	memory, err := p.GetResourceSamples(ctx, namespace, podName, containerName, window, false)
	if err != nil {
		return nil, fmt.Errorf("failed to query memory metrics: %w", err)
	}
	return &ContainerMetrics{CPU: Statistics(cpu, true), Memory: Statistics(memory, false)}, nil
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
