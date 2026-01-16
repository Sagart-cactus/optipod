# Version Handling in OptipPod Helm Chart

## Overview

This document explains how versions are handled across the OptipPod project, particularly the relationship between Git tags, Helm chart versions, and container image tags.

## Version Format

### Git Tags
- Format: `vX.Y.Z` (e.g., `v1.4.1`)
- Always include the `v` prefix
- Used to trigger releases via GitHub Actions

### Helm Chart Version
- Format: `X.Y.Z` (e.g., `1.4.1`)
- **No** `v` prefix (per Helm conventions)
- Set in `Chart.yaml` as `version: X.Y.Z`

### Container Image Tags
- Format: `vX.Y.Z` (e.g., `v1.4.1`)
- **Include** the `v` prefix
- Set in `Chart.yaml` as `appVersion: "vX.Y.Z"`

### Chart appVersion
- Format: `vX.Y.Z` (e.g., `v1.4.1`)
- **Include** the `v` prefix to match image tags
- Set in `Chart.yaml` as `appVersion: "vX.Y.Z"`
- Used as default image tag when `values.yaml` has `tag: ""`

## Why This Matters

When you install the Helm chart, the image tag is determined by:

1. If `values.yaml` has `tag: ""` (empty) → uses `Chart.appVersion`
2. If `values.yaml` has `tag: "vX.Y.Z"` → uses that specific tag
3. If you override with `--set image.tag=vX.Y.Z` → uses your override

**Critical:** The `appVersion` in `Chart.yaml` MUST match the actual container image tag format, which includes the `v` prefix.

## Example

For release `v1.4.1`:

```yaml
# Chart.yaml
version: 1.4.1        # Helm chart version (no v)
appVersion: "v1.4.1"  # Container image tag (with v)

# values.yaml
image:
  repository: ghcr.io/sagart-cactus/optipod
  tag: ""  # Empty = use Chart.appVersion = v1.4.1
```

This results in pulling: `ghcr.io/sagart-cactus/optipod:v1.4.1`

## Release Workflow

The `.github/workflows/release.yml` handles version updates automatically:

1. **Git tag** `v1.4.1` triggers the workflow
2. **Chart version** is set to `1.4.1` (removes `v`)
3. **appVersion** is set to `v1.4.1` (keeps `v`)
4. **values.yaml tag** is kept empty to use appVersion
5. **Container images** are built and tagged as `v1.4.1`

## Common Issues

### Issue: ImagePullBackOff with wrong tag

**Symptom:**
```
Failed to pull image "ghcr.io/sagart-cactus/optipod:1.4.0": not found
```

**Cause:** Chart.yaml has `appVersion: "1.4.0"` but images are tagged as `v1.4.0`

**Fix:** Update Chart.yaml to include `v` prefix:
```yaml
appVersion: "v1.4.0"  # Not "1.4.0"
```

### Issue: Helm chart version mismatch

**Symptom:**
```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod --version 1.4.1
# But pulls wrong image version
```

**Cause:** The published chart has incorrect appVersion

**Fix:** Override during installation:
```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --set image.tag=v1.4.1
```

## Testing Version Handling

### Test 1: Verify Chart Metadata

```bash
helm show chart ./charts/optipod
# Should show:
# version: 1.4.1
# appVersion: v1.4.1
```

### Test 2: Verify Image Tag Resolution

```bash
helm template test ./charts/optipod | grep "image:"
# Should show: image: ghcr.io/sagart-cactus/optipod:v1.4.1
```

### Test 3: Verify with Override

```bash
helm template test ./charts/optipod --set image.tag=v1.5.0 | grep "image:"
# Should show: image: ghcr.io/sagart-cactus/optipod:v1.5.0
```

## Best Practices

1. **Always use `v` prefix for Git tags**: `v1.4.1`, not `1.4.1`
2. **Never use `v` prefix for Helm chart version**: `1.4.1`, not `v1.4.1`
3. **Always use `v` prefix for appVersion**: `v1.4.1`, not `1.4.1`
4. **Keep values.yaml tag empty**: Let it default to appVersion
5. **Test locally before releasing**: Use `helm template` to verify

## Updating Versions

### For Development

```bash
# Update Chart.yaml manually
version: 1.5.0-dev
appVersion: "v1.5.0-dev"

# Build and test locally
helm template test ./charts/optipod
```

### For Release

The release workflow handles this automatically when you push a tag:

```bash
git tag v1.5.0
git push origin v1.5.0
```

The workflow will:
- Update Chart.yaml version to `1.5.0`
- Update Chart.yaml appVersion to `v1.5.0`
- Build images tagged as `v1.5.0`
- Package and publish Helm chart as `1.5.0`

## Migration from Old Versions

If you have an old chart with incorrect version handling:

```bash
# Option 1: Upgrade with explicit tag
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --set image.tag=v1.4.1 \
  --reuse-values

# Option 2: Reinstall with correct chart
helm uninstall optipod -n optipod-system
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system
```

## References

- [Helm Chart Best Practices - Chart Metadata](https://helm.sh/docs/topics/charts/#the-chartyaml-file)
- [Semantic Versioning](https://semver.org/)
- [Container Image Tagging Best Practices](https://docs.docker.com/engine/reference/commandline/tag/)
