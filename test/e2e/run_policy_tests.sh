#!/bin/bash

# Script to run Policy Mode E2E tests with proper cluster management

set -e

CLUSTER_NAME="optipod-e2e-test"
KEEP_CLUSTER=${KEEP_CLUSTER:-false}

echo "🧹 Cleaning up any existing cluster..."
kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true

echo "🚀 Running Policy Mode E2E tests..."
echo "Cluster will be $([ "$KEEP_CLUSTER" = "true" ] && echo "kept" || echo "deleted") after tests"

# Set environment variables
export KIND_CLUSTER_NAME="$CLUSTER_NAME"
export KEEP_CLUSTER="$KEEP_CLUSTER"

# Run the tests with focus on Policy Modes
go test -tags=e2e ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Policy Modes" -timeout=45m

echo "✅ Policy Mode E2E tests completed!"

if [ "$KEEP_CLUSTER" != "true" ]; then
    echo "🧹 Cleaning up cluster..."
    kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
    echo "✅ Cleanup completed!"
else
    echo "🔧 Cluster '$CLUSTER_NAME' kept for debugging"
    echo "   To delete manually: kind delete cluster --name $CLUSTER_NAME"
fi