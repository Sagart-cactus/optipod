# OptipPod Helm Chart Installation Guide

This guide walks you through installing OptipPod using Helm with automatic cert-manager detection.

## Prerequisites

- Kubernetes cluster (1.21+)
- Helm 3.8+
- kubectl configured to access your cluster

## Quick Start

### 1. Install with Auto cert-manager Detection (Recommended)

This is the simplest installation method. The chart will automatically detect if cert-manager is installed, and if not, install it for you:

```bash
# Add the Helm repository (when published)
helm repo add optipod https://optipod.github.io/charts
helm repo update

# Install OptipPod
helm install optipod optipod/optipod \
  --namespace optipod-system \
  --create-namespace
```

### 2. Install from Local Chart (Development)

If you're developing or testing locally:

```bash
# Navigate to the project root
cd /path/to/optipod

# Build Helm dependencies
helm dependency build charts/optipod

# Install the chart
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace
```

## Installation Scenarios

### Scenario 1: Fresh Cluster (No cert-manager)

The chart will automatically install cert-manager:

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=auto
```

**What happens:**
1. Helm checks if cert-manager CRDs exist
2. Not found → Installs cert-manager v1.14.2
3. Creates self-signed Issuer for webhook certificates
4. Deploys webhook with automatic certificate management

### Scenario 2: Existing cert-manager

The chart detects and uses your existing cert-manager:

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=auto
```

**What happens:**
1. Helm checks if cert-manager CRDs exist
2. Found → Skips cert-manager installation
3. Uses existing cert-manager for certificates
4. Deploys webhook with automatic certificate management

### Scenario 3: Production with Custom Values

Create a custom values file:

```yaml
# production-values.yaml
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
  install: false  # Use existing production cert-manager

metrics:
  enabled: true
  serviceMonitor:
    enabled: true  # For Prometheus Operator

logging:
  level: info
  format: json
```

Install with custom values:

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --values production-values.yaml
```

### Scenario 4: Development/Testing (kind/minikube)

For local development clusters:

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=auto \
  --set webhook.failurePolicy=Ignore \
  --set webhook.deployment.replicaCount=1
```

### Scenario 5: SSA Mode Only (No Webhook)

Install without the mutating webhook:

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

**Use case:** When you only want Server-Side Apply strategy without admission webhooks.

## Verification

### 1. Check Installation Status

```bash
# Check all resources
helm status optipod -n optipod-system

# Check webhook deployment
kubectl get deployment optipod-webhook -n optipod-system

# Check webhook pods
kubectl get pods -n optipod-system -l app.kubernetes.io/component=webhook

# Check certificate status
kubectl get certificate -n optipod-system
kubectl describe certificate -n optipod-system
```

### 2. Verify cert-manager

```bash
# Check if cert-manager is installed
kubectl get crd certificates.cert-manager.io

# Check cert-manager pods
kubectl get pods -n cert-manager

# View certificate details
kubectl get certificate -n optipod-system -o yaml
```

### 3. Verify Webhook Configuration

```bash
# Check webhook configuration
kubectl get mutatingwebhookconfiguration

# Verify CA bundle is injected
kubectl get mutatingwebhookconfiguration optipod-mutating-webhook \
  -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | base64 -d | openssl x509 -text -noout
```

### 4. Test Webhook Functionality

Create a test pod with webhook enabled:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-webhook
  annotations:
    optipod.io/webhook-enabled: "true"
spec:
  containers:
  - name: nginx
    image: nginx:latest
EOF
```

Check webhook logs:

```bash
kubectl logs -n optipod-system -l app.kubernetes.io/component=webhook --tail=50
```

## Upgrading

### Upgrade to Latest Version

```bash
# Update Helm repository
helm repo update

# Upgrade release
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values
```

### Upgrade with New Values

```bash
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --values production-values.yaml
```

### Enable Webhook After Installation

```bash
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values \
  --set webhook.enabled=true
```

## Troubleshooting

### Issue: cert-manager Not Found

**Symptoms:**
- Webhook deployment fails
- Certificate resources not created

**Solution:**
```bash
# Force install cert-manager
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values \
  --set certManager.install=true
```

### Issue: Certificate Not Ready

**Symptoms:**
- Certificate stuck in "Pending" state
- Webhook pods crashlooping

**Debug:**
```bash
# Check certificate status
kubectl describe certificate -n optipod-system

# Check cert-manager logs
kubectl logs -n cert-manager -l app=cert-manager

# Check issuer status
kubectl describe issuer -n optipod-system
```

**Solution:**
```bash
# Delete and recreate certificate
kubectl delete certificate --all -n optipod-system
kubectl delete secret webhook-server-certs -n optipod-system
kubectl rollout restart deployment -n optipod-system
```

### Issue: Webhook CA Bundle Not Injected

**Symptoms:**
- Webhook returns TLS errors
- CA bundle field is empty

**Debug:**
```bash
# Check CA bundle
kubectl get mutatingwebhookconfiguration optipod-mutating-webhook \
  -o jsonpath='{.webhooks[0].clientConfig.caBundle}'

# Check cert-manager webhook
kubectl get pods -n cert-manager | grep webhook
```

**Solution:**
```bash
# Restart cert-manager webhook
kubectl rollout restart deployment cert-manager-webhook -n cert-manager

# Wait for CA injection (may take 1-2 minutes)
kubectl wait --for=condition=available deployment/cert-manager-webhook -n cert-manager
```

### Issue: Duplicate cert-manager Installation

**Symptoms:**
- Helm shows cert-manager conflict
- cert-manager resources already exist

**Solution:**
```bash
# Use existing cert-manager
helm upgrade optipod optipod/optipod \
  --namespace optipod-system \
  --reuse-values \
  --set certManager.install=false
```

## Uninstallation

### Standard Uninstall

```bash
# Uninstall OptipPod
helm uninstall optipod --namespace optipod-system

# Delete namespace
kubectl delete namespace optipod-system
```

### Complete Cleanup (Including CRDs)

```bash
# Uninstall OptipPod
helm uninstall optipod --namespace optipod-system

# Delete CRDs
kubectl delete crd optimizationpolicies.optipod.optipod.io

# Delete namespace
kubectl delete namespace optipod-system

# Optionally delete cert-manager if installed by this chart
helm uninstall cert-manager --namespace cert-manager
kubectl delete namespace cert-manager
kubectl delete crd certificates.cert-manager.io
kubectl delete crd issuers.cert-manager.io
```

## Configuration Reference

### Essential Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable webhook | `true` |
| `certManager.install` | cert-manager install mode (auto/true/false) | `auto` |
| `webhook.failurePolicy` | Webhook failure policy (Ignore/Fail) | `Ignore` |
| `webhook.deployment.replicaCount` | Webhook replicas | `2` |

### All Values

See [`charts/optipod/values.yaml`](optipod/values.yaml) for complete configuration options.

## Next Steps

1. **Create OptimizationPolicies**: Define policies for your workloads
2. **Monitor Optimization**: Watch logs and metrics
3. **Fine-tune Settings**: Adjust webhook behavior based on your needs

## Support

- **Documentation**: https://optipod.io/docs
- **GitHub Issues**: https://github.com/optipod/optipod/issues
- **Community Slack**: https://optipod.io/slack

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for how to contribute to the project.
