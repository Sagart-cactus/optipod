# Metrics-Server Rolling Window Sampling (In-Memory Cache) — Spec

## Problem

`metrics-server` only exposes near-real-time, point-in-time CPU/memory usage via the Kubernetes Metrics API. It does not support historical range queries.

Today OptiPod tries to approximate a rolling window by collecting multiple samples inline inside reconciliation. That makes the effective “lookback window” small (bounded by the number of samples and the sleep interval) unless we are willing to block reconciliation for long periods, which is not acceptable in production.

## Goal

Support “real” rolling-window percentile recommendations when using `metrics-server`, without blocking reconciliations, by:

- Sampling metrics continuously in the background.
- Storing samples in an in-memory time-series cache (ring buffer) per (workload, container).
- Computing percentiles from cached samples for the requested `spec.metricsConfig.rollingWindow`.

## Non-Goals (Phase 1)

- Persisting samples across controller restarts (in-memory only).
- Providing multi-day retention by default (will be bounded by memory/limits).
- Adding a new CRD schema field (optional Phase 2).
- Sampling every pod in a workload (Phase 1 will sample a single chosen pod per workload/container).

## Current Behavior (Baseline)

- Policy lookback configuration: `spec.metricsConfig.rollingWindow` (default 24h).
- For `metrics-server`: OptiPod samples `PodMetrics` repeatedly and computes percentiles over the sampled set, with caps (e.g., 10 samples × 15s = ~2.5 minutes).
- For Prometheus: OptiPod performs a true range query over `now-window → now`.

## Proposed Architecture (Phase 1)

### Components

1. **MetricsServerSampler** (background goroutine)
   - Periodically fetches `PodMetrics` for a set of registered targets.
   - Appends `(timestamp, cpuMilli, memBytes)` samples into an in-memory series per target.
   - Evicts stale targets and old samples outside the configured retention window.

2. **InMemoryTimeSeriesStore**
   - Map: `TargetKey -> RingBuffer[Sample]`
   - Provides:
     - `AppendSample(TargetKey, Sample)`
     - `GetSamples(TargetKey, window) []Sample` (only last `window`)
     - `Prune(TargetKey, cutoffTime)`
     - `EvictStaleTargets(lastSeenCutoff)`

3. **MetricsServerProvider (cached)**
   - Implements existing `metrics.MetricsProvider` interface:
     - `GetContainerMetrics(ctx, namespace, podName, containerName, window)`
   - Instead of sleeping to gather samples, it:
     - Resolves a `TargetKey` (see below).
     - Reads cached samples for `window`.
     - Computes P50/P90/P99 from the cached samples and returns `ContainerMetrics`.
     - Returns a typed “insufficient samples” error if there is not enough data.

4. **WorkloadProcessor integration**
   - When processing workloads, it selects a pod (as it already does today).
   - It registers/refreshes sampling targets for `(workload, container)` with the sampler.
   - Recommendation computation reads cached percentiles rather than triggering inline sampling.

### Target Identity (TargetKey)

To avoid losing history when pods are recreated, the cache must be keyed by workload identity rather than pod name.

Phase 1 key format:

- `TargetKey = { namespace, workloadKind, workloadName, containerName }`

The sampler also stores the current podName to query:

- `Target = { key: TargetKey, podName: string, lastSeen: time.Time }`

If the chosen pod changes, the target is updated (same TargetKey).

### Sampling Strategy

- Interval: configurable (default recommended: 30s).
- Pod selection: use the existing “first pod” strategy in Phase 1 (future: sample N pods and aggregate).
- Aggregation: compute percentiles across all collected samples within the requested rolling window.

### Insufficient Data Behavior

When using `metrics-server`, a long rolling window (e.g., 24h) requires time to “warm up”.

Define:

- `minSamplesRequired` (default: 10, configurable)

If `len(samples) < minSamplesRequired`, return `ErrInsufficientSamples` and:

- **Recommend mode**: still record annotations if desired, but mark the policy/workload as “insufficient data”.
- **Auto mode**: skip applying changes for that container and emit an event/condition explaining warm-up.

## Configuration

### Controller flags / env (Phase 1)

Add controller-manager flags (or env vars) to control sampling:

- `--metrics-server-sampling-interval` (default `30s`)
- `--metrics-server-max-samples-per-target` (default derived from retention + interval, but cap hard, e.g. `2880` for 24h @ 30s)
- `--metrics-server-min-samples-required` (default `10`)
- `--metrics-server-target-ttl` (default `15m`)  
  If a target hasn’t been refreshed by reconciliation in this time, evict it.

### Policy fields

Reuse existing:

- `spec.metricsConfig.rollingWindow` controls how far back OptiPod reads from the cache for percentile computation.

Per-policy override: `spec.metricsConfig.metricsServer.minSamplesRequired` can be used to override the operator default.

## Data Model

### Sample

- `timestamp: time.Time`
- `cpuMilli: int64` (millicores)
- `memoryBytes: int64`

### Ring buffer

- Fixed-capacity slice with head index.
- Prune logic should drop samples older than `now - retention`.
- Capacity should be sized to cover the maximum expected rolling window:
  - `capacity = min(maxSamplesPerTarget, ceil(retention/interval)+1)`

## Threading & Safety

- Sampler runs one goroutine for tick scheduling.
- Store access must be synchronized (RWMutex) or use per-target locks.
- Fetching `PodMetrics` should not hold locks.

## Observability

Add metrics/logs:

- Sampling loop duration and fetch errors per target.
- Cache size (#targets, samples per target).
- “Insufficient samples” count per policy.

## Failure Modes

- `metrics-server` unavailable: sampler records errors; provider returns error/insufficient data.
- Controller restart: cache is empty; warm-up required again.
- Pod missing: sampler updates error; next reconcile should refresh target with a new pod.

## Backward Compatibility

- Prometheus behavior unchanged.
- `metrics-server` behavior changes from “inline sampling” to “cached sampling”; warm-up required for long windows.
- Existing policies still valid; `rollingWindow` retains meaning, but now it determines cache lookback rather than sleep duration.

## Acceptance Criteria

1. Reconciliation does not block on metric sampling sleeps.
2. With `metrics-server` and `rollingWindow=24h`, after warm-up OptiPod computes percentiles over cached samples for up to 24h (bounded by retention/capacity).
3. With insufficient samples, OptiPod skips apply and surfaces a clear reason (event/log/status).
4. Tests can run fast by configuring a small interval and low min sample threshold.

## Implementation Plan (High Level)

1. Add sampler + store (`internal/metrics/sampler_*`).
2. Wire sampler lifecycle into controller manager startup.
3. Update `MetricsServerProvider` to read from cache rather than sleeping.
4. Update `WorkloadProcessor` to register sampling targets per workload/container and to handle insufficient-samples errors.
5. Add unit tests for ring buffer/pruning and for “insufficient samples” behavior.
