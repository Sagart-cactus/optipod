#!/bin/bash

# Simple Policy Mode test script that sets up a minimal cluster

set -e

CLUSTER_NAME="optipod-policy-test"

echo "🧹 Cleaning up any existing cluster..."
kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true

echo "🚀 Creating minimal Kind cluster..."
cat <<EOF | kind create cluster --name "$CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
EOF

echo "📋 Setting kubectl context..."
kubectl config use-context "kind-$CLUSTER_NAME"

echo "⏳ Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=300s

echo "📦 Installing CRDs directly..."
kubectl apply -f config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml --validate=false

echo "🏷️ Labeling default namespace..."
kubectl label namespace default environment=development --overwrite

echo "📦 Creating optipod-system namespace..."
kubectl create namespace optipod-system

echo "🧪 Creating test policy..."
kubectl apply -f hack/test-policy-auto-mode.yaml

echo "🚀 Creating test workload..."
kubectl apply -f hack/test-workload-auto-mode.yaml

echo "⏳ Waiting for workload to be ready..."
kubectl wait --for=condition=available --timeout=300s deployment/policy-mode-auto-test -n default

echo "✅ Basic Policy Mode test setup completed!"

echo "📊 Checking resources..."
echo "Policies:"
kubectl get optimizationpolicy -n optipod-system
echo ""
echo "Workloads:"
kubectl get deployment -n default
echo ""
echo "Workload details:"
kubectl describe deployment policy-mode-auto-test -n default

echo "🧹 Cleaning up..."
if [ "$KEEP_CLUSTER" != "true" ]; then
    kind delete cluster --name "$CLUSTER_NAME"
    echo "✅ Cleanup completed!"
else
    echo "🔧 Cluster '$CLUSTER_NAME' kept for debugging"
fi
