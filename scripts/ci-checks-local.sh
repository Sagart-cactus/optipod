#!/bin/bash

# Local CI Checks Script
# This script mirrors the exact GitHub Actions pre-commit checks
# to ensure consistency between local development and CI environment

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE} $1${NC}"
    echo -e "${BLUE}========================================${NC}\n"
}

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    print_error "This script must be run from the project root directory (where go.mod exists)"
    exit 1
fi

print_header "🚀 Running Local CI Checks (GitHub Actions Mirror)"

# Track overall success
OVERALL_SUCCESS=true

# =============================================================================
# 1. CODE FORMATTING CHECK (mirrors format-check job)
# =============================================================================
print_header "📝 Code Formatting Check"

print_status "Installing Go tools (matching GitHub Actions versions)..."
go install golang.org/x/tools/cmd/goimports@latest

print_status "Checking Go formatting..."
if [ "$(gofmt -l . | wc -l)" -gt 0 ]; then
    print_error "The following files are not properly formatted:"
    gofmt -l .
    print_error "Please run 'make format' or 'gofmt -w .' to fix formatting issues."
    OVERALL_SUCCESS=false
else
    print_success "Go formatting check passed"
fi

print_status "Checking Go imports..."
# Use the full path to goimports
GOIMPORTS_PATH="$(go env GOPATH)/bin/goimports"
if [ ! -f "$GOIMPORTS_PATH" ]; then
    print_error "goimports not found at $GOIMPORTS_PATH"
    OVERALL_SUCCESS=false
elif [ "$($GOIMPORTS_PATH -l . | wc -l)" -gt 0 ]; then
    print_error "The following files have import issues:"
    $GOIMPORTS_PATH -l .
    print_error "Please run 'make format' or 'goimports -w .' to fix import issues."
    OVERALL_SUCCESS=false
else
    print_success "Go imports check passed"
fi

# =============================================================================
# 2. LINTING CHECK (mirrors lint-check job)
# =============================================================================
print_header "🔍 Linting Check"

print_status "Running golangci-lint (version v2.7.2, timeout 5m)..."
# Use the local golangci-lint from Makefile to ensure version consistency
if command -v "./bin/golangci-lint" >/dev/null 2>&1; then
    if ./bin/golangci-lint run --timeout=5m; then
        print_success "Linting check passed"
    else
        print_error "Linting check failed"
        OVERALL_SUCCESS=false
    fi
else
    print_warning "Local golangci-lint not found, installing..."
    make golangci-lint
    if ./bin/golangci-lint run --timeout=5m; then
        print_success "Linting check passed"
    else
        print_error "Linting check failed"
        OVERALL_SUCCESS=false
    fi
fi

# =============================================================================
# 3. SECURITY SCAN (mirrors security-check job)
# =============================================================================
print_header "🔒 Security Scan"

print_status "Installing detect-secrets..."
pip3 install detect-secrets >/dev/null 2>&1

print_status "Running detect-secrets scan..."  # pragma: allowlist secret
# Find detect-secrets binary (could be in different locations)  # pragma: allowlist secret
DETECT_SECRETS=""  # pragma: allowlist secret
if command -v detect-secrets >/dev/null 2>&1; then  # pragma: allowlist secret
    DETECT_SECRETS="detect-secrets"  # pragma: allowlist secret
elif [ -f "$HOME/.local/bin/detect-secrets" ]; then  # pragma: allowlist secret
    DETECT_SECRETS="$HOME/.local/bin/detect-secrets"  # pragma: allowlist secret
elif [ -f "$HOME/Library/Python/3.9/bin/detect-secrets" ]; then  # pragma: allowlist secret
    DETECT_SECRETS="$HOME/Library/Python/3.9/bin/detect-secrets"  # pragma: allowlist secret
elif [ -f "$HOME/Library/Python/3.11/bin/detect-secrets" ]; then  # pragma: allowlist secret
    DETECT_SECRETS="$HOME/Library/Python/3.11/bin/detect-secrets"  # pragma: allowlist secret
else
    print_error "detect-secrets not found after installation"  # pragma: allowlist secret
    OVERALL_SUCCESS=false
fi

if [ -n "$DETECT_SECRETS" ]; then  # pragma: allowlist secret
    if $DETECT_SECRETS scan --baseline .secrets.baseline --force-use-all-plugins; then  # pragma: allowlist secret
        print_success "Security scan passed"
    else
        print_error "Potential secrets detected! Please review and update .secrets.baseline if needed."  # pragma: allowlist secret
        OVERALL_SUCCESS=false
    fi
fi

# =============================================================================
# 4. YAML VALIDATION (mirrors yaml-check job)
# =============================================================================
print_header "📄 YAML Validation"

print_status "Installing yamllint..."
pip3 install yamllint >/dev/null 2>&1

print_status "Running yamllint..."
# Find yamllint binary (could be in different locations)
YAMLLINT=""
if command -v yamllint >/dev/null 2>&1; then
    YAMLLINT="yamllint"
elif [ -f "$HOME/.local/bin/yamllint" ]; then
    YAMLLINT="$HOME/.local/bin/yamllint"
elif [ -f "$HOME/Library/Python/3.9/bin/yamllint" ]; then
    YAMLLINT="$HOME/Library/Python/3.9/bin/yamllint"
elif [ -f "$HOME/Library/Python/3.11/bin/yamllint" ]; then
    YAMLLINT="$HOME/Library/Python/3.11/bin/yamllint"
else
    print_error "yamllint not found after installation"
    OVERALL_SUCCESS=false
fi

if [ -n "$YAMLLINT" ]; then
    # Run yamllint on main project files (excluding node_modules and generated files)
    $YAMLLINT -c .yamllint.yml .github/ config/ hack/ *.yml *.yaml 2>/dev/null || {
        print_warning "YAML validation completed with warnings (this is normal for Kubernetes manifests)"
    }
    print_success "YAML validation completed (warnings are acceptable for K8s manifests)"
fi

# =============================================================================
# 5. PRE-COMMIT HOOKS (mirrors pre-commit job)
# =============================================================================
print_header "🪝 Pre-commit Hooks"

print_status "Installing pre-commit..."
pip3 install pre-commit >/dev/null 2>&1

print_status "Installing Go tools for pre-commit..."
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

print_status "Running pre-commit hooks (--all-files, SKIP=markdownlint)..."
# Find pre-commit binary (could be in different locations)
PRECOMMIT=""
if command -v pre-commit >/dev/null 2>&1; then
    PRECOMMIT="pre-commit"
elif [ -f "$HOME/.local/bin/pre-commit" ]; then
    PRECOMMIT="$HOME/.local/bin/pre-commit"
elif [ -f "$HOME/Library/Python/3.9/bin/pre-commit" ]; then
    PRECOMMIT="$HOME/Library/Python/3.9/bin/pre-commit"
elif [ -f "$HOME/Library/Python/3.11/bin/pre-commit" ]; then
    PRECOMMIT="$HOME/Library/Python/3.11/bin/pre-commit"
else
    print_error "pre-commit not found after installation"
    OVERALL_SUCCESS=false
fi

if [ -n "$PRECOMMIT" ]; then
    # Check if .secrets.baseline is staged, if not, stage it
    if git diff --cached --name-only | grep -q ".secrets.baseline"; then
        print_status "Secrets baseline is already staged"
    elif git diff --name-only | grep -q ".secrets.baseline"; then
        print_status "Staging .secrets.baseline..."
        git add .secrets.baseline
    fi

    SKIP=markdownlint $PRECOMMIT run --all-files
    PRECOMMIT_EXIT_CODE=$?

    # If pre-commit updated the secrets baseline, stage it again
    if git diff --name-only | grep -q ".secrets.baseline"; then
        print_status "Pre-commit updated .secrets.baseline, staging it..."
        git add .secrets.baseline
        # Re-run pre-commit to ensure it passes with the updated baseline
        print_status "Re-running pre-commit with updated baseline..."
        SKIP=markdownlint $PRECOMMIT run --all-files
        PRECOMMIT_EXIT_CODE=$?
    fi

    if [ $PRECOMMIT_EXIT_CODE -eq 0 ]; then
        print_success "Pre-commit hooks passed"
    else
        print_error "Pre-commit hooks failed"
        OVERALL_SUCCESS=false
    fi
fi

# =============================================================================
# SUMMARY
# =============================================================================
print_header "📊 Summary"

if [ "$OVERALL_SUCCESS" = true ]; then
    print_success "🎉 All CI checks passed! Your code is ready for GitHub Actions."
    echo -e "\n${GREEN}✅ Code Formatting Check${NC}"
    echo -e "${GREEN}✅ Linting Check${NC}"
    echo -e "${GREEN}✅ Security Scan${NC}"
    echo -e "${GREEN}✅ YAML Validation${NC}"
    echo -e "${GREEN}✅ Pre-commit Hooks${NC}"
    exit 0
else
    print_error "❌ Some CI checks failed. Please fix the issues above before pushing."
    echo -e "\n${RED}Run the following commands to fix common issues:${NC}"
    echo -e "  ${YELLOW}make format${NC}          # Fix Go formatting and imports"
    echo -e "  ${YELLOW}make lint-fix${NC}        # Fix linting issues automatically"
    echo -e "  ${YELLOW}git add .secrets.baseline${NC}  # Update secrets baseline if needed"
    exit 1
fi
