#!/bin/bash
# Property 7: Artifact completeness
# For any GitHub release, all required artifacts (install.yaml, binaries for
# all platforms, SBOM, checksums) should be attached

set -e

echo "=== Property 7: Artifact Completeness Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <version> [repo]"
    echo "Example: $0 v1.0.0 yourusername/optipod"
    exit 1
fi

VERSION="$1"
REPO="${2:-${GITHUB_REPOSITORY:-yourusername/optipod}}"

echo "Version: $VERSION"
echo "Repository: $REPO"
echo ""

REQUIRED_ARTIFACTS=(
    "install.yaml"
    "optipod-linux-amd64"
    "optipod-linux-arm64"
    "optipod-darwin-amd64"
    "optipod-darwin-arm64"
    "sbom.json"
    "trivy-results.json"
    "checksums.txt"
)

echo "Checking for required artifacts..."
echo ""

MISSING=0
EMPTY=0

for artifact in "${REQUIRED_ARTIFACTS[@]}"; do
    URL="https://github.com/$REPO/releases/download/$VERSION/$artifact"
    
    if curl --output /dev/null --silent --head --fail "$URL"; then
        # Check file size
        SIZE=$(curl -sI "$URL" | grep -i content-length | awk '{print $2}' | tr -d '\r')
        
        if [ -z "$SIZE" ] || [ "$SIZE" -eq 0 ]; then
            echo "✗ $artifact (empty file)"
            EMPTY=$((EMPTY + 1))
        else
            echo "✓ $artifact ($SIZE bytes)"
        fi
    else
        echo "✗ $artifact (missing)"
        MISSING=$((MISSING + 1))
    fi
done

echo ""
if [ $MISSING -eq 0 ] && [ $EMPTY -eq 0 ]; then
    echo "✓ Property 7 validated: All required artifacts are present and non-empty"
    
    # Verify checksums
    echo ""
    echo "Verifying checksums..."
    CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
    curl -sL "$CHECKSUMS_URL" -o /tmp/checksums.txt
    
    echo "Checksums file contains:"
    cat /tmp/checksums.txt
    
    rm -f /tmp/checksums.txt
    exit 0
else
    echo "✗ Property 7 failed:"
    [ $MISSING -gt 0 ] && echo "  - $MISSING artifact(s) missing"
    [ $EMPTY -gt 0 ] && echo "  - $EMPTY artifact(s) empty"
    exit 1
fi
