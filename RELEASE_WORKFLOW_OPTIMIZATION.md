# Release Workflow Optimization Analysis

## Current Problem

The `publish-ghcr` job takes 20+ minutes because:
1. ARM64 images are built using QEMU emulation on amd64 runners
2. Go compilation under QEMU is 10-20x slower than native
3. The workflow rebuilds both amd64 and arm64 from scratch in the publish step

## Root Cause

```yaml
# Current approach in publish-ghcr job
- name: Build and push multi-platform image (for arm64 support)
  uses: docker/build-push-action@v6
  with:
    platforms: linux/amd64,linux/arm64  # ← arm64 uses QEMU emulation
```

When building for `linux/arm64` on an amd64 runner:
- Docker Buildx uses QEMU to emulate ARM architecture
- The entire Go build (70+ dependencies) runs under emulation
- This is extremely slow for CPU-intensive tasks like compilation

## Solution: Native ARM64 Runners

GitHub now provides **free ARM64 hosted runners** for public repositories:
- Labels: `ubuntu-24.04-arm` or `ubuntu-22.04-arm`
- Native ARM Cobalt 100 processors
- Up to 40% faster than previous generation
- No emulation overhead

Source: https://github.blog/changelog/2025-01-16-linux-arm64-hosted-runners-now-available-for-free-in-public-repositories-public-preview

## Proposed Architecture

### Current Flow (Slow)
```
build (amd64 only) → security-scan → sign → publish-ghcr (rebuild both amd64 + arm64 with QEMU)
                                                          ↑
                                                    20+ minutes here
```

### Optimized Flow (Fast)
```
build-amd64 (ubuntu-latest) ──┐
                              ├─→ security-scan → sign → publish-ghcr (combine manifests)
build-arm64 (ubuntu-24.04-arm)┘                                        ↑
                                                                   < 5 minutes
Both run in parallel natively
```

## Implementation Strategy

### Option 1: Matrix Build with Native Runners (Recommended)

**Pros:**
- Native builds on both architectures (fastest)
- Parallel execution (both build simultaneously)
- No QEMU overhead
- Clean separation of concerns

**Cons:**
- Security scanning needs to handle both images
- Slightly more complex workflow

### Option 2: Cross-Compilation on AMD64

**Pros:**
- Simpler workflow
- Single runner type

**Cons:**
- Still slower than native arm64 build
- CGO complications if needed in future
- Less testing of actual runtime environment

## Recommended Changes

### 1. Update `build` job to use matrix strategy

```yaml
build:
  name: Build Multi-Architecture Images
  runs-on: ${{ matrix.runner }}
  needs: [setup, lint, test, clusterless-e2e-test]
  strategy:
    matrix:
      include:
        - platform: linux/amd64
          runner: ubuntu-latest
          arch: amd64
        - platform: linux/arm64
          runner: ubuntu-24.04-arm
          arch: arm64
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3

    - name: Build image for ${{ matrix.platform }}
      uses: docker/build-push-action@v6
      with:
        context: .
        platforms: ${{ matrix.platform }}
        push: false
        load: true
        tags: |
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}
        build-args: |
          VERSION=${{ needs.setup.outputs.version }}
          COMMIT=${{ github.sha }}
          BUILD_DATE=${{ github.event.repository.updated_at }}
        cache-from: type=gha,scope=${{ matrix.arch }}
        cache-to: type=gha,mode=max,scope=${{ matrix.arch }}

    - name: Save image to tar
      run: |
        docker save ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }} -o /tmp/image-${{ matrix.arch }}.tar

    - name: Upload image artifact
      uses: actions/upload-artifact@v4
      with:
        name: container-image-${{ matrix.arch }}
        path: /tmp/image-${{ matrix.arch }}.tar
        retention-days: 1
```

### 2. Update `security-scan` to scan both architectures

```yaml
security-scan:
  name: Security Scanning
  runs-on: ubuntu-latest
  needs: [setup, build]
  strategy:
    matrix:
      arch: [amd64, arm64]
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Download ${{ matrix.arch }} image artifact
      uses: actions/download-artifact@v7
      with:
        name: container-image-${{ matrix.arch }}
        path: /tmp

    - name: Load Docker image
      run: docker load --input /tmp/image-${{ matrix.arch }}.tar

    - name: Install Trivy
      run: |
        sudo apt-get update
        sudo apt-get install wget apt-transport-https gnupg lsb-release
        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo gpg --dearmor -o /usr/share/keyrings/trivy.gpg
        echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main" | sudo tee -a /etc/apt/sources.list.d/trivy.list
        sudo apt-get update
        sudo apt-get install trivy

    - name: Run Trivy vulnerability scanner
      run: |
        trivy image \
          --format sarif \
          --output trivy-results-${{ matrix.arch }}.sarif \
          --severity CRITICAL,HIGH,MEDIUM,LOW \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Generate JSON report
      run: |
        trivy image \
          --format json \
          --output trivy-results-${{ matrix.arch }}.json \
          --severity CRITICAL,HIGH,MEDIUM,LOW \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Generate table report
      run: |
        trivy image \
          --format table \
          --severity CRITICAL,HIGH,MEDIUM,LOW \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Upload SARIF to GitHub Security
      uses: github/codeql-action/upload-sarif@v4
      if: always()
      with:
        sarif_file: trivy-results-${{ matrix.arch }}.sarif
        category: security-scan-${{ matrix.arch }}

    - name: Upload scan results as artifacts
      uses: actions/upload-artifact@v4
      if: always()
      with:
        name: security-scan-results-${{ matrix.arch }}
        path: |
          trivy-results-${{ matrix.arch }}.sarif
          trivy-results-${{ matrix.arch }}.json
        retention-days: 30

    - name: Check for critical/high vulnerabilities
      run: |
        CRITICAL=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="CRITICAL")] | length' trivy-results-${{ matrix.arch }}.json)
        HIGH=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="HIGH")] | length' trivy-results-${{ matrix.arch }}.json)

        echo "[${{ matrix.arch }}] Critical vulnerabilities: $CRITICAL"
        echo "[${{ matrix.arch }}] High vulnerabilities: $HIGH"

        if [ "$CRITICAL" -gt 0 ] || [ "$HIGH" -gt 0 ]; then
          echo "Error: Found $CRITICAL critical and $HIGH high severity vulnerabilities in ${{ matrix.arch }} image"
          echo "Release blocked due to security vulnerabilities"
          exit 1
        fi

        echo "No critical or high severity vulnerabilities found in ${{ matrix.arch }} image"
```

### 3. Update `sbom` to generate for both architectures

```yaml
sbom:
  name: Generate SBOM
  runs-on: ubuntu-latest
  needs: [setup, build]
  strategy:
    matrix:
      arch: [amd64, arm64]
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Download ${{ matrix.arch }} image artifact
      uses: actions/download-artifact@v7
      with:
        name: container-image-${{ matrix.arch }}
        path: /tmp

    - name: Load Docker image
      run: docker load --input /tmp/image-${{ matrix.arch }}.tar

    - name: Install Syft
      run: |
        curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin

    - name: Generate SBOM in SPDX JSON format
      run: |
        syft ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }} \
          -o spdx-json=sbom-${{ matrix.arch }}.json

    - name: Upload SBOM as artifact
      uses: actions/upload-artifact@v4
      with:
        name: sbom-${{ matrix.arch }}
        path: sbom-${{ matrix.arch }}.json
        retention-days: 90
```

### 4. Update `sign` to sign both architectures

```yaml
sign:
  name: Sign Images
  runs-on: ubuntu-latest
  needs: [setup, build, security-scan, sbom]
  strategy:
    matrix:
      arch: [amd64, arm64]
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Download ${{ matrix.arch }} image artifact
      uses: actions/download-artifact@v7
      with:
        name: container-image-${{ matrix.arch }}
        path: /tmp

    - name: Download ${{ matrix.arch }} SBOM
      uses: actions/download-artifact@v7
      with:
        name: sbom-${{ matrix.arch }}
        path: /tmp

    - name: Load Docker image
      run: docker load --input /tmp/image-${{ matrix.arch }}.tar

    - name: Install cosign
      uses: sigstore/cosign-installer@v3

    - name: Log in to GHCR
      uses: docker/login-action@v3
      with:
        registry: ${{ env.REGISTRY_GHCR }}
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    - name: Push ${{ matrix.arch }} image to GHCR (for signing)
      run: |
        docker push ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Sign container image with keyless signing
      env:
        COSIGN_EXPERIMENTAL: 1
      run: |
        cosign sign --yes \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Generate provenance attestation
      env:
        COSIGN_EXPERIMENTAL: 1
      run: |
        echo "{
          \"buildType\": \"https://github.com/Attestations/GitHubActionsWorkflow@v1\",
          \"builder\": {
            \"id\": \"https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}\"
          },
          \"invocation\": {
            \"configSource\": {
              \"uri\": \"git+https://github.com/${{ github.repository }}\",
              \"digest\": {
                \"sha1\": \"${{ github.sha }}\"
              },
              \"entryPoint\": \".github/workflows/release.yml\"
            }
          },
          \"metadata\": {
            \"buildInvocationId\": \"${{ github.run_id }}\",
            \"buildStartedOn\": \"${{ github.event.repository.updated_at }}\",
            \"completeness\": {
              \"parameters\": true,
              \"environment\": false,
              \"materials\": false
            },
            \"reproducible\": false,
            \"architecture\": \"${{ matrix.arch }}\"
          },
          \"materials\": [
            {
              \"uri\": \"git+https://github.com/${{ github.repository }}\",
              \"digest\": {
                \"sha1\": \"${{ github.sha }}\"
              }
            }
          ]
        }" > provenance-${{ matrix.arch }}.json

        cosign attest --yes \
          --predicate provenance-${{ matrix.arch }}.json \
          --type slsaprovenance \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Attach SBOM as attestation
      env:
        COSIGN_EXPERIMENTAL: 1
      run: |
        cosign attest --yes \
          --predicate /tmp/sbom-${{ matrix.arch }}.json \
          --type spdx \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }}

    - name: Verify signature
      env:
        COSIGN_EXPERIMENTAL: 1
      run: |
        cosign verify \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ needs.setup.outputs.version }}-${{ matrix.arch }} | jq .
```

### 4. Update `publish-ghcr` to combine manifests

```yaml
publish-ghcr:
  name: Publish to GitHub Container Registry
  runs-on: ubuntu-latest
  needs: [setup, build, security-scan, sign]
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    # Note: Images are already pushed by the sign job, no need to download/load

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3

    - name: Log in to GHCR
      uses: docker/login-action@v3
      with:
        registry: ${{ env.REGISTRY_GHCR }}
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    - name: Extract version components
      id: version
      run: |
        VERSION="${{ needs.setup.outputs.version }}"
        VERSION_NO_V="${VERSION#v}"
        MAJOR=$(echo $VERSION_NO_V | cut -d. -f1)
        MINOR=$(echo $VERSION_NO_V | cut -d. -f2)
        PATCH=$(echo $VERSION_NO_V | cut -d. -f3 | cut -d- -f1)
        echo "full=${VERSION}" >> $GITHUB_OUTPUT
        echo "major=${MAJOR}" >> $GITHUB_OUTPUT
        echo "minor=${MAJOR}.${MINOR}" >> $GITHUB_OUTPUT
        echo "patch=${MAJOR}.${MINOR}.${PATCH}" >> $GITHUB_OUTPUT

    - name: Create and push multi-arch manifest
      run: |
        # Create manifest for full version
        docker buildx imagetools create -t ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }} \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-amd64 \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-arm64

        # Create manifest for minor version
        docker buildx imagetools create -t ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.minor }} \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-amd64 \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-arm64

        # Create manifest for major version
        docker buildx imagetools create -t ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.major }} \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-amd64 \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-arm64

        # Create manifest for latest
        docker buildx imagetools create -t ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:latest \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-amd64 \
          ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}-arm64

    - name: Verify multi-arch manifest
      run: |
        docker buildx imagetools inspect ${{ env.REGISTRY_GHCR }}/${{ needs.setup.outputs.image_name_lower }}:${{ steps.version.outputs.full }}
```

## Expected Performance Improvement

### Current Timing
- Build (amd64 only): ~3 minutes
- Publish (rebuild both with QEMU): ~22 minutes
- **Total: ~25 minutes**

### Optimized Timing
- Build amd64 (native): ~3 minutes
- Build arm64 (native): ~3-4 minutes (parallel)
- Publish (combine manifests): ~2 minutes
- **Total: ~5-6 minutes** (80% reduction)

## Additional Benefits

1. **Better caching**: Separate cache scopes for each architecture
2. **Easier debugging**: Can test each architecture independently
3. **Future-proof**: Easy to add more architectures (e.g., arm/v7)
4. **Cost**: Free for public repositories

## Considerations

1. **ARM64 runners are in public preview**: May have queue times during peak hours
2. **Private repos**: ARM64 runners not available for private repos (would need paid runners)
3. **Image signing**: Both architectures are signed individually, then combined into multi-arch manifest
4. **Security scanning**: Both architectures are scanned - important because base images may have different vulnerabilities per architecture
5. **SBOM**: Generated for both architectures - dependencies may differ slightly between platforms

## Next Steps

1. Implement matrix build strategy
2. Test with a non-release workflow first
3. Update security scanning to handle both images (or just amd64)
4. Extend signing to cover both architectures
5. Monitor build times and adjust as needed
