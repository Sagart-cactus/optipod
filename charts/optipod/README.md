# OptipPod Helm Chart

A Kubernetes operator for intelligent pod resource optimization with optional mutating webhook support.

## Features

- 🎯 **Automatic Resource Optimization** - Intelligently adjusts pod resource requests and limits
- 🔄 **Multiple Strategies** - Server-Side Apply (SSA) and Mutating Webhook modes
- 📊 **Policy-Based** - Flexible OptimizationPolicy CRD for fine-grained control
- 🔒 **Production Ready** - Built-in security, HA support, and observability
- 📦 **Batteries Included** - Auto-detects and installs cert-manager if needed

## Prerequisites

- Kubernetes 1.21+
- Helm 3.8+
- cert-manager 1.14+ (auto-installed if not present)

## Installation

### Quick Start (Default Configuration)

Install OptipPod with webhook enabled and auto cert-manager detection:

```bash
helm repo add optipod https://optipod.github.io/charts
helm repo update
helm install optipod optipod/optipod --namespace optipod-system --create-namespace
```

### Installation Options

#### 1. With Automatic cert-manager Detection (Recommended)

The chart automatically detects if cert-manager is installed. If not found, it installs cert-manager automatically:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=auto
```

#### 2. Force cert-manager Installation

Force installation of cert-manager even if it already exists:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true
```

#### 3. Use Existing cert-manager

Skip cert-manager installation (requires cert-manager already installed):

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=false
```

#### 4. Webhook Disabled (SSA Mode Only)

Install without webhook (only Server-Side Apply strategy):

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

#### 5. Development/Testing Setup

Install with relaxed security for kind/minikube:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.failurePolicy=Ignore \
  --set certManager.install=auto
```

## Configuration

### Key Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable mutating webhook | `true` |
| `webhook.failurePolicy` | Webhook failure policy (Ignore/Fail) | `Ignore` |
| `certManager.install` | cert-manager installation (auto/true/false) | `auto` |
| `certManager.installCRDs` | Install cert-manager CRDs | `true` |
| `webhook.deployment.replicaCount` | Number of webhook replicas | `2` |
| `webhook.pdb.enabled` | Enable PodDisruptionBudget | `true` |
| `webhook.networkPolicy.enabled` | Enable NetworkPolicy | `true` |

### cert-manager Auto-Detection

The chart uses Helm's `lookup` function to detect cert-manager:

- **`certManager.install: auto`** (default) - Checks if `certificates.cert-manager.io` CRD exists
  - If found: Uses existing cert-manager
  - If not found: Installs cert-manager as a subchart
- **`certManager.install: true`** - Always installs cert-manager (may conflict if already present)
- **`certManager.install: false`** - Never installs cert-manager (assumes already installed)

### Full Configuration Values

See [values.yaml](values.yaml) for all available configuration options.

## Usage

### 1. Create an OptimizationPolicy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-apps
  namespace: default
spec:
  mode: Recommend  # Auto, Recommend, or Disabled
  selector:
    namespaces:
      allow:
        - production
    workloadSelector:
      matchLabels:
        tier: backend
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
    strategy: webhook        # or ssa
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    updateRequestsOnly: true
```

### 2. Enable Webhook for Pods

Add the annotation to your pod specification:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  annotations:
    optipod.io/webhook-enabled: "true"
spec:
  containers:
  - name: app
    image: nginx:latest
```

### 3. Monitor Optimization

```bash
# Check policy status
kubectl get optimizationpolicy -A

# View webhook logs
kubectl logs -n optipod-system -l app.kubernetes.io/component=webhook

# Check webhook health
kubectl get mutatingwebhookconfiguration
```

## Architecture

### Components

1. **Controller Manager** - Reconciles OptimizationPolicy CRs and applies recommendations via SSA or webhook strategy
2. **Webhook Server** - Mutates pods at admission time (optional for GitOps-safe strategy)
3. **cert-manager** - Manages TLS certificates for webhook (auto-installed)

### Webhook Flow

```
Pod Creation → API Server → Webhook (mutate) → Scheduler → Node
                                ↓
                        OptimizationPolicy Matcher
                                ↓
                        Resource Recommendations
                                ↓
                        JSON Patch Generation
```

## Upgrading

### Upgrade Chart Version

```bash
helm repo update
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values
```

### Enable/Disable Webhook

```bash
# Enable webhook
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --set webhook.enabled=true

# Disable webhook
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --set webhook.enabled=false
```

## Troubleshooting

### Webhook Not Working

1. **Check cert-manager installation**
   ```bash
   kubectl get crd certificates.cert-manager.io
   kubectl get pods -n cert-manager
   ```

2. **Verify certificate is ready**
   ```bash
   kubectl get certificate -n optipod-system
   kubectl describe certificate -n optipod-system
   ```

3. **Check CA bundle injection**
   ```bash
   kubectl get mutatingwebhookconfiguration optipod-mutating-webhook \
     -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d
   ```

4. **View webhook logs**
   ```bash
   kubectl logs -n optipod-system -l app.kubernetes.io/component=webhook --tail=100
   ```

### cert-manager Conflicts

If cert-manager is already installed but not detected:

```bash
# Check existing cert-manager
kubectl get deployment -A | grep cert-manager

# Force use of existing cert-manager
helm upgrade optipod optipod/optipod \
  --set certManager.install=false
```

### Certificate Errors

If webhook fails with certificate errors:

```bash
# Check certificate secret
kubectl get secret webhook-server-certs -n optipod-system -o yaml

# Force certificate renewal
kubectl delete certificate -n optipod-system --all
kubectl delete secret webhook-server-certs -n optipod-system

# Restart webhook pods
kubectl rollout restart deployment -n optipod-system -l app.kubernetes.io/component=webhook
```

## Uninstallation

```bash
# Uninstall release
helm uninstall optipod --namespace optipod-system

# Clean up CRDs (optional)
kubectl delete crd optimizationpolicies.optipod.optipod.io

# Remove cert-manager if installed by this chart (optional)
helm uninstall cert-manager --namespace cert-manager
kubectl delete namespace cert-manager
```

## Development

### Linting

```bash
helm lint charts/optipod
```

### Dry Run

```bash
helm install optipod charts/optipod --dry-run --debug
```

### Template Rendering

```bash
helm template optipod charts/optipod \
  --namespace optipod-system \
  --set webhook.enabled=true
```

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md)

## License

Apache 2.0 License - see [LICENSE](../../LICENSE)

## Support

- Documentation: https://optipod.io/docs
- Issues: https://github.com/optipod/optipod/issues
- Slack: https://optipod.io/slack
