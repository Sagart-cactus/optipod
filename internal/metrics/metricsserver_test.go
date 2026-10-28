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
	"sync"
	"testing"
	"time"

	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestSampleRingCapacity(t *testing.T) {
	ring := newSampleRing(2)
	now := time.Now()

	ring.append(Sample{Timestamp: now.Add(-2 * time.Minute), CPUMilli: 100})
	ring.append(Sample{Timestamp: now.Add(-1 * time.Minute), CPUMilli: 200})
	ring.append(Sample{Timestamp: now, CPUMilli: 300})

	values := ring.valuesSince(now.Add(-10 * time.Minute))
	if len(values) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(values))
	}
	if values[0].CPUMilli != 200 || values[1].CPUMilli != 300 {
		t.Fatalf("unexpected ring contents: %+v", values)
	}
}

func TestTimeSeriesStoreResolve(t *testing.T) {
	store := newTimeSeriesStore(5)
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "api",
		Container:    "app",
	}

	store.registerTarget(key, "api-123")

	resolved, ok := store.resolveTargetKey("default", "api-123", "app")
	if !ok {
		t.Fatalf("expected target key to resolve")
	}
	if resolved != key {
		t.Fatalf("resolved key mismatch: %+v", resolved)
	}
}

func TestSamplesSince(t *testing.T) {
	store := newTimeSeriesStore(5)
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "api",
		Container:    "app",
	}

	store.registerTarget(key, "api-123")

	now := time.Now()
	store.appendSample(key, Sample{Timestamp: now.Add(-5 * time.Minute), CPUMilli: 100})
	store.appendSample(key, Sample{Timestamp: now.Add(-2 * time.Minute), CPUMilli: 200})
	store.appendSample(key, Sample{Timestamp: now.Add(-1 * time.Minute), CPUMilli: 300})

	samples := store.samplesSince(key, now.Add(-3*time.Minute))
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
	if samples[0].CPUMilli != 200 || samples[1].CPUMilli != 300 {
		t.Fatalf("unexpected samples: %+v", samples)
	}
}

// TestPercentileComputation verifies that percentiles can be computed with small sample sizes
func TestPercentileComputation(t *testing.T) {
	tests := []struct {
		name    string
		samples []int64
		p       int
		want    int64
	}{
		{
			name:    "3 samples - P50",
			samples: []int64{100, 200, 300},
			p:       50,
			want:    200,
		},
		{
			name:    "3 samples - P90",
			samples: []int64{100, 200, 300},
			p:       90,
			want:    280, // Linear interpolation: 200 + 0.8 * (300-200)
		},
		{
			name:    "1 sample",
			samples: []int64{100},
			p:       90,
			want:    100,
		},
		{
			name:    "2 samples - P50",
			samples: []int64{100, 200},
			p:       50,
			want:    150, // Linear interpolation: 100 + 0.5 * (200-100)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.samples, tt.p)
			if got != tt.want {
				t.Errorf("percentile(%v, %d) = %d, want %d", tt.samples, tt.p, got, tt.want)
			}
		})
	}
}

// TestSampleRingConcurrency tests concurrent access to the ring buffer
func TestSampleRingConcurrency(t *testing.T) {
	ring := newSampleRing(100)
	now := time.Now()

	// Start multiple goroutines writing to the ring
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				sample := Sample{
					Timestamp:  now.Add(time.Duration(id*100+j) * time.Millisecond), // Ensure unique timestamps
					CPUMilli:   int64(id*10 + j),
					MemoryByte: int64((id*10 + j) * 1024),
				}
				ring.append(sample)
			}
		}(i)
	}

	wg.Wait()

	// Verify we have samples
	values := ring.valuesSince(now.Add(-time.Hour))
	if len(values) == 0 {
		t.Fatal("expected samples after concurrent writes")
	}

	// Note: Due to concurrent access and ring buffer overflow, we can't guarantee
	// strict chronological order, but we can verify basic functionality
	if len(values) > ring.count {
		t.Errorf("returned more values (%d) than ring capacity (%d)", len(values), ring.count)
	}
}

// TestTimeSeriesStoreConcurrency tests concurrent access to the time series store
func TestTimeSeriesStoreConcurrency(t *testing.T) {
	store := newTimeSeriesStore(50)
	now := time.Now()

	keys := []TargetKey{
		{Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "app1", Container: "web"},
		{Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "app2", Container: "api"},
		{Namespace: "system", WorkloadKind: "StatefulSet", WorkloadName: "db", Container: "postgres"},
	}

	var wg sync.WaitGroup

	// Concurrent target registration
	for i, key := range keys {
		wg.Add(1)
		go func(k TargetKey, id int) {
			defer wg.Done()
			store.registerTarget(k, k.WorkloadName+"-pod-"+string(rune('0'+id)))
		}(key, i)
	}

	// Concurrent sample appending
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(sampleID int) {
			defer wg.Done()
			key := keys[sampleID%len(keys)]
			sample := Sample{
				Timestamp:  now.Add(time.Duration(sampleID) * time.Second),
				CPUMilli:   int64(sampleID * 10),
				MemoryByte: int64(sampleID * 1024),
			}
			store.appendSample(key, sample)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, key := range keys {
				store.samplesSince(key, now.Add(-time.Hour))
			}
		}()
	}

	wg.Wait()

	// Verify all targets were registered
	targets := store.listTargets()
	if len(targets) != len(keys) {
		t.Errorf("expected %d targets, got %d", len(keys), len(targets))
	}
}

// TestTimeSeriesStoreEviction tests stale target eviction
func TestTimeSeriesStoreEviction(t *testing.T) {
	store := newTimeSeriesStore(10)

	key1 := TargetKey{Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "app1", Container: "web"}
	key2 := TargetKey{Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "app2", Container: "api"}

	// Register targets with different timestamps
	store.registerTarget(key1, "app1-pod")
	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps
	store.registerTarget(key2, "app2-pod")

	// Manually set lastSeen to simulate stale target
	store.mu.Lock()
	if target, ok := store.targets[key1]; ok {
		target.lastSeen = time.Now().Add(-time.Hour) // Make it stale
	}
	store.mu.Unlock()

	// Add samples to both targets
	now := time.Now()
	store.appendSample(key1, Sample{Timestamp: now, CPUMilli: 100, MemoryByte: 1024})
	store.appendSample(key2, Sample{Timestamp: now, CPUMilli: 200, MemoryByte: 2048})

	// Verify both targets exist before eviction
	targets := store.listTargets()
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets before eviction, got %d", len(targets))
	}

	// Evict stale targets (TTL of 30 minutes should evict key1)
	store.evictStaleTargets(30 * time.Minute)

	// Verify only key2 remains
	targets = store.listTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 target after eviction, got %d", len(targets))
	}
	if targets[0].key != key2 {
		t.Errorf("expected key2 to remain, got %+v", targets[0].key)
	}

	// Verify samples for key1 were also removed
	samples := store.samplesSince(key1, now.Add(-time.Hour))
	if len(samples) != 0 {
		t.Errorf("expected no samples for evicted target, got %d", len(samples))
	}

	// Verify samples for key2 still exist
	samples = store.samplesSince(key2, now.Add(-time.Hour))
	if len(samples) != 1 {
		t.Errorf("expected 1 sample for remaining target, got %d", len(samples))
	}
}

// TestMetricsServerSamplerLifecycle tests the sampler start/stop lifecycle
func TestMetricsServerSamplerLifecycle(t *testing.T) {
	// Create fake metrics client
	fakeClient := metricsfake.NewSimpleClientset()
	store := newTimeSeriesStore(10)
	sampler := NewMetricsServerSampler(fakeClient, store, 100*time.Millisecond, time.Minute)

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start sampler in background
	done := make(chan error, 1)
	go func() {
		done <- sampler.Start(ctx)
	}()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel context
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

// TestMetricsServerSamplerSampleOnce tests the sampling logic
func TestMetricsServerSamplerSampleOnce(t *testing.T) {
	// Create fake metrics client
	fakeClient := metricsfake.NewSimpleClientset()

	store := newTimeSeriesStore(10)
	sampler := NewMetricsServerSampler(fakeClient, store, time.Second, time.Minute)

	// Register a target
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "test-app",
		Container:    "web",
	}
	sampler.RegisterTarget(key, "test-pod")

	// Test that the sampler handles missing pods gracefully by attempting to sample
	// a non-existent pod (which will fail silently)
	sampler.sampleOnce(context.Background())

	// Since fake client doesn't have the pod, no samples will be collected
	// This tests that the sampler handles missing pods gracefully
	samples := store.samplesSince(key, time.Now().Add(-time.Hour))
	if len(samples) != 0 {
		t.Errorf("expected 0 samples with fake client, got %d", len(samples))
	}

	// Test that the target was registered
	targets := store.listTargets()
	if len(targets) != 1 {
		t.Errorf("expected 1 registered target, got %d", len(targets))
	}
}

// TestMetricsServerProviderInsufficientSamples tests insufficient samples error handling
func TestMetricsServerProviderInsufficientSamples(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   time.Second,
		MaxSamples: 100,
		MinSamples: 5, // Require 5 samples minimum
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)

	// Try to get metrics without any samples
	_, err := provider.GetContainerMetrics(
		context.Background(),
		"default",
		"test-pod",
		"web",
		time.Hour,
	)

	// Should get insufficient samples error
	var insufficientErr *ErrInsufficientSamples
	if !errors.As(err, &insufficientErr) {
		t.Fatalf("expected ErrInsufficientSamples, got %T: %v", err, err)
	}

	if insufficientErr.Have != 0 {
		t.Errorf("expected 0 samples, got %d", insufficientErr.Have)
	}
	if insufficientErr.Need != 5 {
		t.Errorf("expected need 5 samples, got %d", insufficientErr.Need)
	}
}

// TestMetricsServerProviderWithSamples tests normal operation with sufficient samples
func TestMetricsServerProviderWithSamples(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()

	config := SamplingConfig{
		Interval:   time.Second,
		MaxSamples: 100,
		MinSamples: 3, // Require 3 samples minimum
		TargetTTL:  time.Minute,
	}

	provider := NewMetricsServerProviderWithConfig(fakeClient, config)

	// Register target and add samples directly to the store
	key := TargetKey{
		Namespace:    "default",
		WorkloadKind: "Deployment",
		WorkloadName: "test-app",
		Container:    "web",
	}
	provider.RegisterTarget(key, "test-pod")

	// Add sufficient samples
	now := time.Now()
	samples := []Sample{
		{Timestamp: now.Add(-3 * time.Minute), CPUMilli: 100, MemoryByte: 100 * 1024 * 1024},
		{Timestamp: now.Add(-2 * time.Minute), CPUMilli: 200, MemoryByte: 200 * 1024 * 1024},
		{Timestamp: now.Add(-1 * time.Minute), CPUMilli: 300, MemoryByte: 300 * 1024 * 1024},
		{Timestamp: now, CPUMilli: 400, MemoryByte: 400 * 1024 * 1024},
	}

	for _, sample := range samples {
		provider.store.appendSample(key, sample)
	}

	// Get metrics
	metrics, err := provider.GetContainerMetrics(
		context.Background(),
		"default",
		"test-pod",
		"web",
		time.Hour,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify metrics were computed
	if metrics.CPU.Samples != 4 {
		t.Errorf("expected 4 CPU samples, got %d", metrics.CPU.Samples)
	}
	if metrics.Memory.Samples != 4 {
		t.Errorf("expected 4 memory samples, got %d", metrics.Memory.Samples)
	}

	// Verify P50 (median) values
	expectedCPU := int64(250) // Median of [100, 200, 300, 400]
	if metrics.CPU.P50.MilliValue() != expectedCPU {
		t.Errorf("expected CPU P50 %dm, got %dm", expectedCPU, metrics.CPU.P50.MilliValue())
	}
}

// TestMetricsServerProviderHealthCheck tests health check functionality
func TestMetricsServerProviderHealthCheck(t *testing.T) {
	fakeClient := metricsfake.NewSimpleClientset()
	provider := NewMetricsServerProvider(fakeClient)

	// Health check should succeed with fake client
	err := provider.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

// TestErrInsufficientSamplesError tests the error message formatting
func TestErrInsufficientSamplesError(t *testing.T) {
	err := &ErrInsufficientSamples{Have: 3, Need: 10}
	expected := "insufficient metrics samples: have 3, need 10"
	if err.Error() != expected {
		t.Errorf("expected error message %q, got %q", expected, err.Error())
	}
}
