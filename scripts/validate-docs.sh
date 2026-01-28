#!/bin/bash
# Documentation validation script
# Validates documentation accuracy and completeness

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
ERRORS=0
WARNINGS=0

echo "=== OptiPod Documentation Validation ==="
echo ""

# Function to report error
error() {
    echo -e "${RED}ERROR:${NC} $1"
    ((ERRORS++))
}

# Function to report warning
warning() {
    echo -e "${YELLOW}WARNING:${NC} $1"
    ((WARNINGS++))
}

# Function to report success
success() {
    echo -e "${GREEN}✓${NC} $1"
}

# 1. Validate script references
echo "Checking script references..."
SCRIPT_REFS=$(grep -r "scripts/" docs/ website/ 2>/dev/null | grep -v ".git" | grep -v "node_modules" || true)
if [ -n "$SCRIPT_REFS" ]; then
    while IFS= read -r line; do
        # Extract script name from the line (macOS compatible)
        SCRIPT_NAME=$(echo "$line" | grep -o 'scripts/[a-zA-Z0-9_-]*\.\(sh\|bash\)' | head -1)
        if [ -n "$SCRIPT_NAME" ]; then
            if [ ! -f "${PROJECT_ROOT}/${SCRIPT_NAME}" ]; then
                error "Script reference not found: ${SCRIPT_NAME}"
            fi
        fi
    done <<< "$SCRIPT_REFS"
fi
success "Script references validated"

# 2. Validate internal links in docs/
echo "Checking internal links in docs/..."
BROKEN_LINKS=0
while IFS= read -r file; do
    # Extract markdown links [text](path) - macOS compatible
    LINKS=$(grep -o '\[.*\]([^)]*)' "$file" 2>/dev/null | sed 's/.*](\([^)]*\)).*/\1/' || true)
    while IFS= read -r link; do
        # Skip empty links
        if [ -z "$link" ]; then
            continue
        fi
        # Skip external links
        if [[ "$link" =~ ^https?:// ]]; then
            continue
        fi
        # Skip anchors
        if [[ "$link" =~ ^# ]]; then
            continue
        fi
        # Check if file exists (relative to docs/)
        LINK_PATH=$(dirname "$file")/"$link"
        if [ ! -f "$LINK_PATH" ] && [ ! -d "$LINK_PATH" ]; then
            warning "Broken link in $file: $link"
            ((BROKEN_LINKS++))
        fi
    done <<< "$LINKS"
done < <(find docs/ -name "*.md" 2>/dev/null)
if [ "$BROKEN_LINKS" -eq 0 ]; then
    success "Internal links validated in docs/"
fi

# 3. Validate internal links in website/
echo "Checking internal links in website/..."
BROKEN_LINKS=0
while IFS= read -r file; do
    # Extract markdown links [text](path) - macOS compatible
    LINKS=$(grep -o '\[.*\]([^)]*)' "$file" 2>/dev/null | sed 's/.*](\([^)]*\)).*/\1/' || true)
    while IFS= read -r link; do
        # Skip empty links
        if [ -z "$link" ]; then
            continue
        fi
        # Skip external links
        if [[ "$link" =~ ^https?:// ]]; then
            continue
        fi
        # Skip anchors
        if [[ "$link" =~ ^# ]]; then
            continue
        fi
        # Skip relative paths starting with /
        if [[ "$link" =~ ^/ ]]; then
            continue
        fi
        # Check if file exists (relative to website/src/content/docs/)
        LINK_PATH=$(dirname "$file")/"$link"
        if [ ! -f "$LINK_PATH" ] && [ ! -d "$LINK_PATH" ]; then
            warning "Broken link in $file: $link"
            ((BROKEN_LINKS++))
        fi
    done <<< "$LINKS"
done < <(find website/src/content/docs -name "*.mdx" 2>/dev/null)
if [ "$BROKEN_LINKS" -eq 0 ]; then
    success "Internal links validated in website/"
fi

# 4. Check for duplicate content structure
echo "Checking documentation structure..."
DOCS_STRUCTURE=$(find docs/ -type d 2>/dev/null | sort)
WEBSITE_STRUCTURE=$(find website/src/content/docs/docs -type d 2>/dev/null | sed 's|website/src/content/docs/docs|docs|' | sort)

# Verify key directories exist
REQUIRED_DIRS=(
    "docs/getting-started"
    "docs/concepts"
    "docs/guides"
    "docs/reference"
    "docs/advanced"
)

for dir in "${REQUIRED_DIRS[@]}"; do
    if [ ! -d "$dir" ]; then
        warning "Missing directory: $dir"
    else
        success "Directory exists: $dir"
    fi
done

# 5. Validate YAML frontmatter in MDX files
echo "Checking MDX frontmatter..."
while IFS= read -r file; do
    # Check if file has frontmatter
    if ! head -1 "$file" | grep -q "^---$"; then
        warning "Missing frontmatter in: $file"
    fi
done < <(find website/src/content/docs -name "*.mdx" 2>/dev/null)
success "MDX frontmatter validated"

# Summary
echo ""
echo "=== Validation Summary ==="
echo "Errors: $ERRORS"
echo "Warnings: $WARNINGS"

if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}Validation failed with $ERRORS errors${NC}"
    exit 1
elif [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}Validation completed with $WARNINGS warnings${NC}"
    exit 0
else
    echo -e "${GREEN}All validations passed!${NC}"
    exit 0
fi
