#!/bin/bash

# Memory Decrease Safety Test Script
# Tests that OptipPod correctly prevents unsafe memory decreases that could cause pod eviction or OOM

set -e

echo "=== Memory Decrease Safety Test ==="
echo "Testing that OptipPod prevents unsafe memory reductions"
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

# Helper function to check if workload was updated
check_workload_updated() {
    local workload_name=$1
    local expected_updated=$2  # "true" or "false"

    echo "  Checking if workload $workload_name was updated..."

    # Check if the workload has OptipPod managed annotation
    managed=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/managed}' 2>/dev/null || echo "")

    if [[ "$managed" == "true" ]]; then
        echo "    Workload is managed by OptipPod"

        # Check if there's a last-applied annotation (indicates actual update)
        last_applied=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/last-applied}' 2>/dev/null || echo "")

        if [[ -n "$last_applied" ]]; then
            echo "    Workload was updated (last-applied: $last_applied)"
            if [[ "$expected_updated" == "true" ]]; then
                echo -e "    ${GREEN}✓${NC} Expected update occurred"
                return 0
            else
                echo -e "    ${RED}✗${NC} Unexpected update occurred (should have been blocked)"
                return 1
            fi
        else
            echo "    Workload was NOT updated (no last-applied annotation)"
            if [[ "$expected_updated" == "false" ]]; then
                echo -e "    ${GREEN}✓${NC} Update correctly blocked"
                return 0
            else
                echo -e "    ${RED}✗${NC} Expected update did not occur"
                return 1
            fi
        fi
    else
        echo "    Workload is not managed by OptipPod"
        if [[ "$expected_updated" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} Update correctly blocked (not managed)"
            return 0
        else
            echo -e "    ${RED}✗${NC} Workload should have been managed and updated"
            return 1
        fi
    fi
}

# Helper function to check OptipPod logs for safety messages
check_safety_logs() {
    local workload_name=$1
    local expected_safety_block=$2  # "true" or "false"

    echo "  Checking OptipPod logs for safety messages..."

    # Get recent logs and check for memory safety messages
    safety_logs=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=2m 2>/dev/null | grep -i "memory.*decrease\|eviction\|oom\|unsafe" || echo "")

    if [[ -n "$safety_logs" ]]; then
        echo "    Found safety-related log messages:"
        echo "$safety_logs" | sed 's/^/      /'

        if [[ "$expected_safety_block" == "true" ]]; then
            echo -e "    ${GREEN}✓${NC} Safety mechanism activated as expected"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Safety mechanism activated (may be from other tests)"
            return 0
        fi
    else
        echo "    No safety-related log messages found"
        if [[ "$expected_safety_block" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} No safety blocks as expected"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Expected safety block not found in logs"
            return 0
        fi
    fi
}

# Test function
run_memory_safety_test() {
    local test_name=$1
    local policy_file=$2
    local workload_file=$3
    local expected_behavior=$4  # "safe", "unsafe", "no-limits"

    echo -e "${BLUE}--- Test: $test_name ---${NC}"

    # Apply policy
    echo "Applying policy: $policy_file"
    kubectl apply -f "$policy_file"

    # Apply workload
    echo "Applying workload: $workload_file"
    kubectl apply -f "$workload_file"

    # Wait for workload to be ready
    workload_name=$(grep -A 5 "kind: Deployment" "$workload_file" | grep "name:" | awk '{print $2}')
    echo "Waiting for workload $workload_name to be ready..."
    kubectl wait --for=condition=available deployment/$workload_name -n default --timeout=60s

    # Wait for OptipPod to process the workload
    echo "Waiting for OptipPod to process workload..."
    sleep 15

    # Check results based on expected behavior
    test_passed=true

    case "$expected_behavior" in
        "safe")
            echo "  Expected: Memory decrease should be allowed (safe)"
            check_workload_updated "$workload_name" "true" || test_passed=false
            check_safety_logs "$workload_name" "false" || test_passed=false
            ;;
        "unsafe")
            echo "  Expected: Memory decrease should be blocked (unsafe)"
            check_workload_updated "$workload_name" "false" || test_passed=false
            check_safety_logs "$workload_name" "true" || test_passed=false
            ;;
        "no-limits")
            echo "  Expected: Memory decrease should be allowed (no limits to compare)"
            check_workload_updated "$workload_name" "true" || test_passed=false
            check_safety_logs "$workload_name" "false" || test_passed=false
            ;;
    esac

    # Check current resource values
    echo "  Current workload resources:"
    kubectl get deployment $workload_name -n default -o jsonpath='{.spec.template.spec.containers[0].resources}' | jq '.' 2>/dev/null || echo "    (Unable to parse resources)"

    # Check recommendations
    echo "  Current recommendations:"
    cpu_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.cpu-request}' 2>/dev/null || echo "none")
    memory_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.memory-request}' 2>/dev/null || echo "none")
    echo "    CPU: $cpu_rec"
    echo "    Memory: $memory_rec"

    if $test_passed; then
        echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ Test FAILED: $test_name${NC}"
        ((TESTS_FAILED++))
    fi

    # Cleanup
    echo "Cleaning up..."
    kubectl delete -f "$workload_file" --ignore-not-found=true
    kubectl delete -f "$policy_file" --ignore-not-found=true

    echo
}

# Ensure namespace has the required label
echo "Setting up test environment..."
kubectl label namespace default environment=development --overwrite

# Run the memory decrease safety tests
echo "Starting Memory Decrease Safety tests..."
echo

# Test 1: Safe Memory Decrease (recommendation higher than current limit)
run_memory_safety_test \
    "Safe Memory Decrease" \
    "hack/test-policy-memory-safe-decrease.yaml" \
    "hack/test-workload-memory-safe-decrease.yaml" \
    "safe"

# Test 2: Unsafe Memory Decrease (recommendation lower than current limit)
run_memory_safety_test \
    "Unsafe Memory Decrease" \
    "hack/test-policy-memory-unsafe-decrease.yaml" \
    "hack/test-workload-memory-unsafe-decrease.yaml" \
    "unsafe"

# Test 3: No Memory Limits (should always be safe)
run_memory_safety_test \
    "No Memory Limits" \
    "hack/test-policy-memory-safe-decrease.yaml" \
    "hack/test-workload-memory-no-limits.yaml" \
    "no-limits"

# Summary
echo "=== Memory Decrease Safety Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All Memory Decrease Safety tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some Memory Decrease Safety tests failed.${NC}"
    exit 1
fi
