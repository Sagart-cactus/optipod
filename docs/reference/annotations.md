# OptiPod Annotations Reference

Complete reference for all annotations used by OptiPod to store recommendations and manage workloads.

## Overview

OptiPod uses Kubernetes annotations to:
- Mark workloads as managed by OptiPod
- Store resource recommendations
- Track policy associations
- Record timestamps of recommendations and applications
- Enable webhook processing

All OptiPod annotations use the `optipod.io/` prefix.

## Management Annotations

These annotations identify and track OptiPod-managed workloads.

### optipod.io/managed

Indicates the workload is managed by OptiPod.

**Value**: `"true"` or `"false"`  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller  
**Required**: Yes (for managed workloads)

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/managed: "true"
```

**Usage**:
- Set to `"true"` when OptiPod starts managing a workload
- Used to identify workloads under OptiPod control
- Checked by webhook to determine if processing is needed

### optipod.io/policy

Indicates which policy manages this workload.

**Value**: Policy name (string)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller  
**Required**: Yes (for managed workloads)

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/policy: "production-policy"
```

**Usage**:
- Stores the name of the OptimizationPolicy resource
- Used for tracking and debugging
- Helps identify which policy generated recommendations

### optipod.io/policy-uid

Stores the unique identifier of the acting optimization policy.

**Value**: Policy UID (string)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller  
**Required**: Yes (for managed workloads)

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/policy-uid: "abc123-def456-ghi789"
```

**Usage**:
- Provides unique identification of the policy
- Used for policy change detection
- Helps prevent conflicts when policies are recreated

### optipod.io/last-recommendation

Timestamp of the last recommendation generation.

**Value**: RFC3339 timestamp (string)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller  
**Required**: No

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
```

**Usage**:
- Tracks when recommendations were last computed
- Used for debugging and monitoring
- Helps identify stale recommendations

### optipod.io/last-applied

Timestamp of the last applied change (Auto mode only).

**Value**: RFC3339 timestamp (string)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller (Auto mode) or Webhook  
**Required**: No

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/last-applied: "2025-01-28T10:35:00Z"
```

**Usage**:
- Tracks when recommendations were last applied
- Only set in Auto mode
- Used for monitoring and audit trails

### optipod.io/strategy

Indicates which strategy is used for this workload.

**Value**: `"ssa"` or `"webhook"`  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller  
**Required**: No

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/strategy: "webhook"
```

**Usage**:
- Indicates the update strategy (SSA or webhook)
- Used for debugging and monitoring
- Helps identify how recommendations are applied

## Recommendation Annotations

These annotations store per-container resource recommendations.

### Format

Recommendation annotations follow this format:

```
optipod.io/recommendation.<container-name>.<resource>-<type>
```

Where:
- `<container-name>` - Name of the container
- `<resource>` - `cpu` or `memory`
- `<type>` - `request` or `limit`

### CPU Request Recommendation

**Format**: `optipod.io/recommendation.<container-name>.cpu-request`  
**Value**: Kubernetes quantity (e.g., `500m`, `1000m`, `2`)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/recommendation.app.cpu-request: "500m"
    optipod.io/recommendation.sidecar.cpu-request: "100m"
```

**Usage**:
- Stores recommended CPU request for each container
- Read by webhook to inject CPU requests
- Used by recommendation report script

### Memory Request Recommendation

**Format**: `optipod.io/recommendation.<container-name>.memory-request`  
**Value**: Kubernetes quantity (e.g., `512Mi`, `1Gi`, `2Gi`)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/recommendation.app.memory-request: "1Gi"
    optipod.io/recommendation.sidecar.memory-request: "256Mi"
```

**Usage**:
- Stores recommended memory request for each container
- Read by webhook to inject memory requests
- Used by recommendation report script

### CPU Limit Recommendation

**Format**: `optipod.io/recommendation.<container-name>.cpu-limit`  
**Value**: Kubernetes quantity (e.g., `750m`, `1500m`, `3`)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller (when `updateRequestsOnly: false`)

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/recommendation.app.cpu-limit: "750m"
    optipod.io/recommendation.sidecar.cpu-limit: "150m"
```

**Usage**:
- Stores recommended CPU limit for each container
- Only set when policy has `updateRequestsOnly: false`
- Read by webhook to inject CPU limits
- Calculated using `cpuLimitMultiplier` from policy

### Memory Limit Recommendation

**Format**: `optipod.io/recommendation.<container-name>.memory-limit`  
**Value**: Kubernetes quantity (e.g., `1.1Gi`, `2Gi`, `4Gi`)  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller (when `updateRequestsOnly: false`)

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/recommendation.app.memory-limit: "1.1Gi"
    optipod.io/recommendation.sidecar.memory-limit: "281Mi"
```

**Usage**:
- Stores recommended memory limit for each container
- Only set when policy has `updateRequestsOnly: false`
- Read by webhook to inject memory limits
- Calculated using `memoryLimitMultiplier` from policy

## Webhook Annotations

These annotations control webhook behavior.

### optipod.io/webhook-enabled

Indicates the workload should be processed by webhook.

**Value**: `"true"` or `"false"`  
**Applied to**: Deployment, StatefulSet, DaemonSet  
**Set by**: Controller (when using webhook strategy)  
**Required**: No

**Example**:
```yaml
metadata:
  annotations:
    optipod.io/webhook-enabled: "true"
```

**Usage**:
- Enables webhook processing for this workload
- Set when policy uses webhook strategy
- Checked by webhook admission controller

## Complete Example

Here's a complete example showing all annotations on a Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: production
  annotations:
    # Management annotations
    optipod.io/managed: "true"
    optipod.io/policy: "production-policy"
    optipod.io/policy-uid: "abc123-def456-ghi789"
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
    optipod.io/last-applied: "2025-01-28T10:35:00Z"
    optipod.io/strategy: "webhook"
    
    # Recommendation annotations (requests only)
    optipod.io/recommendation.app.cpu-request: "500m"
    optipod.io/recommendation.app.memory-request: "1Gi"
    optipod.io/recommendation.sidecar.cpu-request: "100m"
    optipod.io/recommendation.sidecar.memory-request: "256Mi"
    
    # Webhook annotation
    optipod.io/webhook-enabled: "true"
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: my-app:v1.0.0
        resources:
          requests:
            cpu: "1000m"      # Will be updated by webhook
            memory: "2Gi"     # Will be updated by webhook
      - name: sidecar
        image: sidecar:v1.0.0
        resources:
          requests:
            cpu: "200m"       # Will be updated by webhook
            memory: "512Mi"   # Will be updated by webhook
```

## Example with Limits

When `updateRequestsOnly: false`, limit recommendations are also stored:

```yaml
metadata:
  annotations:
    # Management annotations
    optipod.io/managed: "true"
    optipod.io/policy: "production-policy"
    optipod.io/policy-uid: "abc123-def456-ghi789"
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
    optipod.io/strategy: "webhook"
    
    # Recommendation annotations (requests and limits)
    optipod.io/recommendation.app.cpu-request: "500m"
    optipod.io/recommendation.app.memory-request: "1Gi"
    optipod.io/recommendation.app.cpu-limit: "750m"      # Limit = Request × 1.5
    optipod.io/recommendation.app.memory-limit: "1.1Gi"  # Limit = Request × 1.1
    
    # Webhook annotation
    optipod.io/webhook-enabled: "true"
```

## Viewing Annotations

### kubectl get

View all annotations on a workload:

```bash
kubectl get deployment my-app -o jsonpath='{.metadata.annotations}' | jq
```

### kubectl describe

View annotations in human-readable format:

```bash
kubectl describe deployment my-app
```

Look for the Annotations section:

```
Annotations:  optipod.io/cpu-request.app: 500m
              optipod.io/memory-request.app: 1Gi
              optipod.io/managed: true
              optipod.io/policy: production-policy
```

### Specific annotation

View a specific annotation:

```bash
# CPU request for container "app"
kubectl get deployment my-app \
  -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.cpu-request}'

# Memory request for container "app"
kubectl get deployment my-app \
  -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.memory-request}'
```

### All OptiPod annotations

View only OptiPod annotations:

```bash
kubectl get deployment my-app -o yaml | grep "optipod.io"
```

## Annotation Lifecycle

### Recommend Mode

1. Controller discovers workload matching policy
2. Controller fetches metrics and computes recommendations
3. Controller adds management annotations:
   - `optipod.io/managed: "true"`
   - `optipod.io/policy: "<policy-name>"`
   - `optipod.io/policy-uid: "<policy-uid>"`
   - `optipod.io/last-recommendation: "<timestamp>"`
4. Controller adds recommendation annotations:
   - `optipod.io/recommendation.<container>.cpu-request`
   - `optipod.io/recommendation.<container>.memory-request`
   - (Optional) `optipod.io/recommendation.<container>.cpu-limit`
   - (Optional) `optipod.io/recommendation.<container>.memory-limit`
5. Annotations remain on workload for review
6. No changes to pod template

### Auto Mode (Webhook Strategy)

1. Controller performs steps 1-4 from Recommend mode
2. Controller adds webhook annotation:
   - `optipod.io/webhook-enabled: "true"`
3. Controller adds strategy annotation:
   - `optipod.io/strategy: "webhook"`
4. If `rolloutStrategy: immediate`:
   - Controller triggers rolling restart
5. Webhook intercepts pod creation
6. Webhook reads recommendation annotations
7. Webhook injects resource values into pod spec
8. Controller updates `optipod.io/last-applied` timestamp

### Auto Mode (SSA Strategy)

1. Controller performs steps 1-4 from Recommend mode
2. Controller adds strategy annotation:
   - `optipod.io/strategy: "ssa"`
3. Controller applies resource changes using Server-Side Apply
4. Kubernetes triggers rolling restart
5. Controller updates `optipod.io/last-applied` timestamp

## Annotation Removal

Annotations are removed when:
- Policy is deleted
- Workload no longer matches policy selector
- Policy mode is changed to `Disabled`

When annotations are removed:
- All `optipod.io/*` annotations are deleted
- Pod template resources remain unchanged
- Workload continues running with last applied values

## Troubleshooting

### Missing Annotations

If annotations are not appearing:

1. Check policy mode:
   ```bash
   kubectl get optimizationpolicy my-policy -o jsonpath='{.spec.mode}'
   ```

2. Check policy selector:
   ```bash
   kubectl get optimizationpolicy my-policy -o yaml
   ```

3. Check workload labels:
   ```bash
   kubectl get deployment my-app -o jsonpath='{.metadata.labels}'
   ```

4. Check controller logs:
   ```bash
   kubectl logs -n optipod-system deployment/optipod-controller
   ```

### Stale Annotations

If annotations are outdated:

1. Check last recommendation timestamp:
   ```bash
   kubectl get deployment my-app \
     -o jsonpath='{.metadata.annotations.optipod\.io/last-recommendation}'
   ```

2. Check policy reconciliation interval:
   ```bash
   kubectl get optimizationpolicy my-policy \
     -o jsonpath='{.spec.reconciliationInterval}'
   ```

3. Force reconciliation by updating policy:
   ```bash
   kubectl annotate optimizationpolicy my-policy \
     force-reconcile="$(date +%s)" --overwrite
   ```

### Annotation Format Errors

If webhook fails to parse annotations:

1. Check annotation format:
   ```bash
   kubectl get deployment my-app -o yaml | grep "optipod.io/recommendation"
   ```

2. Verify container names match:
   ```bash
   kubectl get deployment my-app \
     -o jsonpath='{.spec.template.spec.containers[*].name}'
   ```

3. Check webhook logs:
   ```bash
   kubectl logs -n optipod-system deployment/optipod-webhook
   ```

## Best Practices

1. **Don't manually edit annotations** - Let OptiPod manage them
2. **Use recommendation report** - Use `optipod-recommendation-report.sh` to view recommendations
3. **Monitor timestamps** - Check `last-recommendation` to ensure recommendations are fresh
4. **Verify container names** - Ensure annotation container names match actual containers
5. **Check policy association** - Verify `optipod.io/policy` matches expected policy
6. **Review before Auto mode** - Review annotations in Recommend mode before enabling Auto
7. **Backup workloads** - Keep backups before enabling Auto mode
8. **Monitor after changes** - Watch workloads after OptiPod applies changes

## Related Documentation

- [CRD Specification](crd-spec.md) - Complete OptimizationPolicy field reference
- [Reviewing Recommendations](../guides/reviewing-recs.md) - How to review recommendations
- [Creating Policies](../guides/creating-policies.md) - Policy configuration guide
- [Troubleshooting](../guides/troubleshooting.md) - Common issues and solutions
- [CLI Tools](cli-tools.md) - Recommendation report script reference
