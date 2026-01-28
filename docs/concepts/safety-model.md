# Safety Model

OptiPod is designed with safety as a core principle. This document explains the safety guarantees, constraints, and mechanisms that protect your workloads during optimization.

## Safety Philosophy

OptiPod follows a "safe by default" approach:

1. **Recommend First**: Default mode generates recommendations without mutations
2. **Explicit Opt-In**: Auto mode requires explicit configuration
3. **Policy-Driven Bounds**: All changes constrained by policy limits
4. **Conservative Defaults**: Safety factors and bounds prevent extreme changes
5. **Explainable Decisions**: Every recommendation includes reasoning

## Safety Layers

OptiPod implements multiple layers of safety controls:

```
User Request
    ↓
Policy Mode Check (Recommend/Auto/Disabled)
    ↓
Global Dry-Run Check
    ↓
Resource Bounds Enforcement (Min/Max)
    ↓
Safety Factor Application (1.2x default)
    ↓
Memory Safety Checks
    ↓
Update Strategy Validation
    ↓
RBAC Permission Check
    ↓
Applied Change (if all checks pass)
```

## Resource Bounds

### Purpose

Resource bounds define the acceptable range for CPU and memory recommendations. They prevent OptiPod from making extreme changes that could destabilize workloads.

### Configuration

Bounds are required in every policy:

```yaml
spec:
  resourceBounds:
    cpu:
      min: "100m"    # Minimum CPU request
      max: "4000m"   # Maximum CPU request
    memory:
      min: "128Mi"   # Minimum memory request
      max: "8Gi"     # Maximum memory request
```

### Enforcement

OptiPod clamps all recommendations to configured bounds:

```
If recommendation < min → Use min
If recommendation > max → Use max
Otherwise → Use recommendation
```

**Example:**
- Bound: 100m - 2000m
- Raw recommendation: 50m → Applied: 100m (clamped to min)
- Raw recommendation: 500m → Applied: 500m (within bounds)
- Raw recommendation: 3000m → Applied: 2000m (clamped to max)

### Best Practices

**Conservative Bounds:**
```yaml
# Start with wide bounds for initial assessment
cpu:
  min: "50m"
  max: "8000m"
memory:
  min: "64Mi"
  max: "16Gi"
```

**Production Bounds:**
```yaml
# Tighter bounds for production workloads
cpu:
  min: "100m"
  max: "2000m"
memory:
  min: "256Mi"
  max: "4Gi"
```

**Per-Workload Bounds:**
```yaml
# Different policies for different workload classes
---
# High-resource workloads
cpu:
  min: "1000m"
  max: "8000m"
memory:
  min: "2Gi"
  max: "16Gi"
---
# Low-resource workloads
cpu:
  min: "50m"
  max: "500m"
memory:
  min: "64Mi"
  max: "512Mi"
```

## Safety Factor

### Purpose

The safety factor adds a buffer above observed usage to prevent resource starvation during traffic spikes or workload changes.

### Configuration

```yaml
spec:
  metricsConfig:
    percentile: P90        # Use 90th percentile of usage
    safetyFactor: 1.2      # Add 20% buffer
```

### Calculation

```
Recommendation = Percentile(Usage) × SafetyFactor
```

**Example:**
- P90 CPU usage: 200m
- Safety factor: 1.2
- Recommendation: 200m × 1.2 = 240m

### Recommended Values

| Workload Type | Safety Factor | Reasoning |
|---------------|---------------|-----------|
| Stable, predictable | 1.1 - 1.2 | Minimal buffer needed |
| Variable traffic | 1.2 - 1.5 | Handle traffic spikes |
| Bursty workloads | 1.5 - 2.0 | Large buffer for bursts |
| Critical services | 1.5 - 2.0 | Extra safety margin |

### Validation

OptiPod validates safety factor configuration:

```yaml
# Valid
safetyFactor: 1.0   # Minimum (no buffer)
safetyFactor: 1.2   # Recommended
safetyFactor: 2.0   # Conservative

# Invalid
safetyFactor: 0.9   # Error: must be >= 1.0
```

## Memory Safety

### Why Memory is Special

Memory is different from CPU:
- **CPU throttling**: Pods slow down but keep running
- **Memory OOM**: Pods are killed immediately
- **No graceful degradation**: Memory limits are hard boundaries

### Memory Safety Checks

OptiPod implements special protections for memory:

**1. Unsafe Memory Decrease Prevention**

By default, OptiPod blocks memory decreases that could cause OOM:

```yaml
spec:
  updateStrategy:
    allowUnsafeMemoryDecrease: false  # Default: block unsafe decreases
```

**2. Gradual Memory Decrease (Not Yet Implemented)**

> **⚠️ Status**: The `gradualDecreaseConfig` field is defined in the CRD schema and validated, but **not currently implemented** in the controller. Setting this configuration has no effect on actual behavior.

The planned feature would apply large memory reductions incrementally:

```yaml
spec:
  updateStrategy:
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10      # Max 10% decrease per reconciliation
      minimumDecreaseThreshold: 100Mi   # Threshold to trigger gradual decrease
      maximumTotalDecrease: 70          # Max 70% total decrease from original
```

**Planned behavior:**
- Current memory: 2Gi
- Recommendation: 1Gi (50% decrease)
- With gradual decrease (10% per cycle):
  - Cycle 1: 2Gi → 1.8Gi (10% decrease)
  - Cycle 2: 1.8Gi → 1.62Gi (10% decrease)
  - Cycle 3: 1.62Gi → 1.46Gi (10% decrease)
  - Cycle 4: 1.46Gi → 1.31Gi (10% decrease)
  - Cycle 5: 1.31Gi → 1.18Gi (10% decrease)
  - Cycle 6: 1.18Gi → 1.06Gi (10% decrease)
  - Cycle 7: 1.06Gi → 1Gi (final)

**Current behavior**: Memory decreases are applied immediately in full, subject to safety checks.

**3. Memory Limit Multipliers**

OptiPod calculates memory limits with a buffer above requests:

```yaml
spec:
  updateStrategy:
    limitConfig:
      memoryLimitMultiplier: 1.1  # Limit = Request × 1.1 (10% buffer)
```

Default: 1.1 (10% buffer above request)

### Memory Safety Best Practices

**1. Start Conservative:**
```yaml
resourceBounds:
  memory:
    min: "256Mi"   # Higher minimum
    max: "4Gi"     # Conservative maximum

metricsConfig:
  safetyFactor: 1.5  # Larger buffer for memory
```

**2. Enable Gradual Decrease (Not Yet Implemented):**

> **⚠️ Note**: This feature is not yet implemented. The configuration is accepted but has no effect.

```yaml
updateStrategy:
  gradualDecreaseConfig:
    enabled: true
    memoryDecreasePercentage: 10
```

**3. Monitor Memory Usage:**
```bash
# Watch for OOMKills
kubectl get events --field-selector reason=OOMKilled

# Check memory metrics
kubectl top pods
```

**4. Test in Non-Production:**
- Validate memory recommendations in staging
- Monitor for OOMKills during testing
- Adjust bounds based on observations

## Update Strategy Safety

### Strategy Selection

OptiPod supports two update strategies with different safety characteristics:

**Webhook Strategy (Safer for GitOps):**
- No direct spec mutations
- Changes applied at pod creation time
- Compatible with ArgoCD/Flux
- Requires webhook infrastructure

**SSA Strategy (Direct Updates):**
- Immediate spec updates
- May conflict with GitOps
- Simpler infrastructure
- Requires SSA permissions

### Rollout Strategy

Control when changes take effect:

```yaml
spec:
  updateStrategy:
    rolloutStrategy: onNextRestart  # Safer: wait for natural restart
    # rolloutStrategy: immediate    # Riskier: trigger restart immediately
```

**onNextRestart (Recommended):**
- Changes applied during next natural pod restart
- No forced disruption
- Gradual rollout as pods restart
- Lower risk

**immediate:**
- Triggers rolling restart immediately
- Faster optimization
- Controlled disruption
- Higher risk

### In-Place Resize Safety

```yaml
spec:
  updateStrategy:
    allowInPlaceResize: true   # Use in-place resize if available
    allowRecreate: false       # Block pod recreation
```

**Safety considerations:**
- In-place resize: No pod restart (safest)
- Pod recreation: Full restart (riskier)
- Block recreation unless explicitly needed

### Update Scope Control

```yaml
spec:
  updateStrategy:
    updateRequestsOnly: true  # Only update requests, not limits
```

**Requests only (Safer):**
- Updates resource requests
- Preserves existing limits
- Lower risk of hitting limits
- Recommended for initial rollout

**Requests and limits:**
- Updates both requests and limits
- Calculated using multipliers
- Higher optimization potential
- Requires careful configuration

## Policy Mode Safety

### Mode Hierarchy

Modes provide progressive safety levels:

```
Disabled → Recommend → Auto
(Safest)              (Most Automated)
```

### Mode Transitions

**Safe path:**
1. Start in Recommend mode
2. Validate recommendations
3. Switch to Auto for non-critical workloads
4. Monitor and expand gradually

**Emergency revert:**
```bash
# Immediately stop all automated changes
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'
```

## Global Dry-Run

### Purpose

Global dry-run provides a cluster-wide safety override:

```bash
# Controller flag
--dry-run=true
```

### Behavior

When enabled:
- All policies compute recommendations
- No changes are applied (even in Auto mode)
- Annotations are still written
- Useful for testing and validation

### Use Cases

- Testing new OptiPod versions
- Validating policy changes
- Troubleshooting issues
- Compliance audits

## RBAC Safety

### Principle of Least Privilege

OptiPod requests minimal permissions:

**Controller permissions:**
- Read: Namespaces, Pods, Workloads, Metrics
- Write: Workloads (for SSA), Events
- No cluster-admin required

**Webhook permissions:**
- Read: Workloads (for annotations)
- Write: MutatingWebhookConfiguration
- No workload mutation permissions

### Permission Validation

OptiPod handles RBAC errors gracefully:
- Logs permission errors
- Continues with other workloads
- Emits events for visibility
- Does not fail entire reconciliation

## Validation and Constraints

### Policy Validation

OptiPod validates policies before processing:

```yaml
# Valid policy
spec:
  mode: Auto
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"  # max > min ✓
```

```yaml
# Invalid policy
spec:
  mode: Auto
  resourceBounds:
    cpu:
      min: "2000m"
      max: "100m"  # max < min ✗ (validation error)
```

### Constraint Enforcement

**Required fields:**
- mode (Auto/Recommend/Disabled)
- selector (at least one selector type)
- metricsConfig.provider
- resourceBounds (CPU and memory)

**Value constraints:**
- safetyFactor >= 1.0
- weight: 1-1000
- percentile: P50, P90, or P99

### Validation Errors

When validation fails:
- Policy marked as not ready
- Event emitted with error details
- No workloads processed
- User must fix policy configuration

## Observability for Safety

### Metrics

Monitor safety-related metrics:

```promql
# Optimization failures
rate(optipod_optimization_failure_total[5m])

# Resource change magnitude
optipod_resource_change_magnitude_percent

# Reconciliation errors
rate(optipod_reconciliation_errors_total[5m])
```

### Events

Watch for safety-related events:

```bash
# Policy validation errors
kubectl get events --field-selector reason=ValidationFailed

# Optimization failures
kubectl get events --field-selector reason=OptimizationFailed

# Memory safety blocks
kubectl get events --field-selector reason=UnsafeMemoryDecrease
```

### Logs

Enable debug logging for detailed safety information:

```yaml
# Controller deployment
args:
  - --zap-log-level=debug
```

## Safety Checklist

Before enabling Auto mode:

- [ ] Validate recommendations in Recommend mode
- [ ] Set conservative resource bounds
- [ ] Configure appropriate safety factor (1.2-1.5)
- [ ] ~~Enable gradual memory decrease~~ (Not yet implemented)
- [ ] Choose appropriate update strategy
- [ ] Test on non-critical workloads first
- [ ] Set up monitoring and alerts
- [ ] Document rollback procedures
- [ ] Review RBAC permissions
- [ ] Test in staging environment

## Emergency Procedures

### Stop All Optimizations

```bash
# Method 1: Switch all policies to Recommend mode
kubectl get optimizationpolicy --all-namespaces -o name | \
  xargs -I {} kubectl patch {} \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'

# Method 2: Enable global dry-run
kubectl set env deployment/optipod-controller \
  -n optipod-system \
  DRY_RUN=true
```

### Rollback Changes

```bash
# Revert to previous workload spec (if using GitOps)
git revert <commit>
git push

# Manual revert (if using SSA)
kubectl patch deployment my-app \
  --type merge \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"app","resources":{"requests":{"cpu":"500m","memory":"1Gi"}}}]}}}}'
```

### Investigate Issues

```bash
# Check policy status
kubectl describe optimizationpolicy my-policy

# View recent events
kubectl get events --sort-by='.lastTimestamp' | grep optipod

# Check controller logs
kubectl logs -n optipod-system deployment/optipod-controller

# Check webhook logs (if using webhook strategy)
kubectl logs -n optipod-system deployment/optipod-webhook
```

## Related Documentation

- [Modes](modes.md) - Operational modes and transitions
- [Update Strategies](update-strategies.md) - SSA vs Webhook strategies
- [Architecture](architecture.md) - System design and components
- [Troubleshooting](../guides/troubleshooting.md) - Common issues and solutions
