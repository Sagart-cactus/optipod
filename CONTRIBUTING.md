# Contributing to Optipod

Thank you for your interest in contributing to Optipod! This document provides guidelines and information for
contributors.

## Code of Conduct

By participating in this project, you agree to abide by our code of conduct. Please be respectful and
constructive in all interactions.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Docker (for building container images)
- Kubernetes cluster (for testing)
- Make

### Development Setup

1. Fork the repository
2. Clone your fork:

   ```bash
   git clone https://github.com/YOUR_USERNAME/optipod.git
   cd optipod
   ```

3. Install dependencies:

   ```bash
   go mod download
   ```

4. Run tests to ensure everything works:

   ```bash
   make test
   ```

## Development Workflow

### Making Changes

1. Create a new branch from main:

   ```bash
   git checkout main
   git pull origin main
   git checkout -b feature/your-feature-name
   ```

2. Make your changes following our coding standards

3. Run tests and linting:

   ```bash
   make test
   make lint
   make format
   ```

4. Commit your changes with a descriptive message:

   ```bash
   git commit -m "feat: add new feature description"
   ```

### Commit Message Format

We follow conventional commits format:

- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for test additions/changes
- `refactor:` for code refactoring
- `chore:` for maintenance tasks

### Pull Request Process

1. Push your branch to your fork:

   ```bash
   git push origin feature/your-feature-name
   ```

2. Create a pull request from your fork to the main repository

3. Fill out the pull request template completely

4. Ensure all CI checks pass:
   - Unit tests
   - Linting
   - Build validation
   - Security scans
   - Code formatting

5. Request review from code owners

6. Address any feedback and update your PR

## Code Standards

### Go Code Style

- Follow standard Go formatting (`gofmt`)
- Use meaningful variable and function names
- Add comments for exported functions and complex logic
- Write unit tests for new functionality
- Maintain test coverage above 80%

### Testing

- Write unit tests for all new functionality
- Include property-based tests where appropriate
- Test error conditions and edge cases
- Update integration tests if needed

### Documentation

- Update README.md if adding new features
- Add or update API documentation
- Include examples for new functionality
- Update ROADMAP.md for significant features

## Project Structure

```text
├── api/                    # Kubernetes API definitions
├── cmd/                    # Main application entry point
├── config/                 # Kubernetes manifests and configuration
├── docs/                   # Documentation
├── internal/               # Internal application code
│   ├── application/        # Application engine
│   ├── controller/         # Kubernetes controllers
│   ├── discovery/          # Workload discovery
│   ├── metrics/            # Metrics collection
│   └── recommendation/     # Recommendation engine
└── test/                   # Test utilities and e2e tests
```

## Reporting Issues

### Bug Reports

Use the bug report template and include:

- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- Environment details
- Relevant logs and configuration

### Feature Requests

Use the feature request template and include:

- Clear description of the feature
- Use case and benefits
- Proposed implementation approach
- Acceptance criteria

## Review Process

All submissions require review from code owners. Reviews focus on:

- Code quality and maintainability
- Test coverage and quality
- Documentation completeness
- Adherence to project standards
- Security considerations

## Getting Help

- Check existing issues and documentation
- Ask questions in issue discussions
- Reach out to maintainers for guidance

## License

By contributing to Optipod, you agree that your contributions will be licensed under the Apache License 2.0.

Thank you for contributing to Optipod! 🚀
