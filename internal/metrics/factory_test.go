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
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"k8s.io/client-go/kubernetes/fake"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// Feature: k8s-workload-rightsizing, Property 30: Metrics provider configurability
// Validates: Requirements 15.2, 15.3
//
// Property: For any new metrics provider implementation, the system should allow
// configuration to select it without code changes to the core controller, and should
// log clear errors with safe fallback if initialization fails.
func TestProperty_ProviderConfigurability(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("valid configurations create providers successfully", prop.ForAll(
		func(providerType string, prometheusURL string) bool {
			var config ProviderConfig

			switch providerType {
			case "metrics-server": //nolint:goconst // Testing against existing constant
				config = ProviderConfig{
					Type:             "metrics-server",
					Clientset:        fake.NewSimpleClientset(),
					MetricsClientset: metricsfake.NewSimpleClientset(),
				}
			case "prometheus": //nolint:goconst // Testing against existing constant
				// Use a valid URL format
				if prometheusURL == "" {
					prometheusURL = "http://prometheus:9090"
				}
				config = ProviderConfig{
					Type:          "prometheus",
					PrometheusURL: prometheusURL,
				}
			default:
				// Invalid provider type should fail
				config = ProviderConfig{
					Type: ProviderType(providerType),
				}
				_, err := NewProvider(config)
				return err != nil // Should return an error
			}

			provider, err := NewProvider(config)

			// Valid configurations should succeed
			if providerType == "metrics-server" || providerType == "prometheus" {
				return err == nil && provider != nil
			}

			// Invalid configurations should fail gracefully
			return err != nil
		},
		gen.OneConstOf("metrics-server", "prometheus", "invalid", "unknown"),
		gen.OneConstOf("http://prometheus:9090", "http://localhost:9090", ""),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 30: Metrics provider configurability
// Validates: Requirements 15.2, 15.3
//
// Property: When a metrics provider fails to initialize, the system should return
// a clear error message indicating the failure reason.
func TestProperty_ProviderInitializationErrors(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("missing required config returns clear error", prop.ForAll(
		func(includeClientset bool, includeMetricsClientset bool) bool {
			config := ProviderConfig{
				Type: "metrics-server",
			}

			if includeClientset {
				config.Clientset = fake.NewSimpleClientset()
			}
			if includeMetricsClientset {
				config.MetricsClientset = metricsfake.NewSimpleClientset()
			}

			provider, err := NewProvider(config)

			// If both are provided, should succeed
			if includeClientset && includeMetricsClientset {
				return err == nil && provider != nil
			}

			// If either is missing, should fail with clear error
			return err != nil && provider == nil
		},
		gen.Bool(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 30: Metrics provider configurability
// Validates: Requirements 15.2, 15.3
//
// Property: The fallback mechanism should successfully create a provider when
// the primary fails but the fallback is valid.
func TestProperty_FallbackMechanism(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("fallback succeeds when primary fails", prop.ForAll(
		func(primaryValid bool, fallbackValid bool) bool {
			var primary, fallback ProviderConfig

			if primaryValid {
				primary = ProviderConfig{
					Type:             "metrics-server",
					Clientset:        fake.NewSimpleClientset(),
					MetricsClientset: metricsfake.NewSimpleClientset(),
				}
			} else {
				// Invalid primary (missing required fields)
				primary = ProviderConfig{
					Type: "metrics-server",
				}
			}

			if fallbackValid {
				fallback = ProviderConfig{
					Type:          "prometheus",
					PrometheusURL: "http://prometheus:9090",
				}
			} else {
				// Invalid fallback (missing required fields)
				fallback = ProviderConfig{
					Type: "prometheus",
				}
			}

			provider, err := NewProviderWithFallback(primary, fallback)

			// If primary is valid, should succeed
			if primaryValid {
				return err == nil && provider != nil
			}

			// If primary is invalid but fallback is valid, should succeed
			if !primaryValid && fallbackValid {
				return err == nil && provider != nil
			}

			// If both are invalid, should fail
			if !primaryValid && !fallbackValid {
				return err != nil && provider == nil
			}

			return true
		},
		gen.Bool(),
		gen.Bool(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 30: Metrics provider configurability
// Validates: Requirements 15.2, 15.3
//
// Property: Provider creation should be deterministic - same config should always
// produce the same result (success or specific error).
func TestProperty_ProviderCreationDeterminism(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("same config produces consistent results", prop.ForAll(
		func(providerType string) bool {
			var config ProviderConfig

			switch providerType {
			case "metrics-server": //nolint:goconst // Testing against existing constant
				config = ProviderConfig{
					Type:             "metrics-server",
					Clientset:        fake.NewSimpleClientset(),
					MetricsClientset: metricsfake.NewSimpleClientset(),
				}
			case "prometheus": //nolint:goconst // Testing against existing constant
				config = ProviderConfig{
					Type:          "prometheus",
					PrometheusURL: "http://prometheus:9090",
				}
			default:
				config = ProviderConfig{
					Type: ProviderType(providerType),
				}
			}

			// Create provider twice with same config
			provider1, err1 := NewProvider(config)
			provider2, err2 := NewProvider(config)

			// Both should succeed or both should fail
			if err1 != nil && err2 != nil {
				return true // Both failed consistently
			}
			if err1 == nil && err2 == nil {
				return provider1 != nil && provider2 != nil // Both succeeded
			}

			return false // Inconsistent results
		},
		gen.OneConstOf("metrics-server", "prometheus", "invalid"),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
