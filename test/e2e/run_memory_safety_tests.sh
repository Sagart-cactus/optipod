#!/bin/bash

# Script to run memory safety integration tests with real workloads
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== Memory Safety Integration Tests ==="
echo "Project root: $PROJECT_ROOT"

# Change to project root
cd "$PROJECT_ROOT"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if cluster is ready
check_cluster_ready() {
    log_info "Checking cluster readiness..."

    if ! kubectl cluster-info &>/dev/null; then
        log_error "Kubernetes cluster is not accessible"
        return 1
    fi

    # Check if OptipPod CRDs exist
    if ! kubectl get crd optimizationpolicies.optipod.optipod.io &>/dev/null; then
        log_error "OptipPod CRDs are not installed"
        return 1
    fi

    # Check if OptipPod controller is running
    if ! kubectl get deployment optipod-controller-manager -n optipod-system &>/dev/null; then
        log_error "OptipPod controller is not deployed"
        return 1
    fi

    log_info "Cluster is ready for testing"
    return 0
}

# Function to deploy test workloads
deploy_test_workloads() {
    log_info "Deploying test workloads..."

    # Clean up any existing test resources
    kubectl delete -f test-memory-safety-workloads.yaml --ignore-not-found=true
    kubectl delete -f test-memory-safety-policies.yaml --ignore-not-found=true

    # Wait for cleanup
    sleep 10

    # Deploy test workloads
    kubectl apply -f test-memory-safety-workloads.yaml

    # Wait for workloads to be ready
    log_info "Waiting for workloads to be ready..."
    kubectl wait --for=condition=available --timeout=120s deployment/memory-safety-underutilized -n default
    kubectl wait --for=condition=available --timeout=120s deployment/memory-safety-overutilized -n default
    kubectl wait --for=condition=available --timeout=120s deployment/memory-safety-moderate -n default

    log_info "Test workloads deployed successfully"
}

# Function to test underutilized workload optimization
test_underutilized_workload() {
    log_info "Testing underutilized workload optimization..."

    # Get initial resources
    log_info "Getting initial resource configuration..."
    INITIAL_MEMORY_REQUEST=$(kubectl get deployment memory-safety-underutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')
    INITIAL_MEMORY_LIMIT=$(kubectl get deployment memory-safety-underutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}')

    log_info "Initial memory request: $INITIAL_MEMORY_REQUEST"
    log_info "Initial memory limit: $INITIAL_MEMORY_LIMIT"

    # Apply optimization policy
    log_info "Applying optimization policy..."
    kubectl apply -f - <<EOF
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-underutilized-test
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
    labels:
      app: memory-safety-underutilized
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
EOF

    # Wait for policy to be ready
    log_info "Waiting for policy to be ready..."
    timeout=120
    while [ $timeout -gt 0 ]; do
        if kubectl get optimizationpolicy memory-safety-underutilized-test -n default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' | grep -q "True"; then
            log_info "Policy is ready"
            break
        fi
        sleep 5
        timeout=$((timeout - 5))
    done

    if [ $timeout -le 0 ]; then
        log_error "Policy did not become ready within timeout"
        return 1
    fi

    # Wait for optimization to occur
    log_info "Waiting for optimization to occur (3 minutes)..."
    sleep 180

    # Check final resources
    FINAL_MEMORY_REQUEST=$(kubectl get deployment memory-safety-underutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')
    FINAL_MEMORY_LIMIT=$(kubectl get deployment memory-safety-underutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}')

    log_info "Final memory request: $FINAL_MEMORY_REQUEST"
    log_info "Final memory limit: $FINAL_MEMORY_LIMIT"

    # Verify optimization occurred
    if [ "$INITIAL_MEMORY_REQUEST" != "$FINAL_MEMORY_REQUEST" ]; then
        log_info "✓ Memory request was optimized"
    else
        log_warn "Memory request was not changed"
    fi

    # Check for OptipPod annotations
    if kubectl get deployment memory-safety-underutilized -n default -o jsonpath='{.metadata.annotations.optipod\.io/managed}' | grep -q "true"; then
        log_info "✓ OptipPod annotation found"
    else
        log_warn "OptipPod annotation not found"
    fi

    # Clean up
    kubectl delete optimizationpolicy memory-safety-underutilized-test -n default --ignore-not-found=true
}

# Function to test overutilized workload optimization
test_overutilized_workload() {
    log_info "Testing overutilized workload optimization..."

    # Get initial resources
    log_info "Getting initial resource configuration..."
    INITIAL_MEMORY_REQUEST=$(kubectl get deployment memory-safety-overutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')
    INITIAL_MEMORY_LIMIT=$(kubectl get deployment memory-safety-overutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}')

    log_info "Initial memory request: $INITIAL_MEMORY_REQUEST"
    log_info "Initial memory limit: $INITIAL_MEMORY_LIMIT"

    # Apply optimization policy with custom limits
    log_info "Applying optimization policy with custom limits..."
    kubectl apply -f - <<EOF
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: memory-safety-overutilized-test
  namespace: default
spec:
  mode: Auto
  reconciliationInterval: 30s
  selector:
    namespaces:
      allow:
        - default
    workloadTypes:
      include:
        - Deployment
    labels:
      app: memory-safety-overutilized
  metricsConfig:
    provider: metrics-server
    rollingWindow: 2m
    percentile: P90
    safetyFactor: 1.1
  resourceBounds:
    cpu:
      min: "10m"
      max: "2000m"
    memory:
      min: "32Mi"
      max: "8Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
    limitConfig:
      cpuLimitMultiplier: 2.0
      memoryLimitMultiplier: 1.5
EOF

    # Wait for policy to be ready
    log_info "Waiting for policy to be ready..."
    timeout=120
    while [ $timeout -gt 0 ]; do
        if kubectl get optimizationpolicy memory-safety-overutilized-test -n default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' | grep -q "True"; then
            log_info "Policy is ready"
            break
        fi
        sleep 5
        timeout=$((timeout - 5))
    done

    if [ $timeout -le 0 ]; then
        log_error "Policy did not become ready within timeout"
        return 1
    fi

    # Wait for optimization to occur
    log_info "Waiting for optimization to occur (3 minutes)..."
    sleep 180

    # Check final resources
    FINAL_MEMORY_REQUEST=$(kubectl get deployment memory-safety-overutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')
    FINAL_MEMORY_LIMIT=$(kubectl get deployment memory-safety-overutilized -n default -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}')

    log_info "Final memory request: $FINAL_MEMORY_REQUEST"
    log_info "Final memory limit: $FINAL_MEMORY_LIMIT"

    # Verify optimization occurred
    if [ "$INITIAL_MEMORY_REQUEST" != "$FINAL_MEMORY_REQUEST" ]; then
        log_info "✓ Memory request was optimized"
    else
        log_warn "Memory request was not changed"
    fi

    # Check for OptipPod annotations
    if kubectl get deployment memory-safety-overutilized -n default -o jsonpath='{.metadata.annotations.optipod\.io/managed}' | grep -q "true"; then
        log_info "✓ OptipPod annotation found"
    else
        log_warn "OptipPod annotation not found"
    fi

    # Clean up
    kubectl delete optimizationpolicy memory-safety-overutilized-test -n default --ignore-not-found=true
}

# Function to monitor for unexpected behavior
monitor_unexpected_behavior() {
    log_info "Monitoring for unexpected behavior..."

    # Check controller logs for errors
    log_info "Checking controller logs for errors..."
    if kubectl logs -n optipod-system deployment/optipod-controller-manager --since=10m | grep -i error; then
        log_warn "Errors found in controller logs"
    else
        log_info "✓ No errors found in controller logs"
    fi

    # Check for safety check function calls (should not exist)
    log_info "Checking for safety check function calls..."
    if kubectl logs -n optipod-system deployment/optipod-controller-manager --since=10m | grep -i "isUnsafeMemoryDecrease"; then
        log_error "Safety check function was called - this should not happen"
        return 1
    else
        log_info "✓ No safety check function calls found"
    fi

    # Check for optimization decisions
    log_info "Checking for optimization decisions..."
    if kubectl logs -n optipod-system deployment/optipod-controller-manager --since=10m | grep -i "optimization"; then
        log_info "✓ Optimization decisions found in logs"
    else
        log_warn "No optimization decisions found in logs"
    fi
}

# Function to cleanup test resources
cleanup_test_resources() {
    log_info "Cleaning up test resources..."

    kubectl delete -f test-memory-safety-workloads.yaml --ignore-not-found=true
    kubectl delete -f test-memory-safety-policies.yaml --ignore-not-found=true
    kubectl delete optimizationpolicy --all -n default --ignore-not-found=true

    log_info "Cleanup completed"
}

# Main execution
main() {
    log_info "Starting memory safety integration tests..."

    # Check if cluster is ready
    if ! check_cluster_ready; then
        log_error "Cluster is not ready for testing"
        exit 1
    fi

    # Deploy test workloads
    if ! deploy_test_workloads; then
        log_error "Failed to deploy test workloads"
        exit 1
    fi

    # Wait for metrics to be available
    log_info "Waiting for metrics to be available (2 minutes)..."
    sleep 120

    # Test underutilized workload
    if ! test_underutilized_workload; then
        log_error "Underutilized workload test failed"
        cleanup_test_resources
        exit 1
    fi

    # Test overutilized workload
    if ! test_overutilized_workload; then
        log_error "Overutilized workload test failed"
        cleanup_test_resources
        exit 1
    fi

    # Monitor for unexpected behavior
    if ! monitor_unexpected_behavior; then
        log_error "Unexpected behavior detected"
        cleanup_test_resources
        exit 1
    fi

    # Cleanup
    cleanup_test_resources

    log_info "✓ All memory safety integration tests passed successfully!"
}

# Run main function
main "$@"
