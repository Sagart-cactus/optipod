# cert-manager Auto-Detection Feature

## Overview

The OptipPod Helm chart can optionally install cert-manager as a dependency when webhook support is enabled.

## Configuration Options

### `certManager.install` Values

| Value | Behavior |
|-------|----------|
| `false` (default) | Uses existing cert-manager in the cluster |
| `true` | Installs cert-manager as a subchart |

## Usage Examples

### Use Existing cert-manager (Default)

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace
```

This assumes cert-manager is already installed in your cluster.

### Install cert-manager with OptipPod

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true
```

This will install cert-manager as a subchart in the same namespace.

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

1. **Flexible Deployment** - Choose between bundled or external cert-manager
2. **No Conflicts** - Clear separation between installation modes
3. **Robust Bootstrap** - Comprehensive checks ensure cert-manager is ready
4. **User-Friendly** - Clear feedback in NOTES.txt about configuration

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

- **Production**: Install cert-manager cluster-wide and set `certManager.install=false`
- **Development/Testing**: Use `certManager.install=true` for quick setup
- **Multi-tenant**: Use cluster-wide cert-manager shared across namespaces
