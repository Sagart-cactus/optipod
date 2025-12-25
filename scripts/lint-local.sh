#!/bin/bash

set -e

echo "🔍 Running local linting checks..."

echo "📝 Checking Go formatting..."
if [ "$(gofmt -l . | wc -l)" -gt 0 ]; then
    echo "❌ Go formatting issues found:"
    gofmt -l .
    echo "Run 'gofmt -w .' to fix"
    exit 1
fi

echo "📦 Checking Go imports..."
if [ "$(goimports -l . | wc -l)" -gt 0 ]; then
    echo "❌ Go import issues found:"
    goimports -l .
    echo "Run 'goimports -w .' to fix"
    exit 1
fi

echo "🔧 Running golangci-lint..."
golangci-lint run

echo "📄 Checking YAML files..."
yamllint -c .yamllint.yml .

echo "📖 Checking Markdown files..."
markdownlint -c .markdownlint.yml ./*.md docs/ || true

echo "✅ All checks passed!"
