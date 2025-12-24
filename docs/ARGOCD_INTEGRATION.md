# ArgoCD Integration Guide

This guide explains how to use OptiPod alongside ArgoCD without sync conflicts, leveraging Kubernetes Server-Side Apply
(SSA) for field-level ownership.

## Overview

OptiPod uses Server-Side Apply (SSA) by default to manage only resource requests and limits, while ArgoCD manages other
fields like image, replicas, and environment variables. This field-level ownership prevents sync conflicts and allows both
tools to coexist peacefully.

### How It Works

With SSA, Kubernetes tracks which tool owns which fields using `managedFields` metadata:

```yaml
Deployment: my-app
├── spec.replicas                    [Owned by: argocd]
├── spec.template.spec.containers[0]
│   ├── image                        [Owned by: argocd]
│   ├── env                          [Owned by: argocd]
│   └── resources
│       ├── requests.cpu             [Owned by: optipod] ✅
│       ├── requests.memory          [Owned by: optipod] ✅
│       ├── limits.cpu               [Owned by: optipod] ✅
│       └── limits.memory            [Owned by: optipod] ✅
```

## Prerequisites

- Kubernetes 1.22+ (SSA is GA)
- ArgoCD 2.5+ (recommended for automatic SSA support)
- OptiPod installed with SSA enabled (default)

## Configuration

### OptiPod Configuration

OptiPod uses SSA by default. You can explicitly enable it in your OptimizationPolicy:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-workloads
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    useServerSideApply: true  # Default: true
```

### ArgoCD Configuration

#### Option 1: ArgoCD 2.5+ (Automatic - Recommended)

ArgoCD 2.5+ automatically respects SSA field ownership. No additional configuration needed!

Simply deploy your application as usual:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/myorg/myapp
    targetRevision: main
    path: k8s
  destination:
    server: https://kubernetes.default.svc
    namespace: production
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

#### Option 2: Explicit Ignore Differences (ArgoCD < 2.5)

For older ArgoCD versions, explicitly ignore OptipPod-managed fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  # ... other fields ...
  ignoreDifferences:
  - group: apps
    kind: Deployment
    managedFieldsManagers:
    - optipod
  - group: apps
    kind: StatefulSet
    managedFieldsManagers:
    - optipod
  - group: apps
    kind: DaemonSet
    managedFieldsManagers:
    - optipod
```

#### Option 3: Server-Side Apply for ArgoCD

Enable SSA for ArgoCD itself (requires ArgoCD 2.5+):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  # ... other fields ...
  syncPolicy:
    syncOptions:
    - ServerSideApply=true
    - RespectIgnoreDifferences=true
```

## Deployment Workflow

### Step 1: Deploy Application with ArgoCD

Create your application manifest (without resource requests/limits or with initial values):

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  labels:
    app: web-app
    optimize: "true"  # Enable OptiPod optimization
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
        optimize: "true"
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
        ports:
        - containerPort: 80
        # Optional: Set initial resource requests
        # OptiPod will optimize these based on actual usage
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
```

Deploy via ArgoCD:

```bash
argocd app create web-app \
  --repo https://github.com/myorg/myapp \
  --path k8s \
  --dest-server https://kubernetes.default.svc \
  --dest-namespace production \
  --sync-policy automated
```

### Step 2: Create OptiPod Policy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-optimizer
  namespace: production
spec:
  mode: Auto
  
  selector:
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
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    useServerSideApply: true
  
  reconciliationInterval: 5m
```

Apply the policy:

```bash
kubectl apply -f optipod-policy.yaml
```

### Step 3: Verify Field Ownership

Check that both tools are managing their respective fields:

```bash
# View managedFields
kubectl get deployment web-app -n production -o yaml | grep -A 30 managedFields

# Check ArgoCD sync status (should be "Synced")
argocd app get web-app

# Check OptiPod status
kubectl describe optimizationpolicy production-optimizer -n production
```

Expected output shows both managers:

```yaml
managedFields:
- manager: argocd
  operation: Apply
  fieldsV1:
    f:spec:
      f:replicas: {}
      f:template:
        f:spec:
          f:containers:
            k:{"name":"nginx"}:
              f:image: {}
              f:ports: {}

- manager: optipod
  operation: Apply
  fieldsV1:
    f:spec:
      f:template:
        f:spec:
          f:containers:
            k:{"name":"nginx"}:
              f:resources:
                f:requests:
                  f:cpu: {}
                  f:memory: {}
```

## Verification and Testing

### 1. Verify No Sync Conflicts

After OptiPod optimizes resources, ArgoCD should remain synced:

```bash
# Check ArgoCD status
argocd app get web-app

# Should show:
# Sync Status:        Synced
# Health Status:      Healthy
```

### 2. Test ArgoCD Sync

Trigger an ArgoCD sync and verify OptiPod's changes are preserved:

```bash
# Sync the application
argocd app sync web-app

# Check that resource requests are still optimized
kubectl get deployment web-app -n production -o jsonpath='{.spec.template.spec.containers[0].resources}'
```

### 3. Test OptiPod Updates

Update the image in Git and verify OptiPod doesn't interfere:

```bash
# Update image in Git
# ArgoCD will sync the new image

# Verify OptiPod still manages resources
kubectl describe optimizationpolicy production-optimizer -n production
```

### 4. Inspect Field Ownership

View detailed field ownership:

```bash
kubectl get deployment web-app -n production -o json | jq '.metadata.managedFields'
```

## Troubleshooting

### ArgoCD Shows OutOfSync

**Symptom**: ArgoCD marks the application as OutOfSync after OptiPod updates resources.

**Solution**:

1. **Upgrade ArgoCD**: If using ArgoCD < 2.5, upgrade to 2.5+ for automatic SSA support
2. **Add ignoreDifferences**: Configure ArgoCD to ignore OptipPod-managed fields (see Option 2 above)
3. **Verify SSA is enabled**: Check that `useServerSideApply: true` in your OptimizationPolicy

### OptiPod Changes Reverted by ArgoCD

**Symptom**: OptiPod updates resources, but ArgoCD reverts them during sync.

**Solution**:

1. **Check ArgoCD version**: Ensure ArgoCD 2.5+ or configure ignoreDifferences
2. **Verify field ownership**: Check managedFields to ensure OptiPod owns resource fields
3. **Disable auto-sync temporarily**: Test manual sync to isolate the issue

```bash
# Check if OptiPod owns resource fields
kubectl get deployment web-app -n production -o yaml | grep -A 50 managedFields | grep -A 10 optipod
```

### Field Ownership Conflicts

**Symptom**: OptiPod logs show SSA conflicts or errors.

**Solution**:

1. **Check Force flag**: OptiPod uses `Force: true` by default to take ownership
2. **Review managedFields**: Identify which manager currently owns the fields
3. **Clear conflicting ownership**: If needed, manually remove the conflicting manager's ownership

```bash
# View OptiPod logs
kubectl logs -n optipod-system deployment/optipod-manager

# Check for SSA conflict events
kubectl get events -n production --field-selector reason=SSAConflict
```

### Disabling SSA

If you need to disable SSA (not recommended):

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-optimizer
spec:
  # ... other fields ...
  updateStrategy:
    useServerSideApply: false  # Use Strategic Merge Patch instead
```

**Note**: Disabling SSA will cause sync conflicts with ArgoCD.

## Best Practices

1. **Use SSA by default**: Keep `useServerSideApply: true` for ArgoCD compatibility
2. **Start with Recommend mode**: Test OptiPod in Recommend mode before enabling Auto
3. **Monitor field ownership**: Regularly check managedFields to ensure proper ownership
4. **Use ArgoCD 2.5+**: Upgrade to the latest ArgoCD for best SSA support
5. **Label workloads explicitly**: Use specific labels to control which workloads OptiPod optimizes
6. **Set appropriate bounds**: Configure min/max bounds to prevent unexpected resource changes
7. **Monitor both tools**: Watch logs and events from both ArgoCD and OptiPod

## Example: Complete Setup

Here's a complete example with ArgoCD and OptiPod:

### 1. Application Manifest (in Git)

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  labels:
    app: api-server
    optimize: "true"
spec:
  replicas: 5
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
        optimize: "true"
    spec:
      containers:
      - name: api
        image: myorg/api-server:v1.2.3
        ports:
        - containerPort: 8080
        env:
        - name: LOG_LEVEL
          value: "info"
        resources:
          requests:
            cpu: "200m"
            memory: "256Mi"
```

### 2. ArgoCD Application

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: api-server
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/myorg/api-server
    targetRevision: main
    path: k8s
  destination:
    server: https://kubernetes.default.svc
    namespace: production
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - ServerSideApply=true  # Optional but recommended
```

### 3. OptiPod Policy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: api-optimizer
  namespace: production
spec:
  mode: Auto
  
  selector:
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
      max: "2000m"
    memory:
      min: "128Mi"
      max: "4Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    useServerSideApply: true
  
  reconciliationInterval: 5m
```

### 4. Deploy Everything

```bash
# Deploy via ArgoCD
argocd app create api-server \
  --repo https://github.com/myorg/api-server \
  --path k8s \
  --dest-server https://kubernetes.default.svc \
  --dest-namespace production \
  --sync-policy automated

# Create OptiPod policy
kubectl apply -f optipod-policy.yaml

# Verify both are working
argocd app get api-server
kubectl describe optimizationpolicy api-optimizer -n production
```

## Additional Resources

- [Kubernetes Server-Side Apply Documentation](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
- [ArgoCD Server-Side Apply Support](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#server-side-apply)
- [OptiPod CRD Reference](CRD_REFERENCE.md)
- [OptiPod Examples](EXAMPLES.md)

## Support

If you encounter issues with ArgoCD integration:

1. Check OptiPod logs: `kubectl logs -n optipod-system deployment/optipod-manager`
2. Check ArgoCD application status: `argocd app get <app-name>`
3. Inspect managedFields: `kubectl get deployment <name> -o yaml | grep -A 50 managedFields`
4. Review events: `kubectl get events -n <namespace>`
5. Open an issue: [GitHub Issues](https://github.com/Sagart-cactus/optipod/issues)
