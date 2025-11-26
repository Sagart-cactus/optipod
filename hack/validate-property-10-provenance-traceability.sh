#!/bin/bash
# Property 10: Provenance traceability
# For any container image, the build provenance should contain the exact
# commit SHA, workflow run ID, and timestamp that produced it

set -e

echo "=== Property 10: Provenance Traceability Validation ==="

if [ -z "$1" ]; then
    echo "Usage: $0 <image:tag>"
    echo "Example: $0 ghcr.io/yourusername/optipod:v1.0.0"
    exit 1
fi

IMAGE="$1"

echo "Image: $IMAGE"
echo ""

# Check if cosign is installed
if ! command -v cosign &> /dev/null; then
    echo "Error: cosign is not installed"
    echo "Install from: https://docs.sigstore.dev/cosign/installation/"
    exit 1
fi

# Get provenance attestation
echo "Retrieving provenance attestation..."
export COSIGN_EXPERIMENTAL=1

if cosign verify-attestation --type slsaprovenance "$IMAGE" > /tmp/provenance.json 2>&1; then
    echo "✓ Provenance attestation found"
    echo ""
    
    # Extract provenance data
    COMMIT_SHA=$(jq -r '.payload' /tmp/provenance.json | base64 -d | jq -r '.predicate.materials[0].digest.sha1 // empty' 2>/dev/null)
    WORKFLOW_RUN=$(jq -r '.payload' /tmp/provenance.json | base64 -d | jq -r '.predicate.builder.id // empty' 2>/dev/null | grep -oE '[0-9]+$')
    TIMESTAMP=$(jq -r '.payload' /tmp/provenance.json | base64 -d | jq -r '.predicate.metadata.buildStartedOn // empty' 2>/dev/null)
    
    echo "Provenance details:"
    echo "  Commit SHA: ${COMMIT_SHA:-Not found}"
    echo "  Workflow Run ID: ${WORKFLOW_RUN:-Not found}"
    echo "  Build Timestamp: ${TIMESTAMP:-Not found}"
    echo ""
    
    # Validate required fields
    MISSING=0
    
    if [ -z "$COMMIT_SHA" ] || [ "$COMMIT_SHA" = "null" ]; then
        echo "✗ Commit SHA missing from provenance"
        MISSING=$((MISSING + 1))
    else
        echo "✓ Commit SHA present"
    fi
    
    if [ -z "$WORKFLOW_RUN" ] || [ "$WORKFLOW_RUN" = "null" ]; then
        echo "✗ Workflow Run ID missing from provenance"
        MISSING=$((MISSING + 1))
    else
        echo "✓ Workflow Run ID present"
    fi
    
    if [ -z "$TIMESTAMP" ] || [ "$TIMESTAMP" = "null" ]; then
        echo "✗ Timestamp missing from provenance"
        MISSING=$((MISSING + 1))
    else
        echo "✓ Timestamp present"
    fi
    
    echo ""
    if [ $MISSING -eq 0 ]; then
        echo "✓ Property 10 validated: All required provenance data is present"
        
        # Show full provenance for reference
        echo ""
        echo "Full provenance (first 50 lines):"
        jq -r '.payload' /tmp/provenance.json | base64 -d | jq . | head -50
        
        rm -f /tmp/provenance.json
        exit 0
    else
        echo "✗ Property 10 failed: $MISSING required field(s) missing from provenance"
        rm -f /tmp/provenance.json
        exit 1
    fi
else
    echo "✗ Failed to retrieve provenance attestation"
    echo ""
    cat /tmp/provenance.json
    echo ""
    echo "✗ Property 10 failed: No provenance attestation found"
    
    rm -f /tmp/provenance.json
    exit 1
fi
