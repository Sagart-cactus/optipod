# CI/CD Pipeline Design Document

## Overview

This design document describes a comprehensive CI/CD pipeline for the OptiPod Kubernetes operator using GitHub Actions. The pipeline automates building, testing, security scanning, and publishing of multi-architecture container images. It implements a trunk-based development workflow with automated releases triggered by semantic version tags.

The system consists of multiple GitHub Actions workflows that handle different aspects of the CI/CD process:
- Continuous Integration (CI) for validation on every commit
- Release automation for tagged versions
- Security scanning and vulnerability management
- Multi-architecture image building and publishing
- Automated dependency updates

## Architecture

### Workflow Structure

The CI/CD system is organized into the following workflows:

1. **CI Workflow** (`ci.yml`): Runs on all pushes and pull requests
   - Linting (already exists in `lint.yml`)
   - Unit tests (already exists in `test.yml`)
   - E2E tests (already exists in `test-e2e.yml`)
   - Build validation

2. **Release Workflow** (`release.yml`): Runs on version tags (v*.*.*)
   - Multi-architecture image builds
   - Security scanning
   - Image signing and attestation
   - Publishing to container registries
   - GitHub release creation with artifacts
   - Manifest generation

3. **Dependency Update Workflow** (Dependabot configuration)
   - Automated dependency updates
   - Security vulnerability patches

### Build Process Flow

```
Tag Push (v*.*.*) → Release Workflow
  ↓
Checkout & Setup
  ↓
Run Tests (lint, unit, e2e)
  ↓
Build Multi-Arch Images (amd64, arm64, s390x, ppc64le)
  ↓
Security Scan (Trivy)
  ↓
Generate SBOM
  ↓
Sign Images (cosign)
  ↓
Push to Registries (GHCR, Docker Hub)
  ↓
Generate Manifests
  ↓
Create GitHub Release
```

### Container Registry Strategy

Images will be published to:
- **Primary**: GitHub Container Registry (ghcr.io)
  - Automatically available for the repository
  - Integrated with GitHub permissions
  - Free for public repositories
  
- **Secondary** (optional): Docker Hub
  - Wider ecosystem compatibility
  - Requires Docker Hub account and credentials

Image naming convention:
- `ghcr.io/<owner>/optipod:<version>`
- `docker.io/<username>/optipod:<version>`

### Tagging Strategy

For each release version `v1.2.3`, the following tags are created:
- `v1.2.3` - Full semantic version
- `v1.2` - Minor version (updated on patch releases)
- `v1` - Major version (updated on minor/patch releases)
- `latest` - Always points to the most recent release

For development builds (non-release):
- `main` - Latest commit on main branch
- `<branch-name>` - Latest commit on feature branches
- `sha-<commit-sha>` - Specific commit identifier

## Components and Interfaces

### GitHub Actions Workflows

#### 1. CI Workflow Enhancement

Extends existing workflows to add build validation:

**Inputs:**
- Triggered by: push, pull_request events
- Branches: all

**Steps:**
1. Checkout code
2. Setup Go environment
3. Run existing lint workflow
4. Run existing test workflow
5. Run existing e2e test workflow
6. Build binary (validation only, no image push)
7. Report status

**Outputs:**
- Build success/failure status
- Test coverage reports
- Lint results

#### 2. Release Workflow

**Inputs:**
- Triggered by: push of tags matching `v*.*.*`
- Manual trigger via workflow_dispatch with version parameter

**Steps:**
1. **Validation Phase**
   - Checkout code
   - Validate tag format
   - Run full test suite
   
2. **Build Phase**
   - Setup Docker Buildx
   - Configure QEMU for multi-arch
   - Build images for all platforms
   - Use layer caching
   
3. **Security Phase**
   - Scan images with Trivy
   - Generate SBOM with Syft
   - Check vulnerability thresholds
   
4. **Signing Phase**
   - Install cosign
   - Sign images with keyless signing
   - Attach attestations
   
5. **Publishing Phase**
   - Login to GHCR
   - Login to Docker Hub (if configured)
   - Push multi-platform manifests
   - Tag with version variants
   
6. **Release Phase**
   - Generate installation manifests
   - Create GitHub release
   - Upload artifacts (manifests, SBOM, binaries)
   - Generate release notes

**Outputs:**
- Container images in registries
- GitHub release with artifacts
- SBOM and security scan results
- Signed image attestations

#### 3. Dependabot Configuration

**Configuration file:** `.github/dependabot.yml`

**Managed dependencies:**
- Go modules
- GitHub Actions
- Docker base images

**Update frequency:**
- Security updates: immediate
- Regular updates: weekly

### Security Scanning Integration

**Tool:** Trivy (Aqua Security)

**Scan targets:**
- Container images
- Filesystem (for Go dependencies)
- Configuration files

**Vulnerability thresholds:**
- CRITICAL: Fail build
- HIGH: Fail build
- MEDIUM: Warn only
- LOW: Warn only

**Scan outputs:**
- SARIF format for GitHub Security tab
- JSON format for artifacts
- Human-readable table for logs

### Image Signing and Attestation

**Tool:** Cosign (Sigstore)

**Signing method:** Keyless signing with OIDC
- Uses GitHub's OIDC token
- No key management required
- Verifiable via public Rekor transparency log

**Attestations include:**
- Build provenance (SLSA)
- SBOM
- Vulnerability scan results

## Data Models

### Workflow Configuration Schema

```yaml
# Release workflow structure
name: Release
on:
  push:
    tags: ['v*.*.*']
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to release'
        required: true
        type: string

env:
  REGISTRY_GHCR: ghcr.io
  REGISTRY_DOCKERHUB: docker.io
  IMAGE_NAME: ${{ github.repository }}
  PLATFORMS: linux/amd64,linux/arm64,linux/s390x,linux/ppc64le

jobs:
  # Job definitions...
```

### Image Metadata

```json
{
  "image": "ghcr.io/owner/optipod:v1.0.0",
  "digest": "sha256:abc123...",
  "platforms": ["linux/amd64", "linux/arm64", "linux/s390x", "linux/ppc64le"],
  "created": "2024-01-15T10:30:00Z",
  "labels": {
    "org.opencontainers.image.source": "https://github.com/owner/optipod",
    "org.opencontainers.image.version": "v1.0.0",
    "org.opencontainers.image.revision": "abc123def456",
    "org.opencontainers.image.licenses": "Apache-2.0"
  },
  "sbom": "attached",
  "signature": "verified",
  "vulnerabilities": {
    "critical": 0,
    "high": 0,
    "medium": 2,
    "low": 5
  }
}
```

### Release Artifact Structure

```
Release v1.0.0
├── install.yaml (Kubernetes manifests)
├── optipod-linux-amd64 (Binary)
├── optipod-linux-arm64 (Binary)
├── optipod-darwin-amd64 (Binary)
├── optipod-darwin-arm64 (Binary)
├── sbom.json (Software Bill of Materials)
├── vulnerability-report.json (Security scan results)
└── checksums.txt (SHA256 checksums)
```

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system—essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Build determinism

*For any* commit SHA, building the container image multiple times should produce images with identical functionality and dependencies (layer hashes may differ due to timestamps, but content should be equivalent)

**Validates: Requirements 1.1, 1.3**

### Property 2: Tag consistency

*For any* semantic version tag v.X.Y.Z, the CI/CD System should create exactly four image tags: vX.Y.Z, vX.Y, vX, and latest, all pointing to the same image digest

**Validates: Requirements 2.3**

### Property 3: Multi-architecture completeness

*For any* release build, all four target platforms (linux/amd64, linux/arm64, linux/s390x, linux/ppc64le) should be present in the multi-platform manifest

**Validates: Requirements 2.2**

### Property 4: Security gate enforcement

*For any* container image with critical or high severity vulnerabilities, the CI/CD System should fail the build and prevent image publication to registries

**Validates: Requirements 3.3**

### Property 5: Registry synchronization

*For any* released version, the image should be available in all configured registries with identical tags and digests

**Validates: Requirements 4.1, 4.3, 4.4**

### Property 6: Manifest validity

*For any* generated deployment manifest, running kubectl apply --dry-run should succeed without errors

**Validates: Requirements 6.4**

### Property 7: Artifact completeness

*For any* GitHub release, all required artifacts (install.yaml, binaries for all platforms, SBOM, checksums) should be attached

**Validates: Requirements 2.4, 6.3**

### Property 8: Cache effectiveness

*For any* workflow run with unchanged dependencies, the build time should be significantly less than a clean build (at least 30% faster)

**Validates: Requirements 5.1, 5.2, 5.3**

### Property 9: Signature verifiability

*For any* published container image, running cosign verify should successfully validate the signature using the public Rekor log

**Validates: Requirements 10.2, 10.5**

### Property 10: Provenance traceability

*For any* container image, the build provenance should contain the exact commit SHA, workflow run ID, and timestamp that produced it

**Validates: Requirements 10.1, 10.3**

## Error Handling

### Build Failures

**Scenario:** Compilation errors, test failures, or lint issues

**Handling:**
- Fail workflow immediately
- Display clear error messages in logs
- Prevent progression to image building
- Report status to PR/commit
- Send notifications (if configured)

### Security Scan Failures

**Scenario:** Critical or high vulnerabilities detected

**Handling:**
- Fail workflow after scan completes
- Upload detailed vulnerability report
- Block image publication
- Create GitHub Security Advisory (if applicable)
- Provide remediation guidance in logs

### Registry Push Failures

**Scenario:** Authentication failures, network issues, quota exceeded

**Handling:**
- Retry with exponential backoff (3 attempts)
- Log detailed error information
- Continue with other registries if one fails
- Mark workflow as failed if all registries fail
- Preserve built images as artifacts for manual push

### Manifest Generation Failures

**Scenario:** Kustomize errors, invalid YAML, missing resources

**Handling:**
- Fail workflow before release creation
- Display kustomize error output
- Validate manifests with kubectl dry-run
- Prevent release publication
- Preserve partial artifacts for debugging

### Signing Failures

**Scenario:** Cosign errors, OIDC token issues, Rekor unavailable

**Handling:**
- Retry signing operation (2 attempts)
- Log detailed error with troubleshooting steps
- Optionally allow unsigned releases (configurable)
- Warn in release notes if unsigned
- Continue with release if signing is non-blocking

### Cache Corruption

**Scenario:** Invalid cache state, corrupted cache data

**Handling:**
- Detect cache validation failures
- Clear corrupted cache automatically
- Rebuild from scratch
- Log cache miss for monitoring
- Continue workflow without failing

## Testing Strategy

### Unit Testing

The CI/CD workflows themselves will be tested through:

1. **Workflow Syntax Validation**
   - Use `actionlint` to validate workflow YAML syntax
   - Check for deprecated actions
   - Verify required secrets and variables

2. **Dry-Run Testing**
   - Test workflows on feature branches before merging
   - Use workflow_dispatch for manual testing
   - Validate with test registries before production

3. **Integration Testing**
   - Test complete release flow on test tags (v0.0.0-test)
   - Verify image builds for all architectures
   - Confirm registry pushes succeed
   - Validate manifest generation

### Property-Based Testing

Property-based tests will validate the correctness properties defined above:

1. **Build Determinism Test** (Property 1)
   - Generate random commit scenarios
   - Build images multiple times
   - Compare dependency lists and binary checksums
   - Verify functional equivalence

2. **Tag Consistency Test** (Property 2)
   - Generate random semantic versions
   - Verify all expected tags are created
   - Confirm all tags point to same digest
   - Check tag format compliance

3. **Multi-Architecture Test** (Property 3)
   - For any release, query manifest
   - Verify all four platforms present
   - Confirm each platform image is pullable
   - Validate architecture metadata

4. **Security Gate Test** (Property 4)
   - Generate images with known vulnerabilities
   - Verify build fails for critical/high
   - Confirm build succeeds for medium/low
   - Check vulnerability report accuracy

5. **Registry Sync Test** (Property 5)
   - For any release, query all registries
   - Verify image exists in each
   - Compare digests across registries
   - Validate tag consistency

6. **Manifest Validity Test** (Property 6)
   - Generate random manifest variations
   - Run kubectl apply --dry-run
   - Verify no errors returned
   - Check resource completeness

7. **Artifact Completeness Test** (Property 7)
   - For any release, list artifacts
   - Verify all required files present
   - Check file sizes are non-zero
   - Validate checksums match

8. **Signature Verification Test** (Property 9)
   - For any published image
   - Run cosign verify
   - Confirm signature valid
   - Check Rekor entry exists

**Testing Framework:** These properties will be validated through:
- GitHub Actions workflow testing on test branches
- Automated validation scripts in the repository
- Manual verification checklists for releases
- Monitoring and alerting on production releases

**Note:** Traditional property-based testing frameworks (like gopter) are not directly applicable to CI/CD workflows. Instead, we'll use:
- Workflow testing with act (local GitHub Actions runner)
- Integration tests with test registries
- Validation scripts that check properties post-release
- Monitoring dashboards to track property compliance over time

### Monitoring and Validation

**Continuous Monitoring:**
- Track build times and cache hit rates
- Monitor registry push success rates
- Alert on security scan failures
- Dashboard for release metrics

**Post-Release Validation:**
- Automated smoke tests after release
- Image pull and deployment tests
- Signature verification checks
- Manifest application tests in test clusters

## Implementation Notes

### Required GitHub Secrets

The following secrets must be configured in the repository:

- `GITHUB_TOKEN` - Automatically provided by GitHub Actions
- `DOCKERHUB_USERNAME` - Docker Hub username (optional)
- `DOCKERHUB_TOKEN` - Docker Hub access token (optional)

### Required GitHub Permissions

The workflows require the following permissions:

```yaml
permissions:
  contents: write        # Create releases and upload artifacts
  packages: write        # Push to GHCR
  id-token: write       # Keyless signing with cosign
  security-events: write # Upload security scan results
```

### Performance Optimizations

1. **Layer Caching**
   - Use Docker buildx cache
   - Cache Go module downloads
   - Cache build tools

2. **Parallel Execution**
   - Build architectures in parallel
   - Run tests concurrently
   - Scan images simultaneously

3. **Conditional Execution**
   - Skip e2e tests on documentation changes
   - Only build images on main/release branches
   - Cache validation before download

### Rollback Strategy

If a release has critical issues:

1. Delete the GitHub release
2. Delete the git tag locally and remotely
3. Untag images in registries (or tag as deprecated)
4. Create a new patch release with fixes
5. Document the rollback in release notes

### Migration Path

To implement this CI/CD system:

1. Create release workflow alongside existing workflows
2. Test with pre-release tags (v0.0.0-alpha.1)
3. Validate all components work correctly
4. Create first official release (v1.0.0)
5. Monitor and iterate based on feedback
6. Add optional features (Docker Hub, signing) incrementally
