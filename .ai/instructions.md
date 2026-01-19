# AI Assistant Instructions for OptiPod

This file provides instructions for AI assistants (CLI tools, IDEs, and other interfaces) working on the OptiPod project.

## Quick Reference

- **Project Type**: Kubernetes Operator (Go)
- **Framework**: Kubebuilder/controller-runtime
- **Deployment**: Helm charts
- **CI Script**: `./scripts/ci-checks-local.sh` (run before every commit)

## Git Workflow (CRITICAL)

### Always Create New Branches
**NEVER push directly to `main` or current branch.**

```bash
# 1. Run CI checks FIRST
./scripts/ci-checks-local.sh

# 2. Create branch
git checkout -b <type>/<description>

# 3. Commit with conventional format
git commit -m "type: description"

# 4. Push to new branch
git push -u origin <branch-name>

# 5. Create PR
gh pr create --title "..." --body "..."
```

### Branch Naming
- `feature/` - New features
- `fix/` - Bug fixes
- `chore/` - Maintenance, documentation
- `refactor/` - Code refactoring
- `test/` - Test changes

### Commit Format
```
<type>: <short description>

<optional longer description>
```

Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`

### Exceptions
Only push to current branch when:
- User explicitly says "push to current branch"
- Already on a feature branch adding more commits
- Fixing issues in existing PR

## AI-Generated Documentation

**ALWAYS store in `.ai-docs/` folder:**

```
.ai-docs/
├── analysis/          # Code reviews, security analysis
├── summaries/         # Implementation summaries
├── checklists/        # Testing checklists
├── workflows/         # Workflow analysis
└── planning/          # Test plans, design docs
```

**NEVER create AI-generated docs in project root.**

## Documentation Updates

**ALWAYS update BOTH locations:**

1. `docs/` - Markdown for developers
2. `website/` - HTML for end users

### Common Mappings
| Topic | docs/ | website/ |
|-------|-------|----------|
| Installation | `docs/installation.md` | `website/docs/installation/` |
| Configuration | `docs/configuration.md` | `website/docs/configuration/` |
| Prometheus Auth | `docs/prometheus-auth.md` | `website/docs/prometheus-authentication/` |
| Getting Started | `docs/getting-started.md` | `website/docs/getting-started/` |

### Workflow
1. Update Markdown in `docs/`
2. Update HTML in `website/`
3. Keep content synchronized
4. Commit both together

**Exception:** Only if user explicitly says "only update docs/" or "only update website/"

## Code Quality

### Go Code
- Follow standard Go conventions
- Use `golangci-lint`
- Write tests for new features
- Add comments for complex logic

### Helm Charts
- Validate: `helm lint charts/optipod`
- Test: `helm template charts/optipod`
- Document all values
- Use semantic versioning

### Testing
```bash
make test           # Unit tests
make test-e2e       # E2E tests
make lint           # Linting
./scripts/ci-checks-local.sh  # All CI checks
```

## Project Structure

```
optipod/
├── api/                    # CRD definitions
├── cmd/                    # Main applications
├── internal/               # Internal packages
│   ├── controller/        # Kubernetes controllers
│   ├── webhook/           # Admission webhook
│   └── metrics/           # Metrics providers
├── charts/optipod/        # Helm chart
├── config/                # Kubernetes manifests
├── docs/                  # Developer documentation (Markdown)
├── website/               # User documentation (HTML)
├── scripts/               # Build and utility scripts
├── test/                  # E2E tests
└── .ai-docs/             # AI-generated docs (gitignored)
```

## Development Commands

```bash
make test              # Run unit tests
make test-e2e          # Run E2E tests
make build             # Build binary
make docker-build      # Build Docker image
make install           # Install CRDs
make deploy            # Deploy to cluster
make run               # Run locally
make lint              # Lint code
./scripts/ci-checks-local.sh  # Run all CI checks
```

## Important Constraints

### What OptiPod Will NOT Do
- ❌ Mutate workloads without explicit opt-in
- ❌ Override GitOps ownership
- ❌ Blindly reduce memory (OOMKill risk)
- ❌ Require SaaS backend
- ❌ Make unexplainable recommendations

### Safety Guarantees
- Default to recommend mode
- Require explicit policy configuration
- Respect GitOps patterns
- Provide clear audit trail

## When Adding Features

1. Add corresponding Helm values
2. Update documentation (both `docs/` and `website/`)
3. Add examples in `charts/optipod/examples/`
4. Consider backward compatibility
5. Update ROADMAP.md if significant
6. Run `./scripts/ci-checks-local.sh` before commit

## Getting Help

- Documentation: `docs/` folder
- Examples: `charts/optipod/examples/`
- Context: `.ai/context.md`
- Contributing: `CONTRIBUTING.md`

## For CLI Tools

To use these instructions with CLI tools:

```bash
# Claude Code CLI (example)
claude-code --instructions .ai/instructions.md

# Or pipe the content
cat .ai/instructions.md | your-ai-cli --system-prompt -

# Or reference in config
your-ai-cli --config .ai/instructions.md
```

Most AI CLI tools support passing custom instruction files via flags or configuration.
