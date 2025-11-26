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

	// LeaderElection enables leader election for high availability
	LeaderElection bool

	// MetricsAddr is the address for the metrics endpoint
	MetricsAddr string

	// ProbeAddr is the address for health/readiness probes
	ProbeAddr string

	// ReconciliationInterval is the default interval for policy reconciliation
	ReconciliationInterval time.Duration
}

// NewOperatorConfig creates a new OperatorConfig with default values
func NewOperatorConfig() *OperatorConfig {
	return &OperatorConfig{
		DryRun:                 false,
		DefaultMetricsProvider: "metrics-server",
		PrometheusURL:          "http://prometheus:9090",
		LeaderElection:         false,
		MetricsAddr:            ":8080",
		ProbeAddr:              ":8081",
		ReconciliationInterval: 5 * time.Minute,
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
	flag.BoolVar(&c.LeaderElection, "leader-elect", c.LeaderElection,
		"Enable leader election for controller manager")
	flag.DurationVar(&c.ReconciliationInterval, "reconciliation-interval", c.ReconciliationInterval,
		"Default interval for policy reconciliation")
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
