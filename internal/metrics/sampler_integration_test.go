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
	"errors"
	"testing"
	"time"

	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// TestSamplerIntegrationLifecycle tests the sampler lifecycle
func TestSamplerIntegrationLifecycle(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   50 * time.Millisecond,
		MaxSamples: 100,
		MinSamples: 5,
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)
	sampler := provider.Sampler()

	// Test 1: Start and stop sampler
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- sampler.Start(ctx)
	}()

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop sampler
	cancel()

	// Wait for sampler to stop
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("sampler returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("sampler did not stop within timeout")
	}
}

// TestSamplerIntegrationTargetRegistration tests target registration
func TestSamplerIntegrationTargetRegistration(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   time.Second, // Slow interval to avoid rapid sampling
		MaxSamples: 100,
		MinSamples: 3,
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)

	// Register multiple targets
	key1 := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "app1",
		Container:    "web",
	}
	key2 := TargetKey{
		Namespace:    "system",
		WorkloadKind: "StatefulSet",
		WorkloadName: "db",
		Container:    "postgres",
	}

	provider.RegisterTarget(key1, "app1-pod")
	provider.RegisterTarget(key2, "db-pod-0")

	// Verify insufficient samples initially
	_, err := provider.GetContainerMetrics(context.Background(), "default", "app1-pod", "web", time.Hour)
	var insufficientErr *ErrInsufficientSamples
	if !errors.As(err, &insufficientErr) {
		t.Errorf("expected ErrInsufficientSamples for new target, got %T: %v", err, err)
	}

	_, err = provider.GetContainerMetrics(context.Background(), "system", "db-pod-0", "postgres", time.Hour)
	if !errors.As(err, &insufficientErr) {
		t.Errorf("expected ErrInsufficientSamples for new target, got %T: %v", err, err)
	}
}

// TestSamplerIntegrationErrorHandling tests error handling during sampling
func TestSamplerIntegrationErrorHandling(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   50 * time.Millisecond,
		MaxSamples: 100,
		MinSamples: 1,
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)
	sampler := provider.Sampler()

	// Start sampler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := sampler.Start(ctx); err != nil {
			t.Errorf("Failed to start sampler: %v", err)
		}
	}()

	// Register target for non-existent pod
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "missing-app",
		Container:    "web",
	}
	provider.RegisterTarget(key, "non-existent-pod")

	// Wait for sampling attempts
	time.Sleep(200 * time.Millisecond)

	// Verify GetContainerMetrics returns appropriate error
	_, err := provider.GetContainerMetrics(
		context.Background(),
		"default",
		"non-existent-pod",
		"web",
		time.Hour,
	)

	var insufficientErr *ErrInsufficientSamples
	if !errors.As(err, &insufficientErr) {
		t.Errorf("expected ErrInsufficientSamples for missing samples, got %T: %v", err, err)
	}

	if insufficientErr.Have != 0 {
		t.Errorf("expected 0 samples for non-existent pod, got %d", insufficientErr.Have)
	}
}

// TestSamplerIntegrationRollingWindow tests rolling window behavior with manual samples
func TestSamplerIntegrationRollingWindow(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   time.Second,
		MaxSamples: 100,
		MinSamples: 3,
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)

	// Register target
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "test-app",
		Container:    "web",
	}
	provider.RegisterTarget(key, "test-app-pod")

	// Create a test provider with direct sample injection for rolling window testing
	now := time.Now()

	testProvider := &testMetricsProvider{
		samples: map[TargetKey][]Sample{
			key: {
				{Timestamp: now.Add(-10 * time.Minute), CPUMilli: 100, MemoryByte: 100 * 1024 * 1024},
				{Timestamp: now.Add(-8 * time.Minute), CPUMilli: 200, MemoryByte: 200 * 1024 * 1024},
				{Timestamp: now.Add(-6 * time.Minute), CPUMilli: 300, MemoryByte: 300 * 1024 * 1024},
				{Timestamp: now.Add(-4 * time.Minute), CPUMilli: 400, MemoryByte: 400 * 1024 * 1024},
				{Timestamp: now.Add(-2 * time.Minute), CPUMilli: 500, MemoryByte: 500 * 1024 * 1024},
				{Timestamp: now, CPUMilli: 600, MemoryByte: 600 * 1024 * 1024},
			},
		},
		minSamples: 3,
	}

	// Test 1: 5-minute rolling window should only include last 3 samples
	metrics, err := testProvider.GetContainerMetrics(
		context.Background(),
		"default",
		"test-app-pod",
		"web",
		5*time.Minute,
	)

	if err != nil {
		t.Fatalf("unexpected error with 5-minute window: %v", err)
	}

	if metrics.CPU.Samples != 3 {
		t.Errorf("expected 3 samples in 5-minute window, got %d", metrics.CPU.Samples)
	}

	// Test 2: 15-minute rolling window should include all samples
	metrics, err = testProvider.GetContainerMetrics(
		context.Background(),
		"default",
		"test-app-pod",
		"web",
		15*time.Minute,
	)

	if err != nil {
		t.Fatalf("unexpected error with 15-minute window: %v", err)
	}

	if metrics.CPU.Samples != 6 {
		t.Errorf("expected 6 samples in 15-minute window, got %d", metrics.CPU.Samples)
	}
}

// testMetricsProvider is a test double that allows direct sample injection
type testMetricsProvider struct {
	samples    map[TargetKey][]Sample
	minSamples int
}

func (t *testMetricsProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*ContainerMetrics, error) {
	// Find the target key by matching namespace and container
	var matchedKey TargetKey
	var found bool
	for key := range t.samples {
		if key.Namespace == namespace && key.Container == containerName {
			matchedKey = key
			found = true
			break
		}
	}

	if !found {
		return nil, &ErrInsufficientSamples{Have: 0, Need: t.minSamples}
	}

	// Filter samples by time window
	cutoff := time.Now().Add(-window)
	var filteredSamples []Sample
	for _, sample := range t.samples[matchedKey] {
		if !sample.Timestamp.Before(cutoff) {
			filteredSamples = append(filteredSamples, sample)
		}
	}

	if len(filteredSamples) < t.minSamples {
		return nil, &ErrInsufficientSamples{Have: len(filteredSamples), Need: t.minSamples}
	}

	// Compute percentiles
	cpuSamples := make([]int64, len(filteredSamples))
	memorySamples := make([]int64, len(filteredSamples))
	for i, sample := range filteredSamples {
		cpuSamples[i] = sample.CPUMilli
		memorySamples[i] = sample.MemoryByte
	}

	return &ContainerMetrics{
		CPU:    computePercentiles(cpuSamples, true),
		Memory: computePercentiles(memorySamples, false),
	}, nil
}

func (t *testMetricsProvider) HealthCheck(ctx context.Context) error {
	return nil
}
