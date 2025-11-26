# Requirements Document

## Introduction

OptiPod is a Kubernetes operator that automatically optimizes workload resource requests (CPU and memory) based on actual usage patterns. The system monitors running workloads, collects metrics over time, and intelligently adjusts resource requests to reduce over-provisioning while maintaining safety margins. OptiPod supports multiple operational modes (auto-apply, recommend-only, disabled), respects user-defined constraints, and provides comprehensive observability into optimization decisions.

## Glossary

- **OptiPod**: The Kubernetes operator system that performs workload resource optimization
- **Workload**: A Kubernetes resource that manages pods (e.g., Deployment, StatefulSet, DaemonSet)
- **Resource Request**: The amount of CPU and memory a container requests from Kubernetes scheduler
- **OptimizationPolicy**: A Custom Resource Definition (CRD) that defines optimization behavior, constraints, and target workloads
- **Metrics Source**: A backend system providing resource usage data (e.g., metrics-server, Prometheus)
- **In-Place Resize**: Kubernetes feature allowing resource request changes without pod recreation
- **Safety Factor**: A multiplier applied to usage percentiles to ensure adequate resource headroom
- **Percentile**: A statistical measure (e.g., P90 = 90th percentile) of resource usage over time
- **Rolling Window**: A time period over which metrics are aggregated (e.g., last 24 hours)
- **Reconciliation Interval**: The frequency at which OptiPod evaluates and applies optimization decisions

## Requirements

### Requirement 1

**User Story:** As a cluster administrator, I want OptiPod to automatically adjust pod resource requests based on actual usage, so that I can reduce waste from over-provisioning and prevent issues from under-provisioning.

#### Acceptance Criteria

1. WHEN a Kubernetes cluster is running workloads with static CPU and memory requests, THE OptiPod SHALL monitor resource usage and compute recommended adjustments
2. WHEN a user enables OptiPod on a workload, THE OptiPod SHALL begin collecting CPU and memory usage metrics for that workload
3. WHEN sufficient usage data has been collected for a workload, THE OptiPod SHALL update the workload's CPU and memory requests according to the configured optimization policy
4. WHEN OptiPod updates a workload, THE OptiPod SHALL apply changes only to resource requests, not limits, unless explicitly configured otherwise

### Requirement 2

**User Story:** As a platform engineer, I want to define safety constraints for resource optimization, so that OptiPod never reduces resources below safe thresholds or exceeds maximum bounds.

#### Acceptance Criteria

1. WHEN applying resource changes to a workload, THE OptiPod SHALL respect per-workload minimum CPU bounds defined in the OptimizationPolicy CRD
2. WHEN applying resource changes to a workload, THE OptiPod SHALL respect per-workload maximum CPU bounds defined in the OptimizationPolicy CRD
3. WHEN applying resource changes to a workload, THE OptiPod SHALL respect per-workload minimum memory bounds defined in the OptimizationPolicy CRD
4. WHEN applying resource changes to a workload, THE OptiPod SHALL respect per-workload maximum memory bounds defined in the OptimizationPolicy CRD
5. WHEN a computed recommendation violates configured bounds, THE OptiPod SHALL clamp the recommendation to the nearest valid value within bounds

### Requirement 3

**User Story:** As a site reliability engineer, I want OptiPod to handle resource changes safely, so that workload stability is never compromised during optimization.

#### Acceptance Criteria

1. WHEN applying resource changes to a workload, THE OptiPod SHALL avoid decreasing memory limits in-place in a way that could cause pod eviction or OOM conditions
2. WHEN in-place pod resize is not supported for a given change, THE OptiPod SHALL fall back to a safe strategy only if explicitly allowed in the policy
3. WHEN in-place pod resize is not supported and no fallback strategy is allowed, THE OptiPod SHALL skip the change and emit a warning event
4. WHEN OptiPod cannot fetch metrics for a workload, THE OptiPod SHALL not apply any resource changes to that workload
5. WHEN OptiPod cannot fetch metrics for a workload, THE OptiPod SHALL expose a status condition indicating missing metrics with a clear reason

### Requirement 4

**User Story:** As a DevOps engineer, I want a recommendation-only mode, so that I can review OptiPod's suggestions before allowing automatic changes.

#### Acceptance Criteria

1. WHEN OptiPod is in recommendation-only mode for a workload, THE OptiPod SHALL never modify any workload resource specifications
2. WHEN OptiPod is in recommendation-only mode for a workload, THE OptiPod SHALL write recommendations to a status field on the OptimizationPolicy resource
3. WHEN OptiPod is in recommendation-only mode for a workload, THE OptiPod SHALL update recommendations on each reconciliation cycle
4. WHEN OptiPod is in recommendation-only mode, THE OptiPod SHALL expose recommendations in a format queryable via kubectl

### Requirement 5

**User Story:** As a cluster operator, I want OptiPod to collect accurate usage metrics, so that optimization decisions are based on real workload behavior.

#### Acceptance Criteria

1. WHEN OptiPod monitors a workload, THE OptiPod SHALL obtain CPU usage metrics per container from the configured metrics source
2. WHEN OptiPod monitors a workload, THE OptiPod SHALL obtain memory usage metrics per container from the configured metrics source
3. WHEN usage metrics are collected, THE OptiPod SHALL aggregate data over a configurable rolling window to compute statistical percentiles
4. WHEN usage metrics are collected, THE OptiPod SHALL compute P50, P90, and P99 percentiles for CPU usage
5. WHEN usage metrics are collected, THE OptiPod SHALL compute P50, P90, and P99 percentiles for memory usage
6. WHEN computing recommended resources, THE OptiPod SHALL derive CPU requests using a configurable strategy defined in the policy
7. WHEN computing recommended resources, THE OptiPod SHALL derive memory requests using a configurable strategy defined in the policy
8. WHEN computing recommended resources using percentile-based strategy, THE OptiPod SHALL apply the configured safety factor to the selected percentile

### Requirement 6

**User Story:** As a platform administrator, I want to define optimization policies declaratively, so that I can control OptiPod behavior through Kubernetes-native configuration.

#### Acceptance Criteria

1. WHEN an OptimizationPolicy resource is created, THE OptiPod SHALL validate all required fields are present and well-formed
2. WHEN an OptimizationPolicy resource contains invalid configuration, THE OptiPod SHALL reject it with descriptive error messages
3. WHEN an OptimizationPolicy references workloads via label selectors, THE OptiPod SHALL automatically discover matching Deployments in the specified namespaces
4. WHEN an OptimizationPolicy references workloads via label selectors, THE OptiPod SHALL automatically discover matching StatefulSets in the specified namespaces
5. WHEN an OptimizationPolicy references workloads via label selectors, THE OptiPod SHALL automatically discover matching DaemonSets in the specified namespaces

### Requirement 7

**User Story:** As a Kubernetes administrator, I want to control when OptiPod applies changes, so that I can choose between automatic optimization and manual review workflows.

#### Acceptance Criteria

1. WHEN a policy mode is set to Auto, THE OptiPod SHALL periodically evaluate resource recommendations for matching workloads
2. WHEN a policy mode is set to Auto, THE OptiPod SHALL apply recommendations to matching workloads within the configured reconciliation interval
3. WHEN a policy mode is set to Recommend, THE OptiPod SHALL periodically evaluate and store recommendations for matching workloads
4. WHEN a policy mode is set to Recommend, THE OptiPod SHALL NOT patch any workload specifications
5. WHEN a policy mode is set to Disabled, THE OptiPod SHALL stop making recommendations for workloads under that policy
6. WHEN a policy mode is set to Disabled, THE OptiPod SHALL stop making changes for workloads under that policy
7. WHEN a policy mode is set to Disabled, THE OptiPod SHALL preserve historical status information for observability

### Requirement 8

**User Story:** As a Kubernetes operator, I want OptiPod to work across different cluster versions, so that I can use it regardless of whether in-place resize is available.

#### Acceptance Criteria

1. WHEN running on Kubernetes version 1.29 or higher, THE OptiPod SHALL detect whether InPlacePodVerticalScaling feature gate is enabled
2. WHEN running on Kubernetes version 1.33 or higher with in-place resize enabled, THE OptiPod SHALL prefer in-place updates for CPU requests whenever allowed by the policy
3. WHEN running on Kubernetes version 1.33 or higher with in-place resize enabled, THE OptiPod SHALL prefer in-place updates for memory requests whenever allowed by the policy
4. WHEN running in clusters without in-place resize support, THE OptiPod SHALL support a recreate-on-change strategy as an optional policy setting
5. WHEN recreate-on-change strategy is not explicitly enabled, THE OptiPod SHALL default to skipping changes that require pod recreation

### Requirement 9

**User Story:** As a cluster operator, I want comprehensive observability into OptiPod's decisions, so that I can understand what changes were made and why.

#### Acceptance Criteria

1. WHEN OptiPod processes a workload, THE OptiPod SHALL expose a status condition indicating the last recommendation timestamp
2. WHEN OptiPod processes a workload, THE OptiPod SHALL expose a status condition indicating the last applied change timestamp
3. WHEN OptiPod skips a change, THE OptiPod SHALL expose a status condition with the reason for skipping
4. WHEN recommendations are produced, THE OptiPod SHALL expose them in a structured format showing per-container CPU request values
5. WHEN recommendations are produced, THE OptiPod SHALL expose them in a structured format showing per-container memory request values
6. WHEN recommendations are produced, THE OptiPod SHALL make them queryable via kubectl get or kubectl describe commands

### Requirement 10

**User Story:** As a monitoring engineer, I want OptiPod to expose Prometheus metrics, so that I can track optimization activity and detect issues.

#### Acceptance Criteria

1. WHEN users query OptiPod metrics via Prometheus, THE OptiPod SHALL expose a metric for the number of workloads currently monitored
2. WHEN users query OptiPod metrics via Prometheus, THE OptiPod SHALL expose a metric for the number of workloads updated in the last reconciliation cycle
3. WHEN users query OptiPod metrics via Prometheus, THE OptiPod SHALL expose a metric for the number of recommendations skipped with reasons
4. WHEN users query OptiPod metrics via Prometheus, THE OptiPod SHALL expose metrics for optimization cycle duration
5. WHEN users query OptiPod metrics via Prometheus, THE OptiPod SHALL expose metrics for optimization cycle errors

### Requirement 11

**User Story:** As a Kubernetes user, I want clear feedback when OptiPod encounters problems, so that I can troubleshoot issues quickly.

#### Acceptance Criteria

1. WHEN a resource update fails, THE OptiPod SHALL create a Kubernetes Event on the affected workload with a clear reason
2. WHEN a resource update fails, THE OptiPod SHALL include actionable suggestions in the Event message
3. WHEN OptiPod encounters a metrics collection error, THE OptiPod SHALL create a Kubernetes Event describing the error
4. WHEN OptiPod encounters a policy validation error, THE OptiPod SHALL create a Kubernetes Event on the OptimizationPolicy resource

### Requirement 12

**User Story:** As a multi-tenant cluster administrator, I want to control which namespaces OptiPod can affect, so that I can isolate optimization policies by team or environment.

#### Acceptance Criteria

1. WHEN OptiPod is deployed in a multi-tenant cluster, THE OptiPod SHALL allow scoping policies by namespace selectors
2. WHEN OptiPod is deployed in a multi-tenant cluster, THE OptiPod SHALL allow scoping policies by workload label selectors
3. WHEN OptiPod is deployed in a multi-tenant cluster, THE OptiPod SHALL support optional allow-lists of namespaces per policy
4. WHEN OptiPod is deployed in a multi-tenant cluster, THE OptiPod SHALL support optional deny-lists of namespaces per policy
5. WHEN a deny-list and allow-list both apply to a namespace, THE OptiPod SHALL treat the deny-list as taking precedence

### Requirement 13

**User Story:** As a security-conscious operator, I want OptiPod to respect RBAC permissions, so that it cannot modify workloads it shouldn't have access to.

#### Acceptance Criteria

1. WHEN RBAC prevents OptiPod from reading a workload, THE OptiPod SHALL not attempt to monitor or modify it
2. WHEN RBAC prevents OptiPod from updating a workload, THE OptiPod SHALL not attempt to modify it
3. WHEN RBAC prevents OptiPod from updating a workload, THE OptiPod SHALL surface an appropriate status condition on the policy
4. WHEN RBAC prevents OptiPod from updating a workload, THE OptiPod SHALL create a Kubernetes Event indicating insufficient permissions

### Requirement 14

**User Story:** As a cautious administrator, I want a global dry-run mode, so that I can test OptiPod across the entire cluster without making any actual changes.

#### Acceptance Criteria

1. WHEN a cluster admin enables global dry-run mode, THE OptiPod SHALL compute recommendations for all matching workloads
2. WHEN a cluster admin enables global dry-run mode, THE OptiPod SHALL never apply recommendations to any workload regardless of policy mode
3. WHEN a cluster admin enables global dry-run mode, THE OptiPod SHALL expose all recommendations in status fields as if in recommend-only mode
4. WHEN global dry-run mode is enabled, THE OptiPod SHALL indicate this state in its own status or configuration

### Requirement 15

**User Story:** As a developer extending OptiPod, I want a pluggable metrics architecture, so that I can integrate new metrics backends without modifying core logic.

#### Acceptance Criteria

1. WHEN new metrics backends are needed, THE OptiPod SHALL provide a well-defined internal interface for metrics providers
2. WHEN a new metrics provider is implemented, THE OptiPod SHALL allow configuration to select it without code changes to the core controller
3. WHEN a metrics provider fails to initialize, THE OptiPod SHALL log a clear error and fall back to a safe state

### Requirement 16

**User Story:** As a product maintainer, I want stable APIs that can evolve, so that future features don't break existing users.

#### Acceptance Criteria

1. WHEN future features are added to OptimizationPolicy, THE OptiPod SHALL keep existing CRD fields backwards compatible
2. WHEN breaking changes are necessary, THE OptiPod SHALL introduce them via new API versions with conversion webhooks
3. WHEN OptiPod is upgraded between compatible versions, THE OptiPod SHALL preserve existing CRD instances without data loss
4. WHEN OptiPod is upgraded between compatible versions, THE OptiPod SHALL preserve status fields on existing policies

### Requirement 17

**User Story:** As a cluster operator managing large environments, I want OptiPod to scale efficiently, so that it doesn't become a bottleneck or resource hog.

#### Acceptance Criteria

1. WHEN running in clusters with up to hundreds of namespaces, THE OptiPod SHALL complete a full optimization scan within five minutes under default settings
2. WHEN running in clusters with up to thousands of pods, THE OptiPod SHALL complete a full optimization scan within five minutes under default settings
3. WHEN OptiPod is idle with no changes to apply, THE OptiPod SHALL consume minimal CPU resources suitable for control-plane environments
4. WHEN OptiPod is idle with no changes to apply, THE OptiPod SHALL consume minimal memory resources suitable for control-plane environments
5. WHEN OptiPod processes workloads, THE OptiPod SHALL use efficient caching to avoid redundant API calls to the Kubernetes API server
