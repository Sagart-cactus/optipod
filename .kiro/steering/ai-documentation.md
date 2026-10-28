---
inclusion: always
---

# AI-Generated Documentation Guidelines

## Documentation Storage

When creating summaries, analysis documents, or any AI-generated documentation during development work, **ALWAYS** store them in a dedicated folder to keep them separate from the project's official documentation.

### Required Location

All AI-generated documentation MUST be placed in:

```
.ai-docs/
```

This includes but is not limited to:
- Analysis documents (e.g., code reviews, security analysis)
- Summary documents (e.g., implementation summaries, migration summaries)
- Testing checklists and plans
- Workflow analysis and optimization documents
- Configuration coverage analysis
- Any temporary documentation created during development

### Folder Structure

Organize AI-generated docs by category:

```
.ai-docs/
├── analysis/          # Code reviews, security analysis, coverage reports
├── summaries/         # Implementation summaries, migration docs
├── checklists/        # Testing checklists, verification lists
├── workflows/         # Workflow analysis and optimization docs
└── planning/          # Test plans, roadmaps, design docs
```

### What NOT to Put in .ai-docs/

The following should remain in the project root or appropriate locations:
- Official README.md
- CONTRIBUTING.md, CODE_OF_CONDUCT.md, GOVERNANCE.md
- Official ROADMAP.md
- Release notes (RELEASE_NOTES_*.md)
- License files
- Official project documentation

### Gitignore

The `.ai-docs/` folder should be added to `.gitignore` to prevent accidental commits:

```gitignore
# AI-generated documentation (temporary)
.ai-docs/
```

## Why This Matters

1. **Keeps the repository clean** - Separates working documents from official documentation
2. **Prevents confusion** - Users know which docs are official vs. temporary
3. **Easy cleanup** - Can delete the entire folder when no longer needed
4. **Better organization** - All AI-generated content in one place

## Example

❌ **Wrong:**
```
HELM_CONFIG_COVERAGE_ANALYSIS.md
PROMETHEUS_CREDENTIALS_ANALYSIS.md
CONTROLLER_DEPLOYMENT_REVIEW.md
```

✅ **Correct:**
```
.ai-docs/analysis/helm-config-coverage.md
.ai-docs/analysis/prometheus-credentials.md
.ai-docs/analysis/controller-deployment-review.md
```
