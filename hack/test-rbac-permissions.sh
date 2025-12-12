#!/bin/bash

# RBAC Permissions Test Script
# Tests that OptipPod correctly handles insufficient RBAC permissions

set -e

echo "=== RBAC Permissions Test ==="
echo "Testing OptipPod behavior with insufficient permissions"
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

# Helper function to check for RBAC errors in logs
check_rbac_errors() {
    local expected_rbac_errors=$1
    
    echo "  Checking OptipPod logs for RBAC error messages..."
    
    # Get recent logs and check for RBAC-related messages
    rbac_logs=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=2m 2>/dev/null | grep -i "rbac\|forbidden\|insufficient.*permission" || echo "")
    
    if [[ -n "$rbac_logs" ]]; then
        echo "    Found RBAC-related log messages:"
        echo "$rbac_logs" | sed 's/^/      /'
        
        if [[ "$expected_rbac_errors" == "true" ]]; then
            echo -e "    ${GREEN}✓${NC} RBAC errors found as expected"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Unexpected RBAC errors found"
            return 1
        fi
    else
        echo "    No RBAC-related log messages found"
        if [[ "$expected_rbac_errors" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} No RBAC errors as expected"
            return 0
        else
            echo -e "    ${RED}✗${NC} Expected RBAC errors not found in logs"
            return 1
        fi
    fi
}

# Helper function to check workload update status
check_workload_updates() {
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
                echo -e "    ${RED}✗${NC} Unexpected update occurred (should have been blocked by RBAC)"
                return 1
            fi
        else
            echo "    Workload was NOT updated (no last-applied annotation)"
            if [[ "$expected_updated" == "false" ]]; then
                echo -e "    ${GREEN}✓${NC} Update correctly blocked by RBAC"
                return 0
            else
                echo -e "    ${RED}✗${NC} Expected update did not occur"
                return 1
            fi
        fi
    else
        echo "    Workload is not managed by OptipPod or has no annotations"
        if [[ "$expected_updated" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} Update correctly blocked (not managed due to RBAC)"
            return 0
        else
            echo -e "    ${RED}✗${NC} Workload should have been managed and updated"
            return 1
        fi
    fi
}

# Helper function to backup and restore OptipPod deployment
backup_optipod_deployment() {
    echo "  Backing up current OptipPod deployment..."
    kubectl get deployment optipod-controller-manager -n optipod-system -o yaml > /tmp/optipod-deployment-backup.yaml
}

restore_optipod_deployment() {
    echo "  Restoring original OptipPod deployment..."
    kubectl apply -f /tmp/optipod-deployment-backup.yaml
    kubectl rollout status deployment/optipod-controller-manager -n optipod-system --timeout=60s
    rm -f /tmp/optipod-deployment-backup.yaml
}

# Test function for RBAC scenarios
run_rbac_test() {
    local test_name=$1
    local use_restricted_sa=$2  # "true" or "false"
    
    echo -e "${BLUE}--- Test: $test_name ---${NC}"
    
    if [[ "$use_restricted_sa" == "true" ]]; then
        echo "Setting up restricted ServiceAccount..."
        
        # Backup current deployment
        backup_optipod_deployment
        
        # Create restricted ServiceAccount and RBAC
        kubectl apply -f hack/test-rbac-restricted-serviceaccount.yaml
        
        # Update OptipPod deployment to use restricted ServiceAccount
        kubectl patch deployment optipod-controller-manager -n optipod-system -p '{"spec":{"template":{"spec":{"serviceAccountName":"optipod-restricted"}}}}'
        
        # Wait for rollout to complete
        echo "Waiting for OptipPod deployment to use restricted ServiceAccount..."
        kubectl rollout status deployment/optipod-controller-manager -n optipod-system --timeout=60s
        sleep 5
    fi
    
    # Apply test policy and workload
    echo "Applying test policy and workload..."
    kubectl apply -f hack/test-policy-rbac-test.yaml
    kubectl apply -f hack/test-workload-rbac-test.yaml
    
    # Wait for workload to be ready
    echo "Waiting for test workload to be ready..."
    kubectl wait --for=condition=available deployment/rbac-test-workload -n default --timeout=60s
    
    # Wait for OptipPod to process the workload
    echo "Waiting for OptipPod to process workload..."
    sleep 15
    
    # Check results based on test scenario
    test_passed=true
    
    if [[ "$use_restricted_sa" == "true" ]]; then
        echo "  Expected: RBAC errors should occur and updates should be blocked"
        check_rbac_errors "true" || test_passed=false
        check_workload_updates "rbac-test-workload" "false" || test_passed=false
    else
        echo "  Expected: No RBAC errors and updates should succeed"
        check_rbac_errors "false" || test_passed=false
        check_workload_updates "rbac-test-workload" "true" || test_passed=false
    fi
    
    # Show current workload resources
    echo "  Current workload resources:"
    kubectl get deployment rbac-test-workload -n default -o jsonpath='{.spec.template.spec.containers[0].resources}' | jq '.' 2>/dev/null || echo "    (Unable to parse resources)"
    
    # Show current annotations
    echo "  Current OptipPod annotations:"
    kubectl get deployment rbac-test-workload -n default -o jsonpath='{.metadata.annotations}' | tr ',' '\n' | grep optipod || echo "    (No OptipPod annotations found)"
    
    if $test_passed; then
        echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ Test FAILED: $test_name${NC}"
        ((TESTS_FAILED++))
    fi
    
    # Cleanup
    echo "Cleaning up test resources..."
    kubectl delete -f hack/test-workload-rbac-test.yaml --ignore-not-found=true
    kubectl delete -f hack/test-policy-rbac-test.yaml --ignore-not-found=true
    
    if [[ "$use_restricted_sa" == "true" ]]; then
        echo "Restoring original OptipPod configuration..."
        restore_optipod_deployment
        kubectl delete -f hack/test-rbac-restricted-serviceaccount.yaml --ignore-not-found=true
        sleep 5
    fi
    
    echo
}

# Ensure namespace has the required label
echo "Setting up test environment..."
kubectl label namespace default environment=development --overwrite

# Run the RBAC permission tests
echo "Starting RBAC Permissions tests..."
echo

# Test 1: Normal permissions (should work)
run_rbac_test \
    "Normal RBAC Permissions" \
    "false"

# Test 2: Restricted permissions (should fail with RBAC errors)
run_rbac_test \
    "Restricted RBAC Permissions" \
    "true"

# Summary
echo "=== RBAC Permissions Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All RBAC Permissions tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some RBAC Permissions tests failed.${NC}"
    exit 1
fi