# cert-manager Auto-Detection Feature

## Overview

The OptipPod Helm chart now supports automatic detection of cert-manager installation in your cluster. This eliminates the need to manually specify whether cert-manager should be installed.

## How It Works

The chart uses Helm's `lookup` function to check if the `certificates.cert-manager.io` CRD exists in the cluster:

- **CRD found** → Uses existing cert-manager installation
- **CRD not found** → Installs cert-manager as a subchart

## Configuration Options

### `certManager.install` Values

| Value | Behavior |
|-------|----------|
| `auto` (default) | Automatically detects and installs only if needed |
| `true` | Always installs cert-manager, even if one exists |
| `false` | Never installs cert-manager, assumes it exists |

## Usage Examples

### Default (Auto-Detection)

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace
```

This will:
1. Check if cert-manager CRDs exist
2. If yes → use existing cert-manager
3. If no → install cert-manager as subchart

### Force Install cert-manager

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true
```

### Use Existing cert-manager Only

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=false
```

## Implementation Details

### Template Helper Functions

**`optipod.certManager.isInstalled`**
- Uses `lookup` to check for `certificates.cert-manager.io` CRD
- Returns `"true"` or `"false"` as string

**`optipod.certManager.shouldInstall`**
- Evaluates the `certManager.install` value
- For `"auto"`: returns opposite of `isInstalled`
- For `"true"`: always returns `"true"`
- For `"false"`: always returns `"false"`

### Affected Templates

1. **cert-manager-bootstrap.yaml** - Only creates bootstrap job when installing cert-manager
2. **certificate.yaml** - Creates Certificate resource when using existing cert-manager
3. **issuer.yaml** - Creates Issuer resource when using existing cert-manager
4. **NOTES.txt** - Shows appropriate message based on detection result

## Benefits

1. **Zero Configuration** - Works out of the box without manual cert-manager setup
2. **No Conflicts** - Avoids installing duplicate cert-manager instances
3. **Flexible** - Can override auto-detection when needed
4. **User-Friendly** - Clear feedback in NOTES.txt about what was detected

## Troubleshooting

### Check Detection Result

After installation, check the NOTES output:

```bash
helm get notes optipod -n optipod-system
```

Look for one of these messages:
- ✅ Using existing cert-manager installation detected in cluster
- 📦 cert-manager will be installed as part of this release (auto-detected as missing)

### Verify cert-manager Status

```bash
# Check if cert-manager CRDs exist
kubectl get crd certificates.cert-manager.io

# Check cert-manager pods
kubectl get pods -n cert-manager
kubectl get pods -n optipod-system | grep cert-manager
```

### Force Reinstall

If auto-detection isn't working as expected:

```bash
# Explicitly set the install flag
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set certManager.install=true \
  --reuse-values
```

## Migration Guide

### From Previous Versions

If you previously installed with `certManager.install=false` (old default):

```bash
# Upgrade with auto-detection
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --reuse-values
```

The chart will detect your existing cert-manager and continue using it.

### Switching from Bundled to External cert-manager

If you want to switch from the bundled cert-manager to a cluster-wide one:

1. Install cluster-wide cert-manager:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml
   ```

2. Upgrade OptipPod to use it:
   ```bash
   helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
     --version 1.4.1 \
     --namespace optipod-system \
     --set certManager.install=false
   ```

3. Clean up old cert-manager (if installed in optipod-system):
   ```bash
   helm uninstall cert-manager -n optipod-system
   ```

## Limitations

1. **Lookup Limitations** - The `lookup` function only works during `helm install` and `helm upgrade`, not during `helm template` or `--dry-run`
2. **Namespace Scope** - Detection is cluster-wide (checks for CRD existence), not namespace-specific
3. **Version Detection** - Does not check cert-manager version compatibility

## Future Enhancements

- Version compatibility checking
- Support for alternative certificate providers
- Better handling of cert-manager in different namespaces
