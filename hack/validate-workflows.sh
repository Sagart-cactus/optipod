#!/bin/bash
# Script to validate GitHub Actions workflow YAML syntax

set -e

echo "=== Validating GitHub Actions Workflows ==="

# Check if actionlint is installed
if ! command -v actionlint &> /dev/null; then
    echo "Installing actionlint..."
    go install github.com/rhysd/actionlint/cmd/actionlint@latest
fi

# Validate all workflow files
echo "Validating workflow files..."
actionlint .github/workflows/*.yml

echo "✓ All workflow files are valid"
