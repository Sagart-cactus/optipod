#!/bin/bash

# verify-release-separation.sh
# Verifies that contract E2E tests are properly separated from the release pipeline

set -e

echo "🔍 Verifying release pipeline separation..."

# Check that release.yml does not reference contract tests
if grep -q "test-e2e-contract" .github/workflows/release.yml; then
    echo "❌ ERROR: release.yml references contract tests - this violates separation requirement"
    exit 1
fi

# Check that release.yml uses comprehensive E2E tests
if ! grep -q "make test-e2e" .github/workflows/release.yml; then
    echo "❌ ERROR: release.yml does not run comprehensive E2E tests"
    exit 1
fi

# Check that e2e.yml does not have release triggers
if grep -q "push:" .github/workflows/e2e.yml && grep -q "tags:" .github/workflows/e2e.yml; then
    echo "❌ ERROR: e2e.yml has release triggers - this violates separation requirement"
    exit 1
fi

# Check that e2e.yml only has allowed triggers
if ! grep -q "workflow_dispatch:" .github/workflows/e2e.yml; then
    echo "❌ ERROR: e2e.yml missing workflow_dispatch trigger"
    exit 1
fi

if ! grep -q "schedule:" .github/workflows/e2e.yml; then
    echo "❌ ERROR: e2e.yml missing schedule trigger"
    exit 1
fi

# Check that contract tests are documented as non-blocking
if ! grep -q "do NOT block releases" .github/workflows/e2e.yml; then
    echo "❌ ERROR: e2e.yml does not clearly state that failures are non-blocking"
    exit 1
fi

echo "✅ Release pipeline separation verified:"
echo "   - release.yml uses comprehensive E2E tests (make test-e2e)"
echo "   - release.yml does NOT reference contract tests"
echo "   - e2e.yml only triggers on manual/scheduled basis"
echo "   - e2e.yml clearly states failures do not block releases"
echo "   - Contract tests are properly isolated from release critical path"
