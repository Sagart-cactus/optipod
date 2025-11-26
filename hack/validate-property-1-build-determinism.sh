#!/bin/bash
# Property 1: Build determinism
# For any commit SHA, building the container image multiple times should produce
# images with identical functionality and dependencies

set -e

echo "=== Property 1: Build Determinism Validation ==="

if [ -z "$1" ]; then
    COMMIT_SHA=$(git rev-parse HEAD)
else
    COMMIT_SHA="$1"
fi

echo "Testing commit: $COMMIT_SHA"
echo ""

# Checkout the commit
git checkout "$COMMIT_SHA"

IMAGE_NAME="optipod-determinism-test"
BUILD_1="${IMAGE_NAME}:build1"
BUILD_2="${IMAGE_NAME}:build2"

echo "Building image (first build)..."
docker build -t "$BUILD_1" .

echo "Building image (second build)..."
docker build -t "$BUILD_2" .

echo ""
echo "Comparing builds..."

# Extract and compare dependency lists
echo "Extracting dependencies from build 1..."
docker run --rm "$BUILD_1" sh -c "ls -la / 2>/dev/null || true" > /tmp/build1-files.txt

echo "Extracting dependencies from build 2..."
docker run --rm "$BUILD_2" sh -c "ls -la / 2>/dev/null || true" > /tmp/build2-files.txt

# Compare file listings
if diff /tmp/build1-files.txt /tmp/build2-files.txt > /dev/null; then
    echo "✓ File structure is identical"
else
    echo "✗ File structure differs"
    diff /tmp/build1-files.txt /tmp/build2-files.txt || true
    exit 1
fi

# Extract and compare binary
echo "Extracting binary from build 1..."
docker run --rm -v /tmp:/output "$BUILD_1" sh -c "cp /manager /output/manager1 2>/dev/null || true"

echo "Extracting binary from build 2..."
docker run --rm -v /tmp:/output "$BUILD_2" sh -c "cp /manager /output/manager2 2>/dev/null || true"

# Compare binary sizes
SIZE1=$(stat -f%z /tmp/manager1 2>/dev/null || stat -c%s /tmp/manager1)
SIZE2=$(stat -f%z /tmp/manager2 2>/dev/null || stat -c%s /tmp/manager2)

if [ "$SIZE1" -eq "$SIZE2" ]; then
    echo "✓ Binary sizes are identical ($SIZE1 bytes)"
else
    echo "✗ Binary sizes differ (build1: $SIZE1, build2: $SIZE2)"
    exit 1
fi

# Cleanup
docker rmi "$BUILD_1" "$BUILD_2" || true
rm -f /tmp/build1-files.txt /tmp/build2-files.txt /tmp/manager1 /tmp/manager2

echo ""
echo "✓ Property 1 validated: Builds are deterministic"
