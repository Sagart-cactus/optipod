# Operational Modes

OptiPod supports three operational modes that control how recommendations are generated and applied. This document explains each mode, when to use it, and how to transition between modes safely.

## Mode Overview

| Mode | Generates Recommendations | Applies Changes | Use Case |
|------|--------------------------|-----------------|----------|
| **Recommend** | ✅ Yes | ❌ No | Safe exploration, validation, GitOps review |
| **Auto** | ✅ Yes | ✅ Yes | Automated optimization with policy controls |
| **Disabled** | ❌ No | ❌ No | Temporary pause, policy maintenance |

## Recommend Mode

**Default and safest mode for getting started.**

### What It Does

- Discovers workloads matching policy selectors
- Fetches metrics from configured provider
- Computes resource recommendations
- Stores recommendations as annotations on workloads
- Updates policy status with aggregate counts
- **Does NOT modify workload specs**
- **Does NOT restart pods**

### Recommendation Storage

Recommendations are stored as annotations on individual workloads:

```yaml
metadata:
  annotations:
    optipod.io/managed: "true"
    optipod.io/policy: "my-policy"
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
    optipod.io/recommendation.app-container.cpu-request: "250m"
    optipod.io/recommendation.app-container.memory-request: "512Mi"
    optipod.io/recommendation.app-container.cpu-limit: "500m"
    optipod.io/recommendation.app-container.memory-limit: "1Gi"
```

### When to Use

- **Initial assessment**: Understand optimization potential before committing
- **Validation**: Verify recommendations align with expectations
- **GitOps workflows**: Review recommendations in Git before applying
- **Compliance**: Document recommendations for audit trails
- **Gradual rollout**: Test on non-critical workloads first

### Example Configuration

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: safe-recommendations
  namespace: default
spec:
  mode: Recommend  # Safe mode - no mutations

  selector:
    workloadSelector:
      matchLabels:
        optipod.io/enabled: "true"

  metricsConfig:
    provider: metrics-server
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
    # Strategy configuration is ignored in Recommend mode
    # but good to define for future Auto mode transition
    strategy: webhook
    rolloutStrategy: onNextRestart
    updateRequestsOnly: true
```

### Viewing Recommendations

**Individual workload:**
```bash
kubectl get deployment my-app -o yaml | grep -A10 "optipod.io/recommendation"
```

**Policy status (aggregate only):**
```bash
kubectl describe optimizationpolicy safe-recommendations
```

**Generate impact report:**
```bash
curl -fsSL https://raw.githubusercontent.com/Sagart-cactus/optipod/main/scripts/optipod-recommendation-report.sh | bash -s -- -o html -f report.html
```

## Auto Mode

**Automated optimization with policy-driven safety controls.**

### What It Does

- Everything Recommend mode does, plus:
- **Applies recommendations** using configured strategy
- **Triggers pod restarts** (if using immediate rollout)
- Respects resource bounds and safety constraints
- Honors update strategy configuration

### Application Strategies

Auto mode supports two strategies for applying recommendations:

**Webhook Strategy (Default):**
- Stores recommendations as annotations
- Webhook injects values during pod creation
- GitOps-safe (no spec mutations)
- Requires webhook server and cert-manager

**SSA Strategy:**
- Directly patches workload specs
- Uses Server-Side Apply for field ownership
- Immediate API updates
- May conflict with GitOps tools

### When to Use

- **Production optimization**: After validating recommendations in Recommend mode
- **Continuous optimization**: Adapt to changing workload patterns
- **Cost reduction**: Automatically right-size resources
- **Performance tuning**: Maintain optimal resource allocation

### Safety Considerations

Before enabling Auto mode:

1. **Validate recommendations** in Recommend mode first
2. **Set conservative bounds** to prevent extreme changes
3. **Test on non-critical workloads** before production
4. **Configure safety factors** appropriately (1.2-1.5x recommended)
5. **Review update strategy** for your environment
6. **Monitor metrics** after enabling Auto mode

### Example Configuration

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: auto-optimization
  namespace: production
spec:
  mode: Auto  # Automated optimization

  selector:
    namespaceSelector:
      matchLabels:
        optipod.io/auto-optimize: "true"
    workloadSelector:
      matchLabels:
        optipod.io/enabled: "true"

  metricsConfig:
    provider: prometheus
    rollingWindow: 7d
    percentile: P90
    safetyFactor: 1.3

  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "256Mi"
      max: "4Gi"

  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true

  reconciliationInterval: 1h
```

### Monitoring Auto Mode

**Check policy status:**
```bash
kubectl get optimizationpolicy auto-optimization -o yaml
```

**Watch for events:**
```bash
kubectl get events --field-selector involvedObject.name=auto-optimization
```

**Monitor Prometheus metrics:**
```promql
# Optimization success rate
rate(optipod_optimization_success_total[5m])

# Resource change magnitude
optipod_resource_change_magnitude_percent
```

## Disabled Mode

**Temporarily pause policy processing without deleting the policy.**

### What It Does

- Stops discovering workloads
- Stops generating recommendations
- Stops applying changes
- Preserves policy configuration
- Updates policy status to indicate disabled state

### When to Use

- **Maintenance windows**: Pause optimization during critical operations
- **Troubleshooting**: Isolate issues by disabling specific policies
- **Policy updates**: Safely modify policy configuration
- **Incident response**: Quickly stop automated changes
- **Gradual rollout**: Disable policy for specific namespaces

### Example Configuration

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: temporarily-disabled
  namespace: default
spec:
  mode: Disabled  # Policy is paused

  # All other configuration preserved
  selector:
    workloadSelector:
      matchLabels:
        optipod.io/enabled: "true"

  metricsConfig:
    provider: metrics-server
    rollingWindow: 24h

  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"

  updateStrategy:
    strategy: webhook
```

### Re-enabling a Policy

Simply change the mode back to Recommend or Auto:

```bash
kubectl patch optimizationpolicy temporarily-disabled \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'
```

## Mode Transitions

### Safe Transition Path

The recommended path for adopting OptiPod:

```
1. Disabled → Recommend
   ↓
2. Validate recommendations
   ↓
3. Recommend → Auto (non-critical workloads)
   ↓
4. Monitor and validate
   ↓
5. Expand to production workloads
```

### Recommend → Auto Transition

**Before switching:**

1. Review recommendations for at least one reconciliation cycle
2. Generate impact report to understand changes
3. Verify resource bounds are appropriate
4. Confirm update strategy matches your environment
5. Test on a small subset of workloads first

**Transition steps:**

```bash
# 1. Generate impact report
./optipod-recommendation-report.sh -o html -f impact-report.html

# 2. Review report and validate recommendations

# 3. Update policy mode
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Auto"}}'

# 4. Monitor for issues
kubectl get events --watch --field-selector involvedObject.name=my-policy
```

### Auto → Recommend Transition

**When to revert:**

- Unexpected resource changes
- Pod restart issues
- Performance degradation
- Need to review recommendations

**Revert steps:**

```bash
# Immediately stop automated changes
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'

# Existing recommendations remain as annotations
# No new changes will be applied
```

### Emergency Disable

**Quick disable all policies:**

```bash
# Disable all policies in a namespace
kubectl get optimizationpolicy -n production -o name | \
  xargs -I {} kubectl patch {} \
  --type merge \
  --patch '{"spec":{"mode":"Disabled"}}'
```

## Mode-Specific Behavior

### Reconciliation Frequency

OptiPod adjusts reconciliation intervals based on mode:

- **Recommend**: 2x base interval (less frequent)
- **Auto**: 1x base interval (normal frequency)
- **Disabled**: 4x base interval (minimal processing)

This adaptive behavior reduces unnecessary API calls while maintaining responsiveness.

### Resource Consumption

Expected resource usage by mode:

| Mode | CPU Usage | Memory Usage | API Calls |
|------|-----------|--------------|-----------|
| Recommend | Low | Low | Moderate |
| Auto | Moderate | Moderate | High |
| Disabled | Minimal | Minimal | Minimal |

### Event Generation

Events are generated based on mode:

- **Recommend**: Recommendation generated, workload skipped
- **Auto**: Optimization applied, optimization failed, rollout triggered
- **Disabled**: Policy disabled (one-time event)

## Best Practices

### Starting with Recommend Mode

1. **Label workloads incrementally**: Start with 1-2 workloads
2. **Review recommendations**: Wait for at least one reconciliation cycle
3. **Validate bounds**: Ensure min/max values are appropriate
4. **Check explanations**: Understand how recommendations are computed
5. **Generate reports**: Use impact report to assess cluster-wide changes

### Operating in Auto Mode

1. **Monitor continuously**: Watch metrics and events
2. **Set alerts**: Alert on optimization failures or extreme changes
3. **Review periodically**: Check if bounds need adjustment
4. **Test changes**: Use staging environments for policy updates
5. **Document decisions**: Keep records of mode transitions

### Using Disabled Mode

1. **Document reason**: Add annotation explaining why disabled
2. **Set reminders**: Don't forget to re-enable after maintenance
3. **Preserve configuration**: Don't delete policies, just disable them
4. **Communicate**: Inform team when policies are disabled

## Troubleshooting

### Recommendations Not Generated

**Check policy status:**
```bash
kubectl describe optimizationpolicy my-policy
```

**Common causes:**
- Mode is Disabled
- No workloads match selectors
- Metrics provider unavailable
- Insufficient metrics data

### Auto Mode Not Applying Changes

**Check policy mode:**
```bash
kubectl get optimizationpolicy my-policy -o jsonpath='{.spec.mode}'
```

**Common causes:**
- Policy is in Recommend mode
- Global dry-run enabled
- Update strategy not configured
- RBAC permissions missing
- Webhook server not running (webhook strategy)

### Unexpected Behavior After Mode Change

**Check recent events:**
```bash
kubectl get events --sort-by='.lastTimestamp' | grep optipod
```

**Common causes:**
- Cached state from previous mode
- Reconciliation hasn't occurred yet
- Configuration errors in policy spec

## Related Documentation

- [Safety Model](safety-model.md) - Safety guarantees and constraints
- [Update Strategies](update-strategies.md) - SSA vs Webhook strategies
- [Creating Policies](../guides/creating-policies.md) - Policy configuration guide
- [Troubleshooting](../guides/troubleshooting.md) - Common issues and solutions
