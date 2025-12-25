#!/bin/bash
# Property 6: Manifest validity
# For any generated deployment manifest, running kubectl apply --dry-run
# should succeed without errors

set -e

echo "=== Property 6: Manifest Validity Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <manifest-file-or-url>"
    echo "Example: $0 install.yaml"
    echo "Example: $0 https://github.com/user/optipod/releases/download/v1.0.0/install.yaml"
    exit 1
fi

MANIFEST="$1"

echo "Manifest: $MANIFEST"
echo ""

# Download if URL
if [[ "$MANIFEST" =~ ^https?:// ]]; then
    echo "Downloading manifest..."
    curl -sL "$MANIFEST" -o /tmp/manifest.yaml
    MANIFEST="/tmp/manifest.yaml"
fi

# Check if file exists
if [ ! -f "$MANIFEST" ]; then
    echo "✗ Manifest file not found: $MANIFEST"
    exit 1
fi

echo "Validating manifest with kubectl..."
echo ""

# Validate with kubectl dry-run
if kubectl apply --dry-run=client -f "$MANIFEST" 2>&1 | tee /tmp/kubectl-output.txt; then
    echo ""
    echo "✓ Property 6 validated: Manifest is valid"

    # Show resource summary
    echo ""
    echo "Resources in manifest:"
    kubectl apply --dry-run=client -f "$MANIFEST" -o json | jq -r '.items[] | "\(.kind)/\(.metadata.name)"' 2>/dev/null || \
        grep -E "^(apiVersion|kind|  name)" "$MANIFEST" | paste - - - | awk '{print $4"/"$6}'

    rm -f /tmp/kubectl-output.txt /tmp/manifest.yaml
    exit 0
else
    echo ""
    echo "✗ Property 6 failed: Manifest validation errors"
    cat /tmp/kubectl-output.txt
    rm -f /tmp/kubectl-output.txt /tmp/manifest.yaml
    exit 1
fi
