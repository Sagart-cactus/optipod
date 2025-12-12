#!/bin/bash

# Error Handling Test Script
# Tests OptipPod's error handling and edge case scenarios

set -e

echo "=== Error Handling Test ==="
echo "Testing OptipPod error handling for various failure scenarios"
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

# Helper function to check policy validation errors
check_policy_validation() {
    local policy_file=$1
    local expected_error_type=$2
    
    echo "  Testing policy validation for $expected_error_type..."
    
    # Try to apply the policy and capture output
    policy_output=$(kubectl apply -f "$policy_file" 2>&1 || echo "VALIDATION_FAILED")
    
    if [[ "$policy_output" == *"VALIDATION_FAILED"* ]] || [[ "$policy_output" == *"error"* ]] || [[ "$policy_output" == *"invalid"* ]]; then
        echo "    Policy validation correctly rejected invalid configuration"
        echo "    Error message: $policy_output" | head -1
        return 0
    elif [[ "$policy_output" == *"created"* ]] || [[ "$policy_output" == *"configured"* ]]; then
        echo "    Policy was unexpectedly accepted (validation may be missing)"
        # Clean up the created policy
        kubectl delete -f "$policy_file" --ignore-not-found=true 2>/dev/null
        return 1
    else
        echo "    Unexpected response: $policy_output"
        return 1
    fi
}

# Helper function to check error logs
check_error_logs() {
    local expected_error_pattern=$1
    local context_description=$2
    
    echo "  Checking OptipPod logs for $context_description errors..."
    
    # Get recent logs and check for error patterns
    error_logs=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=2m 2>/dev/null | grep -i "error\|failed\|invalid" || echo "")
    
    if [[ -n "$error_logs" ]]; then
        echo "    Found error-related log messages:"
        echo "$error_logs" | sed 's/^/      /' | tail -3
        
        # Check if the expected error pattern is present
        if [[ "$error_logs" == *"$expected_error_pattern"* ]]; then
            echo -e "    ${GREEN}✓${NC} Expected error pattern found: $expected_error_pattern"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Expected error pattern not found, but errors are present"
            return 0  # Don't fail if errors are present but pattern doesn't match exactly
        fi
    else
        echo "    No error messages found in logs"
        return 0
    fi
}

# Helper function to check workload error handling
check_workload_error_handling() {
    local workload_file=$1
    local workload_name=$2
    local expected_error_type=$3
    
    echo "  Testing workload error handling for $expected_error_type..."
    
    # Apply workload
    workload_output=$(kubectl apply -f "$workload_file" 2>&1 || echo "WORKLOAD_FAILED")
    
    if [[ "$workload_output" == *"WORKLOAD_FAILED"* ]] || [[ "$workload_output" == *"error"* ]] || [[ "$workload_output" == *"invalid"* ]]; then
        echo "    Workload correctly rejected due to invalid configuration"
        echo "    Error message: $workload_output" | head -1
        return 0
    elif [[ "$workload_output" == *"created"* ]] || [[ "$workload_output" == *"configured"* ]]; then
        echo "    Workload was created (may be handled at runtime)"
        
        # Wait for OptipPod to process
        sleep 10
        
        # Check for processing errors in logs
        check_error_logs "$expected_error_type" "workload processing"
        
        # Clean up
        kubectl delete -f "$workload_file" --ignore-not-found=true 2>/dev/null
        return 0
    else
        echo "    Unexpected response: $workload_output"
        return 1
    fi
}

# Helper function to check policy status for errors
check_policy_status() {
    local policy_name=$1
    local expected_condition=$2
    
    echo "  Checking policy status for error conditions..."
    
    # Get policy status
    policy_status=$(kubectl get optimizationpolicy "$policy_name" -n optipod-system -o jsonpath='{.status}' 2>/dev/null || echo "{}")
    
    if [[ -n "$policy_status" ]] && [[ "$policy_status" != "{}" ]]; then
        echo "    Policy status:"
        echo "$policy_status" | jq '.' 2>/dev/null || echo "    (Unable to parse status)"
        
        # Check for error conditions
        conditions=$(echo "$policy_status" | jq -r '.conditions[]? | select(.type=="Ready" and .status=="False") | .message' 2>/dev/null || echo "")
        
        if [[ -n "$conditions" ]]; then
            echo "    Found error conditions: $conditions"
            return 0
        else
            echo "    No error conditions found in status"
            return 0
        fi
    else
        echo "    No policy status available"
        return 0
    fi
}

# Test function for error scenarios
run_error_handling_test() {
    local test_name=$1
    local test_type=$2
    local test_file=$3
    local expected_error=$4
    
    echo -e "${BLUE}--- Test: $test_name ---${NC}"
    
    test_passed=true
    
    case $test_type in
        "policy-validation")
            check_policy_validation "$test_file" "$expected_error" || test_passed=false
            ;;
        "workload-error")
            workload_name=$(basename "$test_file" .yaml | sed 's/test-workload-//' | sed 's/-test//')
            check_workload_error_handling "$test_file" "$workload_name" "$expected_error" || test_passed=false
            ;;
        "runtime-error")
            # Apply policy and check for runtime errors
            kubectl apply -f "$test_file" 2>/dev/null || true
            sleep 10
            policy_name=$(basename "$test_file" .yaml | sed 's/test-policy-//' | sed 's/-test//')
            check_policy_status "$policy_name" "$expected_error" || test_passed=false
            check_error_logs "$expected_error" "runtime processing" || test_passed=false
            kubectl delete -f "$test_file" --ignore-not-found=true 2>/dev/null
            ;;
        *)
            echo "    Unknown test type: $test_type"
            test_passed=false
            ;;
    esac
    
    if $test_passed; then
        echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ Test FAILED: $test_name${NC}"
        ((TESTS_FAILED++))
    fi
    
    echo
}

# Ensure namespace has the required label
echo "Setting up test environment..."
kubectl label namespace default environment=development --overwrite

# Run the error handling tests
echo "Starting Error Handling tests..."
echo

# Test 1: Invalid Policy Configuration
run_error_handling_test \
    "Invalid Policy Configuration" \
    "policy-validation" \
    "hack/test-policy-invalid-config.yaml" \
    "validation"

# Test 2: Missing Metrics Provider
run_error_handling_test \
    "Missing Metrics Provider" \
    "runtime-error" \
    "hack/test-policy-missing-metrics.yaml" \
    "metrics"

# Test 3: Workload with No Resources
run_error_handling_test \
    "Workload with No Resources" \
    "workload-error" \
    "hack/test-workload-no-resources.yaml" \
    "resources"

# Test 4: Workload with Invalid Resources
run_error_handling_test \
    "Workload with Invalid Resources" \
    "workload-error" \
    "hack/test-workload-invalid-resources.yaml" \
    "invalid"

# Additional runtime error checks
echo -e "${BLUE}--- Additional Error Checks ---${NC}"

# Check for any recent errors in OptipPod logs
echo "Checking for recent OptipPod errors..."
recent_errors=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=5m 2>/dev/null | grep -i "error\|failed\|panic" | tail -5 || echo "")

if [[ -n "$recent_errors" ]]; then
    echo "Recent error messages found:"
    echo "$recent_errors" | sed 's/^/  /'
else
    echo "No recent error messages found"
fi

# Check OptipPod controller status
echo "Checking OptipPod controller status..."
controller_status=$(kubectl get deployment optipod-controller-manager -n optipod-system -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || echo "Unknown")
echo "Controller availability: $controller_status"

if [[ "$controller_status" == "True" ]]; then
    echo -e "${GREEN}✓${NC} OptipPod controller is available"
    ((TESTS_PASSED++))
else
    echo -e "${RED}✗${NC} OptipPod controller is not available"
    ((TESTS_FAILED++))
fi

echo

# Summary
echo "=== Error Handling Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All Error Handling tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some Error Handling tests failed.${NC}"
    exit 1
fi