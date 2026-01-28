# OptimizationPolicy CRD Specification

Complete field reference for the OptimizationPolicy Custom Resource Definition.

## Overview

The `OptimizationPolicy` CRD defines how OptiPod optimizes Kubernetes workloads. Each policy specifies:
- Which workloads to target (selector)
- How to collect metrics (metricsConfig)
- Resource constraints (resourceBounds)
- How to apply changes (updateStrategy)
- When to apply changes (mode)

## API Version and Kind

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
```

## Metadata

Standard Kubernetes metadata fields:

```yaml
metadata:
  name: my-policy          # Policy name (required)
  namespace: default       # Policy namespace (required)
  labels:                  # Optional labels
    environment: production
  annotations:             # Optional annotations
    description: "Production workload optimization"
```

## Spec Fields

### mode (required)

Defines the operational behavior of the policy.

**Type**: `string`  
**Required**: Yes  
**Enum**: `Auto`, `Recommend`, `Disabled`  
**Default**: None (must be specified)

**Values**:
- `Auto` - Automatically applies resource recommendations to workloads
- `Recommend` - Computes recommendations but does not apply them (safe mode)
- `Disabled` - Stops processing workloads under this policy

**Example**:
```yaml
spec:
  mode: Recommend  # Start in safe mode
```

### weight (optional)

Defines the priority of this policy when multiple policies match the same workload. Higher weight policies take precedence.

**Type**: `integer`  
**Required**: No  
**Default**: `100`  
**Range**: `1` to `1000`

**Example**:
```yaml
spec:
  weight: 200  # Higher priority than default
```

### selector (required)

Defines which workloads this policy applies to. At least one selector type must be specified.

**Type**: `object`  
**Required**: Yes

#### selector.namespaceSelector (optional)

Selects namespaces by labels using Kubernetes label selector syntax.

**Type**: `LabelSelector`  
**Required**: No

**Example**:
```yaml
selector:
  namespaceSelector:
    matchLabels:
      environment: production
      team: platform
```

**With match expressions**:
```yaml
selector:
  namespaceSelector:
    matchExpressions:
    - key: environment
      operator: In
      values:
      - production
      - staging
    - key: team
      operator: NotIn
      values:
      - experimental
```

**Valid operators**: `In`, `NotIn`, `Exists`, `DoesNotExist`

#### selector.workloadSelector (optional)

Selects workloads by labels using Kubernetes label selector syntax.

**Type**: `LabelSelector`  
**Required**: No

**Example**:
```yaml
selector:
  workloadSelector:
    matchLabels:
      optimize: "true"
      tier: backend
```

#### selector.namespaces (optional)

Defines allow and deny lists for namespace filtering.

**Type**: `object`  
**Required**: No

**Fields**:
- `allow` ([]string) - List of namespaces to include
- `deny` ([]string) - List of namespaces to exclude (takes precedence over allow)

**Example**:
```yaml
selector:
  namespaces:
    allow:
      - production
      - staging
    deny:
      - kube-system
      - kube-public
```

#### selector.workloadTypes (optional)

Defines include/exclude filters for workload types.

**Type**: `object`  
**Required**: No

**Fields**:
- `include` ([]WorkloadType) - Workload types to include (if empty, includes all)
- `exclude` ([]WorkloadType) - Workload types to exclude (takes precedence over include)

**Valid WorkloadType values**: `Deployment`, `StatefulSet`, `DaemonSet`

**Example**:
```yaml
selector:
  workloadTypes:
    include:
      - Deployment
      - StatefulSet
```

Or exclude specific types:
```yaml
selector:
  workloadTypes:
    exclude:
      - DaemonSet
```

### metricsConfig (required)

Defines metrics collection and processing configuration.

**Type**: `object`  
**Required**: Yes

#### metricsConfig.provider (required)

Specifies the metrics backend.

**Type**: `string`  
**Required**: Yes  
**Enum**: `prometheus`, `metrics-server`, `custom`

**Example**:
```yaml
metricsConfig:
  provider: prometheus
```

#### metricsConfig.metricsServer (optional)

Configures metrics-server specific behavior. Only applicable when `provider: metrics-server`.

**Type**: `object`  
**Required**: No

**Fields**:
- `minSamplesRequired` (integer) - Minimum number of cached samples required before computing recommendations
  - **Range**: `1` to `100000`
  - **Default**: Operator-wide default

**Example**:
```yaml
metricsConfig:
  provider: metrics-server
  metricsServer:
    minSamplesRequired: 20
```

#### metricsConfig.rollingWindow (optional)

Defines the time period over which metrics are aggregated.

**Type**: `duration`  
**Required**: No  
**Default**: `24h`  
**Format**: Kubernetes duration (e.g., `12h`, `24h`, `48h`)

**Example**:
```yaml
metricsConfig:
  rollingWindow: 48h  # 48 hours of data
```

#### metricsConfig.percentile (optional)

Defines which percentile to use for recommendations.

**Type**: `string`  
**Required**: No  
**Default**: `P90`  
**Enum**: `P50`, `P90`, `P99`

**Example**:
```yaml
metricsConfig:
  percentile: P90  # Use 90th percentile
```

#### metricsConfig.safetyFactor (optional)

Multiplier applied to the selected percentile value. Provides a buffer above observed usage.

**Type**: `float64`  
**Required**: No  
**Default**: `1.2`  
**Minimum**: `1.0`

**Example**:
```yaml
metricsConfig:
  safetyFactor: 1.5  # 50% buffer above P90
```

**Calculation**:
```
Recommendation = Percentile(Usage) × SafetyFactor
```

### resourceBounds (required)

Defines min/max constraints for CPU and memory recommendations.

**Type**: `object`  
**Required**: Yes

#### resourceBounds.cpu (required)

Defines CPU resource bounds.

**Type**: `object`  
**Required**: Yes

**Fields**:
- `min` (Quantity) - Minimum allowed CPU value (required)
- `max` (Quantity) - Maximum allowed CPU value (required)

**Example**:
```yaml
resourceBounds:
  cpu:
    min: "100m"    # 100 millicores
    max: "4000m"   # 4 cores
```

**Validation**: `min` must be less than or equal to `max`

#### resourceBounds.memory (required)

Defines memory resource bounds.

**Type**: `object`  
**Required**: Yes

**Fields**:
- `min` (Quantity) - Minimum allowed memory value (required)
- `max` (Quantity) - Maximum allowed memory value (required)

**Example**:
```yaml
resourceBounds:
  memory:
    min: "128Mi"   # 128 mebibytes
    max: "8Gi"     # 8 gibibytes
```

**Validation**: `min` must be less than or equal to `max`

### updateStrategy (required)

Defines how resource updates are applied to workloads.

**Type**: `object`  
**Required**: Yes

#### updateStrategy.strategy (optional)

Defines how resource updates are applied.

**Type**: `string`  
**Required**: No  
**Default**: `webhook`  
**Enum**: `ssa`, `webhook`

**Values**:
- `ssa` - Uses Server-Side Apply for resource updates
- `webhook` - Uses mutating webhooks for resource updates (GitOps-safe)

**Example**:
```yaml
updateStrategy:
  strategy: webhook
```

#### updateStrategy.rolloutStrategy (optional)

Defines when resource changes take effect. Only applicable for webhook strategy.

**Type**: `string`  
**Required**: No  
**Default**: `onNextRestart`  
**Enum**: `immediate`, `onNextRestart`

**Values**:
- `immediate` - Triggers rolling restart after applying annotations
- `onNextRestart` - Only applies annotations without triggering restarts

**Example**:
```yaml
updateStrategy:
  rolloutStrategy: onNextRestart
```

#### updateStrategy.allowInPlaceResize (optional)

Enables in-place pod resize when supported by Kubernetes (1.27+).

**Type**: `boolean`  
**Required**: No  
**Default**: `true`

**Example**:
```yaml
updateStrategy:
  allowInPlaceResize: true
```

#### updateStrategy.allowRecreate (optional)

Enables pod recreation when in-place resize is not available.

**Type**: `boolean`  
**Required**: No  
**Default**: `false`

**Example**:
```yaml
updateStrategy:
  allowRecreate: false  # Safer default
```

#### updateStrategy.updateRequestsOnly (optional)

Controls whether to update only requests or both requests and limits.

**Type**: `boolean`  
**Required**: No  
**Default**: `true`

**Example**:
```yaml
updateStrategy:
  updateRequestsOnly: true  # Only update requests
```

#### updateStrategy.useServerSideApply (optional)

Enables Server-Side Apply for field-level ownership. Only applicable for SSA strategy.

**Type**: `boolean`  
**Required**: No  
**Default**: `true`

**Example**:
```yaml
updateStrategy:
  strategy: ssa
  useServerSideApply: true
```

#### updateStrategy.limitConfig (optional)

Defines how resource limits are calculated from recommendations. Only applicable when `updateRequestsOnly: false`.

**Type**: `object`  
**Required**: No

**Fields**:
- `cpuLimitMultiplier` (float64) - Multiplier applied to CPU recommendation to calculate limit
  - **Default**: `1.0` (limit equals recommendation)
  - **Range**: `1.0` to `10.0`
  - **Example**: `1.5` means limit = recommendation × 1.5

- `memoryLimitMultiplier` (float64) - Multiplier applied to memory recommendation to calculate limit
  - **Default**: `1.1` (limit is 10% higher than recommendation)
  - **Range**: `1.0` to `10.0`
  - **Example**: `1.2` means limit = recommendation × 1.2

**Example**:
```yaml
updateStrategy:
  updateRequestsOnly: false
  limitConfig:
    cpuLimitMultiplier: 1.5      # Limit = Request × 1.5
    memoryLimitMultiplier: 1.1   # Limit = Request × 1.1
```

#### updateStrategy.allowUnsafeMemoryDecrease (optional)

Disables memory safety checks when true. Use with caution.

**Type**: `boolean`  
**Required**: No  
**Default**: `false`

**Behavior**:
- When `false` (default): OptiPod blocks memory decreases that could cause pod eviction or OOM
- When `true`: OptiPod applies all memory recommendations regardless of safety concerns

**Example**:
```yaml
updateStrategy:
  allowUnsafeMemoryDecrease: false  # Safe default
```

**Warning**: Setting this to `true` can cause pod instability. Only use in controlled environments.

#### updateStrategy.gradualDecreaseConfig (optional)

Configures gradual memory reduction for safer optimization. When enabled, large memory decreases are applied incrementally over multiple reconciliations.

**Type**: `object`  
**Required**: No

**Fields**:

- `enabled` (boolean) - Activates gradual decrease functionality
  - **Default**: `false`

- `memoryDecreasePercentage` (integer) - Maximum percentage to decrease memory per reconciliation
  - **Default**: `10` (10% per reconciliation)
  - **Range**: `1` to `50`

- `minimumDecreaseThreshold` (Quantity) - Minimum decrease amount to trigger gradual reduction
  - **Default**: `100Mi`
  - Decreases smaller than this threshold are applied immediately

- `maximumTotalDecrease` (integer) - Maximum total percentage decrease from original value
  - **Default**: `70` (70% maximum total decrease)
  - **Range**: `1` to `90`
  - Prevents excessive optimization that could destabilize workloads

**Example**:
```yaml
updateStrategy:
  gradualDecreaseConfig:
    enabled: true
    memoryDecreasePercentage: 10      # Max 10% per cycle
    minimumDecreaseThreshold: 100Mi   # Threshold to trigger
    maximumTotalDecrease: 70          # Max 70% total decrease
```

### reconciliationInterval (optional)

Defines how often the policy is evaluated.

**Type**: `duration`  
**Required**: No  
**Default**: `5m`  
**Format**: Kubernetes duration (e.g., `5m`, `10m`, `1h`)

**Example**:
```yaml
spec:
  reconciliationInterval: 5m  # Evaluate every 5 minutes
```

## Status Fields

The status subresource provides information about the policy's current state.

### conditions

Represents the current state of the OptimizationPolicy resource using standard Kubernetes conditions.

**Type**: `[]Condition`  
**List Type**: `map` (keyed by `type`)

**Example**:
```yaml
status:
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2025-01-28T10:00:00Z"
    reason: PolicyActive
    message: "Policy is active and processing workloads"
```

### workloadsDiscovered

Count of workloads matching this policy's selector.

**Type**: `integer`

**Example**:
```yaml
status:
  workloadsDiscovered: 10
```

### workloadsProcessed

Count of workloads successfully processed by this policy.

**Type**: `integer`

**Example**:
```yaml
status:
  workloadsProcessed: 8
```

### lastReconciliation

Timestamp of the last reconciliation.

**Type**: `Time`

**Example**:
```yaml
status:
  lastReconciliation: "2025-01-28T10:30:00Z"
```

### workloadsByType

Provides breakdown of workloads by type.

**Type**: `object`

**Fields**:
- `deployments` (integer) - Count of Deployment workloads
- `statefulSets` (integer) - Count of StatefulSet workloads
- `daemonSets` (integer) - Count of DaemonSet workloads

**Example**:
```yaml
status:
  workloadsByType:
    deployments: 6
    statefulSets: 2
    daemonSets: 0
```

## Complete Example

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-policy
  namespace: default
  labels:
    environment: production
spec:
  # Operational mode
  mode: Auto
  weight: 200
  
  # Workload selection
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadSelector:
      matchLabels:
        optimize: "true"
    namespaces:
      deny:
        - kube-system
    workloadTypes:
      include:
        - Deployment
        - StatefulSet
  
  # Metrics configuration
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  
  # Resource constraints
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "256Mi"
      max: "8Gi"
  
  # Update strategy
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10
      minimumDecreaseThreshold: 100Mi
      maximumTotalDecrease: 70
  
  # Reconciliation
  reconciliationInterval: 5m

status:
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2025-01-28T10:00:00Z"
    reason: PolicyActive
    message: "Policy is active and processing workloads"
  workloadsDiscovered: 10
  workloadsProcessed: 8
  lastReconciliation: "2025-01-28T10:30:00Z"
  workloadsByType:
    deployments: 6
    statefulSets: 2
    daemonSets: 0
```

## Validation Rules

The OptimizationPolicy CRD enforces the following validation rules:

### Required Fields
- `spec.mode` - Must be one of: `Auto`, `Recommend`, `Disabled`
- `spec.selector` - At least one selector type must be specified
- `spec.metricsConfig.provider` - Must be specified
- `spec.resourceBounds.cpu.min` - Must be greater than zero
- `spec.resourceBounds.cpu.max` - Must be greater than zero
- `spec.resourceBounds.memory.min` - Must be greater than zero
- `spec.resourceBounds.memory.max` - Must be greater than zero

### Value Constraints
- `spec.weight` - Must be between 1 and 1000
- `spec.metricsConfig.safetyFactor` - Must be >= 1.0
- `spec.metricsConfig.metricsServer.minSamplesRequired` - Must be >= 1
- `spec.resourceBounds.cpu.min` - Must be <= `spec.resourceBounds.cpu.max`
- `spec.resourceBounds.memory.min` - Must be <= `spec.resourceBounds.memory.max`
- `spec.updateStrategy.limitConfig.cpuLimitMultiplier` - Must be between 1.0 and 10.0
- `spec.updateStrategy.limitConfig.memoryLimitMultiplier` - Must be between 1.0 and 10.0
- `spec.updateStrategy.gradualDecreaseConfig.memoryDecreasePercentage` - Must be between 1 and 50
- `spec.updateStrategy.gradualDecreaseConfig.maximumTotalDecrease` - Must be between 1 and 90

### Label Selector Validation
- `matchExpressions[].key` - Required for each expression
- `matchExpressions[].operator` - Must be one of: `In`, `NotIn`, `Exists`, `DoesNotExist`
- `matchExpressions[].values` - Required for `In` and `NotIn` operators
- `matchExpressions[].values` - Must not be specified for `Exists` and `DoesNotExist` operators

### Workload Type Validation
- All workload types must be one of: `Deployment`, `StatefulSet`, `DaemonSet`

## kubectl Commands

### Create a policy
```bash
kubectl apply -f policy.yaml
```

### List policies
```bash
kubectl get optimizationpolicies
# or use short name
kubectl get optpol
```

### Describe a policy
```bash
kubectl describe optimizationpolicy production-policy
```

### View policy status
```bash
kubectl get optimizationpolicy production-policy -o yaml
```

### Delete a policy
```bash
kubectl delete optimizationpolicy production-policy
```

### Watch policy changes
```bash
kubectl get optimizationpolicies --watch
```

## Related Documentation

- [Creating Policies](../guides/creating-policies.md) - Policy configuration guide
- [Annotations Reference](annotations.md) - Workload annotation format
- [Operational Modes](../concepts/modes.md) - Understanding Auto, Recommend, and Disabled modes
- [Update Strategies](../concepts/update-strategies.md) - SSA vs Webhook strategies
- [Safety Model](../concepts/safety-model.md) - Understanding safety guarantees
