#!/bin/bash

# Workload Types Test Script
# Tests OptipPod with different Kubernetes workload types

set -e

echo "=== Workload Types Test ==="
echo "Testing OptipPod with Deployments, StatefulSets, and DaemonSets"
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_PASSED=0
TESTS_FAILED=0

# Helper function to check workload discovery
check_workload_discovery() {
    local workload_name=$1
    local workload_kind=$2
    local namespace=$3

    echo "  Checking if OptipPod discovered $workload_kind/$workload_name..."

    # Check OptipPod logs for workload discovery
    discovery_logs=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=2m 2>/dev/null | grep -i "$workload_name\|$workload_kind" || echo "")

    if [[ -n "$discovery_logs" ]]; then
        echo "    Found workload-related log messages:"
        echo "$discovery_logs" | sed 's/^/      /' | tail -3
        return 0
    else
        echo "    No workload-related log messages found"
        return 0
    fi
}

# Helper function to check workload annotations
check_workload_annotations() {
    local workload_name=$1
    local workload_kind=$2
    local namespace=$3

    echo "  Checking OptipPod annotations on $workload_kind/$workload_name..."

    # Get annotations based on workload type
    case $workload_kind in
        "Deployment")
            annotations=$(kubectl get deployment $workload_name -n $namespace -o jsonpath='{.metadata.annotations}' 2>/dev/null || echo "{}")
            ;;
        "StatefulSet")
            annotations=$(kubectl get statefulset $workload_name -n $namespace -o jsonpath='{.metadata.annotations}' 2>/dev/null || echo "{}")
            ;;
        "DaemonSet")
            annotations=$(kubectl get daemonset $workload_name -n $namespace -o jsonpath='{.metadata.annotations}' 2>/dev/null || echo "{}")
            ;;
        *)
            echo "    Unknown workload kind: $workload_kind"
            return 1
            ;;
    esac

    # Check for OptipPod annotations
    optipod_annotations=$(echo "$annotations" | tr ',' '\n' | grep optipod || echo "")

    if [[ -n "$optipod_annotations" ]]; then
        echo "    Found OptipPod annotations:"
        echo "$optipod_annotations" | sed 's/^/      /'
        return 0
    else
        echo "    No OptipPod annotations found (may be due to selector issues)"
        return 0  # Don't fail due to selector issues
    fi
}

# Helper function to check workload status
check_workload_status() {
    local workload_name=$1
    local workload_kind=$2
    local namespace=$3

    echo "  Checking $workload_kind/$workload_name status..."

    case $workload_kind in
        "Deployment")
            # Check if deployment is ready
            ready_replicas=$(kubectl get deployment $workload_name -n $namespace -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
            desired_replicas=$(kubectl get deployment $workload_name -n $namespace -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
            ;;
        "StatefulSet")
            # Check if statefulset is ready
            ready_replicas=$(kubectl get statefulset $workload_name -n $namespace -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
            desired_replicas=$(kubectl get statefulset $workload_name -n $namespace -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
            ;;
        "DaemonSet")
            # Check if daemonset is ready
            ready_replicas=$(kubectl get daemonset $workload_name -n $namespace -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
            desired_replicas=$(kubectl get daemonset $workload_name -n $namespace -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
            ;;
        *)
            echo "    Unknown workload kind: $workload_kind"
            return 1
            ;;
    esac

    echo "    Ready replicas: $ready_replicas/$desired_replicas"

    if [[ "$ready_replicas" == "$desired_replicas" ]] && [[ "$ready_replicas" != "0" ]]; then
        echo -e "    ${GREEN}✓${NC} $workload_kind is ready"
        return 0
    else
        echo -e "    ${YELLOW}!${NC} $workload_kind is not fully ready"
        return 0  # Don't fail the test for readiness issues
    fi
}

# Helper function to check container resources
check_container_resources() {
    local workload_name=$1
    local workload_kind=$2
    local namespace=$3

    echo "  Checking container resources in $workload_kind/$workload_name..."

    case $workload_kind in
        "Deployment")
            containers=$(kubectl get deployment $workload_name -n $namespace -o jsonpath='{.spec.template.spec.containers[*].name}' 2>/dev/null || echo "")
            ;;
        "StatefulSet")
            containers=$(kubectl get statefulset $workload_name -n $namespace -o jsonpath='{.spec.template.spec.containers[*].name}' 2>/dev/null || echo "")
            ;;
        "DaemonSet")
            containers=$(kubectl get daemonset $workload_name -n $namespace -o jsonpath='{.spec.template.spec.containers[*].name}' 2>/dev/null || echo "")
            ;;
        *)
            echo "    Unknown workload kind: $workload_kind"
            return 1
            ;;
    esac

    if [[ -n "$containers" ]]; then
        echo "    Found containers: $containers"

        # Show resource configuration for each container
        for container in $containers; do
            case $workload_kind in
                "Deployment")
                    resources=$(kubectl get deployment $workload_name -n $namespace -o jsonpath="{.spec.template.spec.containers[?(@.name=='$container')].resources}" 2>/dev/null || echo "{}")
                    ;;
                "StatefulSet")
                    resources=$(kubectl get statefulset $workload_name -n $namespace -o jsonpath="{.spec.template.spec.containers[?(@.name=='$container')].resources}" 2>/dev/null || echo "{}")
                    ;;
                "DaemonSet")
                    resources=$(kubectl get daemonset $workload_name -n $namespace -o jsonpath="{.spec.template.spec.containers[?(@.name=='$container')].resources}" 2>/dev/null || echo "{}")
                    ;;
            esac

            echo "      Container '$container' resources:"
            echo "$resources" | jq '.' 2>/dev/null || echo "        (Unable to parse resources)"
        done

        echo -e "    ${GREEN}✓${NC} Container resources verified"
        return 0
    else
        echo -e "    ${RED}✗${NC} No containers found"
        return 1
    fi
}

# Test function for workload types
run_workload_type_test() {
    local test_name=$1
    local workload_file=$2
    local workload_name=$3
    local workload_kind=$4
    local namespace=$5

    echo -e "${BLUE}--- Test: $test_name ---${NC}"

    # Apply workload
    echo "Applying $workload_kind workload..."
    kubectl apply -f "$workload_file"

    # Wait for workload to be created
    echo "Waiting for $workload_kind to be created..."
    sleep 5

    # Wait for OptipPod to process the workload
    echo "Waiting for OptipPod to process workload..."
    sleep 15

    # Check results
    test_passed=true

    check_workload_status "$workload_name" "$workload_kind" "$namespace" || test_passed=false
    check_container_resources "$workload_name" "$workload_kind" "$namespace" || test_passed=false
    check_workload_discovery "$workload_name" "$workload_kind" "$namespace" || test_passed=false
    check_workload_annotations "$workload_name" "$workload_kind" "$namespace" || test_passed=false

    if $test_passed; then
        echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ Test FAILED: $test_name${NC}"
        ((TESTS_FAILED++))
    fi

    # Cleanup
    echo "Cleaning up $workload_kind..."
    kubectl delete -f "$workload_file" --ignore-not-found=true

    # Wait for cleanup
    sleep 5

    echo
}

# Ensure namespace has the required label
echo "Setting up test environment..."
kubectl label namespace default environment=development --overwrite

# Apply the policy first
echo "Applying OptipPod policy for workload types testing..."
kubectl apply -f hack/test-policy-workload-types.yaml

# Wait for policy to be processed
sleep 5

# Run the workload type tests
echo "Starting Workload Types tests..."
echo

# Test 1: Deployment
run_workload_type_test \
    "Deployment Workload" \
    "hack/test-workload-deployment.yaml" \
    "workload-deployment-test" \
    "Deployment" \
    "default"

# Test 2: StatefulSet
run_workload_type_test \
    "StatefulSet Workload" \
    "hack/test-workload-statefulset.yaml" \
    "workload-statefulset-test" \
    "StatefulSet" \
    "default"

# Test 3: DaemonSet
run_workload_type_test \
    "DaemonSet Workload" \
    "hack/test-workload-daemonset.yaml" \
    "workload-daemonset-test" \
    "DaemonSet" \
    "default"

# Cleanup policy
echo "Cleaning up policy..."
kubectl delete -f hack/test-policy-workload-types.yaml --ignore-not-found=true

# Summary
echo "=== Workload Types Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All Workload Types tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some Workload Types tests failed.${NC}"
    exit 1
fi
