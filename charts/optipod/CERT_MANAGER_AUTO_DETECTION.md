# cert-manager Integration

## Overview

OptipPod webhook requires cert-manager for TLS certificate management. By default, OptipPod installs cert-manager as a subchart, but you can also use an existing cert-manager installation.

## Default Behavior

By default, OptipPod installs with:
- ✅ Webhook enabled
- ✅ cert-manager bundled as a subchart

This provides a complete, ready-to-use installation with no additional setup required.

## Configuration Options

### `certManager.install` Values

| Value | Behavior |
|-------|----------|
| `true` (default) | Installs cert-manager as a subchart |
| `false` | Uses existing cert-manager in the cluster |

### `webhook.enabled` Values

| Value | Behavior |
|-------|----------|
| `true` (default) | Enables the mutating webhook |
| `false` | Disables webhook (SSA mode only) |

## Usage Examples

### Default Installation (Recommended)

Installs OptipPod with webhook and bundled cert-manager:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace
```

This is the simplest option - everything works out of the box.

### Use Existing cert-manager

If you already have cert-manager installed cluster-wide:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=false
```

This avoids installing a duplicate cert-manager instance.

### Install Without Webhook

For environments where you only want SSA mode:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

This doesn't require cert-manager at all.

## Implementation Details

### Bootstrap Job

When `certManager.install=true`, a bootstrap job is created that:
1. Waits for cert-manager CRDs to be available
2. Waits for cert-manager webhook to be ready
3. Creates the Issuer and Certificate resources

The bootstrap job includes:
- CRD readiness checks
- Webhook deployment detection (supports both subchart and standalone installations)
- Endpoint verification
- Comprehensive error handling and logging

### Affected Templates

1. **cert-manager-bootstrap.yaml** - Creates bootstrap job when `certManager.install=true`
2. **certificate.yaml** - Creates Certificate resource when `certManager.install=false`
3. **issuer.yaml** - Creates Issuer resource when `certManager.install=false`
4. **NOTES.txt** - Shows appropriate message based on configuration

## Benefits

1. **Zero Configuration** - Works out of the box with webhook and cert-manager
2. **Flexible Deployment** - Choose between bundled or external cert-manager
3. **No Conflicts** - Can use existing cert-manager to avoid duplication
4. **Robust Bootstrap** - Comprehensive checks ensure cert-manager is ready
5. **User-Friendly** - Clear feedback about configuration and status

## Troubleshooting

### Check cert-manager Status

```bash
# Check if cert-manager CRDs exist
kubectl get crd certificates.cert-manager.io

# Check cert-manager pods
kubectl get pods -n cert-manager
kubectl get pods -n optipod-system | grep cert-manager
```

### Bootstrap Job Logs

If cert-manager installation fails, check the bootstrap job logs:

```bash
kubectl logs -n optipod-system job/optipod-cert-manager-bootstrap
```

### Common Issues

**CRDs not found**
- Ensure cert-manager is installed before OptipPod
- Or set `certManager.install=true` to install it automatically

**Webhook not ready**
- The bootstrap job waits up to 5 minutes for cert-manager webhook
- Check cert-manager pod status: `kubectl get pods -n cert-manager`

## Migration Guide

### Installing cert-manager Separately

For production environments, it's recommended to install cert-manager cluster-wide:

1. Install cert-manager:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml
   ```

2. Install OptipPod (uses existing cert-manager):
   ```bash
   helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
     --version 1.4.1 \
     --namespace optipod-system \
     --create-namespace
   ```

### Switching from Bundled to External cert-manager

If you initially installed with `certManager.install=true` and want to switch:

1. Install cluster-wide cert-manager:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml
   ```

2. Upgrade OptipPod to use external cert-manager:
   ```bash
   helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
     --version 1.4.1 \
     --namespace optipod-system \
     --set certManager.install=false \
     --reuse-values
   ```

## Limitations

1. **Namespace Scope** - When installing cert-manager as a subchart, it's installed in the same namespace as OptipPod
2. **Single Instance** - If cert-manager is already installed, set `certManager.install=false` to avoid conflicts
3. **Version** - The bundled cert-manager version is v1.14.2

## Recommendations

- **Quick Start/Development**: Use default settings (bundled cert-manager)
- **Production with existing cert-manager**: Set `certManager.install=false`
- **Production without cert-manager**: Use default settings or install cert-manager separately first
- **Multi-tenant clusters**: Install cert-manager cluster-wide once, then set `certManager.install=false` for all OptipPod installations
