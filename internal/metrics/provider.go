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
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

// MetricsProvider defines the interface for collecting resource usage metrics
// from various backends (metrics-server, Prometheus, custom providers).
type MetricsProvider interface {
	// GetContainerMetrics returns CPU and memory usage statistics for a container
	// over the specified time window.
	GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*ContainerMetrics, error)

	// HealthCheck verifies the metrics backend is accessible and functioning.
	HealthCheck(ctx context.Context) error
}

// ContainerMetrics contains resource usage statistics for a single container.
type ContainerMetrics struct {
	CPU    ResourceMetrics
	Memory ResourceMetrics
}

// ResourceMetrics contains percentile-based statistics for a resource type.
type ResourceMetrics struct {
	P50     resource.Quantity // 50th percentile (median)
	P90     resource.Quantity // 90th percentile
	P99     resource.Quantity // 99th percentile
	Samples int               // Number of data points used to compute percentiles
}

// MetricsError represents an error from the metrics provider.
type MetricsError struct {
	Message string
}

func (e *MetricsError) Error() string {
	return e.Message
}
