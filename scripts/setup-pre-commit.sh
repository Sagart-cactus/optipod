#!/bin/bash

# Setup script for pre-commit hooks in OptipPod project
# This script installs pre-commit and sets up the hooks

set -euo pipefail

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

# Function to find pre-commit executable
find_precommit() {
    # Try common locations
    local locations=(
        "pre-commit"
        "$HOME/.local/bin/pre-commit"
        "$HOME/Library/Python/3.9/bin/pre-commit"
        "$HOME/Library/Python/3.10/bin/pre-commit"
        "$HOME/Library/Python/3.11/bin/pre-commit"
        "$HOME/Library/Python/3.12/bin/pre-commit"
        "/usr/local/bin/pre-commit"
    )

    for location in "${locations[@]}"; do
        if command -v "$location" &> /dev/null; then
            echo "$location"
            return 0
        fi
    done

    return 1
}

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    print_error "This script must be run from within a git repository"
    exit 1
fi

print_status "Setting up pre-commit hooks for OptipPod..."

# Check if Python is available
if ! command -v python3 &> /dev/null; then
    print_error "Python 3 is required but not installed. Please install Python 3 first."
    exit 1
fi

# Check if pip is available
if ! command -v pip3 &> /dev/null; then
    print_error "pip3 is required but not installed. Please install pip3 first."
    exit 1
fi

# Find or install pre-commit
PRECOMMIT_CMD=""
if PRECOMMIT_CMD=$(find_precommit); then
    print_status "pre-commit found at: $PRECOMMIT_CMD"
else
    print_status "Installing pre-commit..."
    pip3 install --user pre-commit

    # Try to find it again after installation
    if PRECOMMIT_CMD=$(find_precommit); then
        print_success "pre-commit installed successfully at: $PRECOMMIT_CMD"
    else
        print_error "Failed to find pre-commit after installation. Please add Python user bin directory to PATH."
        print_status "Try adding this to your shell profile:"
        print_status "  export PATH=\"\$HOME/.local/bin:\$PATH\"  # Linux"
        print_status "  export PATH=\"\$HOME/Library/Python/3.x/bin:\$PATH\"  # macOS"
        exit 1
    fi
fi

# Install pre-commit hooks
print_status "Installing pre-commit hooks..."
"$PRECOMMIT_CMD" install

# Install commit-msg hook for conventional commits (optional)
print_status "Installing commit-msg hook..."
"$PRECOMMIT_CMD" install --hook-type commit-msg

# Run pre-commit on all files to ensure everything works
print_status "Running pre-commit on all files to verify setup..."
if "$PRECOMMIT_CMD" run --all-files; then
    print_success "All pre-commit hooks passed!"
else
    print_warning "Some pre-commit hooks failed. This is normal for the first run."
    print_status "The hooks will automatically fix most issues on the next commit."
fi

# Install additional tools if not present
print_status "Checking for additional required tools..."

# Check for golangci-lint
if ! command -v golangci-lint &> /dev/null; then
    print_warning "golangci-lint not found. Installing..."
    if command -v brew &> /dev/null; then
        brew install golangci-lint
    else
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)/bin"
    fi
    print_success "golangci-lint installed"
fi

# Check for goimports
if ! command -v goimports &> /dev/null; then
    print_warning "goimports not found. Installing..."
    go install golang.org/x/tools/cmd/goimports@latest
    print_success "goimports installed"
fi

print_success "Pre-commit setup completed successfully!"
print_status "From now on, pre-commit hooks will run automatically on every commit."
print_status ""
print_status "Useful commands:"
print_status "  $PRECOMMIT_CMD run --all-files    # Run all hooks on all files"
print_status "  $PRECOMMIT_CMD run <hook-name>    # Run specific hook"
print_status "  $PRECOMMIT_CMD autoupdate         # Update hook versions"
print_status "  $PRECOMMIT_CMD uninstall          # Remove hooks (if needed)"
print_status ""
print_status "To skip hooks for a specific commit (not recommended):"
print_status "  git commit --no-verify -m 'commit message'"
