#!/bin/bash
# Script to verify container image signatures using cosign

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <image:tag>"
    echo "Example: $0 ghcr.io/yourusername/optipod:v1.0.0"
    exit 1
fi

IMAGE="$1"

echo "=== Verifying Image Signature ==="
echo "Image: $IMAGE"

# Check if cosign is installed
if ! command -v cosign &> /dev/null; then
    echo "Error: cosign is not installed"
    echo "Install from: https://docs.sigstore.dev/cosign/installation/"
    exit 1
fi

# Verify signature
echo "Verifying signature..."
export COSIGN_EXPERIMENTAL=1
cosign verify "$IMAGE"

echo ""
echo "=== Verifying Attestations ==="

# Verify SLSA provenance
echo "Checking SLSA provenance..."
cosign verify-attestation --type slsaprovenance "$IMAGE" || echo "No SLSA provenance found"

# Verify SBOM
echo "Checking SBOM attestation..."
cosign verify-attestation --type spdx "$IMAGE" || echo "No SBOM attestation found"

echo ""
echo "✓ Signature verification complete"
