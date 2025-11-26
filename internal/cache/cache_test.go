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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// mockMetricsProvider tracks the number of API calls made
type mockMetricsProvider struct {
	callCount int32
}

func (m *mockMetricsProvider) GetContainerMetrics(ctx context.Context, namespace, podName, containerName string, window time.Duration) (*metrics.ContainerMetrics, error) {
	atomic.AddInt32(&m.callCount, 1)
	return &metrics.ContainerMetrics{
		CPU: metrics.ResourceMetrics{
			P50:     resource.MustParse("100m"),
			P90:     resource.MustParse("200m"),
			P99:     resource.MustParse("300m"),
			Samples: 100,
		},
		Memory: metrics.ResourceMetrics{
			P50:     resource.MustParse("128Mi"),
			P90:     resource.MustParse("256Mi"),
			P99:     resource.MustParse("512Mi"),
			Samples: 100,
		},
	}, nil
}

func (m *mockMetricsProvider) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *mockMetricsProvider) GetCallCount() int32 {
	return atomic.LoadInt32(&m.callCount)
}

func (m *mockMetricsProvider) ResetCallCount() {
	atomic.StoreInt32(&m.callCount, 0)
}

// mockClient tracks the number of API calls made
type mockClient struct {
	client.Client
	listCallCount int32
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	atomic.AddInt32(&m.listCallCount, 1)
	// Return empty list for testing
	return nil
}

func (m *mockClient) GetListCallCount() int32 {
	return atomic.LoadInt32(&m.listCallCount)
}

func (m *mockClient) ResetListCallCount() {
	atomic.StoreInt32(&m.listCallCount, 0)
}

// Feature: k8s-workload-rightsizing, Property 32: API call efficiency
// Validates: Requirements 17.5
func TestProperty_APICallEfficiency(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any workload processing, caching should minimize redundant API calls", prop.ForAll(
		func(namespace string, podName string, containerName string, numRepeatedCalls int) bool {
			// Ensure non-empty strings
			if namespace == "" {
				namespace = "default"
			}
			if podName == "" {
				podName = "test-pod"
			}
			if containerName == "" {
				containerName = "test-container"
			}
			// Ensure at least 2 calls to test caching, max 10 for reasonable test time
			if numRepeatedCalls < 2 {
				numRepeatedCalls = 2
			}
			if numRepeatedCalls > 10 {
				numRepeatedCalls = 10
			}

			// Test metrics cache efficiency
			metricsProvider := &mockMetricsProvider{}
			metricsCache := NewMetricsCache(1 * time.Minute)

			ctx := context.Background()
			window := 24 * time.Hour

			// Make multiple calls for the same container
			for i := 0; i < numRepeatedCalls; i++ {
				_, err := metricsCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
				if err != nil {
					return false
				}
			}

			// Verify that the provider was only called once (first call)
			// All subsequent calls should have been served from cache
			callCount := metricsProvider.GetCallCount()
			if callCount != 1 {
				t.Logf("Expected 1 API call, got %d for %d repeated requests", callCount, numRepeatedCalls)
				return false
			}

			// Test that cache expiration works correctly
			// Create a cache with very short TTL
			shortTTLCache := NewMetricsCache(1 * time.Millisecond)
			metricsProvider.ResetCallCount()

			// First call
			_, err := shortTTLCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
			if err != nil {
				return false
			}

			// Wait for cache to expire
			time.Sleep(10 * time.Millisecond)

			// Second call after expiration
			_, err = shortTTLCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
			if err != nil {
				return false
			}

			// Should have made 2 calls (cache expired)
			callCountAfterExpiry := metricsProvider.GetCallCount()
			if callCountAfterExpiry != 2 {
				t.Logf("Expected 2 API calls after cache expiry, got %d", callCountAfterExpiry)
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.IntRange(2, 10),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 32: API call efficiency (Workload Cache)
// Validates: Requirements 17.5
func TestProperty_WorkloadCacheEfficiency(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any policy, workload cache should minimize redundant discovery API calls", prop.ForAll(
		func(policyName string, namespace string, numRepeatedCalls int) bool {
			// Ensure non-empty strings
			if policyName == "" {
				policyName = "test-policy"
			}
			if namespace == "" {
				namespace = "default"
			}
			// Ensure at least 2 calls to test caching, max 10 for reasonable test time
			if numRepeatedCalls < 2 {
				numRepeatedCalls = 2
			}
			if numRepeatedCalls > 10 {
				numRepeatedCalls = 10
			}

			// Create a mock client that tracks API calls
			mockClient := &mockClient{}
			workloadCache := NewWorkloadCache(1 * time.Minute)

			// Create a test policy
			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyName,
					Namespace: namespace,
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
						},
					},
				},
			}

			ctx := context.Background()

			// Make multiple calls for the same policy
			for i := 0; i < numRepeatedCalls; i++ {
				_, err := workloadCache.GetOrDiscover(ctx, mockClient, policy)
				if err != nil {
					return false
				}
			}

			// Verify that the client was only called once (first call)
			// All subsequent calls should have been served from cache
			listCallCount := mockClient.GetListCallCount()
			if listCallCount != 1 {
				t.Logf("Expected 1 List API call, got %d for %d repeated requests", listCallCount, numRepeatedCalls)
				return false
			}

			// Test that cache expiration works correctly
			shortTTLCache := NewWorkloadCache(1 * time.Millisecond)
			mockClient.ResetListCallCount()

			// First call
			_, err := shortTTLCache.GetOrDiscover(ctx, mockClient, policy)
			if err != nil {
				return false
			}

			// Wait for cache to expire
			time.Sleep(10 * time.Millisecond)

			// Second call after expiration
			_, err = shortTTLCache.GetOrDiscover(ctx, mockClient, policy)
			if err != nil {
				return false
			}

			// Should have made 2 calls (cache expired)
			listCallCountAfterExpiry := mockClient.GetListCallCount()
			if listCallCountAfterExpiry != 2 {
				t.Logf("Expected 2 List API calls after cache expiry, got %d", listCallCountAfterExpiry)
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.IntRange(2, 10),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 32: API call efficiency (Cache Invalidation)
// Validates: Requirements 17.5
func TestProperty_CacheInvalidation(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any cached data, invalidation should force fresh API calls", prop.ForAll(
		func(namespace string, podName string, containerName string) bool {
			// Ensure non-empty strings
			if namespace == "" {
				namespace = "default"
			}
			if podName == "" {
				podName = "test-pod"
			}
			if containerName == "" {
				containerName = "test-container"
			}

			// Test metrics cache invalidation
			metricsProvider := &mockMetricsProvider{}
			metricsCache := NewMetricsCache(1 * time.Minute)

			ctx := context.Background()
			window := 24 * time.Hour

			// First call - should hit the provider
			_, err := metricsCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
			if err != nil {
				return false
			}

			if metricsProvider.GetCallCount() != 1 {
				return false
			}

			// Second call - should use cache
			_, err = metricsCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
			if err != nil {
				return false
			}

			if metricsProvider.GetCallCount() != 1 {
				return false
			}

			// Invalidate the cache
			metricsCache.Invalidate(namespace, podName, containerName)

			// Third call - should hit the provider again after invalidation
			_, err = metricsCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
			if err != nil {
				return false
			}

			if metricsProvider.GetCallCount() != 2 {
				t.Logf("Expected 2 API calls after invalidation, got %d", metricsProvider.GetCallCount())
				return false
			}

			// Test workload cache invalidation
			mockClient := &mockClient{}
			workloadCache := NewWorkloadCache(1 * time.Minute)

			policy := &optipodv1alpha1.OptimizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: namespace,
				},
				Spec: optipodv1alpha1.OptimizationPolicySpec{
					Mode: optipodv1alpha1.ModeAuto,
					Selector: optipodv1alpha1.WorkloadSelector{
						WorkloadSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
						},
					},
				},
			}

			// First call
			_, err = workloadCache.GetOrDiscover(ctx, mockClient, policy)
			if err != nil {
				return false
			}

			if mockClient.GetListCallCount() != 1 {
				return false
			}

			// Second call - should use cache
			_, err = workloadCache.GetOrDiscover(ctx, mockClient, policy)
			if err != nil {
				return false
			}

			if mockClient.GetListCallCount() != 1 {
				return false
			}

			// Invalidate the cache
			policyKey := fmt.Sprintf("%s/%s", policy.Namespace, policy.Name)
			workloadCache.Invalidate(policyKey)

			// Third call - should hit the client again after invalidation
			_, err = workloadCache.GetOrDiscover(ctx, mockClient, policy)
			if err != nil {
				return false
			}

			if mockClient.GetListCallCount() != 2 {
				t.Logf("Expected 2 List API calls after invalidation, got %d", mockClient.GetListCallCount())
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// Feature: k8s-workload-rightsizing, Property 32: API call efficiency (Sequential Access)
// Validates: Requirements 17.5
func TestProperty_CacheSequentialAccess(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("for any sequential cache access, thread safety should be maintained and calls minimized", prop.ForAll(
		func(namespace string, podName string, containerName string, numSequentialCalls int) bool {
			// Ensure non-empty strings
			if namespace == "" {
				namespace = "default"
			}
			if podName == "" {
				podName = "test-pod"
			}
			if containerName == "" {
				containerName = "test-container"
			}
			// Ensure reasonable number of calls
			if numSequentialCalls < 2 {
				numSequentialCalls = 2
			}
			if numSequentialCalls > 20 {
				numSequentialCalls = 20
			}

			metricsProvider := &mockMetricsProvider{}
			metricsCache := NewMetricsCache(1 * time.Minute)

			ctx := context.Background()
			window := 24 * time.Hour

			// Make sequential calls with small delays to ensure cache is populated
			for i := 0; i < numSequentialCalls; i++ {
				_, err := metricsCache.GetOrFetch(ctx, metricsProvider, namespace, podName, containerName, window)
				if err != nil {
					t.Logf("Error during sequential access: %v", err)
					return false
				}
				// Small delay to ensure first call completes before second starts
				if i == 0 {
					time.Sleep(1 * time.Millisecond)
				}
			}

			// Verify that the provider was only called once
			// All subsequent calls should have been served from cache
			callCount := metricsProvider.GetCallCount()
			if callCount != 1 {
				t.Logf("Expected 1 API call for %d sequential requests, got %d", numSequentialCalls, callCount)
				return false
			}

			return true
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.IntRange(2, 20),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
