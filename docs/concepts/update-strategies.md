# Update Strategies

OptiPod supports two strategies for applying resource recommendations to workloads: **Server-Side Apply (SSA)** and **Webhook**. Each strategy has different characteristics, trade-offs, and use cases.

## Strategy Overview

| Aspect | SSA Strategy | Webhook Strategy |
|--------|--------------|------------------|
| **Application Method** | Direct spec updates | Mutation at pod creation |
| **GitOps Compatibility** | May conflict | Fully compatible |
| **Infrastructure** | Simpler (no webhook) | Requires webhook + cert-manager |
| **Field Ownership** | Yes (via SSA) | No |
| **Rollout Control** | Limited | Flexible (immediate/onNextRestart) |
| **Default** | No | Yes |

## Server-Side Apply (SSA) Strategy

### How It Works

SSA directly updates workload specifications using Kubernetes Server-Side Apply:

1. Controller computes recommendations
2. Controller builds SSA patch with resource fields
3. Controller applies patch with `fieldManager: optipod`
4. Kubernetes tracks OptiPod's field ownership
5. Workload controller triggers rolling restart

### Configuration

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-policy
spec:
  mode: Auto
  updateStrategy:
    strategy: ssa                    # Use SSA strategy
    useServerSideApply: true         # Enable SSA (default: true)
    allowInPlaceResize: true         # Use in-place resize if available
    allowRecreate: false             # Block pod recreation
    updateRequestsOnly: true         # Only update requests, not limits
```

### Field Ownership

SSA provides field-level ownership tracking:

```bash
# View field managers for a deployment
kubectl get deployment my-app -o yaml | grep -A 10 managedFields

# Example output showing OptiPod ownership
managedFields:
- apiVersion: apps/v1
  fieldsType: FieldsV1
  fieldsV1:
    f:spec:
      f:template:
        f:spec:
          f:containers:
            k:{"name":"app"}:
              f:resources:
                f:requests:
                  f:cpu: {}
                  f:memory: {}
  manager: optipod
  operation: Apply
```

### Advantages

**Simpler Infrastructure:**
- No webhook deployment required
- No certificate management
- Fewer moving parts

**Field Ownership:**
- Kubernetes tracks which fields OptiPod manages
- Prevents accidental overwrites
- Clear ownership boundaries

**Immediate Updates:**
- Changes applied directly to spec
- No waiting for pod restart

### Disadvantages

**GitOps Conflicts:**
- Direct spec mutations may conflict with GitOps controllers
- ArgoCD/Flux may revert OptiPod's changes
- Requires careful configuration to avoid fights

**Limited Rollout Control:**
- Changes trigger immediate rolling restart
- No "apply on next restart" option
- Less control over disruption timing

### GitOps Compatibility

SSA can work with GitOps, but requires configuration:

**ArgoCD:**
```yaml
# Application configuration
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  ignoreDifferences:
  - group: apps
    kind: Deployment
    jsonPointers:
    - /spec/template/spec/containers/0/resources/requests/cpu
    - /spec/template/spec/containers/0/resources/requests/memory
    - /spec/template/spec/containers/0/resources/limits/cpu
    - /spec/template/spec/containers/0/resources/limits/memory
```

**Flux:**
```yaml
# Kustomization configuration
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: my-app
spec:
  force: false  # Don't force overwrite OptiPod changes
```

### Use Cases

**Best for:**
- Non-GitOps environments
- Direct kubectl/API management
- Environments where immediate updates are acceptable
- Simpler infrastructure requirements

**Avoid when:**
- Using ArgoCD/Flux without ignore configuration
- Need precise rollout control
- Want to avoid direct spec mutations

## Webhook Strategy

### How It Works

Webhook strategy uses a mutating admission webhook to inject resources at pod creation:

1. Controller computes recommendations
2. Controller stores recommendations as workload annotations
3. (Optional) Controller triggers rolling restart if `rolloutStrategy: immediate`
4. Workload controller creates new pods
5. Webhook intercepts pod creation requests
6. Webhook reads annotations and injects resource values
7. Pod created with optimized resources

### Configuration

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-policy
spec:
  mode: Auto
  updateStrategy:
    strategy: webhook                # Use webhook strategy (default)
    rolloutStrategy: onNextRestart   # Wait for natural restart (default)
    # rolloutStrategy: immediate     # Trigger restart immediately
    updateRequestsOnly: true         # Only update requests, not limits
```

### Rollout Strategies

**onNextRestart (Default - Safer):**
```yaml
updateStrategy:
  strategy: webhook
  rolloutStrategy: onNextRestart
```

- Stores annotations without triggering restart
- Changes applied during next natural pod restart
- No forced disruption
- Gradual rollout as pods restart naturally
- Lower risk

**immediate (Faster - Riskier):**
```yaml
updateStrategy:
  strategy: webhook
  rolloutStrategy: immediate
```

- Stores annotations and triggers rolling restart
- Changes applied immediately
- Controlled disruption
- Faster optimization
- Higher risk

### Annotations

Webhook strategy uses annotations to communicate recommendations:

```yaml
# Workload annotations
metadata:
  annotations:
    optipod.io/managed: "true"
    optipod.io/policy: "webhook-policy"
    optipod.io/strategy: "webhook"
    optipod.io/webhook-enabled: "true"
    
    # Per-container recommendations
    optipod.io/cpu-request.app: "500m"
    optipod.io/memory-request.app: "1Gi"
    optipod.io/cpu-limit.app: "750m"
    optipod.io/memory-limit.app: "1.1Gi"
    
    # Metadata
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
```

### Advantages

**GitOps Compatible:**
- No direct spec mutations
- Annotations don't conflict with GitOps
- ArgoCD/Flux manage specs, OptiPod manages runtime
- Clean separation of concerns

**Flexible Rollout Control:**
- Choose when changes take effect
- `onNextRestart`: wait for natural restart
- `immediate`: trigger restart now
- Fine-grained disruption control

**Safe by Default:**
- Default `onNextRestart` avoids forced disruption
- Changes applied gradually
- Lower risk of widespread issues

### Disadvantages

**Complex Infrastructure:**
- Requires webhook deployment
- Requires cert-manager for TLS certificates
- More components to manage
- Additional failure points

**Certificate Management:**
- TLS certificates must be valid
- Certificates expire and need renewal
- cert-manager adds dependency
- Certificate issues block pod creation

**Delayed Application:**
- With `onNextRestart`, changes wait for restart
- Slower optimization compared to SSA
- May take time for all pods to update

### Infrastructure Requirements

**Webhook Deployment:**
```yaml
# Deployed by Helm chart
apiVersion: apps/v1
kind: Deployment
metadata:
  name: optipod-webhook
  namespace: optipod-system
spec:
  replicas: 2  # High availability
  template:
    spec:
      containers:
      - name: webhook
        image: optipod/optipod:latest
        args:
        - webhook
        - --cert-dir=/tmp/k8s-webhook-server/serving-certs
        - --port=9443
```

**Certificate Management:**
```yaml
# cert-manager Certificate
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: optipod-webhook-cert
  namespace: optipod-system
spec:
  secretName: optipod-webhook-tls
  dnsNames:
  - optipod-webhook.optipod-system.svc
  - optipod-webhook.optipod-system.svc.cluster.local
  issuer:
    name: optipod-selfsigned-issuer
    kind: Issuer
```

**MutatingWebhookConfiguration:**
```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: optipod-webhook
  annotations:
    cert-manager.io/inject-ca-from: optipod-system/optipod-webhook-cert
webhooks:
- name: mutate.optipod.io
  clientConfig:
    service:
      name: optipod-webhook
      namespace: optipod-system
      path: /mutate
  rules:
  - operations: ["CREATE"]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]
```

### Use Cases

**Best for:**
- GitOps environments (ArgoCD, Flux)
- Environments requiring precise rollout control
- Production systems where safety is paramount
- Teams comfortable with webhook infrastructure

**Avoid when:**
- Infrastructure complexity is a concern
- Certificate management is problematic
- Immediate updates are required
- Simpler deployment is preferred

## Choosing a Strategy

### Decision Matrix

```
┌─────────────────────────────────────────────────────────────┐
│                    Using GitOps?                            │
│                    (ArgoCD/Flux)                            │
└────────────┬────────────────────────────────────┬───────────┘
             │                                    │
            Yes                                  No
             │                                    │
             v                                    v
    ┌────────────────┐                  ┌────────────────┐
    │    Webhook     │                  │  Need Simple   │
    │   Strategy     │                  │ Infrastructure?│
    │  (Recommended) │                  └────┬───────┬───┘
    └────────────────┘                       │       │
                                            Yes     No
                                             │       │
                                             v       v
                                      ┌──────────┐ ┌──────────┐
                                      │   SSA    │ │ Webhook  │
                                      │ Strategy │ │ Strategy │
                                      └──────────┘ └──────────┘
```

### Recommendation Guidelines

**Use Webhook Strategy when:**
- ✅ Using ArgoCD or Flux
- ✅ Need precise rollout control
- ✅ Want to avoid spec mutations
- ✅ Safety is top priority
- ✅ Can manage webhook infrastructure

**Use SSA Strategy when:**
- ✅ Not using GitOps
- ✅ Want simpler infrastructure
- ✅ Need immediate updates
- ✅ Comfortable with spec mutations
- ✅ Can configure GitOps ignore rules (if applicable)

## Update Scope Control

Both strategies support controlling what gets updated:

### Requests Only (Safer)

```yaml
updateStrategy:
  updateRequestsOnly: true  # Default
```

- Updates only resource requests
- Preserves existing limits
- Lower risk of hitting limits
- Recommended for initial rollout

### Requests and Limits

```yaml
updateStrategy:
  updateRequestsOnly: false
  limitConfig:
    cpuLimitMultiplier: 1.5      # Limit = Request × 1.5
    memoryLimitMultiplier: 1.1   # Limit = Request × 1.1
```

- Updates both requests and limits
- Calculates limits using multipliers
- Higher optimization potential
- Requires careful configuration

## In-Place Resize

Both strategies support in-place pod resize (Kubernetes 1.27+):

```yaml
updateStrategy:
  allowInPlaceResize: true   # Use in-place resize if available
  allowRecreate: false       # Block pod recreation
```

**In-place resize:**
- No pod restart required
- Fastest update method
- Requires Kubernetes 1.27+ with feature gate
- Not all resource changes supported

**Pod recreation:**
- Full pod restart
- Works on all Kubernetes versions
- More disruptive
- Requires `allowRecreate: true`

## Migration Between Strategies

### SSA to Webhook

1. Deploy webhook infrastructure:
```bash
helm upgrade optipod optipod/optipod \
  --set webhook.enabled=true \
  --set certManager.enabled=true
```

2. Update policies to use webhook strategy:
```yaml
spec:
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
```

3. Verify webhook is working:
```bash
kubectl logs -n optipod-system deployment/optipod-webhook
```

4. Monitor for issues:
```bash
kubectl get events --field-selector reason=OptimizationApplied
```

### Webhook to SSA

1. Update policies to use SSA strategy:
```yaml
spec:
  updateStrategy:
    strategy: ssa
    useServerSideApply: true
```

2. (Optional) Disable webhook:
```bash
helm upgrade optipod optipod/optipod \
  --set webhook.enabled=false
```

3. Clean up annotations (optional):
```bash
kubectl annotate deployment my-app \
  optipod.io/webhook-enabled- \
  optipod.io/cpu-request.app- \
  optipod.io/memory-request.app-
```

## Troubleshooting

### SSA Strategy Issues

**Conflict Errors:**
```
Error: SSA conflict: another field manager owns these fields
```

**Solution:**
- Check which field manager owns the fields
- Configure GitOps to ignore resource fields
- Use `force: true` in SSA patch (automatic)

**RBAC Errors:**
```
Error: RBAC: insufficient permissions for Server-Side Apply
```

**Solution:**
- Verify OptiPod has `update` permission on workloads
- Check ClusterRole/Role bindings
- Review RBAC configuration

### Webhook Strategy Issues

**Certificate Errors:**
```
Error: x509: certificate has expired
```

**Solution:**
- Check cert-manager is running
- Verify Certificate resource is valid
- Check certificate expiry time
- Renew certificates if needed

**Webhook Not Called:**
```
Annotations stored but resources not updated
```

**Solution:**
- Verify MutatingWebhookConfiguration exists
- Check webhook service is reachable
- Verify CA bundle is injected
- Check webhook logs for errors

**Rollout Not Triggered:**
```
Annotations updated but pods not restarted
```

**Solution:**
- Check `rolloutStrategy` is set to `immediate`
- Verify controller has permission to update workloads
- Check for errors in controller logs
- Manually trigger restart if needed

## Best Practices

### General

1. **Start with Recommend mode** to validate recommendations
2. **Test in non-production** before enabling Auto mode
3. **Monitor metrics** for optimization success/failure
4. **Set up alerts** for strategy-specific issues
5. **Document your choice** for team awareness

### SSA Strategy

1. **Configure GitOps ignore rules** if using ArgoCD/Flux
2. **Use field ownership** to track OptiPod's changes
3. **Monitor for conflicts** with other controllers
4. **Test SSA patches** in staging first
5. **Have rollback plan** ready

### Webhook Strategy

1. **Monitor certificate expiry** proactively
2. **Deploy webhook with HA** (multiple replicas)
3. **Test webhook failures** and fallback behavior
4. **Use `onNextRestart`** for safer rollouts
5. **Keep cert-manager updated** for security

## Related Documentation

- [Modes](modes.md) - Operational modes (Recommend/Auto/Disabled)
- [Safety Model](safety-model.md) - Safety guarantees and constraints
- [Architecture](architecture.md) - System design and components
- [GitOps Integration](../guides/gitops-integration.md) - ArgoCD and Flux setup
- [Troubleshooting](../guides/troubleshooting.md) - Common issues and solutions
