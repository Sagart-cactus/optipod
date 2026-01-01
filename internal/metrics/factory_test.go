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
	"time"

	"k8s.io/client-go/kubernetes/fake"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// TestNewProviderMetricsServer tests creating a metrics-server provider
func TestNewProviderMetricsServer(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	fakeMetricsClientset := metricsfake.NewSimpleClientset()

	config := ProviderConfig{
		Type:                             ProviderTypeMetricsServer,
		Clientset:                        fakeClientset,
		MetricsClientset:                 fakeMetricsClientset,
		MetricsServerSamplingInterval:    30 * time.Second,
		MetricsServerMaxSamplesPerTarget: 1000,
		MetricsServerMinSamplesRequired:  5,
		MetricsServerTargetTTL:           10 * time.Minute,
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create metrics-server provider: %v", err)
	}

	// Verify it's the right type
	msProvider, ok := provider.(*MetricsServerProvider)
	if !ok {
		t.Fatalf("expected *MetricsServerProvider, got %T", provider)
	}

	// Verify configuration was applied
	if msProvider.defaultMinSamples != 5 {
		t.Errorf("expected defaultMinSamples 5, got %d", msProvider.defaultMinSamples)
	}

	if msProvider.maxSamples != 1000 {
		t.Errorf("expected maxSamples 1000, got %d", msProvider.maxSamples)
	}

	// Verify it implements the required interfaces
	if _, ok := provider.(SamplingTargetRegistrar); !ok {
		t.Error("MetricsServerProvider should implement SamplingTargetRegistrar")
	}

	// Verify sampler is available
	if msProvider.Sampler() == nil {
		t.Error("expected non-nil sampler")
	}
}

// TestNewProviderMetricsServerDefaults tests default configuration
func TestNewProviderMetricsServerDefaults(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	fakeMetricsClientset := metricsfake.NewSimpleClientset()

	config := ProviderConfig{
		Type:             ProviderTypeMetricsServer,
		Clientset:        fakeClientset,
		MetricsClientset: fakeMetricsClientset,
		// No other config - should use defaults
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create metrics-server provider with defaults: %v", err)
	}

	msProvider := provider.(*MetricsServerProvider)

	// Verify defaults were applied
	if msProvider.defaultMinSamples != 10 {
		t.Errorf("expected default minSamples 10, got %d", msProvider.defaultMinSamples)
	}

	if msProvider.maxSamples != 2880 { // 24h @ 30s
		t.Errorf("expected default maxSamples 2880, got %d", msProvider.maxSamples)
	}

	if msProvider.interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", msProvider.interval)
	}
}

// TestNewProviderMetricsServerLegacyConfig tests backward compatibility with legacy config
func TestNewProviderMetricsServerLegacyConfig(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	fakeMetricsClientset := metricsfake.NewSimpleClientset()

	config := ProviderConfig{
		Type:             ProviderTypeMetricsServer,
		Clientset:        fakeClientset,
		MetricsClientset: fakeMetricsClientset,
		MaxSamples:       50, // Legacy field
		SampleInterval:   15, // Legacy field (seconds)
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create metrics-server provider with legacy config: %v", err)
	}

	msProvider := provider.(*MetricsServerProvider)

	// Verify legacy config was applied
	if msProvider.maxSamples != 50 {
		t.Errorf("expected maxSamples from legacy config 50, got %d", msProvider.maxSamples)
	}

	if msProvider.interval != 15*time.Second {
		t.Errorf("expected interval from legacy config 15s, got %v", msProvider.interval)
	}
}

// TestNewProviderMetricsServerMissingClientset tests error handling for missing clientset
func TestNewProviderMetricsServerMissingClientset(t *testing.T) {
	config := ProviderConfig{
		Type: ProviderTypeMetricsServer,
		// Missing Clientset and MetricsClientset
	}

	_, err := NewProvider(config)
	if err == nil {
		t.Fatal("expected error for missing clientset")
	}

	expectedMsg := "clientset is required for metrics-server provider"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

// TestNewProviderMetricsServerMissingMetricsClientset tests error handling for missing metrics clientset
func TestNewProviderMetricsServerMissingMetricsClientset(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()

	config := ProviderConfig{
		Type:      ProviderTypeMetricsServer,
		Clientset: fakeClientset,
		// Missing MetricsClientset
	}

	_, err := NewProvider(config)
	if err == nil {
		t.Fatal("expected error for missing metrics clientset")
	}

	expectedMsg := "metrics clientset is required for metrics-server provider"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

// TestNewProviderPrometheus tests creating a prometheus provider
func TestNewProviderPrometheus(t *testing.T) {
	config := ProviderConfig{
		Type:          ProviderTypePrometheus,
		PrometheusURL: "http://prometheus:9090",
	}

	// This will fail because we don't have a real Prometheus server
	// but we can test the configuration validation
	_, err := NewProvider(config)

	// We expect this to fail with a connection error, not a config error
	// The error should be about connection/creation, not missing URL
	if err != nil && err.Error() == "prometheus URL is required for prometheus provider" {
		t.Error("should not get URL validation error when URL is provided")
	}

	// If it succeeds unexpectedly, that's also fine for this test
	// (might happen if there's actually a prometheus server running)
}

// TestNewProviderPrometheusMissingURL tests error handling for missing Prometheus URL
func TestNewProviderPrometheusMissingURL(t *testing.T) {
	config := ProviderConfig{
		Type: ProviderTypePrometheus,
		// Missing PrometheusURL
	}

	_, err := NewProvider(config)
	if err == nil {
		t.Fatal("expected error for missing prometheus URL")
	}

	expectedMsg := "prometheus URL is required for prometheus provider"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

// TestNewProviderUnknownType tests error handling for unknown provider type
func TestNewProviderUnknownType(t *testing.T) {
	config := ProviderConfig{
		Type: ProviderType("unknown"),
	}

	_, err := NewProvider(config)
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}

	expectedMsg := "unknown provider type: unknown"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

// TestNewProviderWithFallback tests fallback provider functionality
func TestNewProviderWithFallback(t *testing.T) {
	// Primary config that will fail (missing clientset)
	primaryConfig := ProviderConfig{
		Type: ProviderTypeMetricsServer,
		// Missing required fields
	}

	// Fallback config that will succeed
	fakeClientset := fake.NewSimpleClientset()
	fakeMetricsClientset := metricsfake.NewSimpleClientset()
	fallbackConfig := ProviderConfig{
		Type:             ProviderTypeMetricsServer,
		Clientset:        fakeClientset,
		MetricsClientset: fakeMetricsClientset,
	}

	provider, err := NewProviderWithFallback(primaryConfig, fallbackConfig)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}

	// Verify we got the fallback provider
	if _, ok := provider.(*MetricsServerProvider); !ok {
		t.Errorf("expected MetricsServerProvider from fallback, got %T", provider)
	}
}

// TestNewProviderWithFallbackBothFail tests when both primary and fallback fail
func TestNewProviderWithFallbackBothFail(t *testing.T) {
	// Both configs will fail
	primaryConfig := ProviderConfig{
		Type: ProviderTypeMetricsServer,
		// Missing required fields
	}

	fallbackConfig := ProviderConfig{
		Type: ProviderType("unknown"),
	}

	_, err := NewProviderWithFallback(primaryConfig, fallbackConfig)
	if err == nil {
		t.Fatal("expected error when both primary and fallback fail")
	}

	// Should mention both errors
	errMsg := err.Error()
	if !contains(errMsg, "primary provider failed") {
		t.Error("error should mention primary provider failure")
	}
	if !contains(errMsg, "fallback provider failed") {
		t.Error("error should mention fallback provider failure")
	}
}

// TestSamplingConfigDefaults tests SamplingConfig default handling
func TestSamplingConfigDefaults(t *testing.T) {
	fakeMetricsClientset := metricsfake.NewSimpleClientset()

	// Test with zero values in SamplingConfig
	provider := NewMetricsServerProviderWithConfig(fakeMetricsClientset, SamplingConfig{})

	// Verify defaults were applied
	if provider.defaultMinSamples != 10 {
		t.Errorf("expected default minSamples 10, got %d", provider.defaultMinSamples)
	}

	if provider.maxSamples != 2880 {
		t.Errorf("expected default maxSamples 2880, got %d", provider.maxSamples)
	}

	if provider.interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", provider.interval)
	}
}

// TestSamplingConfigCustomValues tests SamplingConfig with custom values
func TestSamplingConfigCustomValues(t *testing.T) {
	fakeMetricsClientset := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   15 * time.Second,
		MaxSamples: 500,
		MinSamples: 3,
		TargetTTL:  5 * time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeMetricsClientset, config)

	// Verify custom values were applied
	if provider.defaultMinSamples != 3 {
		t.Errorf("expected minSamples 3, got %d", provider.defaultMinSamples)
	}

	if provider.maxSamples != 500 {
		t.Errorf("expected maxSamples 500, got %d", provider.maxSamples)
	}

	if provider.interval != 15*time.Second {
		t.Errorf("expected interval 15s, got %v", provider.interval)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsAt(s, substr))))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
