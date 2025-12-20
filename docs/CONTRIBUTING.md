# Contributing to OptipPod

Thank you for your interest in contributing to OptipPod! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Pre-commit Hooks](#pre-commit-hooks)
- [Code Quality Standards](#code-quality-standards)
- [Testing Guidelines](#testing-guidelines)
- [Submitting Changes](#submitting-changes)

## Code of Conduct

This project adheres to a code of conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally
3. Set up the development environment
4. Create a feature branch
5. Make your changes
6. Test your changes
7. Submit a pull request

## Development Setup

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- kubectl
- Kind (for local Kubernetes testing)
- Python 3.11+ (for pre-commit hooks)

### Initial Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/optipod.git
cd optipod

# Set up pre-commit hooks (recommended)
make setup-pre-commit

# Install dependencies
go mod download

# Build the project
make build

# Run tests
make test
```

## Pre-commit Hooks

We use pre-commit hooks to ensure code quality and consistency. These hooks automatically run checks before each commit.

### Automatic Setup

```bash
# Install and configure pre-commit hooks
make setup-pre-commit
```

This will:
- Install pre-commit if not already installed
- Set up all configured hooks
- Install required tools (golangci-lint, goimports, etc.)
- Run initial checks on all files

### Manual Setup

If you prefer manual setup:

```bash
# Install pre-commit
pip3 install pre-commit

# Install hooks
pre-commit install
pre-commit install --hook-type commit-msg

# Run on all files
pre-commit run --all-files
```

### Available Hooks

Our pre-commit configuration includes:

#### Go Code Quality
- **gofmt**: Formats Go code according to standard conventions
- **goimports**: Organizes and formats Go imports
- **go-mod-tidy**: Cleans up Go module dependencies
- **go-vet**: Runs static analysis on Go code
- **golangci-lint**: Comprehensive Go linting with multiple analyzers
- **go-unit-tests**: Runs unit tests (excludes e2e tests)

#### General Code Quality
- **trailing-whitespace**: Removes trailing whitespace
- **end-of-file-fixer**: Ensures files end with newline
- **check-yaml**: Validates YAML syntax
- **check-json**: Validates JSON syntax
- **check-merge-conflict**: Detects merge conflict markers

#### Security
- **detect-secrets**: Scans for potential secrets in code
- **shellcheck**: Analyzes shell scripts for issues

#### Documentation
- **yamllint**: Lints YAML files for style and syntax
- **markdownlint**: Lints Markdown files for consistency
- **hadolint**: Lints Dockerfiles for best practices

#### Kubernetes
- **kubeval**: Validates Kubernetes YAML manifests

### Running Hooks Manually

```bash
# Run all hooks on all files
make pre-commit-run

# Run specific hook
pre-commit run golangci-lint

# Run on specific files
pre-commit run --files path/to/file.go

# Update hook versions
make pre-commit-update
```

### Skipping Hooks (Not Recommended)

In rare cases, you may need to skip hooks:

```bash
# Skip all hooks for a commit (use sparingly)
git commit --no-verify -m "commit message"

# Skip specific hook
SKIP=golangci-lint git commit -m "commit message"
```

## Code Quality Standards

### Go Code Standards

- Follow standard Go formatting (enforced by gofmt)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions small and focused
- Handle errors appropriately
- Write tests for new functionality

### Linting Rules

We use golangci-lint with the following key rules:
- **errcheck**: Check for unchecked errors
- **gofmt**: Ensure proper formatting
- **goimports**: Ensure proper import organization
- **govet**: Run go vet static analysis
- **ineffassign**: Detect ineffectual assignments
- **misspell**: Detect commonly misspelled words
- **staticcheck**: Advanced static analysis

### Documentation Standards

- Use clear, concise language
- Include code examples where helpful
- Keep documentation up to date with code changes
- Follow Markdown best practices

## Testing Guidelines

### Unit Tests

- Write unit tests for all new functionality
- Aim for high test coverage (>80%)
- Use table-driven tests where appropriate
- Mock external dependencies

```bash
# Run unit tests
make test

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...
```

### E2E Tests

- Write E2E tests for critical user workflows
- Use the existing E2E test framework
- Ensure tests are reliable and not flaky

```bash
# Run E2E tests
make test-e2e

# Run specific E2E test
make test-e2e-focus FOCUS="Policy Modes"
```

### Property-Based Tests

- Use property-based testing for complex logic
- Focus on invariants and universal properties
- Include in the main test suite

## Submitting Changes

### Pull Request Process

1. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** following the coding standards

3. **Run pre-commit checks**:
   ```bash
   make pre-commit-run
   ```

4. **Run tests**:
   ```bash
   make test
   make test-e2e
   ```

5. **Commit your changes**:
   ```bash
   git add .
   git commit -m "feat: add new feature description"
   ```

6. **Push to your fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

7. **Create a pull request** on GitHub

### Commit Message Format

We follow conventional commit format:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(controller): add resource optimization logic
fix(api): handle nil pointer in policy validation
docs(readme): update installation instructions
test(e2e): add policy mode validation tests
```

### Pull Request Guidelines

- **Title**: Use a clear, descriptive title
- **Description**: Explain what changes you made and why
- **Testing**: Describe how you tested your changes
- **Documentation**: Update relevant documentation
- **Breaking Changes**: Clearly mark any breaking changes

### Review Process

1. Automated checks must pass (CI/CD pipeline)
2. Code review by at least one maintainer
3. All feedback addressed
4. Final approval and merge

## Getting Help

- **Issues**: Check existing issues or create a new one
- **Discussions**: Use GitHub Discussions for questions
- **Documentation**: Check the docs/ directory
- **Code Examples**: Look at existing code and tests

## Development Tips

### Useful Make Targets

```bash
make help              # Show all available targets
make build             # Build the binary
make test              # Run unit tests
make test-e2e          # Run E2E tests
make lint              # Run linting
make format            # Format code
make setup-pre-commit  # Set up pre-commit hooks
make pre-commit-run    # Run pre-commit on all files
```

### IDE Setup

For VS Code, consider these extensions:
- Go extension
- YAML extension
- Markdown extension
- GitLens
- Pre-commit hooks extension

### Debugging

- Use `dlv` for Go debugging
- Add logging with structured logging
- Use `kubectl logs` for controller debugging
- Check E2E test logs for integration issues

Thank you for contributing to OptipPod! 🚀