#!/bin/bash
# Property 3: Multi-architecture completeness
# For any release build, all four target platforms (linux/amd64, linux/arm64,
# linux/s390x, linux/ppc64le) should be present in the multi-platform manifest

set -e

echo "=== Property 3: Multi-Architecture Completeness Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <image:tag>"
    echo "Example: $0 ghcr.io/yourusername/optipod:v1.0.0"
    exit 1
fi

IMAGE="$1"

echo "Image: $IMAGE"
echo ""

REQUIRED_PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/s390x"
    "linux/ppc64le"
)

echo "Required platforms:"
for platform in "${REQUIRED_PLATFORMS[@]}"; do
    echo "  - $platform"
done
echo ""

# Get manifest
echo "Fetching manifest..."
MANIFEST=$(docker manifest inspect "$IMAGE")

# Extract platforms
echo "Found platforms:"
FOUND_PLATFORMS=$(echo "$MANIFEST" | jq -r '.manifests[] | "\(.platform.os)/\(.platform.architecture)"')
echo "$FOUND_PLATFORMS"
echo ""

# Check each required platform
MISSING=0
for platform in "${REQUIRED_PLATFORMS[@]}"; do
    if echo "$FOUND_PLATFORMS" | grep -q "^${platform}$"; then
        echo "✓ $platform present"
    else
        echo "✗ $platform missing"
        MISSING=$((MISSING + 1))
    fi
done

echo ""
if [ $MISSING -eq 0 ]; then
    echo "✓ Property 3 validated: All required platforms are present"
    
    # Additional check: verify each platform image is pullable
    echo ""
    echo "Verifying platform images are pullable..."
    for platform in "${REQUIRED_PLATFORMS[@]}"; do
        PLATFORM_IMAGE="${IMAGE}@$(echo "$MANIFEST" | jq -r ".manifests[] | select(.platform.os==\"$(echo $platform | cut -d/ -f1)\" and .platform.architecture==\"$(echo $platform | cut -d/ -f2)\") | .digest")"
        if docker manifest inspect "$PLATFORM_IMAGE" > /dev/null 2>&1; then
            echo "✓ $platform image is pullable"
        else
            echo "✗ $platform image is not pullable"
            exit 1
        fi
    done
    
    echo ""
    echo "✓ All platform images are pullable"
    exit 0
else
    echo "✗ Property 3 failed: $MISSING platform(s) missing"
    exit 1
fi
