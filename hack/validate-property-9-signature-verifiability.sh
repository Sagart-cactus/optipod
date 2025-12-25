#!/bin/bash
# Property 9: Signature verifiability
# For any published container image, running cosign verify should successfully
# validate the signature using the public Rekor log

set -e

echo "=== Property 9: Signature Verifiability Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <image:tag>"
    echo "Example: $0 ghcr.io/yourusername/optipod:v1.0.0"
    exit 1
fi

IMAGE="$1"

echo "Image: $IMAGE"
echo ""

# Check if cosign is installed
if ! command -v cosign &> /dev/null; then
    echo "Error: cosign is not installed"
    echo "Install from: https://docs.sigstore.dev/cosign/installation/"
    exit 1
fi

# Verify signature
echo "Verifying signature with cosign..."
export COSIGN_EXPERIMENTAL=1

if cosign verify "$IMAGE" > /tmp/cosign-verify.json 2>&1; then
    echo "✓ Signature verification successful"
    echo ""

    # Show signature details
    echo "Signature details:"
    jq -r '.[0] | "  Issuer: \(.optional.Issuer // "N/A")\n  Subject: \(.optional.Subject // "N/A")\n  Timestamp: \(.optional.Bundle.Payload.integratedTime // "N/A")"' /tmp/cosign-verify.json 2>/dev/null || echo "  (Details not available)"
    echo ""

    # Check Rekor entry
    echo "Checking Rekor transparency log..."
    REKOR_UUID=$(jq -r '.[0].optional.Bundle.Payload.logID' /tmp/cosign-verify.json 2>/dev/null || echo "")

    if [ -n "$REKOR_UUID" ] && [ "$REKOR_UUID" != "null" ]; then
        echo "✓ Rekor log entry found: $REKOR_UUID"
    else
        echo "⚠ Rekor log entry not found in verification output"
    fi

    echo ""
    echo "✓ Property 9 validated: Signature is verifiable"

    rm -f /tmp/cosign-verify.json
    exit 0
else
    echo "✗ Signature verification failed"
    echo ""
    cat /tmp/cosign-verify.json
    echo ""
    echo "✗ Property 9 failed: Signature is not verifiable"

    rm -f /tmp/cosign-verify.json
    exit 1
fi
