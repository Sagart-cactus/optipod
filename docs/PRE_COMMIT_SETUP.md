# Pre-commit Hooks Setup Guide

This document describes the pre-commit hooks configuration for the OptipPod project and how to use them.

## Overview

Pre-commit hooks are automated checks that run before each commit to ensure code quality, consistency, and security. They
help catch issues early and maintain high code standards across the project.

## Quick Start

```bash
# One-time setup
make setup-pre-commit

# That's it! Hooks will now run automatically on every commit
```

## What Gets Checked

### Go Code Quality

- **gofmt**: Automatically formats Go code
- **goimports**: Organizes and formats imports
- **go-mod-tidy**: Cleans up module dependencies
- **go-vet**: Runs static analysis
- **golangci-lint**: Comprehensive linting (28 linters)
- **go-unit-tests**: Runs unit tests (excludes e2e)

### General Code Quality

- **trailing-whitespace**: Removes trailing whitespace
- **end-of-file-fixer**: Ensures files end with newline
- **check-yaml**: Validates YAML syntax
- **check-json**: Validates JSON syntax
- **check-merge-conflict**: Detects merge conflict markers
- **check-added-large-files**: Prevents committing large files (>1MB)

### Security

- **detect-secrets**: Scans for potential secrets
- **shellcheck**: Analyzes shell scripts

### Documentation

- **yamllint**: Lints YAML files
- **markdownlint**: Lints Markdown files
- **hadolint**: Lints Dockerfiles

### Kubernetes

- **kubeval**: Validates Kubernetes manifests

## Installation

### Automatic Installation (Recommended)

```bash
make setup-pre-commit
```

This script will:

1. Install pre-commit if not already installed
2. Set up all configured hooks
3. Install required tools (golangci-lint, goimports)
4. Run initial checks on all files

### Manual Installation

```bash
# Install pre-commit
pip3 install --user pre-commit

# Install hooks
pre-commit install
pre-commit install --hook-type commit-msg

# Run on all files
pre-commit run --all-files
```

### PATH Configuration

If pre-commit is not found after installation, add the Python user bin directory to your PATH:

**Linux:**

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

**macOS:**

```bash
echo 'export PATH="$HOME/Library/Python/3.x/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Replace `3.x` with your Python version (e.g., `3.9`, `3.11`).

## Usage

### Automatic Execution

Once installed, hooks run automatically on every commit:

```bash
git add .
git commit -m "feat: add new feature"
# Hooks run automatically here
```

### Manual Execution

Run hooks manually without committing:

```bash
# Run all hooks on all files
make pre-commit-run

# Run all hooks on staged files only
pre-commit run

# Run specific hook
pre-commit run golangci-lint

# Run on specific files
pre-commit run --files path/to/file.go
```

### Skipping Hooks

**Not recommended**, but sometimes necessary:

```bash
# Skip all hooks for a commit
git commit --no-verify -m "commit message"

# Skip specific hook
SKIP=golangci-lint git commit -m "commit message"

# Skip multiple hooks
SKIP=golangci-lint,go-unit-tests git commit -m "commit message"
```

## Configuration Files

### `.pre-commit-config.yaml`

Main configuration file defining all hooks and their settings.

### `.yamllint.yml`

YAML linting rules (line length, indentation, etc.).

### `.markdownlint.yml`

Markdown linting rules (heading styles, line length, etc.).

### `.secrets.baseline`

Baseline file for detect-secrets to track known false positives.

### `.golangci.yml`

golangci-lint configuration (already exists in project).

## Updating Hooks

Pre-commit hooks should be updated periodically:

```bash
# Update to latest hook versions
make pre-commit-update

# Or manually
pre-commit autoupdate
```

## Troubleshooting

### Hook Fails on First Run

This is normal! Many hooks auto-fix issues. Just commit again:

```bash
git add .
git commit -m "your message"
# Hooks fail and fix issues
git add .
git commit -m "your message"
# Should pass now
```

### golangci-lint Not Found

```bash
# macOS
brew install golangci-lint

# Linux/macOS (alternative)
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
  sh -s -- -b $(go env GOPATH)/bin
```

### goimports Not Found

```bash
go install golang.org/x/tools/cmd/goimports@latest
```

### Pre-commit Command Not Found

Add Python user bin to PATH (see PATH Configuration above).

### Hooks Take Too Long

Some hooks can be slow on first run. Subsequent runs are faster due to caching.

To skip slow hooks temporarily:

```bash
SKIP=go-unit-tests git commit -m "message"
```

### False Positive in detect-secrets

If detect-secrets flags a false positive:

```bash
# Audit the finding
detect-secrets audit .secrets.baseline

# Mark as false positive when prompted
# Then commit the updated baseline
git add .secrets.baseline
git commit -m "chore: update secrets baseline"
```

## CI/CD Integration

Pre-commit checks also run in CI/CD:

- **GitHub Actions**: `.github/workflows/pre-commit.yml`
- Runs on all pull requests and pushes
- Ensures code quality before merge

## Best Practices

1. **Run hooks before pushing**: `make pre-commit-run`
2. **Don't skip hooks**: They catch real issues
3. **Update regularly**: `make pre-commit-update`
4. **Fix issues, don't ignore**: Understand why hooks fail
5. **Add new hooks**: As project needs evolve

## Adding New Hooks

To add a new hook:

1. Edit `.pre-commit-config.yaml`
2. Add the hook configuration
3. Run `pre-commit install`
4. Test with `pre-commit run --all-files`
5. Commit the configuration

Example:

```yaml
- repo: https://github.com/example/hook-repo
  rev: v1.0.0
  hooks:
    - id: my-new-hook
      name: My New Hook
      description: What it does
      args: ['--flag']
```

## Performance Tips

- Hooks run in parallel when possible
- Use `--files` to run on specific files only
- Cache is used to speed up subsequent runs
- Skip slow hooks during rapid development (but run before push)

## Getting Help

- **Documentation**: This file and [CONTRIBUTING.md](CONTRIBUTING.md)
- **Issues**: Report problems via GitHub Issues
- **Pre-commit docs**: https://pre-commit.com/

## Summary

Pre-commit hooks are your first line of defense against code quality issues. They:

- Save time by catching issues early
- Ensure consistency across the codebase
- Reduce code review burden
- Improve overall code quality

Make them part of your development workflow!
