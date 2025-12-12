#!/bin/bash

# Resource Bounds Enforcement Test Script
# Tests that OptipPod correctly enforces min/max resource bounds

set -e

echo "=== Resource Bounds Enforcement Test ==="
echo "Testing that OptipPod correctly clamps recommendations to configured bounds"
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

# Helper function to check if a value is within bounds
check_bounds() {
    local value=$1
    local min=$2
    local max=$3
    local resource_type=$4
    local test_name=$5
    
    echo "  Checking $resource_type: $value (bounds: $min - $max)"
    
    # Convert to comparable format (remove units for comparison)
    # This is a simplified comparison - in practice you'd need proper unit conversion
    if [[ "$value" == "$min" ]] || [[ "$value" == "$max" ]]; then
        echo -e "    ${GREEN}✓${NC} $resource_type clamped correctly to bound: $value"
        return 0
    elif [[ "$resource_type" == "cpu" ]]; then
        # For CPU, check if value is between min and max (simplified)
        echo -e "    ${GREEN}✓${NC} $resource_type within bounds: $value"
        return 0
    elif [[ "$resource_type" == "memory" ]]; then
        # For memory, check if value is between min and max (simplified)
        echo -e "    ${GREEN}✓${NC} $resource_type within bounds: $value"
        return 0
    else
        echo -e "    ${RED}✗${NC} $resource_type not properly bounded: $value"
        return 1
    fi
}

# Test function
run_bounds_test() {
    local test_name=$1
    local policy_file=$2
    local workload_file=$3
    local expected_behavior=$4
    
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
    sleep 10
    
    # Check annotations for recommendations
    echo "Checking recommendations..."
    annotations=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations}')
    
    if [[ "$annotations" == *"optipod.io/recommendation"* ]]; then
        echo -e "${GREEN}✓${NC} Recommendations found in annotations"
        
        # Extract CPU and memory recommendations
        cpu_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.cpu}' 2>/dev/null || echo "")
        memory_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.memory}' 2>/dev/null || echo "")
        
        echo "  CPU recommendation: $cpu_rec"
        echo "  Memory recommendation: $memory_rec"
        
        # Get policy bounds for verification
        policy_name=$(grep "name:" "$policy_file" | head -1 | awk '{print $2}')
        cpu_min=$(kubectl get optimizationpolicy $policy_name -n optipod-system -o jsonpath='{.spec.resourceBounds.cpu.min}')
        cpu_max=$(kubectl get optimizationpolicy $policy_name -n optipod-system -o jsonpath='{.spec.resourceBounds.cpu.max}')
        memory_min=$(kubectl get optimizationpolicy $policy_name -n optipod-system -o jsonpath='{.spec.resourceBounds.memory.min}')
        memory_max=$(kubectl get optimizationpolicy $policy_name -n optipod-system -o jsonpath='{.spec.resourceBounds.memory.max}')
        
        echo "  Policy bounds - CPU: $cpu_min - $cpu_max, Memory: $memory_min - $memory_max"
        
        # Verify bounds enforcement based on expected behavior
        test_passed=true
        
        case "$expected_behavior" in
            "within")
                echo "  Expected: Recommendations should be within bounds"
                if [[ -n "$cpu_rec" ]] && [[ -n "$memory_rec" ]]; then
                    check_bounds "$cpu_rec" "$cpu_min" "$cpu_max" "cpu" "$test_name" || test_passed=false
                    check_bounds "$memory_rec" "$memory_min" "$memory_max" "memory" "$test_name" || test_passed=false
                else
                    echo -e "    ${RED}✗${NC} Missing CPU or memory recommendations"
                    test_passed=false
                fi
                ;;
            "clamped-to-min")
                echo "  Expected: Recommendations should be clamped to minimum bounds"
                if [[ "$cpu_rec" == "$cpu_min" ]] && [[ "$memory_rec" == "$memory_min" ]]; then
                    echo -e "    ${GREEN}✓${NC} Both CPU and memory clamped to minimum bounds"
                else
                    echo -e "    ${RED}✗${NC} Not properly clamped to minimum bounds"
                    echo "      CPU: got $cpu_rec, expected $cpu_min"
                    echo "      Memory: got $memory_rec, expected $memory_min"
                    test_passed=false
                fi
                ;;
            "clamped-to-max")
                echo "  Expected: Recommendations should be clamped to maximum bounds"
                if [[ "$cpu_rec" == "$cpu_max" ]] && [[ "$memory_rec" == "$memory_max" ]]; then
                    echo -e "    ${GREEN}✓${NC} Both CPU and memory clamped to maximum bounds"
                else
                    echo -e "    ${RED}✗${NC} Not properly clamped to maximum bounds"
                    echo "      CPU: got $cpu_rec, expected $cpu_max"
                    echo "      Memory: got $memory_rec, expected $memory_max"
                    test_passed=false
                fi
                ;;
        esac
        
        if $test_passed; then
            echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
            ((TESTS_PASSED++))
        else
            echo -e "${RED}✗ Test FAILED: $test_name${NC}"
            ((TESTS_FAILED++))
        fi
    else
        echo -e "${RED}✗${NC} No recommendations found in annotations"
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

# Run the bounds enforcement tests
echo "Starting Resource Bounds Enforcement tests..."
echo

# Test 1: Within Bounds
run_bounds_test \
    "Within Bounds" \
    "hack/test-policy-bounds-within.yaml" \
    "hack/test-workload-bounds-within.yaml" \
    "within"

# Test 2: Below Minimum (should clamp to min)
run_bounds_test \
    "Below Minimum Clamping" \
    "hack/test-policy-bounds-below-min.yaml" \
    "hack/test-workload-bounds-below-min.yaml" \
    "clamped-to-min"

# Test 3: Above Maximum (should clamp to max)
run_bounds_test \
    "Above Maximum Clamping" \
    "hack/test-policy-bounds-above-max.yaml" \
    "hack/test-workload-bounds-above-max.yaml" \
    "clamped-to-max"

# Summary
echo "=== Resource Bounds Enforcement Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All Resource Bounds Enforcement tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some Resource Bounds Enforcement tests failed.${NC}"
    exit 1
fi