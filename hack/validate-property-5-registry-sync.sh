#!/bin/bash
# Property 5: Registry synchronization
# For any released version, the image should be available in all configured
# registries with identical tags and digests

set -e

echo "=== Property 5: Registry Synchronization Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <version> [registry1] [registry2] ..."
    echo "Example: $0 v1.0.0 ghcr.io/user/optipod docker.io/user/optipod"
    exit 1
fi

VERSION="$1"
shift
REGISTRIES=("$@")

if [ ${#REGISTRIES[@]} -eq 0 ]; then
    echo "Error: At least one registry must be specified"
    exit 1
fi

echo "Version: $VERSION"
echo "Registries:"
for registry in "${REGISTRIES[@]}"; do
    echo "  - $registry"
done
echo ""

# Get digest from first registry as reference
REFERENCE_REGISTRY="${REGISTRIES[0]}"
REFERENCE_IMAGE="${REFERENCE_REGISTRY}:${VERSION}"

echo "Getting reference digest from $REFERENCE_IMAGE..."
REFERENCE_DIGEST=$(docker manifest inspect "$REFERENCE_IMAGE" | jq -r '.config.digest')
echo "Reference digest: $REFERENCE_DIGEST"
echo ""

# Check each registry
ALL_MATCH=true
for registry in "${REGISTRIES[@]}"; do
    IMAGE="${registry}:${VERSION}"
    echo "Checking $IMAGE..."
    
    if ! DIGEST=$(docker manifest inspect "$IMAGE" 2>/dev/null | jq -r '.config.digest'); then
        echo "✗ Image not found in registry"
        ALL_MATCH=false
        continue
    fi
    
    if [ "$DIGEST" = "$REFERENCE_DIGEST" ]; then
        echo "✓ Digest matches: $DIGEST"
    else
        echo "✗ Digest mismatch: $DIGEST (expected: $REFERENCE_DIGEST)"
        ALL_MATCH=false
    fi
done

echo ""
if [ "$ALL_MATCH" = true ]; then
    echo "✓ Property 5 validated: All registries have identical images"
    exit 0
else
    echo "✗ Property 5 failed: Registry synchronization mismatch"
    exit 1
fi
