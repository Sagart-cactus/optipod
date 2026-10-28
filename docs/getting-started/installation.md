# Installation Guide

OptiPod is a Kubernetes operator that provides explainable recommendations for CPU and memory requests/limits. This guide covers installation methods and verification steps.

## Prerequisites

- Kubernetes 1.21 or later
- Helm 3.8+ (for Helm installation)
- kubectl configured to access your cluster
- cert-manager 1.14+ (required for webhook mode, auto-installed by default)

## Installation Methods

### Helm Installation (Recommended)

Helm is the recommended installation method as it provides the most flexibility and handles cert-manager dependencies automatically.

#### Install from OCI Registry

```bash
# Install latest version
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --namespace optipod-system \
  --create-namespace

# Install specific version
VERSION=1.5.3  # Replace with desired version
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version "${VERSION}" \
  --namespace optipod-system \
  --create-namespace
```

#### Install from Helm Repository

```bash
# Add the OptiPod Helm repository
helm repo add optipod https://optipod.github.io/charts
helm repo update

# Install OptiPod
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace
```

### kubectl Installation

For GitOps environments or when you prefer kubectl, use the pre-built manifests:

#### Webhook Strategy (ArgoCD/GitOps Compatible)

```bash
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install-webhook.yaml
```

#### SSA Strategy (Traditional Kubernetes)

```bash
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install.yaml
```

### Automated Installation Script

For quick setup with interactive prompts:

```bash
curl -sSL https://raw.githubusercontent.com/Sagart-cactus/optipod/main/config/webhook/install.sh | bash
```

## Installation Options

### cert-manager Behavior

The Helm chart automatically manages cert-manager installation:

- **Auto-detect (default)**: Checks if cert-manager is installed and installs it if needed
- **Force install**: Always installs cert-manager, even if one exists
- **Use existing**: Skips cert-manager installation (requires cert-manager already installed)

```bash
# Auto-detect cert-manager (default)
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace

# Force install cert-manager
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true

# Use existing cert-manager only
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=false
```

### Webhook Configuration

#### Enable Webhook (Default)

Webhook mode is enabled by default and recommended for GitOps environments:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=true
```

#### Disable Webhook (SSA Only)

For environments without webhook requirements:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

#### Development/Testing Setup

For local development with kind or minikube:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.failurePolicy=Ignore
```

## Verification

### Check Installation Status

```bash
# Check OptiPod pods are running
kubectl get pods -n optipod-system

# Expected output:
# NAME                                    READY   STATUS    RESTARTS   AGE
# optipod-controller-manager-xxx          1/1     Running   0          2m
# optipod-webhook-xxx                     1/1     Running   0          2m
```

### Verify CRD Installation

```bash
# Check OptimizationPolicy CRD is installed
kubectl get crd optimizationpolicies.optipod.optipod.io

# Expected output:
# NAME                                      CREATED AT
# optimizationpolicies.optipod.optipod.io   2025-01-28T10:00:00Z
```

### Verify Webhook Configuration (if enabled)

```bash
# Check webhook configuration
kubectl get mutatingwebhookconfiguration optipod-mutating-webhook

# Check certificate is ready
kubectl get certificate -n optipod-system

# Expected output:
# NAME                    READY   SECRET                  AGE
# webhook-server-certs    True    webhook-server-certs    2m
```

### Check Controller Logs

```bash
# View controller logs
kubectl logs -n optipod-system -l app.kubernetes.io/component=controller --tail=50

# View webhook logs (if enabled)
kubectl logs -n optipod-system -l app.kubernetes.io/component=webhook --tail=50
```

## Configuration

### Key Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable mutating webhook | `true` |
| `webhook.failurePolicy` | Webhook failure policy (Ignore/Fail) | `Ignore` |
| `certManager.install` | Install cert-manager subchart | `auto` |
| `image.repository` | OptiPod image repository | `ghcr.io/sagart-cactus/optipod` |
| `image.tag` | OptiPod image tag | Chart.appVersion |
| `webhook.deployment.replicaCount` | Number of webhook replicas | `2` |
| `controller.replicaCount` | Number of controller replicas | `1` |
| `metricsProvider.type` | Metrics provider type | `metrics-server` |
| `metricsProvider.prometheus.url` | Prometheus server URL | `http://prometheus:9090` |
| `metricsProvider.prometheus.auth.type` | Prometheus auth type (none/basic/bearer) | `none` |

### Metrics Provider Configuration

OptiPod supports two metrics providers: `metrics-server` (default) and `prometheus`.

#### Using metrics-server (Default)

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=metrics-server
```

#### Using Prometheus

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=http://prometheus:9090
```

#### Prometheus with Authentication

**Basic Authentication:**

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=http://prometheus:9090 \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.username=admin \
  --set metricsProvider.prometheus.auth.basic.password=secret
```

**Bearer Token Authentication:**

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=http://prometheus:9090 \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.token=your-token
```

**Using Existing Secrets (Recommended):**

Create a secret with credentials:

```bash
# For basic auth
kubectl create secret generic prometheus-auth \
  --namespace optipod-system \
  --from-literal=username=admin \
  --from-literal=password=secret

# For bearer token
kubectl create secret generic prometheus-auth \
  --namespace optipod-system \
  --from-literal=token=your-token
```

Install with secret reference:

```bash
# Basic auth with secret
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=http://prometheus:9090 \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.existingSecret.name=prometheus-auth \
  --set metricsProvider.prometheus.auth.basic.existingSecret.usernameKey=username \
  --set metricsProvider.prometheus.auth.basic.existingSecret.passwordKey=password

# Bearer token with secret
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=http://prometheus:9090 \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=prometheus-auth \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.key=token
```

#### Prometheus with TLS

```bash
# Create secret with TLS certificates
kubectl create secret generic prometheus-tls \
  --namespace optipod-system \
  --from-file=ca.crt=ca.crt \
  --from-file=tls.crt=client.crt \
  --from-file=tls.key=client.key

# Install with TLS
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus:9090 \
  --set metricsProvider.prometheus.tls.enabled=true \
  --set metricsProvider.prometheus.tls.existingSecret.name=prometheus-tls \
  --set metricsProvider.prometheus.tls.existingSecret.caKey=ca.crt \
  --set metricsProvider.prometheus.tls.existingSecret.certKey=tls.crt \
  --set metricsProvider.prometheus.tls.existingSecret.keyKey=tls.key
```

### Custom Values File

Create a `values.yaml` file with your configuration:

```yaml
# values.yaml
webhook:
  enabled: true
  failurePolicy: Ignore
  deployment:
    replicaCount: 3

controller:
  replicaCount: 2
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi

# Metrics provider configuration
metricsProvider:
  type: prometheus
  prometheus:
    url: http://prometheus-server.monitoring:9090
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-auth
          usernameKey: username
          passwordKey: password
    tls:
      enabled: false
    timeout: 30s
```

Install with custom values:

```bash
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace \
  --values values.yaml
```

## Upgrading

### Upgrade to Latest Version

```bash
# Update Helm repository
helm repo update

# Upgrade OptiPod
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values
```

### Upgrade to Specific Version

```bash
VERSION=1.5.3  # Replace with desired version
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version "${VERSION}" \
  --namespace optipod-system \
  --reuse-values
```

## Uninstallation

### Remove OptiPod

```bash
# Uninstall Helm release
helm uninstall optipod --namespace optipod-system

# Delete namespace
kubectl delete namespace optipod-system
```

### Clean Up CRDs (Optional)

```bash
# Remove OptimizationPolicy CRD
kubectl delete crd optimizationpolicies.optipod.optipod.io
```

### Remove cert-manager (Optional)

If cert-manager was installed by OptiPod and is not used by other applications:

```bash
helm uninstall cert-manager --namespace cert-manager
kubectl delete namespace cert-manager
```

## Troubleshooting

### Webhook Not Working

1. **Check cert-manager installation**:
   ```bash
   kubectl get crd certificates.cert-manager.io
   kubectl get pods -n cert-manager
   ```

2. **Verify certificate is ready**:
   ```bash
   kubectl get certificate -n optipod-system
   kubectl describe certificate -n optipod-system
   ```

3. **Check CA bundle injection**:
   ```bash
   kubectl get mutatingwebhookconfiguration optipod-mutating-webhook \
     -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d
   ```

### cert-manager Conflicts

If cert-manager is already installed:

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

## Next Steps

- [Quick Start Guide](quick-start.md) - Create your first optimization policy
- [Creating Your First Policy](first-policy.md) - Detailed policy creation guide
- [Architecture Overview](../concepts/architecture.md) - Understand how OptiPod works

---

*For more detailed configuration options, see the [Helm Values Reference](../reference/helm-values.md).*
