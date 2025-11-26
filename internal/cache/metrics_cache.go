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
	"sync"
	"time"

	"github.com/optipod/optipod/internal/metrics"
)

// MetricsCache caches container metrics with a short TTL to avoid overwhelming metrics backends
type MetricsCache struct {
	mu      sync.RWMutex
	entries map[string]*metricsCacheEntry
	ttl     time.Duration
}

type metricsCacheEntry struct {
	metrics   *metrics.ContainerMetrics
	timestamp time.Time
}

// NewMetricsCache creates a new metrics cache with the specified TTL (typically 1 minute)
func NewMetricsCache(ttl time.Duration) *MetricsCache {
	return &MetricsCache{
		entries: make(map[string]*metricsCacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves cached metrics for a container if available and not expired
func (c *MetricsCache) Get(namespace, podName, containerName string) (*metrics.ContainerMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := getMetricsKey(namespace, podName, containerName)
	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.timestamp) > c.ttl {
		return nil, false
	}

	return entry.metrics, true
}

// Set stores metrics in the cache for a container
func (c *MetricsCache) Set(namespace, podName, containerName string, containerMetrics *metrics.ContainerMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getMetricsKey(namespace, podName, containerName)
	c.entries[key] = &metricsCacheEntry{
		metrics:   containerMetrics,
		timestamp: time.Now(),
	}
}

// Invalidate removes cached metrics for a specific container
func (c *MetricsCache) Invalidate(namespace, podName, containerName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getMetricsKey(namespace, podName, containerName)
	delete(c.entries, key)
}

// InvalidateAll clears the entire cache
func (c *MetricsCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*metricsCacheEntry)
}

// GetOrFetch retrieves metrics from cache or fetches them from the provider if not cached
func (c *MetricsCache) GetOrFetch(ctx context.Context, provider metrics.MetricsProvider, namespace, podName, containerName string, window time.Duration) (*metrics.ContainerMetrics, error) {
	// Try to get from cache first
	if containerMetrics, found := c.Get(namespace, podName, containerName); found {
		return containerMetrics, nil
	}

	// Not in cache, fetch from provider
	containerMetrics, err := provider.GetContainerMetrics(ctx, namespace, podName, containerName, window)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.Set(namespace, podName, containerName, containerMetrics)

	return containerMetrics, nil
}

// getMetricsKey generates a unique key for container metrics
func getMetricsKey(namespace, podName, containerName string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
}
