#!/bin/bash
# Property 8: Cache effectiveness
# For any workflow run with unchanged dependencies, the build time should be
# significantly less than a clean build (at least 30% faster)

set -e

echo "=== Property 8: Cache Effectiveness Validation ==="

IMAGE_NAME="optipod-cache-test"

echo "This test will:"
echo "1. Build the image from scratch (no cache)"
echo "2. Build the image again with cache"
echo "3. Compare build times"
echo ""

# Clean build (no cache)
echo "=== Clean Build (no cache) ==="
docker builder prune -af > /dev/null 2>&1

START_TIME=$(date +%s)
docker build --no-cache -t "$IMAGE_NAME:clean" . > /tmp/build-clean.log 2>&1
END_TIME=$(date +%s)
CLEAN_BUILD_TIME=$((END_TIME - START_TIME))

echo "Clean build time: ${CLEAN_BUILD_TIME}s"
echo ""

# Cached build
echo "=== Cached Build ==="
START_TIME=$(date +%s)
docker build -t "$IMAGE_NAME:cached" . > /tmp/build-cached.log 2>&1
END_TIME=$(date +%s)
CACHED_BUILD_TIME=$((END_TIME - START_TIME))

echo "Cached build time: ${CACHED_BUILD_TIME}s"
echo ""

# Calculate improvement
if [ $CLEAN_BUILD_TIME -eq 0 ]; then
    echo "Error: Clean build time is 0"
    exit 1
fi

IMPROVEMENT=$(awk "BEGIN {printf \"%.1f\", (($CLEAN_BUILD_TIME - $CACHED_BUILD_TIME) / $CLEAN_BUILD_TIME) * 100}")
SPEEDUP=$(awk "BEGIN {printf \"%.2f\", $CLEAN_BUILD_TIME / $CACHED_BUILD_TIME}")

echo "Results:"
echo "  Clean build: ${CLEAN_BUILD_TIME}s"
echo "  Cached build: ${CACHED_BUILD_TIME}s"
echo "  Improvement: ${IMPROVEMENT}%"
echo "  Speedup: ${SPEEDUP}x"
echo ""

# Check cache layers
echo "Cache analysis:"
grep -c "CACHED" /tmp/build-cached.log || echo "0 cached layers"
echo ""

# Validate 30% improvement threshold
THRESHOLD=30
if (( $(echo "$IMPROVEMENT >= $THRESHOLD" | bc -l) )); then
    echo "✓ Property 8 validated: Cache provides ${IMPROVEMENT}% improvement (>= ${THRESHOLD}%)"
    
    # Cleanup
    docker rmi "$IMAGE_NAME:clean" "$IMAGE_NAME:cached" > /dev/null 2>&1 || true
    rm -f /tmp/build-clean.log /tmp/build-cached.log
    exit 0
else
    echo "✗ Property 8 failed: Cache provides only ${IMPROVEMENT}% improvement (< ${THRESHOLD}%)"
    echo ""
    echo "Note: This may be expected for small projects or fast builds."
    echo "Cache effectiveness is more noticeable in larger projects with many dependencies."
    
    # Cleanup
    docker rmi "$IMAGE_NAME:clean" "$IMAGE_NAME:cached" > /dev/null 2>&1 || true
    rm -f /tmp/build-clean.log /tmp/build-cached.log
    exit 1
fi
