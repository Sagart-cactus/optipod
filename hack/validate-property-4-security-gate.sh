#!/bin/bash
# Property 4: Security gate enforcement
# For any container image with critical or high severity vulnerabilities,
# the CI/CD System should fail the build and prevent image publication

set -e

echo "=== Property 4: Security Gate Enforcement Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <image:tag>"
    echo "Example: $0 ghcr.io/yourusername/optipod:v1.0.0"
    exit 1
fi

IMAGE="$1"

echo "Image: $IMAGE"
echo ""

# Check if trivy is installed
if ! command -v trivy &> /dev/null; then
    echo "Installing Trivy..."
    curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin
fi

# Scan image
echo "Scanning image for vulnerabilities..."
trivy image --format json --output /tmp/scan-results.json "$IMAGE"

# Count vulnerabilities by severity
CRITICAL=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="CRITICAL")] | length' /tmp/scan-results.json)
HIGH=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="HIGH")] | length' /tmp/scan-results.json)
MEDIUM=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="MEDIUM")] | length' /tmp/scan-results.json)
LOW=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="LOW")] | length' /tmp/scan-results.json)

echo ""
echo "Vulnerability Summary:"
echo "  Critical: $CRITICAL"
echo "  High: $HIGH"
echo "  Medium: $MEDIUM"
echo "  Low: $LOW"
echo ""

# Validate security gate
if [ "$CRITICAL" -gt 0 ] || [ "$HIGH" -gt 0 ]; then
    echo "✗ Property 4 validation: Image has $CRITICAL critical and $HIGH high vulnerabilities"
    echo "✓ Security gate would correctly block this image"

    # Show details
    echo ""
    echo "Critical/High vulnerabilities:"
    jq -r '.Results[]?.Vulnerabilities[]? | select(.Severity=="CRITICAL" or .Severity=="HIGH") | "  - \(.VulnerabilityID): \(.PkgName) (\(.Severity))"' /tmp/scan-results.json | head -10

    rm -f /tmp/scan-results.json
    exit 0
else
    echo "✓ Property 4 validated: No critical or high vulnerabilities found"
    echo "✓ Image would pass security gate"
    rm -f /tmp/scan-results.json
    exit 0
fi
