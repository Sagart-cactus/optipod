#!/bin/bash
# Documentation build script
# Builds both docs/ (Markdown) and website/ (MDX) documentation

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "=== OptiPod Documentation Build ==="
echo ""

# 1. Validate documentation first
echo -e "${BLUE}Step 1: Validating documentation...${NC}"
"${SCRIPT_DIR}/validate-docs.sh"
echo ""

# 2. Build website
echo -e "${BLUE}Step 2: Building website...${NC}"
cd "${PROJECT_ROOT}/website"
if [ ! -d "node_modules" ]; then
    echo "Installing dependencies..."
    npm install
fi
echo "Building Astro site..."
npm run build
echo -e "${GREEN}✓ Website built successfully${NC}"
echo ""

# 3. Generate documentation index
echo -e "${BLUE}Step 3: Generating documentation index...${NC}"
cd "${PROJECT_ROOT}"
"${SCRIPT_DIR}/generate-docs-index.sh"
echo ""

echo -e "${GREEN}=== Documentation build complete! ===${NC}"
echo ""
echo "Documentation locations:"
echo "  - Markdown docs: ${PROJECT_ROOT}/docs/"
echo "  - Website build: ${PROJECT_ROOT}/website/dist/"
echo ""
