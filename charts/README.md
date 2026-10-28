# OptipPod Helm Charts

This directory contains the official Helm chart for OptipPod - a Kubernetes operator for intelligent pod resource optimization.

## What's Included

The OptipPod chart is a **unified, batteries-included chart** that installs everything you need:

- ✅ **CustomResourceDefinitions (CRDs)** - OptimizationPolicy CRD
- ✅ **Controller** - Main operator for reconciling policies
- ✅ **Webhook** (optional) - Mutating admission webhook
- ✅ **cert-manager** (auto) - Certificate management (installed if needed)
- ✅ **RBAC** - All required permissions
- ✅ **Observability** - Metrics, health checks, events

## Quick Start

```bash
# Install OptipPod with all defaults
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace

# Install without webhook (controller only)
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false

# Install with custom values
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --values my-values.yaml
```

## Chart Architecture

```
optipod/
├── Chart.yaml           # Chart metadata + dependencies
├── values.yaml          # Default configuration
├── crds/                # CRDs (installed first, never deleted)
│   └── optimizationpolicies.yaml
└── templates/           # Kubernetes manifests
    ├── controller-deployment.yaml
    ├── webhook-deployment.yaml
    ├── rbac.yaml
    ├── certificate.yaml
    └── ...
```

### Components

1. **Controller Deployment** - Always installed
   - Reconciles OptimizationPolicy resources
   - Applies SSA (Server-Side Apply) strategy
   - Manages resource optimization

2. **Webhook Deployment** - Optional (enabled by default)
   - Mutating admission webhook
   - Intercepts pod creation
   - Applies resource recommendations at admission time
   - Requires cert-manager

3. **cert-manager** - Auto-managed
   - Installed automatically if not present (`certManager.install: auto`)
   - Manages TLS certificates for webhook
   - Injects CA bundle into webhook configuration

## Key Features

### 🎯 Single Command Installation
```bash
helm install optipod optipod/optipod -n optipod-system --create-namespace
```
Everything is configured automatically!

### 🔍 Auto-Detection
- **cert-manager detection**: Automatically detects and installs if missing
- **CRD management**: Safely manages CRDs (never deleted on uninstall)
- **Smart defaults**: Works out of the box for dev and prod

### 🔧 Flexible Configuration
```yaml
# Disable webhook
webhook:
  enabled: false

# Use existing cert-manager
certManager:
  install: false

# Production settings
webhook:
  failurePolicy: Fail
  deployment:
    replicaCount: 3
```

### 🔐 Production Ready
- RBAC with least privilege
- Pod security contexts
- Network policies
- Pod disruption budgets
- Health checks and metrics

## Documentation

- **[Installation Guide](INSTALLATION.md)** - Step-by-step installation instructions
- **[Architecture](ARCHITECTURE.md)** - Design decisions and comparisons
- **[Chart README](optipod/README.md)** - Detailed chart documentation

## Configuration

See [`optipod/values.yaml`](optipod/values.yaml) for all configuration options.

### Essential Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable mutating webhook | `true` |
| `certManager.install` | cert-manager install mode (auto/true/false) | `auto` |
| `webhook.failurePolicy` | Webhook failure policy (Ignore/Fail) | `Ignore` |
| `controller.replicaCount` | Controller replicas | `1` |
| `webhook.deployment.replicaCount` | Webhook replicas | `2` |

### Example Configurations

#### Development (kind/minikube)
```yaml
# dev-values.yaml
webhook:
  enabled: true
  failurePolicy: Ignore
  deployment:
    replicaCount: 1

certManager:
  install: auto

controller:
  replicaCount: 1
```

#### Production
```yaml
# prod-values.yaml
webhook:
  enabled: true
  failurePolicy: Fail  # Stricter enforcement
  deployment:
    replicaCount: 3
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 512Mi

certManager:
  install: false  # Use existing

controller:
  replicaCount: 2
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi

metrics:
  enabled: true
  serviceMonitor:
    enabled: true  # Prometheus Operator
```

## Verification

```bash
# Check all resources
helm status optipod -n optipod-system

# Check pods
kubectl get pods -n optipod-system

# Check CRDs
kubectl get crd optimizationpolicies.optipod.optipod.io

# Check webhook configuration
kubectl get mutatingwebhookconfiguration

# View logs
kubectl logs -n optipod-system -l app.kubernetes.io/component=controller
kubectl logs -n optipod-system -l app.kubernetes.io/component=webhook
```

## Upgrading

```bash
# Update repository
helm repo update

# Upgrade release
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values

# Upgrade with new values
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --values new-values.yaml
```

### CRD Upgrades

CRDs in the `crds/` directory are NOT automatically upgraded. To upgrade CRDs:

```bash
# Apply CRDs manually
kubectl apply -f charts/optipod/crds/

# Then upgrade the chart
helm upgrade optipod optipod/optipod -n optipod-system
```

## Uninstallation

```bash
# Uninstall release (CRDs are preserved)
helm uninstall optipod --namespace optipod-system

# Delete namespace
kubectl delete namespace optipod-system

# Optional: Delete CRDs (⚠️  deletes all OptimizationPolicy resources!)
kubectl delete crd optimizationpolicies.optipod.optipod.io
```

## Development

### Testing Locally

```bash
# Lint chart
helm lint charts/optipod

# Dry run
helm install optipod-test charts/optipod \
  --dry-run --debug \
  --namespace optipod-system

# Template rendering
helm template optipod-test charts/optipod \
  --namespace optipod-system \
  --set certManager.install=false
```

### Building Dependencies

```bash
# Download cert-manager dependency
helm dependency build charts/optipod

# Update dependencies
helm dependency update charts/optipod
```

## Why a Unified Chart?

We chose a unified chart (CRDs + Controller + Webhook in one) because:

1. ✅ **Better UX** - Single command installation
2. ✅ **Version compatibility** - All components tested together
3. ✅ **Industry standard** - Follows cert-manager, prometheus-operator patterns
4. ✅ **Easier maintenance** - One chart to update
5. ✅ **Flexible** - Components can be disabled via values

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed comparison of approaches.

## Troubleshooting

See [INSTALLATION.md](INSTALLATION.md#troubleshooting) for common issues and solutions.

### Quick Checks

```bash
# Check cert-manager
kubectl get pods -n cert-manager

# Check certificates
kubectl get certificate -n optipod-system
kubectl describe certificate -n optipod-system

# Check webhook CA bundle
kubectl get mutatingwebhookconfiguration -o yaml | grep caBundle

# View events
kubectl get events -n optipod-system --sort-by='.lastTimestamp'
```

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for contribution guidelines.

## Support

- **Documentation**: https://optipod.io/docs
- **Issues**: https://github.com/optipod/optipod/issues
- **Community**: https://optipod.io/community

## License

Apache 2.0 - See [LICENSE](../LICENSE)
