# OptipPod Configuration Guide

## Quick Start

### Default Installation (Recommended)

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace
```

This installs OptipPod with:
- ✅ Webhook enabled
- ✅ cert-manager bundled
- ✅ Ready to use immediately

## Configuration Matrix

| Scenario | webhook.enabled | certManager.install | Use Case |
|----------|----------------|---------------------|----------|
| **Default** | `true` | `true` | Quick start, development, testing |
| **Existing cert-manager** | `true` | `false` | Production with cluster-wide cert-manager |
| **SSA Mode Only** | `false` | `false` | No webhook needed, manual optimization |
| **Webhook without bundled CM** | `true` | `false` | Use existing cert-manager installation |

## Common Scenarios

### 1. First-Time Installation (Default)

Perfect for getting started quickly:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace
```

**What happens:**
- OptipPod webhook is installed
- cert-manager is installed in the same namespace
- Bootstrap job creates certificates automatically
- Everything works out of the box

### 2. Production with Existing cert-manager

If you already have cert-manager installed cluster-wide:

```bash
# Verify cert-manager exists
kubectl get crd certificates.cert-manager.io

# Install OptipPod
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=false
```

**What happens:**
- OptipPod webhook is installed
- Uses your existing cert-manager
- Certificate and Issuer resources are created directly
- No duplicate cert-manager installation

### 3. SSA Mode Only (No Webhook)

For environments where webhook is not needed:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

**What happens:**
- Only the controller is installed
- No webhook components
- No cert-manager needed
- Server-Side Apply (SSA) mode only

### 4. Multi-Tenant Cluster Setup

For clusters with multiple OptipPod installations:

```bash
# Install cert-manager once, cluster-wide
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml

# Install OptipPod in multiple namespaces
helm install optipod-team-a oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace team-a \
  --create-namespace \
  --set certManager.install=false

helm install optipod-team-b oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace team-b \
  --create-namespace \
  --set certManager.install=false
```

## Key Configuration Values

### Webhook Settings

```yaml
webhook:
  enabled: true  # Enable/disable webhook
  port: 9443
  replicas: 2
  resources:
    limits:
      cpu: 500m
      memory: 256Mi
    requests:
      cpu: 100m
      memory: 128Mi
```

### cert-manager Settings

```yaml
certManager:
  install: true  # Install cert-manager as subchart
  installCRDs: true  # Install cert-manager CRDs
  
  issuer:
    kind: Issuer  # or ClusterIssuer
    name: optipod-selfsigned-issuer
    selfSigned: true
  
  certificate:
    secretName: webhook-server-certs
    duration: 8760h  # 1 year
    renewBefore: 720h  # 30 days
```

### cert-manager Subchart Configuration

When `certManager.install=true`, you can pass values to the cert-manager subchart:

```yaml
cert-manager:
  installCRDs: true
  # Add any cert-manager chart values here
```

## Upgrade Scenarios

### From Bundled to External cert-manager

```bash
# Install cluster-wide cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml

# Upgrade OptipPod
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set certManager.install=false \
  --reuse-values
```

### Enable Webhook on Existing Installation

```bash
# Ensure cert-manager is available
kubectl get crd certificates.cert-manager.io

# Upgrade to enable webhook
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set webhook.enabled=true \
  --reuse-values
```

### Disable Webhook

```bash
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set webhook.enabled=false \
  --reuse-values
```

## Troubleshooting

### Check Installation Status

```bash
# Check all pods
kubectl get pods -n optipod-system

# Check webhook deployment
kubectl get deployment -n optipod-system | grep webhook

# Check cert-manager (if bundled)
kubectl get pods -n optipod-system | grep cert-manager

# Check certificates
kubectl get certificate -n optipod-system
kubectl get issuer -n optipod-system
```

### Bootstrap Job Issues

If cert-manager bootstrap fails:

```bash
# Check job status
kubectl get job -n optipod-system

# View logs
kubectl logs -n optipod-system job/optipod-cert-manager-bootstrap

# Check events
kubectl get events -n optipod-system --sort-by='.lastTimestamp'
```

### Certificate Not Ready

```bash
# Check certificate status
kubectl describe certificate -n optipod-system

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
# or for bundled
kubectl logs -n optipod-system deployment/optipod-cert-manager
```

## Best Practices

1. **Development/Testing**: Use default settings (bundled cert-manager)
2. **Production**: Install cert-manager cluster-wide, set `certManager.install=false`
3. **Multi-tenant**: Share one cert-manager across all namespaces
4. **Air-gapped**: Pre-install cert-manager, use `certManager.install=false`
5. **Resource-constrained**: Consider SSA mode only (`webhook.enabled=false`)

## Related Documentation

- [cert-manager Integration](./CERT_MANAGER_AUTO_DETECTION.md)
- [Testing Guide](./TESTING.md)
- [Version Handling](./VERSION_HANDLING.md)
