# OptipPod v1.5.0 Release Notes

## 🎉 Major Features

### ArgoCD Webhook Compatibility
**Full support for ArgoCD's `selfHeal: true` feature with webhook strategy**

OptipPod now works seamlessly with ArgoCD's self-healing by storing resource recommendations on workload metadata instead of pod templates. This prevents ArgoCD from reverting OptipPod's optimizations.

**Key improvements:**
- ✅ Webhook reads annotations from parent workload (Deployment/StatefulSet/DaemonSet) metadata
- ✅ ArgoCD preserves workload metadata annotations while managing pod specs
- ✅ No manual intervention or workarounds required
- ✅ Backward compatible with existing deployments

**Supported workload types:**
- Deployments (via ReplicaSet ownership chain)
- StatefulSets (direct ownership)
- DaemonSets (direct ownership)

**Testing results:**
- Deployment: 99.5% CPU reduction, 99.2% memory reduction
- StatefulSet: 99% CPU reduction, 98.4% memory reduction
- DaemonSet: 98% CPU reduction, 96.9% memory reduction

### Cert-Manager Auto-Detection and Integration
**Simplified installation with intelligent cert-manager handling**

The Helm chart now automatically detects and manages cert-manager installation, making webhook setup effortless.

**Key features:**
- 🔍 Auto-detection of existing cert-manager installations
- 🚀 Automatic installation when not present (configurable)
- 📦 Bootstrap job ensures CRDs are ready before webhook deployment
- ⚙️ Flexible configuration: `auto`, `true`, or `false`
- 🔒 Webhook and cert-manager enabled by default for better out-of-box experience

**Configuration options:**
```yaml
certManager:
  install: auto  # auto-detect (default) | true (always install) | false (use existing)

webhook:
  enabled: true  # Now enabled by default
```

## 🐛 Bug Fixes

### Webhook & RBAC
- Added RBAC permissions for webhook to read deployments, statefulsets, daemonsets, and replicasets
- Fixed webhook to traverse ownership chains correctly
- Improved error handling for missing parent workloads

### Cert-Manager Bootstrap
- Fixed CRD permissions in bootstrap job service account
- Improved kubectl download reliability in bootstrap job
- Enhanced CRD detection logic to prevent race conditions
- Fixed invalid template syntax in values.yaml

## 📚 Documentation

### New Documentation
- `CERT_MANAGER_AUTO_DETECTION.md` - Detailed guide on cert-manager integration
- `CHANGELOG_CERT_MANAGER.md` - Complete changelog for cert-manager features
- `CONFIGURATION.md` - Comprehensive configuration reference
- `TESTING.md` - Testing guide for cert-manager integration
- `VERSION_HANDLING.md` - Version management and upgrade guide

### Updated Documentation
- Installation guide updated with OCI registry instructions
- README updated with latest chart installation options
- Removed hardcoded version references for better maintainability
- Improved NOTES.txt with better post-install guidance

## 🔧 Technical Changes

### Core Changes
- **internal/webhook/mutator.go**:
  - Added `getParentDeploymentAnnotations()` for ownership chain traversal
  - Added `getStatefulSetAnnotations()` and `getDaemonSetAnnotations()` helpers
  - Modified `isWebhookEnabled()` to check parent workload annotations
  - Modified `getRecommendationsFromAnnotations()` to prefer parent annotations
  - Added constants for string literals (`trueValue`, `appsV1APIGroup`)

- **internal/webhook/annotations.go**:
  - Removed `copyRecommendationAnnotations()` function (no longer needed)
  - Removed pod template annotation copying logic
  - Added ArgoCD compatibility comments

- **charts/optipod/templates/rbac.yaml**:
  - Added read permissions for deployments, statefulsets, daemonsets, replicasets

- **charts/optipod/templates/cert-manager-bootstrap.yaml**:
  - Enhanced bootstrap job with better CRD detection
  - Improved kubectl download with fallback mechanisms
  - Added proper RBAC for CRD operations

### Chart Changes
- Version bumped to 1.5.0
- Webhook enabled by default
- Cert-manager auto-detection enabled by default
- Improved Helm hooks and dependencies

## 📊 Statistics

- **23 files changed**: 1,422 insertions(+), 126 deletions(-)
- **16 commits** since v1.4.1
- **3 major features** delivered
- **Multiple bug fixes** and improvements

## 🚀 Upgrade Instructions

### From v1.4.x

1. **Update your Helm repository:**
   ```bash
   helm repo update
   ```

2. **Upgrade OptipPod:**
   ```bash
   helm upgrade optipod oci://ghcr.io/optipod/charts/optipod --version 1.5.0
   ```

3. **For ArgoCD users:**
   - No changes needed! The webhook now works automatically with `selfHeal: true`
   - Existing deployments will continue to work
   - New deployments will benefit from improved ArgoCD compatibility

4. **For cert-manager users:**
   - If you have cert-manager installed, set `certManager.install: false`
   - If you want auto-detection, use `certManager.install: auto` (default)
   - The bootstrap job will handle CRD readiness automatically

### Breaking Changes

**None** - This release is fully backward compatible.

## 🙏 Contributors

Thank you to everyone who contributed to this release!

## 📝 Full Changelog

For a complete list of changes, see: https://github.com/Sagart-cactus/optipod/compare/v1.4.1...v1.5.0
