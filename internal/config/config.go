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

package config

import (
	"flag"
	"os"
	"time"
)

// OperatorConfig holds global configuration for the OptiPod operator
type OperatorConfig struct {
	// DryRun enables global dry-run mode where recommendations are computed but never applied
	DryRun bool

	// DefaultMetricsProvider specifies the default metrics backend to use
	DefaultMetricsProvider string

	// PrometheusURL is the URL for the Prometheus server (if using Prometheus provider)
	PrometheusURL string

	// Prometheus authentication configuration
	PrometheusAuthType    string
	PrometheusUsername    string
	PrometheusPassword    string
	PrometheusBearerToken string
	PrometheusTLSCAFile   string
	PrometheusTLSCertFile string
	PrometheusTLSKeyFile  string
	PrometheusTLSInsecure bool
	PrometheusTimeout     time.Duration

	// LeaderElection enables leader election for high availability
	LeaderElection bool

	// MetricsAddr is the address for the metrics endpoint
	MetricsAddr string

	// ProbeAddr is the address for health/readiness probes
	ProbeAddr string

	// ReconciliationInterval is the default interval for policy reconciliation
	ReconciliationInterval time.Duration

	// MetricsMaxSamples is the maximum number of samples to collect for metrics (0 = use default)
	MetricsMaxSamples int

	// MetricsSampleInterval is the interval between samples in seconds (0 = use default)
	MetricsSampleInterval int

	// MetricsServerSamplingInterval is the background sampling cadence for metrics-server.
	MetricsServerSamplingInterval time.Duration

	// MetricsServerMaxSamplesPerTarget caps cached samples per target for metrics-server.
	MetricsServerMaxSamplesPerTarget int

	// MetricsServerMinSamplesRequired is the minimum samples needed before recommendations.
	MetricsServerMinSamplesRequired int

	// MetricsServerTargetTTL evicts inactive targets from the metrics-server cache.
	MetricsServerTargetTTL time.Duration
}

// NewOperatorConfig creates a new OperatorConfig with default values
func NewOperatorConfig() *OperatorConfig {
	return &OperatorConfig{
		DryRun:                           false,
		DefaultMetricsProvider:           "metrics-server",
		PrometheusURL:                    "http://prometheus:9090",
		PrometheusAuthType:               "none",
		PrometheusUsername:               "",
		PrometheusPassword:               "",
		PrometheusBearerToken:            "",
		PrometheusTLSCAFile:              "",
		PrometheusTLSCertFile:            "",
		PrometheusTLSKeyFile:             "",
		PrometheusTLSInsecure:            false,
		PrometheusTimeout:                30 * time.Second,
		LeaderElection:                   false,
		MetricsAddr:                      ":8080",
		ProbeAddr:                        ":8081",
		ReconciliationInterval:           5 * time.Minute,
		MetricsMaxSamples:                0, // 0 = use default (10 for production)
		MetricsSampleInterval:            0, // 0 = use default (15 seconds)
		MetricsServerSamplingInterval:    5 * time.Minute,
		MetricsServerMaxSamplesPerTarget: 2880,
		MetricsServerMinSamplesRequired:  10,
		MetricsServerTargetTTL:           15 * time.Minute,
	}
}

// BindFlags binds configuration options to command-line flags
func (c *OperatorConfig) BindFlags() {
	flag.BoolVar(&c.DryRun, "dry-run", c.DryRun,
		"Enable global dry-run mode. When enabled, OptiPod computes recommendations but never applies them.")
	flag.StringVar(&c.DefaultMetricsProvider, "metrics-provider", c.DefaultMetricsProvider,
		"Default metrics provider to use (metrics-server, prometheus, or custom)")
	flag.StringVar(&c.PrometheusURL, "prometheus-url", c.PrometheusURL,
		"URL for Prometheus server (used when metrics-provider is prometheus)")
	flag.StringVar(&c.PrometheusAuthType, "prometheus-auth-type", c.PrometheusAuthType,
		"Prometheus authentication type: none, basic, bearer")
	flag.StringVar(&c.PrometheusUsername, "prometheus-username", c.PrometheusUsername,
		"Prometheus basic auth username (or use PROMETHEUS_USERNAME env)")
	flag.StringVar(&c.PrometheusPassword, "prometheus-password", c.PrometheusPassword,
		"Prometheus basic auth password (or use PROMETHEUS_PASSWORD env)")
	flag.StringVar(&c.PrometheusBearerToken, "prometheus-bearer-token", c.PrometheusBearerToken,
		"Prometheus bearer token (or use PROMETHEUS_BEARER_TOKEN env)")
	flag.StringVar(&c.PrometheusTLSCAFile, "prometheus-tls-ca-file", c.PrometheusTLSCAFile,
		"Prometheus TLS CA certificate file")
	flag.StringVar(&c.PrometheusTLSCertFile, "prometheus-tls-cert-file", c.PrometheusTLSCertFile,
		"Prometheus TLS client certificate file")
	flag.StringVar(&c.PrometheusTLSKeyFile, "prometheus-tls-key-file", c.PrometheusTLSKeyFile,
		"Prometheus TLS client key file")
	flag.BoolVar(&c.PrometheusTLSInsecure, "prometheus-tls-insecure-skip-verify", c.PrometheusTLSInsecure,
		"Skip Prometheus TLS certificate verification (not recommended for production)")
	flag.DurationVar(&c.PrometheusTimeout, "prometheus-timeout", c.PrometheusTimeout,
		"Prometheus HTTP client timeout")
	flag.BoolVar(&c.LeaderElection, "leader-elect", c.LeaderElection,
		"Enable leader election for controller manager")
	flag.DurationVar(&c.ReconciliationInterval, "reconciliation-interval", c.ReconciliationInterval,
		"Default interval for policy reconciliation")
	flag.IntVar(&c.MetricsMaxSamples, "metrics-max-samples", c.MetricsMaxSamples,
		"Deprecated: inline metrics-server sampling cap (0 = default). Use metrics-server-max-samples-per-target instead.")
	flag.IntVar(&c.MetricsSampleInterval, "metrics-sample-interval", c.MetricsSampleInterval,
		"Deprecated: inline metrics-server sampling interval in seconds (0 = default). Use metrics-server-sampling-interval instead.")
	flag.DurationVar(&c.MetricsServerSamplingInterval, "metrics-server-sampling-interval", c.MetricsServerSamplingInterval,
		"Background sampling interval for metrics-server (e.g. 30s)")
	flag.IntVar(&c.MetricsServerMaxSamplesPerTarget, "metrics-server-max-samples-per-target", c.MetricsServerMaxSamplesPerTarget,
		"Maximum cached samples per target for metrics-server (e.g. 2880 for 24h @ 30s)")
	flag.IntVar(&c.MetricsServerMinSamplesRequired, "metrics-server-min-samples-required", c.MetricsServerMinSamplesRequired,
		"Minimum samples required before recommendations are computed (metrics-server)")
	flag.DurationVar(&c.MetricsServerTargetTTL, "metrics-server-target-ttl", c.MetricsServerTargetTTL,
		"Evict metrics-server sampling targets if not refreshed within this TTL (e.g. 15m)")
}

// IsDryRun returns true if global dry-run mode is enabled
func (c *OperatorConfig) IsDryRun() bool {
	return c.DryRun
}

// GetMetricsProvider returns the configured metrics provider type
func (c *OperatorConfig) GetMetricsProvider() string {
	return c.DefaultMetricsProvider
}

// GetPrometheusURL returns the Prometheus URL
func (c *OperatorConfig) GetPrometheusURL() string {
	return c.PrometheusURL
}

// IsLeaderElectionEnabled returns true if leader election is enabled
func (c *OperatorConfig) IsLeaderElectionEnabled() bool {
	return c.LeaderElection
}

// GetReconciliationInterval returns the default reconciliation interval
func (c *OperatorConfig) GetReconciliationInterval() time.Duration {
	return c.ReconciliationInterval
}

// GetMetricsMaxSamples returns the maximum number of samples to collect
func (c *OperatorConfig) GetMetricsMaxSamples() int {
	return c.MetricsMaxSamples
}

// GetMetricsSampleInterval returns the interval between samples in seconds
func (c *OperatorConfig) GetMetricsSampleInterval() int {
	return c.MetricsSampleInterval
}

// GetMetricsServerSamplingInterval returns the metrics-server sampling interval.
func (c *OperatorConfig) GetMetricsServerSamplingInterval() time.Duration {
	return c.MetricsServerSamplingInterval
}

// GetMetricsServerMaxSamplesPerTarget returns the max cached samples per target.
func (c *OperatorConfig) GetMetricsServerMaxSamplesPerTarget() int {
	return c.MetricsServerMaxSamplesPerTarget
}

// GetMetricsServerMinSamplesRequired returns the minimum samples required.
func (c *OperatorConfig) GetMetricsServerMinSamplesRequired() int {
	return c.MetricsServerMinSamplesRequired
}

// GetMetricsServerTargetTTL returns the target eviction TTL.
func (c *OperatorConfig) GetMetricsServerTargetTTL() time.Duration {
	return c.MetricsServerTargetTTL
}

// GetPrometheusAuthType returns the Prometheus authentication type.
func (c *OperatorConfig) GetPrometheusAuthType() string {
	return c.PrometheusAuthType
}

// GetPrometheusUsername returns the Prometheus username (from flag or env).
func (c *OperatorConfig) GetPrometheusUsername() string {
	if c.PrometheusUsername != "" {
		return c.PrometheusUsername
	}
	return os.Getenv("PROMETHEUS_USERNAME")
}

// GetPrometheusPassword returns the Prometheus password (from flag or env).
func (c *OperatorConfig) GetPrometheusPassword() string {
	if c.PrometheusPassword != "" {
		return c.PrometheusPassword
	}
	return os.Getenv("PROMETHEUS_PASSWORD")
}

// GetPrometheusBearerToken returns the Prometheus bearer token (from flag or env).
func (c *OperatorConfig) GetPrometheusBearerToken() string {
	if c.PrometheusBearerToken != "" {
		return c.PrometheusBearerToken
	}
	return os.Getenv("PROMETHEUS_BEARER_TOKEN")
}

// GetPrometheusTLSCAFile returns the Prometheus TLS CA file path.
func (c *OperatorConfig) GetPrometheusTLSCAFile() string {
	return c.PrometheusTLSCAFile
}

// GetPrometheusTLSCertFile returns the Prometheus TLS cert file path.
func (c *OperatorConfig) GetPrometheusTLSCertFile() string {
	return c.PrometheusTLSCertFile
}

// GetPrometheusTLSKeyFile returns the Prometheus TLS key file path.
func (c *OperatorConfig) GetPrometheusTLSKeyFile() string {
	return c.PrometheusTLSKeyFile
}

// GetPrometheusTLSInsecure returns whether to skip TLS verification.
func (c *OperatorConfig) GetPrometheusTLSInsecure() bool {
	return c.PrometheusTLSInsecure
}

// GetPrometheusTimeout returns the Prometheus HTTP client timeout.
func (c *OperatorConfig) GetPrometheusTimeout() time.Duration {
	return c.PrometheusTimeout
}
