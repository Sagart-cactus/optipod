# Scripts Directory

This directory contains utility scripts for the OptipPod project.

## 📋 Available Scripts

### `ci-checks-local.sh`

A comprehensive script that mirrors the exact GitHub Actions pre-commit checks locally. This ensures consistency between local development and CI environment.

#### 🚀 Usage

```bash
# Run via Makefile (recommended)
make ci-checks-local

# Or run directly
./scripts/ci-checks-local.sh
```

#### ✅ What it checks

1. **Code Formatting Check**
   - Go formatting (`gofmt`)
   - Go imports (`goimports`)

2. **Linting Check**
   - golangci-lint with same version as CI (v2.7.2)

3. **Security Scan**
   - detect-secrets baseline validation

4. **YAML Validation**
   - yamllint with project configuration

5. **Pre-commit Hooks**
   - All configured pre-commit hooks (excluding markdownlint)

#### 🔧 Features

- **Version Consistency**: Uses exact same tool versions as GitHub Actions
- **Automatic Tool Installation**: Installs missing tools automatically
- **Path Resolution**: Handles different tool installation locations
- **Colored Output**: Clear success/error indicators
- **Detailed Error Reporting**: Shows exactly what needs to be fixed
- **Secrets Baseline Management**: Automatically handles baseline updates

#### 🎯 Benefits

- **Prevent CI Failures**: Catch issues before pushing to GitHub
- **Faster Development**: No need to wait for CI to see failures
- **Consistent Environment**: Same checks locally and in CI
- **Developer Friendly**: Clear instructions on how to fix issues

#### 📝 Example Output

```
🚀 Running Local CI Checks (GitHub Actions Mirror)

========================================
 📝 Code Formatting Check
========================================

[SUCCESS] Go formatting check passed
[SUCCESS] Go imports check passed

========================================
 🔍 Linting Check
========================================

[SUCCESS] Linting check passed

========================================
 🔒 Security Scan
========================================

[SUCCESS] Security scan passed

========================================
 📄 YAML Validation
========================================

[SUCCESS] YAML validation completed (warnings are acceptable for K8s manifests)

========================================
 🪝 Pre-commit Hooks
========================================

[SUCCESS] Pre-commit hooks passed

========================================
 📊 Summary
========================================

🎉 All CI checks passed! Your code is ready for GitHub Actions.

✅ Code Formatting Check
✅ Linting Check
✅ Security Scan
✅ YAML Validation
✅ Pre-commit Hooks
```

#### 🛠️ Troubleshooting

If checks fail, the script provides specific commands to fix issues:

```bash
make format          # Fix Go formatting and imports
make lint-fix        # Fix linting issues automatically
git add .secrets.baseline  # Update secrets baseline if needed
```

#### 🔄 Integration

This script is integrated into the project's Makefile as `make ci-checks-local` for easy access. It's recommended to run this before committing changes to ensure all CI checks will pass.