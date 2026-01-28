# Creating Optimization Policies

This guide explains how to create and configure OptimizationPolicy resources to optimize your Kubernetes workloads.

## Overview

An OptimizationPolicy defines:
- **Which workloads** to optimize (selector)
- **How to collect metrics** (metricsConfig)
- **Resource constraints** (resourceBounds)
- **How to apply changes** (updateStrategy)
- **When to apply changes** (mode)

## Quick Start

Here's a minimal policy to get started:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: my-first-policy
  namespace: default
spec:
  mode: Recommend  # Start in safe mode
  
  selector:
    namespaceSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

## Required Fields

Every policy must specify these fields:

### 1. Mode

Defines the operational behavior:

```yaml
spec:
  mode: Recommend  # or Auto, Disabled
```

**Options:**
- `Recommend`: Compute recommendations, don't apply (safe default)
- `Auto`: Automatically apply recommendations
- `Disabled`: Stop processing workloads

**Best practice:** Start with `Recommend` mode to validate recommendations before enabling `Auto`.

### 2. Selector

Defines which workloads the policy applies to. At least one selector type is required:

```yaml
spec:
  selector:
    # Option 1: Select by namespace labels
    namespaceSelector:
      matchLabels:
        environment: production
    
    # Option 2: Select by workload labels
    workloadSelector:
      matchLabels:
        optimize: "true"
    
    # Option 3: Select by namespace names
    namespaces:
      allow:
        - production
        - staging
      deny:
        - kube-system
```

### 3. Metrics Configuration

Defines how metrics are collected:

```yaml
spec:
  metricsConfig:
    provider: prometheus  # or metrics-server
    rollingWindow: 24h    # Time window for metrics
    percentile: P90       # P50, P90, or P99
    safetyFactor: 1.2     # Multiplier >= 1.0
```

### 4. Resource Bounds

Defines min/max constraints for recommendations:

```yaml
spec:
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
```

### 5. Update Strategy

Defines how changes are applied:

```yaml
spec:
  updateStrategy:
    strategy: webhook              # or ssa
    rolloutStrategy: onNextRestart # or immediate
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

## Selector Configuration

### Namespace Selector

Select namespaces by labels:

```yaml
selector:
  namespaceSelector:
    matchLabels:
      environment: production
      team: platform
```

With match expressions:

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

### Workload Selector

Select workloads by labels:

```yaml
selector:
  workloadSelector:
    matchLabels:
      optimize: "true"
      tier: backend
```

### Namespace Allow/Deny Lists

Explicitly allow or deny namespaces:

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

**Note:** Deny takes precedence over allow.

### Workload Type Filtering

Target specific workload types:

```yaml
selector:
  # Include only specific types
  workloadTypes:
    include:
      - Deployment
      - StatefulSet
```

Or exclude specific types:

```yaml
selector:
  # Exclude specific types
  workloadTypes:
    exclude:
      - DaemonSet
```

**Note:** Exclude takes precedence over include.

### Combining Selectors

You can combine multiple selector types:

```yaml
selector:
  # Namespaces with label environment=production
  namespaceSelector:
    matchLabels:
      environment: production
  
  # Workloads with label optimize=true
  workloadSelector:
    matchLabels:
      optimize: "true"
  
  # But not in kube-system
  namespaces:
    deny:
      - kube-system
  
  # Only Deployments
  workloadTypes:
    include:
      - Deployment
```

## Metrics Configuration

### Provider Selection

OptiPod supports two metrics providers:

**Prometheus (Recommended):**
```yaml
metricsConfig:
  provider: prometheus
  rollingWindow: 24h
  percentile: P90
  safetyFactor: 1.2
```

**Metrics-Server:**
```yaml
metricsConfig:
  provider: metrics-server
  rollingWindow: 12h
  percentile: P90
  safetyFactor: 1.2
  metricsServer:
    minSamplesRequired: 10
```

### Rolling Window

Time period for metrics aggregation:

```yaml
metricsConfig:
  rollingWindow: 24h  # 24 hours of data
```

**Recommendations:**
- Production: `24h` or `48h`
- Development: `12h`
- Stateful workloads: `48h` or longer

### Percentile

Which percentile to use for recommendations:

```yaml
metricsConfig:
  percentile: P90  # P50, P90, or P99
```

**Recommendations:**
- Stable workloads: `P90`
- Variable traffic: `P95` or `P99`
- Development: `P50` or `P90`

### Safety Factor

Multiplier applied to percentile value:

```yaml
metricsConfig:
  safetyFactor: 1.2  # 20% buffer
```

**Recommendations:**
- Stable workloads: `1.1` - `1.2`
- Variable traffic: `1.2` - `1.5`
- Bursty workloads: `1.5` - `2.0`
- Critical services: `1.5` - `2.0`

**Calculation:**
```
Recommendation = Percentile(Usage) × SafetyFactor
```

## Resource Bounds

### CPU Bounds

```yaml
resourceBounds:
  cpu:
    min: "100m"    # Minimum CPU request
    max: "4000m"   # Maximum CPU request
```

**Recommendations:**
- Start with wide bounds for assessment
- Tighten based on observed usage
- Consider workload characteristics

**Examples:**

Conservative (initial assessment):
```yaml
cpu:
  min: "50m"
  max: "8000m"
```

Production (after assessment):
```yaml
cpu:
  min: "100m"
  max: "2000m"
```

High-resource workloads:
```yaml
cpu:
  min: "1000m"
  max: "8000m"
```

### Memory Bounds

```yaml
resourceBounds:
  memory:
    min: "128Mi"   # Minimum memory request
    max: "8Gi"     # Maximum memory request
```

**Recommendations:**
- Be conservative with memory (OOM kills pods)
- Start with higher minimums
- Monitor for OOMKills

**Examples:**

Conservative:
```yaml
memory:
  min: "256Mi"
  max: "4Gi"
```

High-memory workloads:
```yaml
memory:
  min: "2Gi"
  max: "16Gi"
```

### Enforcement

OptiPod clamps recommendations to bounds:

```
If recommendation < min → Use min
If recommendation > max → Use max
Otherwise → Use recommendation
```

## Update Strategy

### Strategy Selection

Choose between SSA and Webhook strategies:

**Webhook (Default - GitOps Compatible):**
```yaml
updateStrategy:
  strategy: webhook
  rolloutStrategy: onNextRestart
```

**SSA (Simpler Infrastructure):**
```yaml
updateStrategy:
  strategy: ssa
  useServerSideApply: true
```

See [Update Strategies](../concepts/update-strategies.md) for detailed comparison.

### Rollout Strategy

Control when changes take effect (webhook strategy only):

**onNextRestart (Safer):**
```yaml
updateStrategy:
  rolloutStrategy: onNextRestart
```
- Waits for natural pod restart
- No forced disruption
- Gradual rollout

**immediate (Faster):**
```yaml
updateStrategy:
  rolloutStrategy: immediate
```
- Triggers rolling restart
- Faster optimization
- Controlled disruption

### In-Place Resize

Enable in-place pod resize (Kubernetes 1.27+):

```yaml
updateStrategy:
  allowInPlaceResize: true   # Use if available
  allowRecreate: false       # Block pod recreation
```

### Update Scope

Control what gets updated:

**Requests Only (Safer):**
```yaml
updateStrategy:
  updateRequestsOnly: true
```

**Requests and Limits:**
```yaml
updateStrategy:
  updateRequestsOnly: false
  limitConfig:
    cpuLimitMultiplier: 1.5      # Limit = Request × 1.5
    memoryLimitMultiplier: 1.1   # Limit = Request × 1.1
```

### Memory Safety

Configure memory safety features:

**Gradual Memory Decrease:**
```yaml
updateStrategy:
  gradualDecreaseConfig:
    enabled: true
    memoryDecreasePercentage: 10      # Max 10% per cycle
    minimumDecreaseThreshold: 100Mi   # Threshold to trigger
    maximumTotalDecrease: 70          # Max 70% total decrease
```

**Unsafe Memory Decrease (Use with Caution):**
```yaml
updateStrategy:
  allowUnsafeMemoryDecrease: true  # Disable safety checks
```

## Optional Fields

### Weight

Priority when multiple policies match the same workload:

```yaml
spec:
  weight: 200  # Default: 100, Range: 1-1000
```

Higher weight = higher priority.

### Reconciliation Interval

How often the policy is evaluated:

```yaml
spec:
  reconciliationInterval: 5m  # Default: 5m
```

## Common Patterns

### Pattern 1: Production Workloads (GitOps)

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-gitops
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "256Mi"
      max: "8Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

### Pattern 2: Development Workloads (Aggressive)

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: development-aggressive
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: development
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 12h
    percentile: P90
    safetyFactor: 1.1
  
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "4Gi"
  
  updateStrategy:
    strategy: ssa
    allowInPlaceResize: true
    allowRecreate: true
    updateRequestsOnly: false
    limitConfig:
      cpuLimitMultiplier: 1.5
      memoryLimitMultiplier: 1.1
```

### Pattern 3: Stateful Workloads (Conservative)

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: stateful-conservative
spec:
  mode: Recommend  # Only recommend for stateful
  
  selector:
    workloadTypes:
      include:
        - StatefulSet
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h  # Longer window
    percentile: P95     # Higher percentile
    safetyFactor: 1.5   # Larger buffer
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "8000m"
    memory:
      min: "512Mi"
      max: "16Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10
```

### Pattern 4: Multiple Policies by Workload Type

```yaml
# Policy for Deployments
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: deployments-policy
spec:
  mode: Auto
  weight: 100
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadTypes:
      include:
        - Deployment
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.1
  
  resourceBounds:
    cpu:
      min: "50m"
      max: "4000m"
    memory:
      min: "64Mi"
      max: "8Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: true
    updateRequestsOnly: true

---
# Policy for StatefulSets
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: statefulsets-policy
spec:
  mode: Recommend
  weight: 100
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadTypes:
      include:
        - StatefulSet
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h
    percentile: P95
    safetyFactor: 1.3
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "8000m"
    memory:
      min: "256Mi"
      max: "16Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

## Validation

OptiPod validates policies before processing. Common validation errors:

### Invalid Mode

```yaml
# ❌ Invalid
spec:
  mode: AutoApply

# ✅ Valid
spec:
  mode: Auto
```

### Missing Selector

```yaml
# ❌ Invalid - no selector
spec:
  mode: Auto
  metricsConfig: ...

# ✅ Valid - at least one selector
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        optimize: "true"
```

### Invalid Bounds

```yaml
# ❌ Invalid - min > max
resourceBounds:
  cpu:
    min: "2000m"
    max: "100m"

# ✅ Valid - min <= max
resourceBounds:
  cpu:
    min: "100m"
    max: "2000m"
```

### Invalid Safety Factor

```yaml
# ❌ Invalid - must be >= 1.0
metricsConfig:
  safetyFactor: 0.9

# ✅ Valid
metricsConfig:
  safetyFactor: 1.2
```

## Troubleshooting

### Policy Not Processing Workloads

Check policy status:

```bash
kubectl describe optimizationpolicy my-policy
```

Common issues:
- Selector doesn't match any workloads
- Policy validation failed
- Metrics provider not configured
- RBAC permissions missing

### Recommendations Not Applied

Check mode and update strategy:

```bash
kubectl get optimizationpolicy my-policy -o yaml
```

Verify:
- Mode is `Auto` (not `Recommend`)
- Update strategy is configured
- Global dry-run is not enabled
- Workload matches selector

### Validation Errors

View events:

```bash
kubectl get events --field-selector reason=ValidationFailed
```

Fix validation errors and reapply policy.

## Best Practices

1. **Start with Recommend mode** to validate recommendations
2. **Use conservative bounds** initially
3. **Test in non-production** before enabling Auto mode
4. **Use webhook strategy** for GitOps environments
5. **Enable gradual memory decrease** for safety
6. **Monitor metrics** for optimization success/failure
7. **Document your policies** for team awareness
8. **Use workload type filtering** for gradual adoption
9. **Set appropriate safety factors** based on workload characteristics
10. **Review recommendations regularly** before switching to Auto mode

## Next Steps

- [Reviewing Recommendations](reviewing-recs.md) - How to review recommendations
- [Switching to Auto Mode](switching-to-auto.md) - Safely enable automatic optimization
- [Update Strategies](../concepts/update-strategies.md) - SSA vs Webhook strategies
- [Safety Model](../concepts/safety-model.md) - Understanding safety guarantees
- [CRD Reference](../reference/crd-spec.md) - Complete field documentation
