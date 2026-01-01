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
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// TargetKey identifies a workload container across pod restarts.
type TargetKey struct {
	Namespace    string
	WorkloadKind string
	WorkloadName string
	Container    string
}

// Sample represents a single metrics-server observation.
type Sample struct {
	Timestamp  time.Time
	CPUMilli   int64
	MemoryByte int64
}

type samplingTarget struct {
	key      TargetKey
	podName  string
	lastSeen time.Time
}

// sampleRing is a fixed-size ring buffer for samples.
type sampleRing struct {
	samples []Sample
	start   int
	count   int
}

func newSampleRing(capacity int) *sampleRing {
	if capacity < 1 {
		capacity = 1
	}
	return &sampleRing{
		samples: make([]Sample, capacity),
	}
}

func (r *sampleRing) append(sample Sample) {
	if r.count < len(r.samples) {
		index := (r.start + r.count) % len(r.samples)
		r.samples[index] = sample
		r.count++
		return
	}

	r.samples[r.start] = sample
	r.start = (r.start + 1) % len(r.samples)
}

func (r *sampleRing) valuesSince(cutoff time.Time) []Sample {
	if r.count == 0 {
		return nil
	}

	values := make([]Sample, 0, r.count)
	for i := 0; i < r.count; i++ {
		index := (r.start + i) % len(r.samples)
		sample := r.samples[index]
		if !sample.Timestamp.Before(cutoff) {
			values = append(values, sample)
		}
	}
	return values
}

type podKey struct {
	Namespace string
	PodName   string
	Container string
}

// timeSeriesStore holds samples and active targets in memory.
type timeSeriesStore struct {
	mu       sync.RWMutex
	series   map[TargetKey]*sampleRing
	targets  map[TargetKey]*samplingTarget
	podIndex map[podKey]TargetKey
	capacity int
}

func newTimeSeriesStore(capacity int) *timeSeriesStore {
	if capacity < 1 {
		capacity = 1
	}
	return &timeSeriesStore{
		series:   make(map[TargetKey]*sampleRing),
		targets:  make(map[TargetKey]*samplingTarget),
		podIndex: make(map[podKey]TargetKey),
		capacity: capacity,
	}
}

func (s *timeSeriesStore) registerTarget(key TargetKey, podName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.targets[key]; ok {
		oldKey := podKey{
			Namespace: key.Namespace,
			PodName:   existing.podName,
			Container: key.Container,
		}
		delete(s.podIndex, oldKey)
	}

	pkey := podKey{
		Namespace: key.Namespace,
		PodName:   podName,
		Container: key.Container,
	}

	target := &samplingTarget{
		key:      key,
		podName:  podName,
		lastSeen: time.Now(),
	}
	s.targets[key] = target
	s.podIndex[pkey] = key

	if _, exists := s.series[key]; !exists {
		s.series[key] = newSampleRing(s.capacity)
	}
}

func (s *timeSeriesStore) listTargets() []samplingTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targets := make([]samplingTarget, 0, len(s.targets))
	for _, target := range s.targets {
		targets = append(targets, *target)
	}
	return targets
}

func (s *timeSeriesStore) appendSample(key TargetKey, sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ring, exists := s.series[key]
	if !exists {
		ring = newSampleRing(s.capacity)
		s.series[key] = ring
	}
	ring.append(sample)
}

func (s *timeSeriesStore) samplesSince(key TargetKey, cutoff time.Time) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ring, exists := s.series[key]
	if !exists {
		return nil
	}
	return ring.valuesSince(cutoff)
}

func (s *timeSeriesStore) resolveTargetKey(namespace, podName, container string) (TargetKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.podIndex[podKey{
		Namespace: namespace,
		PodName:   podName,
		Container: container,
	}]
	return key, ok
}

func (s *timeSeriesStore) evictStaleTargets(ttl time.Duration) {
	if ttl <= 0 {
		return
	}

	cutoff := time.Now().Add(-ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, target := range s.targets {
		if target.lastSeen.Before(cutoff) {
			delete(s.targets, key)
			delete(s.series, key)
			pkey := podKey{
				Namespace: key.Namespace,
				PodName:   target.podName,
				Container: key.Container,
			}
			delete(s.podIndex, pkey)
		}
	}
}

// MetricsServerSampler periodically samples metrics-server for registered targets.
type MetricsServerSampler struct {
	metricsClientset metricsclientset.Interface
	store            *timeSeriesStore
	interval         time.Duration
	targetTTL        time.Duration
}

// NewMetricsServerSampler creates a new sampler.
func NewMetricsServerSampler(metricsClientset metricsclientset.Interface, store *timeSeriesStore, interval time.Duration, targetTTL time.Duration) *MetricsServerSampler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &MetricsServerSampler{
		metricsClientset: metricsClientset,
		store:            store,
		interval:         interval,
		targetTTL:        targetTTL,
	}
}

// RegisterTarget refreshes or adds a sampling target.
func (s *MetricsServerSampler) RegisterTarget(key TargetKey, podName string) {
	s.store.registerTarget(key, podName)
}

// Start begins the sampling loop. It stops when the context is cancelled.
func (s *MetricsServerSampler) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.sampleOnce(ctx)
		}
	}
}

func (s *MetricsServerSampler) sampleOnce(ctx context.Context) {
	targets := s.store.listTargets()
	for _, target := range targets {
		podMetrics, err := s.metricsClientset.MetricsV1beta1().PodMetricses(target.key.Namespace).Get(ctx, target.podName, metav1.GetOptions{})
		if err != nil {
			continue
		}

		containerMetrics := findContainerMetrics(podMetrics, target.key.Container)
		if containerMetrics == nil {
			continue
		}

		sample := Sample{
			Timestamp:  time.Now(),
			CPUMilli:   containerMetrics.Usage.Cpu().MilliValue(),
			MemoryByte: containerMetrics.Usage.Memory().Value(),
		}
		s.store.appendSample(target.key, sample)
	}

	s.store.evictStaleTargets(s.targetTTL)
}

func findContainerMetrics(podMetrics *metricsv1beta1.PodMetrics, containerName string) *metricsv1beta1.ContainerMetrics {
	for i := range podMetrics.Containers {
		if podMetrics.Containers[i].Name == containerName {
			return &podMetrics.Containers[i]
		}
	}
	return nil
}

// ErrInsufficientSamples indicates the cache has not warmed up yet.
type ErrInsufficientSamples struct {
	Have int
	Need int
}

func (e *ErrInsufficientSamples) Error() string {
	return fmt.Sprintf("insufficient metrics samples: have %d, need %d", e.Have, e.Need)
}
