#!/bin/bash
# Property 2: Tag consistency
# For any semantic version tag vX.Y.Z, the CI/CD System should create exactly
# four image tags: vX.Y.Z, vX.Y, vX, and latest, all pointing to the same image digest

set -e

echo "=== Property 2: Tag Consistency Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <version> [registry]"
    echo "Example: $0 v1.2.3 ghcr.io/yourusername/optipod"
    exit 1
fi

VERSION="$1"
REGISTRY="${2:-ghcr.io/yourusername/optipod}"

echo "Version: $VERSION"
echo "Registry: $REGISTRY"
echo ""

# Parse version
if ! echo "$VERSION" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Error: Invalid version format. Expected vX.Y.Z"
    exit 1
fi

VERSION_NO_V="${VERSION#v}"
MAJOR=$(echo "$VERSION_NO_V" | cut -d. -f1)
MINOR=$(echo "$VERSION_NO_V" | cut -d. -f2)
# PATCH variable is defined but not used in this script
# PATCH=$(echo "$VERSION_NO_V" | cut -d. -f3)

EXPECTED_TAGS=(
    "$VERSION"
    "v${MAJOR}.${MINOR}"
    "v${MAJOR}"
    "latest"
)

echo "Expected tags:"
for tag in "${EXPECTED_TAGS[@]}"; do
    echo "  - $tag"
done
echo ""

# Get digest for each tag
declare -A DIGESTS

for tag in "${EXPECTED_TAGS[@]}"; do
    IMAGE="${REGISTRY}:${tag}"
    echo "Checking $IMAGE..."

    if ! DIGEST=$(docker manifest inspect "$IMAGE" 2>/dev/null | jq -r '.config.digest'); then
        echo "✗ Tag $tag not found"
        exit 1
    fi

    DIGESTS[$tag]="$DIGEST"
    echo "  Digest: $DIGEST"
done

echo ""
echo "Comparing digests..."

# Compare all digests
REFERENCE_DIGEST="${DIGESTS[$VERSION]}"
ALL_MATCH=true

for tag in "${EXPECTED_TAGS[@]}"; do
    if [ "${DIGESTS[$tag]}" != "$REFERENCE_DIGEST" ]; then
        echo "✗ Tag $tag has different digest: ${DIGESTS[$tag]}"
        ALL_MATCH=false
    else
        echo "✓ Tag $tag matches"
    fi
done

echo ""
if [ "$ALL_MATCH" = true ]; then
    echo "✓ Property 2 validated: All tags point to the same digest"
    exit 0
else
    echo "✗ Property 2 failed: Tags have different digests"
    exit 1
fi
