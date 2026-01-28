#!/bin/bash
# Documentation synchronization checker
# Verifies that docs/ and website/ documentation are in sync

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== Documentation Synchronization Check ==="
echo ""

SYNC_ISSUES=0

# Function to check if corresponding file exists
check_sync() {
    local md_file=$1
    local category=$2
    local filename=$(basename "$md_file" .md)

    # Construct expected MDX path
    local mdx_file="${PROJECT_ROOT}/website/src/content/docs/docs/${category}/${filename}.mdx"

    if [ ! -f "$mdx_file" ]; then
        echo -e "${YELLOW}Missing MDX version:${NC} $md_file -> $mdx_file"
        ((SYNC_ISSUES++))
        return 1
    fi

    # Check if both files have similar content length (rough check)
    local md_lines=$(wc -l < "$md_file")
    local mdx_lines=$(wc -l < "$mdx_file")

    # Allow 20% difference (accounting for frontmatter and formatting)
    local diff=$((md_lines - mdx_lines))
    local abs_diff=${diff#-}
    local threshold=$((md_lines / 5))

    if [ $abs_diff -gt $threshold ]; then
        echo -e "${YELLOW}Content length mismatch:${NC} $md_file ($md_lines lines) vs $mdx_file ($mdx_lines lines)"
        ((SYNC_ISSUES++))
        return 1
    fi

    return 0
}

# Check each category
CATEGORIES=("getting-started" "concepts" "guides" "reference" "advanced")

for category in "${CATEGORIES[@]}"; do
    echo "Checking ${category}..."

    md_dir="${PROJECT_ROOT}/docs/${category}"
    if [ ! -d "$md_dir" ]; then
        echo -e "${YELLOW}Directory not found:${NC} $md_dir"
        continue
    fi

    for md_file in "$md_dir"/*.md; do
        if [ -f "$md_file" ]; then
            if check_sync "$md_file" "$category"; then
                echo -e "${GREEN}✓${NC} $(basename "$md_file")"
            fi
        fi
    done
    echo ""
done

# Check for orphaned MDX files
echo "Checking for orphaned MDX files..."
for category in "${CATEGORIES[@]}"; do
    mdx_dir="${PROJECT_ROOT}/website/src/content/docs/docs/${category}"
    if [ ! -d "$mdx_dir" ]; then
        continue
    fi

    for mdx_file in "$mdx_dir"/*.mdx; do
        if [ -f "$mdx_file" ]; then
            filename=$(basename "$mdx_file" .mdx)
            md_file="${PROJECT_ROOT}/docs/${category}/${filename}.md"

            if [ ! -f "$md_file" ]; then
                echo -e "${YELLOW}Orphaned MDX file:${NC} $mdx_file (no corresponding .md file)"
                ((SYNC_ISSUES++))
            fi
        fi
    done
done

echo ""
echo "=== Synchronization Summary ==="
if [ $SYNC_ISSUES -eq 0 ]; then
    echo -e "${GREEN}All documentation is synchronized!${NC}"
    exit 0
else
    echo -e "${YELLOW}Found $SYNC_ISSUES synchronization issues${NC}"
    echo ""
    echo "To fix synchronization issues:"
    echo "  1. Create missing MDX files from Markdown files"
    echo "  2. Update content to match between versions"
    echo "  3. Remove orphaned files"
    exit 1
fi
