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

package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/optipod/optipod/internal/metrics"
)

// These tests complement the existing property-based cache checks with
// deterministic fault and recovery cases (VAL-FAST-007): provider errors are
// never cached, expired entries are always re-fetched, and invalidation forces
// fresh provider calls for every container.

// faultingProvider is a controllable production-interface provider whose
// failure state and returned data can be flipped mid-test.
type faultingProvider struct {
	failing    bool
	getCalls   int
	windowSeen []time.Duration
}

func (p *faultingProvider) GetContainerMetrics(
	ctx context.Context,
	namespace, podName, containerName string,
	window time.Duration,
) (*metrics.ContainerMetrics, error) {
	p.getCalls++
	p.windowSeen = append(p.windowSeen, window)
	if p.failing {
		return nil, errors.New("metrics backend unavailable")
	}
	return &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P50: resource.MustParse("100m"),
			P90: resource.MustParse("250m"),
			P99: resource.MustParse("400m"),
		},
		Memory: metrics.ResourceMetrics{
			P50: resource.MustParse("128Mi"),
			P90: resource.MustParse("250Mi"),
			P99: resource.MustParse("400Mi"),
		},
	}, nil
}

func (p *faultingProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// TestGetOrFetchDoesNotCacheProviderErrorsAndRecovers proves a failing fetch
// is surfaced (never cached as success) and the very next fetch through the
// same cache succeeds and caches once valid data returns (VAL-FAST-007).
func TestGetOrFetchDoesNotCacheProviderErrorsAndRecovers(t *testing.T) {
	ctx := context.Background()
	provider := &faultingProvider{failing: true}
	cache := NewMetricsCache(time.Hour)

	// Outage: the error propagates.
	if _, err := cache.GetOrFetch(ctx, provider, TestNamespace, TestPodName, TestContainerName, 24*time.Hour); err == nil {
		t.Fatal("expected the provider error to propagate through GetOrFetch")
	}

	// Recovery: valid data returns and is served to the next caller from
	// the cache (the provider is consulted only once more).
	provider.failing = false
	fetched, err := cache.GetOrFetch(ctx, provider, TestNamespace, TestPodName, TestContainerName, 24*time.Hour)
	if err != nil {
		t.Fatalf("GetOrFetch did not recover after valid data returned: %v", err)
	}
	if got := fetched.CPU.P90.MilliValue(); got != 250 {
		t.Errorf("recovered CPU P90 = %d milli, want 250 milli", got)
	}

	if _, err := cache.GetOrFetch(ctx, provider, TestNamespace, TestPodName, TestContainerName, 24*time.Hour); err != nil {
		t.Fatalf("cached read failed after recovery: %v", err)
	}
	if provider.getCalls != 2 {
		t.Errorf("provider was consulted %d times, want 2 (one failed attempt, one successful fetch)", provider.getCalls)
	}
}

// TestMetricsCacheExpiredEntryIsNeverServed proves a TTL-expired entry is
// treated as a miss so recovery data is always re-fetched.
func TestMetricsCacheExpiredEntryIsNeverServed(t *testing.T) {
	ctx := context.Background()
	provider := &faultingProvider{}
	// A negative TTL makes every entry expired deterministically (no sleeps).
	cache := NewMetricsCache(-time.Hour)

	cache.Set(TestNamespace, TestPodName, TestContainerName, &metrics.ContainerMetrics{})
	if _, found := cache.Get(TestNamespace, TestPodName, TestContainerName); found {
		t.Fatal("an expired entry must never be served from the cache")
	}

	if _, err := cache.GetOrFetch(ctx, provider, TestNamespace, TestPodName, TestContainerName, 24*time.Hour); err != nil {
		t.Fatalf("GetOrFetch failed: %v", err)
	}
	if _, err := cache.GetOrFetch(ctx, provider, TestNamespace, TestPodName, TestContainerName, 24*time.Hour); err != nil {
		t.Fatalf("second GetOrFetch failed: %v", err)
	}
	if provider.getCalls != 2 {
		t.Errorf("provider was consulted %d times for an always-expired entry, want 2", provider.getCalls)
	}
}

// TestInvalidateAllForcesRefetchOfEveryContainer proves InvalidateAll drops
// every container's entry so the next reads hit the provider again.
func TestInvalidateAllForcesRefetchOfEveryContainer(t *testing.T) {
	ctx := context.Background()
	provider := &faultingProvider{}
	cache := NewMetricsCache(time.Hour)

	containers := [][3]string{
		{TestNamespace, TestPodName, "app"},
		{TestNamespace, TestPodName, "worker"},
	}
	for _, c := range containers {
		if _, err := cache.GetOrFetch(ctx, provider, c[0], c[1], c[2], 24*time.Hour); err != nil {
			t.Fatalf("GetOrFetch failed: %v", err)
		}
	}
	if provider.getCalls != 2 {
		t.Fatalf("provider was consulted %d times, want 2", provider.getCalls)
	}

	// Cached reads do not touch the provider.
	for _, c := range containers {
		if _, err := cache.GetOrFetch(ctx, provider, c[0], c[1], c[2], 24*time.Hour); err != nil {
			t.Fatalf("cached GetOrFetch failed: %v", err)
		}
	}
	if provider.getCalls != 2 {
		t.Fatalf("cached reads consulted the provider %d times, want 2", provider.getCalls)
	}

	// InvalidateAll forces a fresh fetch for every container.
	cache.InvalidateAll()
	for _, c := range containers {
		if _, err := cache.GetOrFetch(ctx, provider, c[0], c[1], c[2], 24*time.Hour); err != nil {
			t.Fatalf("GetOrFetch after InvalidateAll failed: %v", err)
		}
	}
	if provider.getCalls != 4 {
		t.Errorf("provider was consulted %d times after InvalidateAll, want 4", provider.getCalls)
	}
}
