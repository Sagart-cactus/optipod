#!/bin/bash

# OptipPod E2E Test Quality Monitoring Script
# This script monitors the quality and health of the e2e test suite

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"
PROJECT_ROOT="$(dirname "$(dirname "$E2E_DIR")")"
REPORT_DIR="${E2E_DIR}/reports"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Create reports directory
mkdir -p "$REPORT_DIR"

# Function to check test file structure
check_test_structure() {
    log_info "Checking test file structure..."
    
    local issues=0
    local report_file="${REPORT_DIR}/structure_check_${TIMESTAMP}.txt"
    
    echo "Test Structure Check Report - $(date)" > "$report_file"
    echo "========================================" >> "$report_file"
    
    # Check for required test files
    local required_files=(
        "e2e_suite_test.go"
        "e2e_test.go"
        "helpers/policy_helpers.go"
        "helpers/workload_helpers.go"
        "helpers/validation_helpers.go"
        "helpers/cleanup_helpers.go"
        "fixtures/generators.go"
    )
    
    echo "Required Files Check:" >> "$report_file"
    for file in "${required_files[@]}"; do
        if [[ -f "${E2E_DIR}/${file}" ]]; then
            echo "  ✓ $file" >> "$report_file"
        else
            echo "  ✗ $file (MISSING)" >> "$report_file"
            ((issues++))
        fi
    done
    
    # Check for documentation files
    local doc_files=(
        "README.md"
        "TESTING_GUIDE.md"
        "TROUBLESHOOTING.md"
        "DEVELOPER_ONBOARDING.md"
    )
    
    echo "" >> "$report_file"
    echo "Documentation Files Check:" >> "$report_file"
    for file in "${doc_files[@]}"; do
        if [[ -f "${E2E_DIR}/${file}" ]]; then
            echo "  ✓ $file" >> "$report_file"
        else
            echo "  ✗ $file (MISSING)" >> "$report_file"
            ((issues++))
        fi
    done
    
    # Check test file naming conventions
    echo "" >> "$report_file"
    echo "Test File Naming Convention Check:" >> "$report_file"
    
    local test_files
    test_files=$(find "$E2E_DIR" -name "*_test.go" -not -path "*/vendor/*")
    
    for file in $test_files; do
        local basename
        basename=$(basename "$file")
        if [[ "$basename" =~ ^[a-z_]+_test\.go$ ]]; then
            echo "  ✓ $basename" >> "$report_file"
        else
            echo "  ⚠ $basename (non-standard naming)" >> "$report_file"
        fi
    done
    
    echo "" >> "$report_file"
    echo "Issues found: $issues" >> "$report_file"
    
    if [[ $issues -eq 0 ]]; then
        log_success "Test structure check passed"
    else
        log_warning "Test structure check found $issues issues"
    fi
    
    cat "$report_file"
    return $issues
}

# Function to analyze test coverage
analyze_test_coverage() {
    log_info "Analyzing test coverage..."
    
    local report_file="${REPORT_DIR}/coverage_analysis_${TIMESTAMP}.txt"
    
    echo "Test Coverage Analysis Report - $(date)" > "$report_file"
    echo "=======================================" >> "$report_file"
    
    # Count test files by category
    local policy_tests
    local bounds_tests
    local rbac_tests
    local error_tests
    local workload_tests
    local observability_tests
    local property_tests
    
    policy_tests=$(find "$E2E_DIR" -name "*policy*test.go" | wc -l)
    bounds_tests=$(find "$E2E_DIR" -name "*bounds*test.go" -o -name "*resource*test.go" | wc -l)
    rbac_tests=$(find "$E2E_DIR" -name "*rbac*test.go" -o -name "*security*test.go" | wc -l)
    error_tests=$(find "$E2E_DIR" -name "*error*test.go" | wc -l)
    workload_tests=$(find "$E2E_DIR" -name "*workload*test.go" | wc -l)
    observability_tests=$(find "$E2E_DIR" -name "*observability*test.go" -o -name "*metrics*test.go" | wc -l)
    property_tests=$(find "$E2E_DIR" -name "*property*test.go" | wc -l)
    
    echo "Test Category Coverage:" >> "$report_file"
    echo "  Policy Mode Tests: $policy_tests files" >> "$report_file"
    echo "  Resource Bounds Tests: $bounds_tests files" >> "$report_file"
    echo "  RBAC/Security Tests: $rbac_tests files" >> "$report_file"
    echo "  Error Handling Tests: $error_tests files" >> "$report_file"
    echo "  Workload Type Tests: $workload_tests files" >> "$report_file"
    echo "  Observability Tests: $observability_tests files" >> "$report_file"
    echo "  Property-Based Tests: $property_tests files" >> "$report_file"
    
    # Calculate coverage score
    local total_categories=7
    local covered_categories=0
    
    [[ $policy_tests -gt 0 ]] && ((covered_categories++))
    [[ $bounds_tests -gt 0 ]] && ((covered_categories++))
    [[ $rbac_tests -gt 0 ]] && ((covered_categories++))
    [[ $error_tests -gt 0 ]] && ((covered_categories++))
    [[ $workload_tests -gt 0 ]] && ((covered_categories++))
    [[ $observability_tests -gt 0 ]] && ((covered_categories++))
    [[ $property_tests -gt 0 ]] && ((covered_categories++))
    
    local coverage_percent=$((covered_categories * 100 / total_categories))
    
    echo "" >> "$report_file"
    echo "Coverage Score: $coverage_percent% ($covered_categories/$total_categories categories)" >> "$report_file"
    
    # Analyze helper usage
    echo "" >> "$report_file"
    echo "Helper Component Analysis:" >> "$report_file"
    
    if [[ -d "${E2E_DIR}/helpers" ]]; then
        local helper_files
        helper_files=$(find "${E2E_DIR}/helpers" -name "*.go" | wc -l)
        echo "  Helper files: $helper_files" >> "$report_file"
        
        # Check if helpers are being used in tests
        local helper_usage=0
        if grep -r "helpers\." "${E2E_DIR}"/*_test.go >/dev/null 2>&1; then
            helper_usage=1
        fi
        
        if [[ $helper_usage -eq 1 ]]; then
            echo "  Helper usage: ✓ Helpers are being used in tests" >> "$report_file"
        else
            echo "  Helper usage: ⚠ Helpers may not be used in tests" >> "$report_file"
        fi
    else
        echo "  ✗ No helpers directory found" >> "$report_file"
    fi
    
    cat "$report_file"
    
    if [[ $coverage_percent -ge 80 ]]; then
        log_success "Test coverage analysis passed ($coverage_percent%)"
        return 0
    else
        log_warning "Test coverage analysis shows room for improvement ($coverage_percent%)"
        return 1
    fi
}

# Function to check test quality metrics
check_test_quality() {
    log_info "Checking test quality metrics..."
    
    local report_file="${REPORT_DIR}/quality_metrics_${TIMESTAMP}.txt"
    local issues=0
    
    echo "Test Quality Metrics Report - $(date)" > "$report_file"
    echo "====================================" >> "$report_file"
    
    # Check for proper test organization
    echo "Test Organization:" >> "$report_file"
    
    # Check for Describe/Context/It structure
    local ginkgo_tests
    ginkgo_tests=$(grep -r "Describe\|Context\|It(" "${E2E_DIR}"/*_test.go | wc -l)
    echo "  Ginkgo test structures: $ginkgo_tests" >> "$report_file"
    
    # Check for proper cleanup patterns
    local cleanup_patterns
    cleanup_patterns=$(grep -r "AfterEach\|cleanupHelper" "${E2E_DIR}"/*_test.go | wc -l)
    echo "  Cleanup patterns: $cleanup_patterns" >> "$report_file"
    
    # Check for property-based test patterns
    local property_patterns
    property_patterns=$(grep -r "DescribeTable\|Entry(" "${E2E_DIR}"/*_test.go | wc -l)
    echo "  Property-based test patterns: $property_patterns" >> "$report_file"
    
    # Check for proper error handling
    local error_handling
    error_handling=$(grep -r "Expect.*NotTo.*HaveOccurred\|Expect.*To.*HaveOccurred" "${E2E_DIR}"/*_test.go | wc -l)
    echo "  Error handling assertions: $error_handling" >> "$report_file"
    
    # Check for timeout handling
    local timeout_handling
    timeout_handling=$(grep -r "Eventually\|timeout" "${E2E_DIR}"/*_test.go | wc -l)
    echo "  Timeout handling: $timeout_handling" >> "$report_file"
    
    # Quality score calculation
    local quality_score=0
    
    [[ $ginkgo_tests -gt 10 ]] && ((quality_score += 20))
    [[ $cleanup_patterns -gt 5 ]] && ((quality_score += 20))
    [[ $property_patterns -gt 3 ]] && ((quality_score += 20))
    [[ $error_handling -gt 20 ]] && ((quality_score += 20))
    [[ $timeout_handling -gt 5 ]] && ((quality_score += 20))
    
    echo "" >> "$report_file"
    echo "Quality Score: $quality_score/100" >> "$report_file"
    
    # Generate recommendations
    echo "" >> "$report_file"
    echo "Recommendations:" >> "$report_file"
    
    if [[ $ginkgo_tests -le 10 ]]; then
        echo "  - Add more structured Ginkgo tests (Describe/Context/It)" >> "$report_file"
        ((issues++))
    fi
    
    if [[ $cleanup_patterns -le 5 ]]; then
        echo "  - Improve test cleanup patterns" >> "$report_file"
        ((issues++))
    fi
    
    if [[ $property_patterns -le 3 ]]; then
        echo "  - Add more property-based tests" >> "$report_file"
        ((issues++))
    fi
    
    if [[ $error_handling -le 20 ]]; then
        echo "  - Improve error handling in tests" >> "$report_file"
        ((issues++))
    fi
    
    if [[ $timeout_handling -le 5 ]]; then
        echo "  - Add proper timeout handling for async operations" >> "$report_file"
        ((issues++))
    fi
    
    if [[ $issues -eq 0 ]]; then
        echo "  - Test quality looks good!" >> "$report_file"
    fi
    
    cat "$report_file"
    
    if [[ $quality_score -ge 80 ]]; then
        log_success "Test quality check passed ($quality_score/100)"
        return 0
    else
        log_warning "Test quality check shows room for improvement ($quality_score/100)"
        return 1
    fi
}

# Function to validate test execution environment
validate_environment() {
    log_info "Validating test execution environment..."
    
    local report_file="${REPORT_DIR}/environment_validation_${TIMESTAMP}.txt"
    local issues=0
    
    echo "Environment Validation Report - $(date)" > "$report_file"
    echo "=====================================" >> "$report_file"
    
    # Check Go version
    echo "Go Environment:" >> "$report_file"
    if command -v go >/dev/null 2>&1; then
        local go_version
        go_version=$(go version)
        echo "  ✓ Go: $go_version" >> "$report_file"
    else
        echo "  ✗ Go: Not installed" >> "$report_file"
        ((issues++))
    fi
    
    # Check Docker
    echo "" >> "$report_file"
    echo "Container Environment:" >> "$report_file"
    if command -v docker >/dev/null 2>&1; then
        local docker_version
        docker_version=$(docker --version)
        echo "  ✓ Docker: $docker_version" >> "$report_file"
        
        # Check if Docker is running
        if docker info >/dev/null 2>&1; then
            echo "  ✓ Docker daemon: Running" >> "$report_file"
        else
            echo "  ⚠ Docker daemon: Not running" >> "$report_file"
            ((issues++))
        fi
    else
        echo "  ✗ Docker: Not installed" >> "$report_file"
        ((issues++))
    fi
    
    # Check Kind
    echo "" >> "$report_file"
    echo "Kubernetes Environment:" >> "$report_file"
    if command -v kind >/dev/null 2>&1; then
        local kind_version
        kind_version=$(kind version)
        echo "  ✓ Kind: $kind_version" >> "$report_file"
    else
        echo "  ⚠ Kind: Not installed" >> "$report_file"
    fi
    
    # Check kubectl
    if command -v kubectl >/dev/null 2>&1; then
        local kubectl_version
        kubectl_version=$(kubectl version --client --short 2>/dev/null || echo "kubectl installed")
        echo "  ✓ kubectl: $kubectl_version" >> "$report_file"
    else
        echo "  ⚠ kubectl: Not installed" >> "$report_file"
    fi
    
    # Check for existing Kind clusters
    if command -v kind >/dev/null 2>&1; then
        echo "" >> "$report_file"
        echo "Existing Kind Clusters:" >> "$report_file"
        local clusters
        clusters=$(kind get clusters 2>/dev/null || echo "none")
        if [[ "$clusters" == "none" ]]; then
            echo "  No Kind clusters found" >> "$report_file"
        else
            echo "$clusters" | while read -r cluster; do
                echo "  - $cluster" >> "$report_file"
            done
        fi
    fi
    
    # Check environment variables
    echo "" >> "$report_file"
    echo "Environment Variables:" >> "$report_file"
    
    local env_vars=(
        "E2E_PARALLEL_NODES"
        "E2E_TIMEOUT_MULTIPLIER"
        "CERT_MANAGER_INSTALL_SKIP"
        "METRICS_SERVER_INSTALL_SKIP"
        "KUBECONFIG"
    )
    
    for var in "${env_vars[@]}"; do
        if [[ -n "${!var:-}" ]]; then
            echo "  ✓ $var: ${!var}" >> "$report_file"
        else
            echo "  - $var: Not set (using defaults)" >> "$report_file"
        fi
    done
    
    echo "" >> "$report_file"
    echo "Issues found: $issues" >> "$report_file"
    
    cat "$report_file"
    
    if [[ $issues -eq 0 ]]; then
        log_success "Environment validation passed"
        return 0
    else
        log_warning "Environment validation found $issues issues"
        return 1
    fi
}

# Function to generate summary report
generate_summary() {
    local structure_result=$1
    local coverage_result=$2
    local quality_result=$3
    local environment_result=$4
    
    local summary_file="${REPORT_DIR}/summary_${TIMESTAMP}.txt"
    
    echo "OptipPod E2E Test Suite Quality Summary" > "$summary_file"
    echo "=======================================" >> "$summary_file"
    echo "Generated: $(date)" >> "$summary_file"
    echo "" >> "$summary_file"
    
    echo "Check Results:" >> "$summary_file"
    
    if [[ $structure_result -eq 0 ]]; then
        echo "  ✓ Test Structure: PASS" >> "$summary_file"
    else
        echo "  ✗ Test Structure: FAIL" >> "$summary_file"
    fi
    
    if [[ $coverage_result -eq 0 ]]; then
        echo "  ✓ Test Coverage: PASS" >> "$summary_file"
    else
        echo "  ⚠ Test Coverage: NEEDS IMPROVEMENT" >> "$summary_file"
    fi
    
    if [[ $quality_result -eq 0 ]]; then
        echo "  ✓ Test Quality: PASS" >> "$summary_file"
    else
        echo "  ⚠ Test Quality: NEEDS IMPROVEMENT" >> "$summary_file"
    fi
    
    if [[ $environment_result -eq 0 ]]; then
        echo "  ✓ Environment: PASS" >> "$summary_file"
    else
        echo "  ⚠ Environment: ISSUES FOUND" >> "$summary_file"
    fi
    
    local total_checks=4
    local passed_checks=0
    
    [[ $structure_result -eq 0 ]] && ((passed_checks++))
    [[ $coverage_result -eq 0 ]] && ((passed_checks++))
    [[ $quality_result -eq 0 ]] && ((passed_checks++))
    [[ $environment_result -eq 0 ]] && ((passed_checks++))
    
    local overall_score=$((passed_checks * 100 / total_checks))
    
    echo "" >> "$summary_file"
    echo "Overall Score: $overall_score% ($passed_checks/$total_checks checks passed)" >> "$summary_file"
    
    echo "" >> "$summary_file"
    echo "Report Files Generated:" >> "$summary_file"
    echo "  - Structure Check: structure_check_${TIMESTAMP}.txt" >> "$summary_file"
    echo "  - Coverage Analysis: coverage_analysis_${TIMESTAMP}.txt" >> "$summary_file"
    echo "  - Quality Metrics: quality_metrics_${TIMESTAMP}.txt" >> "$summary_file"
    echo "  - Environment Validation: environment_validation_${TIMESTAMP}.txt" >> "$summary_file"
    
    cat "$summary_file"
    
    if [[ $overall_score -ge 75 ]]; then
        log_success "Overall test suite quality: GOOD ($overall_score%)"
        return 0
    elif [[ $overall_score -ge 50 ]]; then
        log_warning "Overall test suite quality: FAIR ($overall_score%)"
        return 1
    else
        log_error "Overall test suite quality: POOR ($overall_score%)"
        return 2
    fi
}

# Main execution
main() {
    log_info "Starting OptipPod E2E Test Quality Monitoring"
    log_info "Report directory: $REPORT_DIR"
    
    # Change to e2e directory
    cd "$E2E_DIR"
    
    # Run all checks
    check_test_structure
    local structure_result=$?
    
    analyze_test_coverage
    local coverage_result=$?
    
    check_test_quality
    local quality_result=$?
    
    validate_environment
    local environment_result=$?
    
    # Generate summary
    generate_summary $structure_result $coverage_result $quality_result $environment_result
    local overall_result=$?
    
    log_info "Test quality monitoring completed"
    log_info "Reports saved in: $REPORT_DIR"
    
    exit $overall_result
}

# Run main function
main "$@"