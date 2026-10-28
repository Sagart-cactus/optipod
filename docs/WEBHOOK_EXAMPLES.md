# OptipPod Webhook Strategy Examples

This document provides practical examples of using OptipPod's webhook strategy for different scenarios.

## Basic Webhook Configuration

### Simple Webhook Policy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: basic-webhook-policy
  namespace: production
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        environment: production
  updateStrategy:
    strategy: webhook                    # Use webhook strategy
    rolloutStrategy: onNextRestart      # Apply on natural restarts
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2h
    percentile: P90
    safetyFactor: 1.3
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
```

### Immediate Rollout Policy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: immediate-webhook-policy
  namespace: development
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        environment: development
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate           # Trigger rolling restarts
  metricsConfig:
    provider: metrics-server
    rollingWindow: 30m                   # Shorter window for dev
    percentile: P90
    safetyFactor: 1.1                    # Lower safety factor for dev
  resourceBounds:
    cpu:
      min: "50m"
      max: "2000m"
    memory:
      min: "64Mi"
      max: "4Gi"
```

## ArgoCD Integration Examples

### ArgoCD-Managed Workloads

```yaml
# OptimizationPolicy for ArgoCD applications
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: argocd-workloads
  namespace: argocd-apps
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        managed-by: argocd
  updateStrategy:
    strategy: webhook                    # Essential for ArgoCD compatibility
    rolloutStrategy: onNextRestart      # Avoid conflicts with ArgoCD sync
  metricsConfig:
    provider: prometheus                 # Often used with ArgoCD setups
    rollingWindow: 4h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "8000m"
    memory:
      min: "256Mi"
      max: "16Gi"
---
# ArgoCD Application with webhook annotations
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/myorg/my-app
    targetRevision: HEAD
    path: k8s
  destination:
    server: https://kubernetes.default.svc
    namespace: production
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true
    # Ignore resource differences managed by OptipPod webhook
    ignoreDifferences:
    - group: apps
      kind: Deployment
      jsonPointers:
      - /spec/template/spec/containers/0/resources/requests
      - /spec/template/spec/containers/0/resources/limits
```

### Deployment Template for ArgoCD

```yaml
# Deployment template that works with OptipPod webhook
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-application
  namespace: production
  labels:
    app: my-application
    environment: production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-application
  template:
    metadata:
      labels:
        app: my-application
        environment: production
      annotations:
        # Enable OptiPod webhook processing
        optipod.io/webhook-enabled: "true"
        # Optional: Pre-populate with initial recommendations
        optipod.io/recommendation.app.cpu-request: "200m"
        optipod.io/recommendation.app.memory-request: "512Mi"
        optipod.io/recommendation.app.cpu-limit: "500m"
        optipod.io/recommendation.app.memory-limit: "1Gi"
    spec:
      containers:
      - name: app
        image: my-app:v1.0.0
        ports:
        - containerPort: 8080
        resources:
          # These will be modified by webhook based on annotations
          requests:
            cpu: "100m"
            memory: "256Mi"
          limits:
            cpu: "300m"
            memory: "512Mi"
```

## Multi-Environment Setup

### Environment-Specific Policies

```yaml
# Production environment - conservative settings
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-webhook
  namespace: production
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart      # Conservative for production
  metricsConfig:
    provider: prometheus
    rollingWindow: 6h                   # Longer observation window
    percentile: P99                     # Higher percentile for safety
    safetyFactor: 1.5                   # Higher safety factor
  resourceBounds:
    cpu:
      min: "200m"                       # Higher minimums
      max: "8000m"
    memory:
      min: "512Mi"
      max: "32Gi"
---
# Staging environment - balanced settings
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: staging-webhook
  namespace: staging
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        environment: staging
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate          # Faster feedback in staging
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "256Mi"
      max: "16Gi"
---
# Development environment - aggressive optimization
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: development-webhook
  namespace: development
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchLabels:
        environment: development
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate          # Immediate updates for dev
  metricsConfig:
    provider: metrics-server
    rollingWindow: 30m                  # Short window for quick feedback
    percentile: P90
    safetyFactor: 1.1                   # Minimal safety factor
  resourceBounds:
    cpu:
      min: "50m"                        # Lower minimums for cost savings
      max: "2000m"
    memory:
      min: "128Mi"
      max: "8Gi"
```

## Workload-Specific Examples

### Microservices Architecture

```yaml
# Policy for API services
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: api-services-webhook
  namespace: microservices
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        tier: api
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  metricsConfig:
    provider: prometheus
    rollingWindow: 4h
    percentile: P90
    safetyFactor: 1.3
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "256Mi"
      max: "4Gi"
---
# Policy for background workers
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: workers-webhook
  namespace: microservices
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        tier: worker
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate          # Workers can restart more freely
  metricsConfig:
    provider: prometheus
    rollingWindow: 2h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "50m"
      max: "4000m"                      # Workers may need more CPU
    memory:
      min: "128Mi"
      max: "8Gi"
---
# Policy for databases (conservative)
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: database-webhook
  namespace: microservices
spec:
  mode: Recommend                       # Only recommend for databases
  selector:
    workloadSelector:
      matchLabels:
        tier: database
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart      # Never auto-restart databases
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h                  # Long observation for databases
    percentile: P99
    safetyFactor: 2.0                   # Very conservative
  resourceBounds:
    cpu:
      min: "500m"                       # Higher minimums for databases
      max: "16000m"
    memory:
      min: "1Gi"
      max: "64Gi"
```

### Batch Processing Workloads

```yaml
# Policy for batch jobs
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: batch-jobs-webhook
  namespace: batch-processing
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        workload-type: batch
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate          # Batch jobs restart frequently
  metricsConfig:
    provider: prometheus
    rollingWindow: 1h                   # Shorter window for batch jobs
    percentile: P90
    safetyFactor: 1.1                   # Lower safety for cost optimization
  resourceBounds:
    cpu:
      min: "100m"
      max: "32000m"                     # High CPU limits for batch processing
    memory:
      min: "512Mi"
      max: "128Gi"                      # High memory limits for data processing
```

## Advanced Configuration Examples

### Multi-Container Pods

```yaml
# Deployment with multiple containers
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-app
  namespace: production
spec:
  replicas: 2
  selector:
    matchLabels:
      app: multi-container-app
  template:
    metadata:
      labels:
        app: multi-container-app
      annotations:
        optipod.io/webhook-enabled: "true"
        # Container-specific recommendations
        optipod.io/recommendation.web.cpu-request: "200m"
        optipod.io/recommendation.web.memory-request: "512Mi"
        optipod.io/recommendation.sidecar.cpu-request: "50m"
        optipod.io/recommendation.sidecar.memory-request: "128Mi"
        optipod.io/recommendation.web.cpu-limit: "500m"
        optipod.io/recommendation.web.memory-limit: "1Gi"
        optipod.io/cpu-limit.sidecar: "100m"
        optipod.io/memory-limit.sidecar: "256Mi"
    spec:
      containers:
      - name: web
        image: my-web-app:v1.0.0
        resources:
          requests:
            cpu: "100m"
            memory: "256Mi"
          limits:
            cpu: "300m"
            memory: "512Mi"
      - name: sidecar
        image: logging-sidecar:v1.0.0
        resources:
          requests:
            cpu: "25m"
            memory: "64Mi"
          limits:
            cpu: "50m"
            memory: "128Mi"
```

### Conditional Webhook Processing

```yaml
# Policy that only applies to specific workload types
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: conditional-webhook
  namespace: production
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        optipod.io/optimize: "enabled"   # Only optimize labeled workloads
    workloadTypes:
    - Deployment
    - StatefulSet                        # Exclude DaemonSets
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  metricsConfig:
    provider: prometheus
    rollingWindow: 4h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "256Mi"
      max: "8Gi"
```

### Namespace-Based Policies

```yaml
# Policy for critical namespaces
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: critical-namespaces-webhook
spec:
  mode: Recommend                        # Only recommend for critical workloads
  selector:
    namespaceSelector:
      matchLabels:
        criticality: high
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  metricsConfig:
    provider: prometheus
    rollingWindow: 12h                   # Long observation for critical workloads
    percentile: P99
    safetyFactor: 2.0                    # Very conservative
  resourceBounds:
    cpu:
      min: "500m"
      max: "16000m"
    memory:
      min: "1Gi"
      max: "32Gi"
---
# Policy for non-critical namespaces
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: non-critical-webhook
spec:
  mode: Auto
  selector:
    namespaceSelector:
      matchExpressions:
      - key: criticality
        operator: NotIn
        values: ["high"]
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate           # More aggressive for non-critical
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2h
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "50m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
```

## Migration Examples

### Gradual Migration from SSA to Webhook

```yaml
# Step 1: Create webhook policy for new workloads
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: new-workloads-webhook
  namespace: production
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        optipod.io/strategy: webhook     # Only new workloads with this label
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
  # ... rest of configuration
---
# Step 2: Keep existing SSA policy for current workloads
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: existing-workloads-ssa
  namespace: production
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchExpressions:
      - key: optipod.io/strategy
        operator: DoesNotExist           # Workloads without strategy label
  updateStrategy:
    strategy: ssa
    useServerSideApply: true
  # ... rest of configuration
```

### Complete Migration Script

```bash
#!/bin/bash
# migrate-to-webhook.sh - Migrate OptipPod policies from SSA to webhook

set -e

NAMESPACE=${NAMESPACE:-production}
DRY_RUN=${DRY_RUN:-false}

echo "Migrating OptipPod policies to webhook strategy in namespace: $NAMESPACE"

# Step 1: Install webhook components
echo "Installing webhook components..."
kubectl apply -k config/webhook-enabled/

# Step 2: Wait for webhook to be ready
echo "Waiting for webhook to be ready..."
kubectl wait --for=condition=available deployment/optipod-webhook-deployment \
  --namespace=optipod-system --timeout=300s

# Step 3: Update policies one by one
for policy in $(kubectl get optimizationpolicy -n $NAMESPACE -o name); do
  echo "Migrating policy: $policy"
  
  if [ "$DRY_RUN" = "true" ]; then
    echo "DRY RUN: Would update $policy"
  else
    kubectl patch $policy -n $NAMESPACE --type='merge' -p='{
      "spec": {
        "updateStrategy": {
          "strategy": "webhook",
          "rolloutStrategy": "onNextRestart"
        }
      }
    }'
  fi
done

echo "Migration completed successfully!"
```

## Monitoring and Observability

### Webhook Metrics Dashboard

```yaml
# Prometheus rules for webhook monitoring
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: optipod-webhook-rules
  namespace: optipod-system
spec:
  groups:
  - name: optipod-webhook
    rules:
    - alert: WebhookHighLatency
      expr: histogram_quantile(0.95, rate(optipod_webhook_admission_duration_seconds_bucket[5m])) > 1
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "OptipPod webhook has high latency"
        description: "Webhook admission requests are taking longer than 1 second"
    
    - alert: WebhookErrors
      expr: rate(optipod_webhook_errors_total[5m]) > 0.1
      for: 2m
      labels:
        severity: critical
      annotations:
        summary: "OptipPod webhook is experiencing errors"
        description: "Webhook error rate is {{ $value }} errors per second"
```

### Grafana Dashboard Query Examples

```promql
# Webhook request rate
rate(optipod_webhook_admission_requests_total[5m])

# Webhook success rate
rate(optipod_webhook_admission_requests_total{status="success"}[5m]) / 
rate(optipod_webhook_admission_requests_total[5m])

# Webhook latency percentiles
histogram_quantile(0.50, rate(optipod_webhook_admission_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(optipod_webhook_admission_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(optipod_webhook_admission_duration_seconds_bucket[5m]))

# Mutation rate
rate(optipod_webhook_mutations_total[5m])
```

These examples provide a comprehensive guide for implementing OptipPod's webhook strategy across different environments and use cases. Choose the configuration that best matches your specific requirements and gradually adopt more advanced features as needed.
