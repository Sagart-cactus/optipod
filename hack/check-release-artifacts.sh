#!/bin/bash
# Script to check release artifact completeness

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.0.0"
    exit 1
fi

VERSION="$1"
REPO="${GITHUB_REPOSITORY:-yourusername/optipod}"

echo "=== Checking Release Artifacts ==="
echo "Version: $VERSION"
echo "Repository: $REPO"

# Required artifacts
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

echo ""
echo "Checking for required artifacts..."

MISSING=0
for artifact in "${REQUIRED_ARTIFACTS[@]}"; do
    URL="https://github.com/$REPO/releases/download/$VERSION/$artifact"
    if curl --output /dev/null --silent --head --fail "$URL"; then
        echo "✓ $artifact"
    else
        echo "✗ $artifact (missing)"
        MISSING=$((MISSING + 1))
    fi
done

echo ""
if [ $MISSING -eq 0 ]; then
    echo "✓ All required artifacts are present"
    exit 0
else
    echo "✗ $MISSING artifact(s) missing"
    exit 1
fi
