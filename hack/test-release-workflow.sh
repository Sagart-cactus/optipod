#!/bin/bash
# Script to test the release workflow with a test tag

set -e

echo "=== Testing Release Workflow ==="

# Generate test version
TEST_VERSION="v0.0.0-test-$(date +%s)"

echo "Test version: $TEST_VERSION"
echo ""
echo "This script will:"
echo "1. Create a test tag: $TEST_VERSION"
echo "2. Push the tag to trigger the release workflow"
echo "3. Monitor the workflow execution"
echo ""
read -p "Continue? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted"
    exit 1
fi

# Create and push test tag
echo "Creating test tag..."
git tag -a "$TEST_VERSION" -m "Test release for CI/CD validation"

echo "Pushing test tag..."
git push origin "$TEST_VERSION"

echo ""
echo "✓ Test tag pushed successfully"
echo ""
echo "Monitor the workflow at:"
echo "https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')/actions"
echo ""
echo "After validation, delete the test tag and release:"
echo "  git tag -d $TEST_VERSION"
echo "  git push origin :refs/tags/$TEST_VERSION"
echo "  gh release delete $TEST_VERSION --yes"
