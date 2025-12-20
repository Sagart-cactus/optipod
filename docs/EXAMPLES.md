# OptimizationPolicy Examples

This document provides example OptimizationPolicy configurations for common use cases.

> **📋 Metrics Provider Status**: Examples use `metrics-server` as the recommended provider. Prometheus support is in development. See [ROADMAP.md](../ROADMAP.md) for current implementation status.

## Table of Contents

- [Basic Examples](#basic-examples)
- [Server-Side Apply Examples](#server-side-apply-examples)
- [Selector Examples](#selector-examples)
- [Metrics Configuration Examples](#metrics-configuration-examples)
- [Update Strategy Examples](#update-strategy-examples)
- [Advanced Examples](#advanced-examples)

## Basic Examples

### Minimal Policy

The simplest possible policy that optimizes all workloads in the default namespace:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: minimal-policy
  namespace: default
spec:
  mode: Recommend
  
  selector:
    namespaces:
      allow:
      - default
  
  metricsConfig:
    provider: metrics-server  # Recommended for current version
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

### Production-Ready Policy

A comprehensive policy suitable for production workloads:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-workloads
  namespace: default
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
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
    useServerSideApply: true
  
  reconciliationInterval: 5m
```

## Server-Side Apply Examples

### SSA Enabled (Default - Recommended)

Use Server-Side Apply for field-level ownership, ideal for GitOps workflows:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-enabled
  namespace: default
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
    useServerSideApply: true  # Explicit SSA (default)
```

**Benefits**:
- No conflicts with ArgoCD or other GitOps tools
- Field-level ownership tracking via managedFields
- OptiPod owns only resource requests/limits
- Other tools can manage image, replicas, env vars, etc.

### SSA Disabled (Legacy Mode)

Use Strategic Merge Patch (not recommended unless required):

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-disabled
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    useServerSideApply: false  # Use Strategic Merge Patch
```

**Note**: Disabling SSA may cause sync conflicts with GitOps tools.

### ArgoCD-Compatible Policy

Optimized for use with ArgoCD (ArgoCD 2.5+):

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: argocd-compatible
  namespace: production
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        argocd.argoproj.io/instance: my-app
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h
    percentile: P90
    safetyFactor: 1.3
  
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
    useServerSideApply: true  # Essential for ArgoCD compatibility
  
  reconciliationInterval: 10m
```

## Selector Examples

### Label-Based Selection

Select workloads by labels:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: label-selector
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
        tier: backend
      matchExpressions:
      - key: environment
        operator: In
        values: [production, staging]
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

### Namespace-Based Selection

Select all workloads in specific namespaces:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: namespace-selector
  namespace: default
spec:
  mode: Auto
  
  selector:
    namespaces:
      allow:
      - production
      - staging
      - development
      deny:
      - kube-system
      - kube-public
      - kube-node-lease
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

### Combined Selectors

Combine namespace and workload selectors:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: combined-selector
  namespace: default
spec:
  mode: Auto
  
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
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

## Metrics Configuration Examples

### Conservative (P99)

Use P99 percentile for workloads with high variability:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: conservative-metrics
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h
    percentile: P99
    safetyFactor: 1.3
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

### Balanced (P90)

Use P90 percentile for typical workloads (recommended):

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: balanced-metrics
  namespace: default
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
```

### Aggressive (P50)

Use P50 percentile for stable workloads with predictable usage:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: aggressive-metrics
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
        stable: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P50
    safetyFactor: 1.1
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
```

### Metrics Server Backend

Use metrics-server instead of Prometheus:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: metrics-server-policy
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: metrics-server
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
```

## Update Strategy Examples

### In-Place Resize Only

Only update workloads that support in-place resize (Kubernetes 1.29+):

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: inplace-only
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false  # Skip workloads requiring recreation
    updateRequestsOnly: true
```

### Allow Pod Recreation

Allow pod recreation when in-place resize is not available:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: allow-recreate
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: true  # Allow pod restarts
    updateRequestsOnly: true
```

**Warning**: Setting `allowRecreate: true` will cause pod restarts.

### Update Requests and Limits

Update both requests and limits:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: update-both
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
  
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
    updateRequestsOnly: false  # Update both requests and limits
```

## Advanced Examples

### Multi-Tier Application

Different policies for different application tiers:

```yaml
# Frontend policy - aggressive optimization
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: frontend-policy
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        tier: frontend
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.1
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "128Mi"
      max: "2Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  
  reconciliationInterval: 5m
---
# Backend policy - conservative optimization
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: backend-policy
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        tier: backend
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h
    percentile: P99
    safetyFactor: 1.3
  
  resourceBounds:
    cpu:
      min: "200m"
      max: "8000m"
    memory:
      min: "256Mi"
      max: "16Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  
  reconciliationInterval: 10m
```

### Development vs Production

Different policies for different environments:

```yaml
# Development - aggressive, fast reconciliation
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: dev-policy
  namespace: default
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: development
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 6h
    percentile: P50
    safetyFactor: 1.1
  
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "4Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: true
    updateRequestsOnly: true
  
  reconciliationInterval: 2m
---
# Production - conservative, slower reconciliation
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: prod-policy
  namespace: default
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 48h
    percentile: P99
    safetyFactor: 1.3
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "8000m"
    memory:
      min: "128Mi"
      max: "16Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  
  reconciliationInterval: 15m
```

### Recommend Mode for Testing

Test optimization recommendations before enabling auto mode:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-recommendations
  namespace: default
spec:
  mode: Recommend  # Review recommendations first
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "test"
  
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
  
  reconciliationInterval: 5m
```

Check recommendations:

```bash
kubectl describe optimizationpolicy test-recommendations
```

Once satisfied, switch to Auto mode:

```bash
kubectl patch optimizationpolicy test-recommendations --type=merge -p '{"spec":{"mode":"Auto"}}'
```

### Batch Jobs and CronJobs

Optimize batch workloads with longer windows:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: batch-jobs
  namespace: default
spec:
  mode: Auto
  
  selector:
    workloadSelector:
      matchLabels:
        workload-type: batch
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 168h  # 7 days
    percentile: P99
    safetyFactor: 1.5
  
  resourceBounds:
    cpu:
      min: "500m"
      max: "16000m"
    memory:
      min: "512Mi"
      max: "32Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: true  # Batch jobs can tolerate recreation
    updateRequestsOnly: true
  
  reconciliationInterval: 1h
```

## Usage Tips

### Labeling Workloads

Add labels to enable optimization:

```bash
# Label a deployment
kubectl label deployment my-app optimize=true

# Label multiple deployments
kubectl label deployment -l tier=backend optimize=true

# Remove optimization label
kubectl label deployment my-app optimize-
```

### Monitoring Policies

Check policy status:

```bash
# List all policies
kubectl get optimizationpolicies

# Describe a policy
kubectl describe optimizationpolicy production-workloads

# Get detailed status
kubectl get optimizationpolicy production-workloads -o yaml
```

### Testing Configurations

1. Start with `mode: Recommend`
2. Review recommendations in policy status
3. Adjust bounds, percentiles, and safety factors
4. Switch to `mode: Auto` when satisfied

### Switching Modes

```bash
# Switch to Recommend mode
kubectl patch optimizationpolicy my-policy --type=merge -p '{"spec":{"mode":"Recommend"}}'

# Switch to Auto mode
kubectl patch optimizationpolicy my-policy --type=merge -p '{"spec":{"mode":"Auto"}}'

# Disable policy
kubectl patch optimizationpolicy my-policy --type=merge -p '{"spec":{"mode":"Disabled"}}'
```

## See Also

- [CRD Reference](CRD_REFERENCE.md) - Complete field documentation
- [ArgoCD Integration](ARGOCD_INTEGRATION.md) - GitOps compatibility guide
- [Installation Guide](INSTALLATION.md) - Setup instructions
