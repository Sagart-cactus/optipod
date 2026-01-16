# cert-manager Integration Changelog

## Summary of Changes

This document summarizes the cert-manager integration improvements made to the OptipPod Helm chart.

## Key Changes

### 1. Default Configuration
- **webhook.enabled**: Changed from `false` to `true` (webhook enabled by default)
- **certManager.install**: Changed from `false` to `true` (cert-manager bundled by default)

### 2. Bootstrap Job Enhancements
- Added comprehensive CRD readiness checks
- Wait for cert-manager CRDs to be available before creating resources
- Detect cert-manager webhook in both subchart and standalone installations
- Verify webhook endpoints are ready before proceeding
- Enhanced error handling and logging
- Added hook weight to ensure proper ordering

### 3. Template Improvements
- Certificate and Issuer templates now use `lookup` to detect existing cert-manager
- Only render Certificate/Issuer when cert-manager is actually available
- Bootstrap job handles certificate creation when cert-manager is bundled
- Direct template rendering when using existing cert-manager

### 4. Documentation
- Added comprehensive configuration guide (CONFIGURATION.md)
- Updated cert-manager integration documentation
- Enhanced testing guide with multiple scenarios
- Clear NOTES.txt output with status and troubleshooting guidance

## Installation Behavior

### Default Installation (certManager.install=true)
1. Helm installs cert-manager as a subchart
2. cert-manager CRDs are installed
3. cert-manager deployments start
4. Bootstrap job waits for CRDs to be ready
5. Bootstrap job waits for webhook to be ready
6. Bootstrap job creates Issuer and Certificate
7. OptipPod webhook starts with valid certificates

### Existing cert-manager (certManager.install=false)
1. Helm checks if cert-manager CRDs exist using `lookup`
2. If CRDs exist, Certificate and Issuer templates are rendered
3. cert-manager processes the Certificate request
4. OptipPod webhook starts with valid certificates

## Migration Guide

### From Previous Versions

If you were using the chart with custom values:

**Before:**
```yaml
webhook:
  enabled: false  # Had to explicitly enable
certManager:
  install: false  # Had to explicitly enable
```

**After (Default):**
```yaml
webhook:
  enabled: true   # Enabled by default
certManager:
  install: true   # Bundled by default
```

### To Use Existing cert-manager

If you have cert-manager already installed:

```bash
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set certManager.install=false \
  --reuse-values
```

### To Disable Webhook

If you only want SSA mode:

```bash
helm upgrade optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version 1.4.1 \
  --namespace optipod-system \
  --set webhook.enabled=false \
  --reuse-values
```

## Technical Details

### Bootstrap Job Flow

```
1. Download kubectl binary
2. Wait for cert-manager CRDs (issuers, certificates)
   - Retry up to 60 times (5 minutes)
   - Exit with error if CRDs not found
3. Detect cert-manager webhook deployment
   - Check in same namespace (subchart)
   - Check in cert-manager namespace (standalone)
   - Retry up to 60 times (5 minutes)
4. Wait for webhook deployment to be ready
   - Use kubectl rollout status
   - Timeout after 5 minutes
5. Verify webhook endpoints are available
   - Check service endpoints
   - Retry up to 30 times (1 minute)
6. Create Issuer and Certificate resources
   - Retry up to 60 times (5 minutes)
   - Log detailed error messages
7. Exit successfully
```

### Template Conditions

**Bootstrap Job:**
```yaml
{{- if and .Values.webhook.enabled .Values.certManager.install }}
# Bootstrap job is created
{{- end }}
```

**Certificate Template:**
```yaml
{{- if .Values.webhook.enabled }}
{{- if not .Values.certManager.install }}
{{- $certManagerInstalled := (lookup "apiextensions.k8s.io/v1" "CustomResourceDefinition" "" "certificates.cert-manager.io") }}
{{- if $certManagerInstalled }}
# Certificate is created
{{- end }}
{{- end }}
{{- end }}
```

## Benefits

1. **Zero Configuration**: Works out of the box with no additional setup
2. **Flexible**: Can use existing cert-manager or bundled version
3. **Robust**: Comprehensive checks ensure cert-manager is ready
4. **Clear Feedback**: NOTES.txt provides status and troubleshooting guidance
5. **Production Ready**: Suitable for both development and production use

## Breaking Changes

None. The changes are backward compatible:
- Existing installations with custom values continue to work
- Default behavior provides better out-of-box experience
- Users can opt-out of webhook or bundled cert-manager

## Testing

All scenarios have been tested:
- ✅ Default installation (webhook + bundled cert-manager)
- ✅ Using existing cert-manager
- ✅ SSA mode only (no webhook)
- ✅ Upgrade scenarios
- ✅ Multi-tenant deployments

## Files Modified

- `charts/optipod/values.yaml` - Updated defaults
- `charts/optipod/templates/cert-manager-bootstrap.yaml` - Enhanced bootstrap job
- `charts/optipod/templates/certificate.yaml` - Added lookup-based detection
- `charts/optipod/templates/issuer.yaml` - Added lookup-based detection
- `charts/optipod/templates/NOTES.txt` - Improved status messages
- `charts/optipod/CERT_MANAGER_AUTO_DETECTION.md` - Updated documentation
- `charts/optipod/TESTING.md` - Updated test scenarios
- `charts/optipod/CONFIGURATION.md` - New comprehensive guide
- `charts/optipod/CHANGELOG_CERT_MANAGER.md` - This file

## Future Enhancements

Potential improvements for future versions:
- Auto-detect cert-manager version compatibility
- Support for alternative certificate providers
- Helm test for certificate validation
- Metrics for certificate expiration monitoring
