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

	// Prometheus contains Prometheus-specific configuration (optional)
	Prometheus PrometheusConfig

	// Clientset is the Kubernetes clientset (required if Type is metrics-server)
	Clientset kubernetes.Interface

	// MetricsClientset is the metrics clientset (required if Type is metrics-server)
	MetricsClientset metricsclientset.Interface

	// MaxSamples is the maximum number of samples to collect (optional, defaults to 10 for production)
	// For e2e tests, use 3 for faster execution
	MaxSamples int

	// SampleInterval is the interval between samples (optional, defaults to 15 seconds)
	SampleInterval int // in seconds

	// MetricsServerSamplingInterval controls background sampling cadence (metrics-server only).
	MetricsServerSamplingInterval time.Duration

	// MetricsServerMaxSamplesPerTarget caps stored samples per target (metrics-server only).
	MetricsServerMaxSamplesPerTarget int

	// MetricsServerMinSamplesRequired is the minimum samples required before recommendations (metrics-server only).
	MetricsServerMinSamplesRequired int

	// MetricsServerTargetTTL evicts targets that have not been refreshed (metrics-server only).
	MetricsServerTargetTTL time.Duration
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
		samplingConfig := SamplingConfig{
			Interval:   config.MetricsServerSamplingInterval,
			MaxSamples: config.MetricsServerMaxSamplesPerTarget,
			MinSamples: config.MetricsServerMinSamplesRequired,
			TargetTTL:  config.MetricsServerTargetTTL,
		}

		if samplingConfig.Interval == 0 && config.SampleInterval > 0 {
			samplingConfig.Interval = time.Duration(config.SampleInterval) * time.Second
		}
		if samplingConfig.MaxSamples == 0 && config.MaxSamples > 0 {
			samplingConfig.MaxSamples = config.MaxSamples
		}

		return NewMetricsServerProviderWithConfig(
			config.MetricsClientset,
			samplingConfig,
		), nil

	case ProviderTypePrometheus:
		if config.PrometheusURL == "" && config.Prometheus.URL == "" {
			return nil, fmt.Errorf("prometheus URL is required for prometheus provider")
		}

		// Use Prometheus config if provided, otherwise create from URL
		promConfig := config.Prometheus
		if promConfig.URL == "" {
			promConfig.URL = config.PrometheusURL
		}

		provider, err := NewPrometheusProvider(promConfig)
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
