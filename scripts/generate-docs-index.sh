#!/bin/bash
# Generate documentation index/sitemap
# Creates an index of all documentation files

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

OUTPUT_FILE="${PROJECT_ROOT}/docs/INDEX.md"

echo "Generating documentation index..."

cat > "$OUTPUT_FILE" << 'EOF'
# OptiPod Documentation Index

This index provides a complete overview of all available documentation.

## Getting Started

Documentation to help you get started with OptiPod quickly.

EOF

# Add getting-started files
if [ -d "${PROJECT_ROOT}/docs/getting-started" ]; then
    for file in "${PROJECT_ROOT}/docs/getting-started"/*.md; do
        if [ -f "$file" ]; then
            filename=$(basename "$file" .md)
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](getting-started/${filename}.md)" >> "$OUTPUT_FILE"
        fi
    done
fi

cat >> "$OUTPUT_FILE" << 'EOF'

## Concepts

Core concepts and architecture documentation.

EOF

# Add concepts files
if [ -d "${PROJECT_ROOT}/docs/concepts" ]; then
    for file in "${PROJECT_ROOT}/docs/concepts"/*.md; do
        if [ -f "$file" ]; then
            filename=$(basename "$file" .md)
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](concepts/${filename}.md)" >> "$OUTPUT_FILE"
        fi
    done
fi

cat >> "$OUTPUT_FILE" << 'EOF'

## Guides

Task-oriented guides for accomplishing specific goals.

EOF

# Add guides files
if [ -d "${PROJECT_ROOT}/docs/guides" ]; then
    for file in "${PROJECT_ROOT}/docs/guides"/*.md; do
        if [ -f "$file" ]; then
            filename=$(basename "$file" .md)
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](guides/${filename}.md)" >> "$OUTPUT_FILE"
        fi
    done
fi

cat >> "$OUTPUT_FILE" << 'EOF'

## Reference

Complete reference documentation for APIs, CRDs, and configuration.

EOF

# Add reference files
if [ -d "${PROJECT_ROOT}/docs/reference" ]; then
    for file in "${PROJECT_ROOT}/docs/reference"/*.md; do
        if [ -f "$file" ]; then
            filename=$(basename "$file" .md)
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](reference/${filename}.md)" >> "$OUTPUT_FILE"
        fi
    done
fi

cat >> "$OUTPUT_FILE" << 'EOF'

## Advanced Topics

Advanced configuration and operational documentation.

EOF

# Add advanced files
if [ -d "${PROJECT_ROOT}/docs/advanced" ]; then
    for file in "${PROJECT_ROOT}/docs/advanced"/*.md; do
        if [ -f "$file" ]; then
            filename=$(basename "$file" .md)
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](advanced/${filename}.md)" >> "$OUTPUT_FILE"
        fi
    done
fi

cat >> "$OUTPUT_FILE" << 'EOF'

## Other Documentation

Additional documentation files.

EOF

# Add root-level docs
for file in "${PROJECT_ROOT}/docs"/*.md; do
    if [ -f "$file" ]; then
        filename=$(basename "$file")
        # Skip INDEX.md itself
        if [ "$filename" != "INDEX.md" ]; then
            title=$(grep -m 1 "^# " "$file" | sed 's/^# //' || echo "$filename")
            echo "- [${title}](${filename})" >> "$OUTPUT_FILE"
        fi
    fi
done

cat >> "$OUTPUT_FILE" << 'EOF'

---

*This index is automatically generated. Run `scripts/generate-docs-index.sh` to update.*
EOF

echo "Documentation index generated: $OUTPUT_FILE"
