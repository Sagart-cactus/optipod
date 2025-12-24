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

#### selector.workloadTypes

**Type**: `object`  
**Optional**: Yes  
**Description**: Include/exclude filters for workload types (Deployment, StatefulSet, DaemonSet)

**Fields**:

- `include` ([]WorkloadType): List of workload types to include (if empty, includes all)
- `exclude` ([]WorkloadType): List of workload types to exclude (takes precedence over include)

**WorkloadType Values**: `Deployment`, `StatefulSet`, `DaemonSet`

**Precedence Rules**:

- If both `include` and `exclude` are specified, `exclude` takes precedence
- If a workload type appears in both lists, it will be excluded
- If `include` is empty or not specified, all workload types are included (backward compatibility)
- If filtering results in no valid workload types, the policy discovers no workloads

**Use Cases**:

- **Gradual Adoption**: Start with low-risk workloads like Deployments before expanding to StatefulSets
- **Risk Management**: Exclude critical stateful workloads (databases, caches) from optimization
- **Team Separation**: Different teams manage different workload types with different policies
- **Testing**: Test optimization on specific workload types before broader rollout

**Examples**:

Include only Deployments (gradual adoption):

```yaml
selector:
  workloadTypes:
    include:
      - Deployment
```

Exclude StatefulSets (protect stateful workloads):

```yaml
selector:
  workloadTypes:
    exclude:
      - StatefulSet
```

Include multiple types:

```yaml
selector:
  workloadTypes:
    include:
      - Deployment
      - DaemonSet
```

Exclude precedence (only StatefulSets and DaemonSets will be optimized):

```yaml
selector:
  workloadTypes:
    include:
      - Deployment
      - StatefulSet
      - DaemonSet
    exclude:
      - Deployment  # Exclude takes precedence
```

**Note**: At least one of `namespaceSelector`, `workloadSelector`, or `namespaces` must be specified.

### metricsConfig (required)

**Type**: `object`  
**Description**: Defines how metrics are collected and processed

#### metricsConfig.provider (required)

**Type**: `string`  
**Enum**: `metrics-server`, `prometheus`, `custom`  
**Description**: Metrics backend to use

**Current Status**:

- `metrics-server`: ✅ Fully supported and recommended
- `prometheus`: 🚧 In development (basic support available)
- `custom`: 📋 Planned for future release

**Example**:

```yaml
metricsConfig:
  provider: metrics-server  # Recommended for current version
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

#### updateStrategy.useServerSideApply

**Type**: `boolean`  
**Default**: `true`  
**Optional**: Yes  
**Description**: Enable Server-Side Apply (SSA) for field-level ownership tracking

When enabled, OptiPod uses Kubernetes Server-Side Apply to manage only resource requests and limits, allowing other tools
(like ArgoCD) to manage different fields without conflicts. SSA tracks field ownership in `managedFields` metadata.

**Benefits of SSA**:

- **GitOps Compatibility**: No sync conflicts with ArgoCD or Flux
- **Field-Level Ownership**: OptiPod owns only resource fields, other tools own their fields
- **Audit Trail**: `managedFields` shows which tool manages which fields
- **Conflict Resolution**: Built-in conflict detection and resolution

**When to use SSA** (recommended):

- Using GitOps tools (ArgoCD, Flux)
- Multiple tools managing the same workloads
- Need clear field ownership tracking
- Kubernetes 1.22+ clusters

**When to disable SSA** (not recommended):

- Kubernetes < 1.22 (SSA not available)
- Legacy environments requiring Strategic Merge Patch
- Specific compatibility requirements

**Example**:

```yaml
updateStrategy:
  useServerSideApply: true  # Default, recommended
```

**Disabling SSA** (may cause GitOps conflicts):

```yaml
updateStrategy:
  useServerSideApply: false  # Use Strategic Merge Patch
```

**See Also**: [ArgoCD Integration Guide](ARGOCD_INTEGRATION.md) for GitOps setup

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

### workloadsDiscovered

**Type**: `integer`  
**Description**: Total count of workloads matching this policy's selectors

### workloadsProcessed

**Type**: `integer`  
**Description**: Count of workloads successfully processed by this policy

### workloadsByType

**Type**: `object`  
**Optional**: Yes  
**Description**: Breakdown of discovered workloads by type

**Fields**:

- `deployments` (integer): Count of Deployment workloads
- `statefulSets` (integer): Count of StatefulSet workloads  
- `daemonSets` (integer): Count of DaemonSet workloads

This field is populated when workload type filtering is used and provides visibility into which workload types are being
discovered and processed.

**Example**:

```yaml
status:
  workloadsDiscovered: 15
  workloadsProcessed: 12
  workloadsByType:
    deployments: 8
    statefulSets: 4
    daemonSets: 3
```

### lastReconciliation

**Type**: `Time`  
**Description**: Timestamp of the last policy reconciliation

### workloads

**Type**: `[]WorkloadStatus`  
**Description**: Per-workload optimization status

#### WorkloadStatus Fields

- `name` (string): Workload name
- `namespace` (string): Workload namespace
- `kind` (string): Workload kind (Deployment, StatefulSet, DaemonSet)
- `lastRecommendation` (Time): Timestamp of last recommendation
- `lastApplied` (Time): Timestamp of last applied change
- `lastApplyMethod` (string): Patch method used ("ServerSideApply" or "StrategicMergePatch")
- `fieldOwnership` (boolean): Whether OptiPod owns resource fields via SSA
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
    lastApplyMethod: "ServerSideApply"
    fieldOwnership: true
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
    
    # Workload type filtering - exclude StatefulSets for safety
    workloadTypes:
      exclude:
        - StatefulSet
  
  # Metrics configuration
  metricsConfig:
    provider: metrics-server  # Recommended for current version
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
    useServerSideApply: true  # Enable SSA for GitOps compatibility
  
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

## Troubleshooting

### Field Ownership Conflicts

When using Server-Side Apply (SSA), field ownership conflicts can occur if multiple tools try to manage the same fields.

#### Symptoms

- OptiPod logs show SSA conflict errors
- Kubernetes events show `SSAConflict` reason
- Resource updates fail with conflict messages

#### Diagnosis

Check which tool currently owns the resource fields:

```bash
# View managedFields
kubectl get deployment <name> -n <namespace> -o yaml | grep -A 50 managedFields

# Check OptiPod logs
kubectl logs -n optipod-system deployment/optipod-manager

# Check for SSA conflict events
kubectl get events -n <namespace> --field-selector reason=SSAConflict
```

#### Common Causes

1. **Another tool owns resource fields**: kubectl, Helm, or another controller previously set resource requests/limits
2. **Multiple OptiPod policies**: Multiple policies targeting the same workload (should use same fieldManager)
3. **Manual kubectl edits**: Direct edits with kubectl can create ownership conflicts

#### Solutions

##### Solution 1: OptiPod takes ownership (recommended)

OptiPod uses `Force: true` by default, which automatically takes ownership of resource fields. If conflicts persist:

1. Verify SSA is enabled:

```yaml
updateStrategy:
  useServerSideApply: true
```

1. Check OptiPod has proper RBAC permissions:

```bash
kubectl auth can-i patch deployments --as=system:serviceaccount:optipod-system:optipod-controller-manager
```

1. Review OptiPod logs for specific error messages

##### Solution 2: Clear conflicting ownership

Manually remove the conflicting manager's ownership:

```bash
# Get current managedFields
kubectl get deployment <name> -n <namespace> -o json > deployment.json

# Edit deployment.json to remove conflicting manager from managedFields
# Then apply:
kubectl apply -f deployment.json
```

##### Solution 3: Use Strategic Merge Patch

Disable SSA if conflicts cannot be resolved (not recommended for GitOps):

```yaml
updateStrategy:
  useServerSideApply: false
```

**Note**: Disabling SSA will cause sync conflicts with ArgoCD and other GitOps tools.

#### Prevention

1. **Use SSA consistently**: Enable SSA for all tools managing the cluster (ArgoCD 2.5+, Flux, etc.)
2. **Avoid manual edits**: Use GitOps or OptiPod policies instead of direct kubectl edits
3. **Single policy per workload**: Ensure only one OptimizationPolicy targets each workload
4. **Label-based selection**: Use specific labels to control which workloads are optimized

### Policy Not Processing Workloads

#### Symptoms

- Policy status shows no workloads
- Workloads not being optimized despite matching selectors

#### Diagnosis

```bash
# Check policy status
kubectl describe optimizationpolicy <name>

# Check policy conditions
kubectl get optimizationpolicy <name> -o jsonpath='{.status.conditions}'

# Check workload type breakdown
kubectl get optimizationpolicy <name> -o jsonpath='{.status.workloadsByType}'

# Verify workload labels
kubectl get deployment <name> --show-labels
```

#### Common Causes

1. **Selector mismatch**: Workload labels don't match policy selectors
1. **Namespace filtering**: Workload namespace is in deny list or not in allow list
1. **Workload type filtering**: Workload type is excluded or not included in workloadTypes filter
1. **Policy mode**: Policy is in Disabled mode
1. **RBAC issues**: OptiPod lacks permissions to access workloads

#### Solutions

1. Verify workload has matching labels:

```bash
kubectl label deployment <name> optimize=true
```

1. Check namespace is allowed:

```yaml
selector:
  namespaces:
    allow:
    - <namespace>
```

1. Verify workload type is included:

```yaml
selector:
  workloadTypes:
    include:
    - Deployment  # Ensure workload type is included
    # exclude: []  # Ensure workload type is not excluded
```

1. Check for workload type filtering conflicts:

```bash
# If a Deployment is not being processed, check if it's excluded
kubectl get optimizationpolicy <name> -o jsonpath='{.spec.selector.workloadTypes.exclude}'
```

1. Ensure policy is in Auto or Recommend mode:

```bash
kubectl patch optimizationpolicy <name> --type=merge -p '{"spec":{"mode":"Auto"}}'
```

### Workload Type Filtering Issues

#### Symptoms

- Expected workload types not being discovered
- `workloadsByType` status shows unexpected counts
- Policy processes some workload types but not others

#### Diagnosis

```bash
# Check workload type filter configuration
kubectl get optimizationpolicy <name> -o jsonpath='{.spec.selector.workloadTypes}'

# Check workload type breakdown in status
kubectl get optimizationpolicy <name> -o jsonpath='{.status.workloadsByType}'

# List all workloads in target namespaces
kubectl get deployments,statefulsets,daemonsets -n <namespace> --show-labels
```

#### Common Causes

1. **Exclude precedence**: Workload type is in both include and exclude lists (exclude wins)
1. **Empty result set**: Include/exclude combination results in no valid workload types
1. **Case sensitivity**: Workload type names must match exactly (Deployment, StatefulSet, DaemonSet)
1. **Validation errors**: Invalid workload type names in configuration

#### Solutions

1. **Check exclude precedence**:

```yaml
# This will exclude Deployments even though they're in include
selector:
  workloadTypes:
    include: [Deployment, StatefulSet]
    exclude: [Deployment]  # Remove this or move to include only
```

1. **Verify workload type names**:

```yaml
# Correct (case-sensitive)
selector:
  workloadTypes:
    include:
    - Deployment      # ✓ Correct
    - StatefulSet     # ✓ Correct  
    - DaemonSet       # ✓ Correct
    # - deployment    # ✗ Wrong case
    # - statefulset   # ✗ Wrong case
```

1. **Check for empty result sets**:

```bash
# This policy will discover no workloads (all types excluded)
kubectl get optimizationpolicy <name> -o yaml | grep -A 10 workloadTypes
```

1. **Validate configuration**:

```bash
# Check for validation errors
kubectl describe optimizationpolicy <name> | grep -A 5 "Events:"
```

### Recommendations Not Applied

#### Symptoms

- Policy shows recommendations in status
- Workload resources not updated

#### Diagnosis

```bash
# Check policy mode
kubectl get optimizationpolicy <name> -o jsonpath='{.spec.mode}'

# Check workload status
kubectl get optimizationpolicy <name> -o jsonpath='{.status.workloads[*].status}'
```

#### Common Causes

1. **Recommend mode**: Policy is in Recommend mode (recommendations not auto-applied)
1. **Update strategy**: Changes require pod recreation but `allowRecreate: false`
1. **In-place resize unavailable**: Kubernetes < 1.29 and `allowRecreate: false`
1. **Bounds violation**: Recommendation exceeds min/max bounds

#### Solutions

1. Switch to Auto mode:

```bash
kubectl patch optimizationpolicy <name> --type=merge -p '{"spec":{"mode":"Auto"}}'
```

1. Allow pod recreation if needed:

```yaml
updateStrategy:
  allowRecreate: true
```

1. Adjust resource bounds:

```yaml
resourceBounds:
  cpu:
    max: "8000m"  # Increase if recommendations exceed current max
```

## Best Practices

1. **Start with Recommend Mode**: Test policies in Recommend mode before switching to Auto
1. **Use Conservative Percentiles**: Start with P90 or P99, adjust based on workload behavior
1. **Set Appropriate Bounds**: Ensure min/max bounds match your workload requirements
1. **Monitor Status**: Regularly check policy status for errors or skipped workloads
1. **Use Specific Selectors**: Target specific workloads rather than broad selectors
1. **Test Safety Factors**: Adjust safety factors based on workload variability
1. **Review Recommendations**: In Recommend mode, review suggestions before enabling Auto mode
1. **Enable SSA**: Use Server-Side Apply for GitOps compatibility (default)
1. **Monitor Field Ownership**: Regularly check managedFields to ensure proper ownership
1. **Use Labels Consistently**: Apply clear, consistent labels for workload selection

### Workload Type Filtering Best Practices

1. **Start with Deployments**: Begin optimization with stateless Deployments before expanding to StatefulSets
1. **Protect Critical Workloads**: Use exclude filters to protect databases, caches, and other critical stateful workloads
1. **Use Multiple Policies**: Create separate policies for different workload types with appropriate settings:
    - **Deployments**: More aggressive optimization (lower safety factors, allow recreation)
    - **StatefulSets**: Conservative optimization (higher safety factors, recommend mode)
    - **DaemonSets**: Careful optimization (consider node resource constraints)
1. **Monitor Workload Type Breakdown**: Check `workloadsByType` status to verify expected workload discovery
1. **Understand Exclude Precedence**: Remember that exclude always takes precedence over include
1. **Test Filter Combinations**: Verify that include/exclude combinations produce expected results
1. **Document Policy Intent**: Use clear naming and annotations to document which workload types each policy targets

### Example Multi-Policy Strategy

```yaml
# Policy 1: Aggressive optimization for stateless workloads
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: stateless-aggressive
spec:
  mode: Auto
  selector:
    workloadTypes:
      include: [Deployment]
  metricsConfig:
    safetyFactor: 1.1  # Lower safety factor
  updateStrategy:
    allowRecreate: true  # Allow pod recreation

---
# Policy 2: Conservative optimization for stateful workloads  
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: stateful-conservative
spec:
  mode: Recommend  # Only recommend, don't auto-apply
  selector:
    workloadTypes:
      include: [StatefulSet]
  metricsConfig:
    safetyFactor: 1.3  # Higher safety factor
    rollingWindow: 48h  # Longer observation window
  updateStrategy:
    allowRecreate: false  # Never recreate stateful pods
```

## See Also

- [Example Policies](EXAMPLES.md)
- [ArgoCD Integration](ARGOCD_INTEGRATION.md)
- [Installation Guide](INSTALLATION.md)
