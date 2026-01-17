# Release Workflow Verification Checklist

## Pre-Merge Verification

- [x] YAML syntax is valid
- [ ] Review all changes in `.github/workflows/release.yml`
- [ ] Verify matrix strategies are correctly configured
- [ ] Check artifact names match between jobs

## Post-Merge Testing

### 1. Trigger Test Release
```bash
# Create a test tag
git tag v0.0.0-test
git push origin v0.0.0-test
```

### 2. Monitor Workflow Execution

Check the following in GitHub Actions:

#### Build Job
- [ ] Both amd64 and arm64 jobs start in parallel
- [ ] amd64 runs on `ubuntu-latest`
- [ ] arm64 runs on `ubuntu-24.04-arm`
- [ ] Both complete in ~3-4 minutes each
- [ ] Artifacts uploaded: `container-image-amd64` and `container-image-arm64`

#### Security Scan Job
- [ ] Both amd64 and arm64 scans run in parallel
- [ ] No critical/high vulnerabilities found (or expected ones)
- [ ] SARIF uploaded to Security tab with categories: `security-scan-amd64` and `security-scan-arm64`
- [ ] Artifacts uploaded: `security-scan-results-amd64` and `security-scan-results-arm64`

#### SBOM Job
- [ ] Both amd64 and arm64 SBOMs generated in parallel
- [ ] Artifacts uploaded: `sbom-amd64` and `sbom-arm64`

#### Sign Job
- [ ] Both amd64 and arm64 images signed in parallel
- [ ] Images pushed to GHCR with `-amd64` and `-arm64` tags
- [ ] Signatures verified successfully
- [ ] Provenance attestations created
- [ ] SBOM attestations attached

#### Publish Job
- [ ] Multi-arch manifests created successfully
- [ ] All version tags created (full, minor, major, latest)
- [ ] Manifest inspection shows both architectures
- [ ] Images are pullable

#### Create Release Job
- [ ] All artifacts downloaded successfully
- [ ] Release notes generated correctly
- [ ] Release created with all artifacts
- [ ] Both architecture SBOMs and scan results included

### 3. Verify Multi-Arch Manifest

```bash
# Inspect the manifest
docker buildx imagetools inspect ghcr.io/YOUR_REPO/optipod:v0.0.0-test

# Expected output should show:
# - MediaType: application/vnd.oci.image.index.v1+json
# - Manifests for both linux/amd64 and linux/arm64
```

### 4. Test Image Pulls

```bash
# Pull on amd64 machine
docker pull ghcr.io/YOUR_REPO/optipod:v0.0.0-test
docker inspect ghcr.io/YOUR_REPO/optipod:v0.0.0-test | grep Architecture
# Should show: "Architecture": "amd64"

# Pull on arm64 machine (if available)
docker pull ghcr.io/YOUR_REPO/optipod:v0.0.0-test
docker inspect ghcr.io/YOUR_REPO/optipod:v0.0.0-test | grep Architecture
# Should show: "Architecture": "arm64"
```

### 5. Verify Signatures

```bash
# Verify amd64 signature
cosign verify ghcr.io/YOUR_REPO/optipod:v0.0.0-test-amd64

# Verify arm64 signature
cosign verify ghcr.io/YOUR_REPO/optipod:v0.0.0-test-arm64
```

### 6. Check Attestations

```bash
# Check provenance for amd64
cosign verify-attestation --type slsaprovenance \
  ghcr.io/YOUR_REPO/optipod:v0.0.0-test-amd64

# Check SBOM for amd64
cosign verify-attestation --type spdx \
  ghcr.io/YOUR_REPO/optipod:v0.0.0-test-amd64

# Repeat for arm64
cosign verify-attestation --type slsaprovenance \
  ghcr.io/YOUR_REPO/optipod:v0.0.0-test-arm64

cosign verify-attestation --type spdx \
  ghcr.io/YOUR_REPO/optipod:v0.0.0-test-arm64
```

### 7. Performance Verification

- [ ] Total workflow time is under 10 minutes
- [ ] Build jobs complete in ~3-4 minutes each (parallel)
- [ ] Publish job completes in ~2 minutes
- [ ] No QEMU emulation warnings in logs

### 8. Cleanup Test Release

```bash
# Delete test tag
git tag -d v0.0.0-test
git push origin :refs/tags/v0.0.0-test

# Delete test release from GitHub UI
# Delete test images from GHCR if needed
```

## Common Issues and Solutions

### Issue: ARM64 runner queue time is long
**Solution**: This is expected during public preview. Wait or retry during off-peak hours.

### Issue: ARM64 runner not available
**Solution**: Verify repository is public. ARM64 runners are only free for public repos.

### Issue: Multi-arch manifest creation fails
**Solution**: Ensure both architecture images were pushed successfully in the sign job.

### Issue: Signature verification fails
**Solution**: Check that COSIGN_EXPERIMENTAL=1 is set and GitHub OIDC token is available.

## Success Criteria

✅ All jobs complete successfully
✅ Total time < 10 minutes (vs previous ~25 minutes)
✅ Both architectures built, scanned, signed, and published
✅ Multi-arch manifest works correctly
✅ Images pullable on both architectures
✅ All attestations verifiable

## Rollback Procedure

If critical issues occur:

1. Revert the commit that changed `.github/workflows/release.yml`
2. Push the revert
3. Re-run the release workflow
4. Investigate issues in a separate branch
