# CI/CD Testing Guide

This guide explains how to test and validate the CI/CD workflows locally and in GitHub Actions.

## Prerequisites

- Docker and Docker Buildx
- kubectl
- cosign (for signature verification)
- actionlint (for workflow validation)
- act (optional, for local workflow testing)

## Workflow Validation

### Validate Workflow Syntax

Use the provided script to validate all workflow YAML files:

```bash
./hack/validate-workflows.sh
```

This will:
- Install actionlint if not present
- Validate all workflow files in `.github/workflows/`
- Report any syntax errors or deprecated actions

### Local Workflow Testing with act

Install act:
```bash
# macOS
brew install act

# Linux
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
```

Test workflows locally:
```bash
# Test CI workflow
act push -W .github/workflows/ci.yml

# Test release workflow (requires secrets)
act push -W .github/workflows/release.yml --secret-file .secrets
```

## Testing the Release Workflow

### Option 1: Test Tag (Recommended)

Use the test script to create a test release:

```bash
./hack/test-release-workflow.sh
```

This will:
1. Create a test tag (e.g., `v0.0.0-test-1234567890`)
2. Push the tag to trigger the release workflow
3. Provide a link to monitor the workflow

After validation, clean up:
```bash
# Delete local tag
git tag -d v0.0.0-test-1234567890

# Delete remote tag
git push origin :refs/tags/v0.0.0-test-1234567890

# Delete GitHub release
gh release delete v0.0.0-test-1234567890 --yes
```

### Option 2: Manual Trigger

Trigger the release workflow manually from GitHub:

1. Go to Actions → Release workflow
2. Click "Run workflow"
3. Enter a test version (e.g., `v0.0.0-test`)
4. Click "Run workflow"

## Verifying Release Artifacts

### Check Artifact Completeness

After a release, verify all artifacts are present:

```bash
./hack/check-release-artifacts.sh v1.0.0
```

This checks for:
- install.yaml
- Binaries (linux-amd64, linux-arm64, darwin-amd64, darwin-arm64)
- SBOM (sbom.json)
- Security scan results (trivy-results.json)
- Checksums (checksums.txt)

### Verify Image Signatures

Verify the signature of a released image:

```bash
./hack/verify-image-signature.sh ghcr.io/yourusername/optipod:v1.0.0
```

This will:
- Verify the image signature using cosign
- Check SLSA provenance attestation
- Check SBOM attestation

### Manual Verification

Pull and inspect the image:
```bash
# Pull the image
docker pull ghcr.io/yourusername/optipod:v1.0.0

# Inspect image metadata
docker inspect ghcr.io/yourusername/optipod:v1.0.0

# Check multi-architecture support
docker manifest inspect ghcr.io/yourusername/optipod:v1.0.0
```

Test the installation manifest:
```bash
# Download the manifest
curl -LO https://github.com/yourusername/optipod/releases/download/v1.0.0/install.yaml

# Validate with kubectl
kubectl apply --dry-run=client -f install.yaml

# Install in a test cluster
kubectl apply -f install.yaml
```

## Troubleshooting

### Workflow Fails at Build Step

Check:
- Dockerfile syntax
- Go module dependencies
- Build arguments

### Security Scan Fails

Check:
- Trivy scan results in workflow logs
- Update base images if vulnerabilities found
- Review vulnerability report artifact

### Image Push Fails

Check:
- GitHub token permissions
- Registry authentication
- Network connectivity

### Signing Fails

Check:
- OIDC token configuration
- Cosign installation
- Rekor availability

### Manifest Generation Fails

Check:
- Kustomize configuration
- Image reference format
- kubectl validation output

## Best Practices

1. **Always test with a test tag first** before creating production releases
2. **Review security scan results** before approving releases
3. **Verify signatures** after each release
4. **Test installation manifests** in a test cluster
5. **Monitor workflow execution** for any warnings or errors
6. **Keep dependencies updated** using Dependabot

## Monitoring

Monitor workflow health:
- Check workflow status badges in README
- Review GitHub Actions dashboard
- Set up notifications for workflow failures
- Track build times and cache hit rates

## Additional Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Docker Buildx Documentation](https://docs.docker.com/buildx/)
- [Cosign Documentation](https://docs.sigstore.dev/cosign/)
- [Trivy Documentation](https://aquasecurity.github.io/trivy/)
