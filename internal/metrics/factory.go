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
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ProviderType represents the type of metrics provider.
type ProviderType string

const (
	// ProviderTypeMetricsServer uses Kubernetes metrics-server
	ProviderTypeMetricsServer ProviderType = "metrics-server"

	// ProviderTypePrometheus uses Prometheus
	ProviderTypePrometheus ProviderType = "prometheus"
)

// ProviderConfig contains configuration for creating a metrics provider.
type ProviderConfig struct {
	// Type specifies which provider to use
	Type ProviderType

	// PrometheusURL is the URL for Prometheus (required if Type is prometheus)
	PrometheusURL string

	// Clientset is the Kubernetes clientset (required if Type is metrics-server)
	Clientset kubernetes.Interface

	// MetricsClientset is the metrics clientset (required if Type is metrics-server)
	MetricsClientset metricsclientset.Interface

	// MaxSamples is the maximum number of samples to collect (optional, defaults to 10 for production)
	// For e2e tests, use 3 for faster execution
	MaxSamples int

	// SampleInterval is the interval between samples (optional, defaults to 15 seconds)
	SampleInterval int // in seconds
}

// NewProvider creates a new MetricsProvider based on the configuration.
// It returns an error if the provider cannot be initialized, with fallback
// handling delegated to the caller.
func NewProvider(config ProviderConfig) (MetricsProvider, error) {
	switch config.Type {
	case ProviderTypeMetricsServer:
		if config.Clientset == nil {
			return nil, fmt.Errorf("clientset is required for metrics-server provider")
		}
		if config.MetricsClientset == nil {
			return nil, fmt.Errorf("metrics clientset is required for metrics-server provider")
		}

		// Use custom configuration if provided, otherwise use defaults
		if config.MaxSamples > 0 || config.SampleInterval > 0 {
			maxSamples := config.MaxSamples
			if maxSamples == 0 {
				maxSamples = 10 // default
			}
			sampleInterval := config.SampleInterval
			if sampleInterval == 0 {
				sampleInterval = 15 // default 15 seconds
			}
			return NewMetricsServerProviderWithConfig(
				config.Clientset,
				config.MetricsClientset,
				maxSamples,
				time.Duration(sampleInterval)*time.Second,
			), nil
		}

		return NewMetricsServerProvider(config.Clientset, config.MetricsClientset), nil

	case ProviderTypePrometheus:
		if config.PrometheusURL == "" {
			return nil, fmt.Errorf("prometheus URL is required for prometheus provider")
		}
		provider, err := NewPrometheusProvider(config.PrometheusURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create prometheus provider: %w", err)
		}
		return provider, nil

	default:
		return nil, fmt.Errorf("unknown provider type: %s", config.Type)
	}
}

// NewProviderWithFallback creates a metrics provider with fallback support.
// If the primary provider fails to initialize, it attempts to create a fallback provider.
// Returns an error only if both primary and fallback fail.
func NewProviderWithFallback(primary, fallback ProviderConfig) (MetricsProvider, error) {
	// Try primary provider
	provider, err := NewProvider(primary)
	if err == nil {
		return provider, nil
	}

	primaryErr := err

	// Try fallback provider
	provider, err = NewProvider(fallback)
	if err == nil {
		// Log that we're using fallback (in production, use proper logging)
		fmt.Printf("Primary provider failed (%v), using fallback provider\n", primaryErr)
		return provider, nil
	}

	// Both failed
	return nil, fmt.Errorf("primary provider failed (%v) and fallback provider failed (%v)", primaryErr, err)
}
