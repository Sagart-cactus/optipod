# Switching to Auto Mode

This guide explains how to safely transition from Recommend mode to Auto mode, enabling OptiPod to automatically apply resource optimizations.

## Overview

OptiPod supports three operational modes:
- **Recommend**: Compute recommendations, don't apply (safe default)
- **Auto**: Automatically apply recommendations
- **Disabled**: Stop processing workloads

Switching to Auto mode enables automatic optimization but requires careful planning and validation.

## Prerequisites

Before switching to Auto mode:

1. **Validate recommendations** in Recommend mode
2. **Test in non-production** environment
3. **Review policy configuration** for safety settings
4. **Set up monitoring** for optimization activity
5. **Document rollback procedures**
6. **Verify RBAC permissions** for updates

## Safety Checklist

Complete this checklist before enabling Auto mode:

- [ ] Recommendations reviewed and validated
- [ ] Policy tested in staging/development
- [ ] Resource bounds are conservative
- [ ] Safety factor is appropriate (1.2-1.5)
- [ ] Update strategy configured (webhook recommended)
- [ ] Rollout strategy set (onNextRestart recommended)
- [ ] Gradual memory decrease enabled
- [ ] Monitoring and alerts configured
- [ ] Rollback procedure documented
- [ ] Team notified of changes

## Gradual Adoption Strategy

### Phase 1: Single Non-Critical Workload

Start with one low-risk workload:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: pilot-workload
spec:
  mode: Auto  # Enable Auto mode
  
  selector:
    workloadSelector:
      matchLabels:
        app: pilot-app  # Single workload
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.5  # Conservative
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "256Mi"
      max: "4Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart  # Safer
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10
```

**Monitor for 1-2 weeks:**
- Check for performance issues
- Monitor resource usage
- Review optimization events
- Validate no OOMKills or throttling

### Phase 2: Non-Critical Workloads

Expand to all non-critical workloads:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: non-critical-workloads
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        criticality: low
    workloadTypes:
      include:
        - Deployment  # Start with Deployments only
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.3
  
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
    gradualDecreaseConfig:
      enabled: true
```

**Monitor for 2-4 weeks:**
- Validate optimization success rate
- Check resource savings
- Monitor for any issues
- Adjust policies based on observations

### Phase 3: Production Workloads

Carefully expand to production:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-workloads
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
    workloadSelector:
      matchLabels:
        optimize: "true"  # Explicit opt-in
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h  # Longer window for production
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
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10
```

**Monitor continuously:**
- Set up alerts for optimization failures
- Review weekly optimization reports
- Adjust policies based on workload behavior
- Document lessons learned

### Phase 4: Stateful Workloads (Optional)

Only after extensive validation:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: stateful-workloads
spec:
  mode: Recommend  # Keep in Recommend mode longer
  # Or use Auto with very conservative settings
  
  selector:
    workloadTypes:
      include:
        - StatefulSet
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 72h  # Very long window
    percentile: P95     # Higher percentile
    safetyFactor: 1.5   # Large buffer
  
  resourceBounds:
    cpu:
      min: "500m"
      max: "8000m"
    memory:
      min: "1Gi"
      max: "16Gi"
  
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false  # Never recreate StatefulSet pods
    updateRequestsOnly: true
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 5  # Very gradual
```

## Switching a Policy to Auto Mode

### Method 1: kubectl patch

```bash
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Auto"}}'
```

### Method 2: kubectl edit

```bash
kubectl edit optimizationpolicy my-policy
```

Change:
```yaml
spec:
  mode: Recommend
```

To:
```yaml
spec:
  mode: Auto
```

### Method 3: Update YAML and Apply

Edit policy file:
```yaml
# policy.yaml
spec:
  mode: Auto  # Changed from Recommend
```

Apply:
```bash
kubectl apply -f policy.yaml
```

### Method 4: GitOps (Recommended)

Update policy in Git repository:

```bash
# Edit policy file
vim policies/production-policy.yaml

# Commit and push
git add policies/production-policy.yaml
git commit -m "Enable Auto mode for production-policy"
git push

# ArgoCD/Flux will sync automatically
```

## Verification

### Check Policy Mode

```bash
kubectl get optimizationpolicy my-policy \
  -o jsonpath='{.spec.mode}'
```

Expected output: `Auto`

### Monitor First Optimization

Watch for optimization events:

```bash
kubectl get events -w --field-selector reason=OptimizationApplied
```

Check workload annotations:

```bash
kubectl get deployment my-app -o yaml | grep "optipod.io/last-applied"
```

### Verify Resource Changes

Compare before and after:

```bash
# Before (from recommendations)
kubectl get deployment my-app -o yaml | grep "optipod.io/.*-request"

# After (actual resources)
kubectl get deployment my-app \
  -o jsonpath='{.spec.template.spec.containers[0].resources}' | jq
```

## Monitoring After Switch

### Kubernetes Events

Monitor optimization activity:

```bash
# All optimization events
kubectl get events -A --field-selector source=optipod

# Successful optimizations
kubectl get events --field-selector reason=OptimizationApplied

# Failed optimizations
kubectl get events --field-selector reason=OptimizationFailed

# Memory safety blocks
kubectl get events --field-selector reason=UnsafeMemoryDecrease
```

### Policy Status

Check policy metrics:

```bash
kubectl get optimizationpolicy my-policy -o yaml
```

Review status:
```yaml
status:
  workloadsDiscovered: 10
  workloadsProcessed: 8
  lastReconciliation: "2025-01-28T10:30:00Z"
```

### Workload Health

Monitor workload health after optimization:

```bash
# Check pod status
kubectl get pods -l app=my-app

# Check for OOMKills
kubectl get events --field-selector reason=OOMKilled

# Check for restarts
kubectl get pods -l app=my-app \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.containerStatuses[0].restartCount}{"\n"}{end}'

# Check resource usage
kubectl top pods -l app=my-app
```

### Prometheus Metrics

Query optimization metrics:

```promql
# Optimization success rate
rate(optipod_optimizations_applied_total[5m])

# Optimization failures
rate(optipod_optimization_failure_total[5m])

# Resource change magnitude
optipod_resource_change_magnitude_percent
```

## Common Issues

### Issue 1: Optimizations Not Applied

**Symptoms:**
- Policy in Auto mode
- Recommendations exist
- No changes applied

**Possible causes:**
1. Global dry-run enabled
2. Update strategy misconfigured
3. RBAC permissions missing
4. Workload doesn't support updates

**Check:**
```bash
# Check controller flags
kubectl get deployment optipod-controller -n optipod-system \
  -o jsonpath='{.spec.template.spec.containers[0].args}' | grep dry-run

# Check update strategy
kubectl get optimizationpolicy my-policy \
  -o jsonpath='{.spec.updateStrategy}' | jq

# Check RBAC
kubectl auth can-i update deployments --as=system:serviceaccount:optipod-system:optipod-controller

# Check controller logs
kubectl logs -n optipod-system deployment/optipod-controller
```

### Issue 2: Optimization Failures

**Symptoms:**
- OptimizationFailed events
- Error messages in logs

**Possible causes:**
1. Recommendations exceed limits
2. SSA conflicts
3. Validation errors
4. RBAC errors

**Check:**
```bash
# View failure events
kubectl get events --field-selector reason=OptimizationFailed

# Check controller logs
kubectl logs -n optipod-system deployment/optipod-controller | grep ERROR

# Describe workload
kubectl describe deployment my-app
```

### Issue 3: Performance Degradation

**Symptoms:**
- Increased latency
- CPU throttling
- OOMKills

**Possible causes:**
1. Safety factor too low
2. Percentile too low
3. Metrics window too short
4. Workload behavior changed

**Actions:**
1. Revert to Recommend mode immediately
2. Increase safety factor
3. Use higher percentile (P95 or P99)
4. Increase rolling window
5. Adjust resource bounds

```bash
# Emergency revert to Recommend mode
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'
```

## Rollback Procedures

### Emergency: Stop All Optimizations

**Method 1: Switch to Recommend mode**
```bash
kubectl get optimizationpolicy --all-namespaces -o name | \
  xargs -I {} kubectl patch {} \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'
```

**Method 2: Enable global dry-run**
```bash
kubectl set env deployment/optipod-controller \
  -n optipod-system \
  DRY_RUN=true
```

**Method 3: Disable policy**
```bash
kubectl patch optimizationpolicy my-policy \
  --type merge \
  --patch '{"spec":{"mode":"Disabled"}}'
```

### Rollback Specific Workload

**If using GitOps:**
```bash
git revert <commit>
git push
```

**If using SSA:**
```bash
kubectl patch deployment my-app \
  --type merge \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"app","resources":{"requests":{"cpu":"1000m","memory":"2Gi"}}}]}}}}'
```

**If using webhook:**
```bash
# Remove OptiPod annotations
kubectl annotate deployment my-app \
  optipod.io/managed- \
  optipod.io/policy- \
  optipod.io/cpu-request.app- \
  optipod.io/memory-request.app-

# Trigger rolling restart to apply original resources
kubectl rollout restart deployment my-app
```

## Best Practices

1. **Start small** - Begin with single workload
2. **Test thoroughly** - Validate in non-production first
3. **Use conservative settings** - Higher safety factors initially
4. **Enable gradual decrease** - Especially for memory
5. **Use webhook strategy** - Safer for GitOps environments
6. **Set onNextRestart** - Avoid forced disruptions
7. **Monitor closely** - Watch for issues after switch
8. **Document everything** - Record decisions and observations
9. **Have rollback ready** - Know how to revert quickly
10. **Communicate changes** - Inform team before switching

## Optimization Settings

### Conservative (Recommended for Initial Auto Mode)

```yaml
spec:
  mode: Auto
  metricsConfig:
    rollingWindow: 48h
    percentile: P95
    safetyFactor: 1.5
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

### Balanced (After Validation)

```yaml
spec:
  mode: Auto
  metricsConfig:
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    gradualDecreaseConfig:
      enabled: true
```

### Aggressive (Development Only)

```yaml
spec:
  mode: Auto
  metricsConfig:
    rollingWindow: 12h
    percentile: P90
    safetyFactor: 1.1
  updateStrategy:
    strategy: ssa
    allowInPlaceResize: true
    allowRecreate: true
    updateRequestsOnly: false
```

## Success Criteria

Measure success after switching to Auto mode:

**Resource Efficiency:**
- CPU utilization improved
- Memory utilization improved
- Cost savings achieved

**Reliability:**
- No increase in OOMKills
- No increase in CPU throttling
- No performance degradation
- Stable pod restart counts

**Operational:**
- Optimization success rate > 95%
- No manual interventions needed
- Team confidence in automation

## Next Steps

- [Reviewing Recommendations](reviewing-recs.md) - Monitor ongoing recommendations
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
- [Safety Model](../concepts/safety-model.md) - Understanding safety guarantees
- [Modes](../concepts/modes.md) - Operational modes explained
- [GitOps Integration](gitops-integration.md) - Using OptiPod with ArgoCD/Flux
