# OptimizationPolicy CRD Reference

Complete reference documentation for the `OptimizationPolicy` Custom Resource Definition.

## API Version

- **Group**: `optipod.optipod.io`
- **Version**: `v1alpha1`
- **Kind**: `OptimizationPolicy`
- **Scope**: Namespaced
- **Short Name**: `optpol`

## Overview

The `OptimizationPolicy` CRD defines how OptiPod should optimize workload resources. Each policy specifies:
- Which workloads to target (via selectors)
- How to collect and analyze metrics
- Resource bounds and safety constraints
- How to apply changes (auto, recommend, or disabled)

## Spec Fields

### mode (required)

**Type**: `string`  
**Enum**: `Auto`, `Recommend`, `Disabled`  
**Description**: Operational mode for the policy

- **Auto**: Automatically applies resource recommendations to matching workloads
- **Recommend**: Computes recommendations but stores them in status without applying
- **Disabled**: Stops processing workloads while preserving historical status

**Example**:
```yaml
spec:
  mode: Auto
```

### selector (required)

**Type**: `object`  
**Description**: Defines which workloads this policy applies to

#### selector.namespaceSelector

**Type**: `LabelSelector`  
**Optional**: Yes  
**Description**: Selects namespaces by labels

**Example**:
```yaml
selector:
  namespaceSelector:
    matchLabels:
      environment: production
    matchExpressions:
    - key: team
      operator: In
      values: [backend, frontend]
```

#### selector.workloadSelector

**Type**: `LabelSelector`  
**Optional**: Yes  
**Description**: Selects workloads (Deployments, StatefulSets, DaemonSets) by labels

**Example**:
```yaml
selector:
  workloadSelector:
    matchLabels:
      optimize: "true"
    matchExpressions:
    - key: tier
      operator: NotIn
      values: [critical]
```

#### selector.namespaces

**Type**: `object`  
**Optional**: Yes  
**Description**: Allow/deny lists for namespace filtering

**Fields**:
- `allow` ([]string): List of namespaces to include
- `deny` ([]string): List of namespaces to exclude (takes precedence)

**Example**:
```yaml
selector:
  namespaces:
    allow:
    - default
    - production
    - staging
    deny:
    - kube-system
    - kube-public
```

**Note**: At least one of `namespaceSelector`, `workloadSelector`, or `namespaces` must be specified.

### metricsConfig (required)

**Type**: `object`  
**Description**: Defines how metrics are collected and processed

#### metricsConfig.provider (required)

**Type**: `string`  
**Enum**: `prometheus`, `metrics-server`, `custom`  
**Description**: Metrics backend to use

**Example**:
```yaml
metricsConfig:
  provider: prometheus
```

#### metricsConfig.rollingWindow

**Type**: `Duration`  
**Default**: `24h`  
**Optional**: Yes  
**Description**: Time period over which metrics are aggregated

**Valid values**: Any valid Go duration string (e.g., `1h`, `24h`, `7d`)

**Example**:
```yaml
metricsConfig:
  rollingWindow: 48h
```

#### metricsConfig.percentile

**Type**: `string`  
**Enum**: `P50`, `P90`, `P99`  
**Default**: `P90`  
**Optional**: Yes  
**Description**: Which percentile to use for recommendations

- **P50**: Median usage (50th percentile) - more aggressive optimization
- **P90**: 90th percentile - balanced approach (recommended)
- **P99**: 99th percentile - conservative, handles spikes better

**Example**:
```yaml
metricsConfig:
  percentile: P90
```

#### metricsConfig.safetyFactor

**Type**: `float64`  
**Default**: `1.2`  
**Minimum**: `1.0`  
**Optional**: Yes  
**Description**: Multiplier applied to selected percentile for safety margin

**Example**:
```yaml
metricsConfig:
  safetyFactor: 1.3  # 30% safety margin
```

### resourceBounds (required)

**Type**: `object`  
**Description**: Min/max constraints for CPU and memory recommendations

#### resourceBounds.cpu (required)

**Type**: `object`  
**Description**: CPU resource bounds

**Fields**:
- `min` (Quantity, required): Minimum CPU request
- `max` (Quantity, required): Maximum CPU request

**Example**:
```yaml
resourceBounds:
  cpu:
    min: "100m"   # 0.1 CPU cores
    max: "4000m"  # 4 CPU cores
```

#### resourceBounds.memory (required)

**Type**: `object`  
**Description**: Memory resource bounds

**Fields**:
- `min` (Quantity, required): Minimum memory request
- `max` (Quantity, required): Maximum memory request

**Example**:
```yaml
resourceBounds:
  memory:
    min: "128Mi"  # 128 mebibytes
    max: "8Gi"    # 8 gibibytes
```

**Validation**: `min` must be less than or equal to `max` for both CPU and memory.

### updateStrategy (required)

**Type**: `object`  
**Description**: Defines how resource updates are applied to workloads

#### updateStrategy.allowInPlaceResize

**Type**: `boolean`  
**Default**: `true`  
**Optional**: Yes  
**Description**: Enable in-place pod resize when supported (Kubernetes 1.29+)

**Example**:
```yaml
updateStrategy:
  allowInPlaceResize: true
```

#### updateStrategy.allowRecreate

**Type**: `boolean`  
**Default**: `false`  
**Optional**: Yes  
**Description**: Allow pod recreation when in-place resize is not available

**Warning**: Setting to `true` will cause pod restarts when changes cannot be applied in-place.

**Example**:
```yaml
updateStrategy:
  allowRecreate: false  # Skip changes requiring recreation
```

#### updateStrategy.updateRequestsOnly

**Type**: `boolean`  
**Default**: `true`  
**Optional**: Yes  
**Description**: Update only resource requests, leaving limits unchanged

**Example**:
```yaml
updateStrategy:
  updateRequestsOnly: true
```

### reconciliationInterval

**Type**: `Duration`  
**Default**: `5m`  
**Optional**: Yes  
**Description**: How often the policy is evaluated and applied

**Example**:
```yaml
reconciliationInterval: 10m
```

## Status Fields

The status is automatically populated by OptiPod and should not be manually edited.

### conditions

**Type**: `[]Condition`  
**Description**: Standard Kubernetes conditions

**Common condition types**:
- `Ready`: Policy is valid and processing workloads
- `Error`: Policy has validation or processing errors

**Example**:
```yaml
status:
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-01-15T10:00:00Z"
    reason: PolicyValid
    message: "Policy is active and processing workloads"
```

### workloads

**Type**: `[]WorkloadStatus`  
**Description**: Per-workload optimization status

#### WorkloadStatus Fields

- `name` (string): Workload name
- `namespace` (string): Workload namespace
- `kind` (string): Workload kind (Deployment, StatefulSet, DaemonSet)
- `lastRecommendation` (Time): Timestamp of last recommendation
- `lastApplied` (Time): Timestamp of last applied change
- `recommendations` ([]ContainerRecommendation): Per-container recommendations
- `status` (string): Current state (Applied, Skipped, Error, Pending)
- `reason` (string): Additional context

**Example**:
```yaml
status:
  workloads:
  - name: web-deployment
    namespace: production
    kind: Deployment
    lastRecommendation: "2024-01-15T10:05:00Z"
    lastApplied: "2024-01-15T10:05:00Z"
    recommendations:
    - container: nginx
      cpu: "500m"
      memory: "512Mi"
      explanation: "P90 usage: 416m CPU, 426Mi memory; applied 1.2x safety factor"
    - container: sidecar
      cpu: "100m"
      memory: "128Mi"
      explanation: "P90 usage: 83m CPU, 106Mi memory; applied 1.2x safety factor"
    status: Applied
    reason: "Successfully updated resource requests"
```

## Complete Example

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-workloads
  namespace: default
spec:
  # Operational mode
  mode: Auto
  
  # Target workload selection
  selector:
    # Select namespaces with environment=production label
    namespaceSelector:
      matchLabels:
        environment: production
    
    # Select workloads with optimize=true label
    workloadSelector:
      matchLabels:
        optimize: "true"
    
    # Additional namespace filtering
    namespaces:
      allow:
      - default
      - production
      - staging
      deny:
      - kube-system
      - kube-public
  
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
      min: "128Mi"
      max: "8Gi"
  
  # Update behavior
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  
  # Reconciliation frequency
  reconciliationInterval: 5m
```

## Validation Rules

OptiPod validates policies on creation and update:

1. **Mode**: Must be one of `Auto`, `Recommend`, or `Disabled`
2. **Selector**: At least one selector type must be specified
3. **Label Selectors**: Must have valid syntax (valid operators, required values for In/NotIn)
4. **Provider**: Must be specified
5. **CPU Bounds**: `min` ≤ `max`, both must be > 0
6. **Memory Bounds**: `min` ≤ `max`, both must be > 0
7. **Safety Factor**: Must be ≥ 1.0

Invalid policies are rejected with descriptive error messages.

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
kubectl describe optimizationpolicy production-workloads
```

### Get policy status
```bash
kubectl get optimizationpolicy production-workloads -o yaml
```

### Watch policy changes
```bash
kubectl get optimizationpolicy -w
```

### Delete a policy
```bash
kubectl delete optimizationpolicy production-workloads
```

## Best Practices

1. **Start with Recommend Mode**: Test policies in Recommend mode before switching to Auto
2. **Use Conservative Percentiles**: Start with P90 or P99, adjust based on workload behavior
3. **Set Appropriate Bounds**: Ensure min/max bounds match your workload requirements
4. **Monitor Status**: Regularly check policy status for errors or skipped workloads
5. **Use Specific Selectors**: Target specific workloads rather than broad selectors
6. **Test Safety Factors**: Adjust safety factors based on workload variability
7. **Review Recommendations**: In Recommend mode, review suggestions before enabling Auto mode

## See Also

- [Example Policies](EXAMPLES.md)
- [Metrics Configuration](METRICS.md)
- [Troubleshooting](TROUBLESHOOTING.md)
