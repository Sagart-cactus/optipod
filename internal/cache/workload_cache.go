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
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
)

// WorkloadCache caches discovered workloads per policy to avoid redundant API calls
type WorkloadCache struct {
	mu      sync.RWMutex
	entries map[string]*workloadCacheEntry
	ttl     time.Duration
}

type workloadCacheEntry struct {
	workloads []discovery.Workload
	timestamp time.Time
}

// NewWorkloadCache creates a new workload cache with the specified TTL
func NewWorkloadCache(ttl time.Duration) *WorkloadCache {
	return &WorkloadCache{
		entries: make(map[string]*workloadCacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves cached workloads for a policy if available and not expired
func (c *WorkloadCache) Get(policyKey string) ([]discovery.Workload, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[policyKey]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.timestamp) > c.ttl {
		return nil, false
	}

	return entry.workloads, true
}

// Set stores workloads in the cache for a policy
func (c *WorkloadCache) Set(policyKey string, workloads []discovery.Workload) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[policyKey] = &workloadCacheEntry{
		workloads: workloads,
		timestamp: time.Now(),
	}
}

// Invalidate removes cached workloads for a specific policy
func (c *WorkloadCache) Invalidate(policyKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, policyKey)
}

// InvalidateAll clears the entire cache
func (c *WorkloadCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*workloadCacheEntry)
}

// GetOrDiscover retrieves workloads from cache or discovers them if not cached
func (c *WorkloadCache) GetOrDiscover(ctx context.Context, k8sClient client.Client, policy *optipodv1alpha1.OptimizationPolicy) ([]discovery.Workload, error) {
	policyKey := getPolicyKey(policy)

	// Try to get from cache first
	if workloads, found := c.Get(policyKey); found {
		return workloads, nil
	}

	// Not in cache, discover workloads
	workloads, err := discovery.DiscoverWorkloads(ctx, k8sClient, policy)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.Set(policyKey, workloads)

	return workloads, nil
}

// getPolicyKey generates a unique key for a policy
func getPolicyKey(policy *optipodv1alpha1.OptimizationPolicy) string {
	return policy.Namespace + "/" + policy.Name
}
