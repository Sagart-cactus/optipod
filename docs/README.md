# OptiPod Documentation

Welcome to the OptiPod documentation! This directory contains the Markdown version of the documentation, organized following the [Diátaxis framework](https://diataxis.fr/).

## Documentation Structure

The documentation is organized into five main categories:

### 📚 Getting Started
**Learning-oriented tutorials** to help you get started with OptiPod.

- `getting-started/installation.md` - How to install OptiPod
- `getting-started/quick-start.md` - Quick start guide
- `getting-started/first-policy.md` - Creating your first optimization policy

### 💡 Concepts
**Understanding-oriented explanations** of core concepts and architecture.

- `concepts/architecture.md` - OptiPod architecture overview
- `concepts/modes.md` - Operational modes (Auto, Recommend, Disabled)
- `concepts/safety-model.md` - Safety guarantees and bounds
- `concepts/update-strategies.md` - SSA vs Webhook strategies

### 📖 Guides
**Task-oriented how-to guides** for accomplishing specific goals.

- `guides/creating-policies.md` - How to create optimization policies
- `guides/reviewing-recs.md` - How to review recommendations
- `guides/switching-to-auto.md` - How to switch to Auto mode
- `guides/troubleshooting.md` - Troubleshooting common issues
- `guides/gitops-integration.md` - GitOps integration guide

### 📋 Reference
**Information-oriented reference** documentation for APIs, CRDs, and configuration.

- `reference/crd-spec.md` - Complete CRD specification
- `reference/annotations.md` - Annotation reference
- `reference/metrics.md` - Prometheus metrics reference
- `reference/cli-tools.md` - CLI tools and scripts reference
- `reference/helm-values.md` - Helm chart values reference

### 🔧 Advanced
**Advanced topics** for experienced users and operators.

- `advanced/rbac.md` - RBAC configuration
- `advanced/webhook-config.md` - Webhook configuration
- `advanced/operations.md` - Operational procedures
- `advanced/observability.md` - Observability and monitoring
- `advanced/performance.md` - Performance tuning

## Dual Documentation

OptiPod maintains documentation in two formats:

1. **Markdown** (this directory) - For developers and contributors
2. **MDX** (`website/src/content/docs/`) - For the website

Both versions are kept synchronized and contain the same information, adapted for their respective mediums.

## Building Documentation

To build and validate the documentation:

```bash
# Validate documentation
./scripts/validate-docs.sh

# Build website
./scripts/build-docs.sh

# Generate documentation index
./scripts/generate-docs-index.sh
```

## Contributing to Documentation

When contributing to documentation:

1. **Update both formats** - Always update both Markdown (docs/) and MDX (website/) versions
2. **Follow Diátaxis** - Place content in the appropriate category
3. **Use examples from code** - Extract examples from actual code, tests, and configuration files
4. **Validate** - Run validation scripts before submitting
5. **Test code examples** - Ensure all code examples are executable

See [CONTRIBUTING.md](../CONTRIBUTING.md) for more details.

## Documentation Index

See [INDEX.md](INDEX.md) for a complete index of all documentation files.

## Questions or Issues?

If you find issues with the documentation or have suggestions:

- Open an issue: https://github.com/Sagart-cactus/optipod/issues
- Submit a PR: https://github.com/Sagart-cactus/optipod/pulls

---

*Documentation follows the [Diátaxis framework](https://diataxis.fr/) for optimal user experience.*
