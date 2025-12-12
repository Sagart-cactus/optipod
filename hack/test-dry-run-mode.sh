#!/bin/bash

# Dry Run Mode Test Script
# Tests that OptipPod correctly handles global dry-run mode

set -e

echo "=== Dry Run Mode Test ==="
echo "Testing OptipPod behavior with global dry-run mode enabled"
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

# Helper function to check for dry-run messages in logs
check_dry_run_logs() {
    local expected_dry_run=$1
    
    echo "  Checking OptipPod logs for dry-run messages..."
    
    # Get recent logs and check for dry-run related messages
    dry_run_logs=$(kubectl logs -n optipod-system deployment/optipod-controller-manager --since=2m 2>/dev/null | grep -i "dry.*run\|dry-run" || echo "")
    
    if [[ -n "$dry_run_logs" ]]; then
        echo "    Found dry-run related log messages:"
        echo "$dry_run_logs" | sed 's/^/      /'
        
        if [[ "$expected_dry_run" == "true" ]]; then
            echo -e "    ${GREEN}✓${NC} Dry-run messages found as expected"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Unexpected dry-run messages found"
            return 1
        fi
    else
        echo "    No dry-run related log messages found"
        if [[ "$expected_dry_run" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} No dry-run messages as expected"
            return 0
        else
            echo -e "    ${RED}✗${NC} Expected dry-run messages not found in logs"
            return 1
        fi
    fi
}

# Helper function to check workload update status
check_workload_updates() {
    local workload_name=$1
    local expected_updated=$2  # "true" or "false"
    
    echo "  Checking if workload $workload_name was updated..."
    
    # Store original resource values for comparison
    original_cpu=$(kubectl get deployment $workload_name -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}')
    original_memory=$(kubectl get deployment $workload_name -n default -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')
    
    echo "    Original resources: CPU=$original_cpu, Memory=$original_memory"
    
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
                echo -e "    ${RED}✗${NC} Unexpected update occurred (should have been blocked by dry-run)"
                return 1
            fi
        else
            echo "    Workload was NOT updated (no last-applied annotation)"
            if [[ "$expected_updated" == "false" ]]; then
                echo -e "    ${GREEN}✓${NC} Update correctly blocked by dry-run mode"
                return 0
            else
                echo -e "    ${RED}✗${NC} Expected update did not occur"
                return 1
            fi
        fi
    else
        echo "    Workload is not managed by OptipPod or has no annotations"
        if [[ "$expected_updated" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} Update correctly blocked (not managed due to dry-run)"
            return 0
        else
            echo -e "    ${RED}✗${NC} Workload should have been managed"
            return 1
        fi
    fi
}

# Helper function to check for recommendations
check_recommendations() {
    local workload_name=$1
    local expected_recommendations=$2  # "true" or "false"
    
    echo "  Checking for OptipPod recommendations..."
    
    # Check for recommendation annotations
    cpu_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.cpu-request}' 2>/dev/null || echo "")
    memory_rec=$(kubectl get deployment $workload_name -n default -o jsonpath='{.metadata.annotations.optipod\.io/recommendation\.app\.memory-request}' 2>/dev/null || echo "")
    
    if [[ -n "$cpu_rec" ]] && [[ -n "$memory_rec" ]]; then
        echo "    Recommendations found: CPU=$cpu_rec, Memory=$memory_rec"
        if [[ "$expected_recommendations" == "true" ]]; then
            echo -e "    ${GREEN}✓${NC} Recommendations generated as expected"
            return 0
        else
            echo -e "    ${YELLOW}!${NC} Unexpected recommendations found"
            return 1
        fi
    else
        echo "    No recommendations found"
        if [[ "$expected_recommendations" == "false" ]]; then
            echo -e "    ${GREEN}✓${NC} No recommendations as expected"
            return 0
        else
            echo -e "    ${RED}✗${NC} Expected recommendations not found"
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

# Helper function to enable/disable dry-run mode
set_dry_run_mode() {
    local enable_dry_run=$1  # "true" or "false"
    
    echo "  Setting dry-run mode to: $enable_dry_run"
    
    # Update the deployment args to set dry-run flag
    kubectl patch deployment optipod-controller-manager -n optipod-system --type='json' -p="[
        {
            \"op\": \"replace\",
            \"path\": \"/spec/template/spec/containers/0/args\",
            \"value\": [
                \"--metrics-bind-address=:8080\",
                \"--leader-elect=true\",
                \"--health-probe-bind-address=:8081\",
                \"--metrics-bind-address=:8080\",
                \"--metrics-provider=metrics-server\",
                \"--prometheus-url=http://prometheus-k8s.monitoring.svc:9090\",
                \"--dry-run=$enable_dry_run\",
                \"--reconciliation-interval=1m\",
                \"--metrics-max-samples=1\",
                \"--metrics-sample-interval=0\"
            ]
        }
    ]"
    
    # Wait for rollout to complete
    echo "    Waiting for OptipPod deployment rollout..."
    kubectl rollout status deployment/optipod-controller-manager -n optipod-system --timeout=60s
    sleep 5
}

# Test function for dry-run scenarios
run_dry_run_test() {
    local test_name=$1
    local enable_dry_run=$2  # "true" or "false"
    
    echo -e "${BLUE}--- Test: $test_name ---${NC}"
    
    # Backup current deployment
    backup_optipod_deployment
    
    # Set dry-run mode
    set_dry_run_mode "$enable_dry_run"
    
    # Apply test policy and workload
    echo "Applying test policy and workload..."
    kubectl apply -f hack/test-policy-dry-run.yaml
    kubectl apply -f hack/test-workload-dry-run.yaml
    
    # Wait for workload to be ready
    echo "Waiting for test workload to be ready..."
    kubectl wait --for=condition=available deployment/dry-run-test-workload -n default --timeout=60s
    
    # Wait for OptipPod to process the workload
    echo "Waiting for OptipPod to process workload..."
    sleep 15
    
    # Check results based on test scenario
    test_passed=true
    
    if [[ "$enable_dry_run" == "true" ]]; then
        echo "  Expected: Dry-run mode should prevent updates but allow recommendations"
        check_dry_run_logs "true" || test_passed=false
        check_workload_updates "dry-run-test-workload" "false" || test_passed=false
        check_recommendations "dry-run-test-workload" "true" || test_passed=false
    else
        echo "  Expected: Normal mode should allow updates and recommendations"
        check_dry_run_logs "false" || test_passed=false
        check_workload_updates "dry-run-test-workload" "true" || test_passed=false
        check_recommendations "dry-run-test-workload" "true" || test_passed=false
    fi
    
    # Show current workload resources
    echo "  Current workload resources:"
    kubectl get deployment dry-run-test-workload -n default -o jsonpath='{.spec.template.spec.containers[0].resources}' | jq '.' 2>/dev/null || echo "    (Unable to parse resources)"
    
    # Show current annotations
    echo "  Current OptipPod annotations:"
    kubectl get deployment dry-run-test-workload -n default -o jsonpath='{.metadata.annotations}' | tr ',' '\n' | grep optipod || echo "    (No OptipPod annotations found)"
    
    if $test_passed; then
        echo -e "${GREEN}✓ Test PASSED: $test_name${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}✗ Test FAILED: $test_name${NC}"
        ((TESTS_FAILED++))
    fi
    
    # Cleanup
    echo "Cleaning up test resources..."
    kubectl delete -f hack/test-workload-dry-run.yaml --ignore-not-found=true
    kubectl delete -f hack/test-policy-dry-run.yaml --ignore-not-found=true
    
    # Restore original deployment
    echo "Restoring original OptipPod configuration..."
    restore_optipod_deployment
    
    echo
}

# Ensure namespace has the required label
echo "Setting up test environment..."
kubectl label namespace default environment=development --overwrite

# Run the dry-run mode tests
echo "Starting Dry Run Mode tests..."
echo

# Test 1: Normal mode (dry-run=false) - should apply updates
run_dry_run_test \
    "Normal Mode (dry-run=false)" \
    "false"

# Test 2: Dry-run mode (dry-run=true) - should block updates
run_dry_run_test \
    "Dry Run Mode (dry-run=true)" \
    "true"

# Summary
echo "=== Dry Run Mode Test Summary ==="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests: $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All Dry Run Mode tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some Dry Run Mode tests failed.${NC}"
    exit 1
fi