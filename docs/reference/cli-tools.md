# CLI Tools and Scripts Reference

Complete reference for OptiPod command-line tools and utility scripts.

## Overview

OptiPod provides several command-line tools and scripts to help with:
- Viewing and analyzing recommendations
- Setting up development environments
- Managing Prometheus authentication
- Validating documentation
- Running local CI checks

All scripts are located in the `scripts/` directory of the OptiPod repository.

## optipod-recommendation-report.sh

Generates comprehensive reports comparing current CPU/memory requests/limits versus OptiPod recommended values.

### Synopsis

```bash
scripts/optipod-recommendation-report.sh [OPTIONS]
```

### Description

The recommendation report script reads OptiPod recommendations from workload annotations and generates detailed comparison reports. It supports both JSON and HTML output formats, with filtering and sorting capabilities.

### Requirements

- `kubectl` - Kubernetes command-line tool
- `jq` - JSON processor
- Access to a Kubernetes cluster with OptiPod installed

### Options

**--output, -o** `json|html`  
Output format. Default: `json`
- `json` - Machine-readable JSON format
- `html` - Interactive HTML report with filtering and sorting

**--file, -f** `PATH`  
Write output to file instead of stdout

**--namespace, -n** `NAMESPACE`  
Restrict report to a specific namespace. Default: all namespaces

**--kinds** `KINDS`  
Comma-separated list of workload kinds to scan. Default: `deploy,sts,ds`
- `deploy` - Deployments
- `sts` - StatefulSets
- `ds` - DaemonSets

**--help, -h**  
Show help message

### Examples

**Generate JSON report (default)**:
```bash
./scripts/optipod-recommendation-report.sh
```

**Generate HTML report**:
```bash
./scripts/optipod-recommendation-report.sh --output html --file report.html
open report.html  # macOS
xdg-open report.html  # Linux
```

**Filter by namespace**:
```bash
./scripts/optipod-recommendation-report.sh --namespace production
```

**Only scan Deployments**:
```bash
./scripts/optipod-recommendation-report.sh --kinds deploy
```

**Scan Deployments and StatefulSets**:
```bash
./scripts/optipod-recommendation-report.sh --kinds deploy,sts
```

### JSON Output Format

The JSON output includes:

**Metadata**:
```json
{
  "generatedAt": "2025-01-28T10:30:00Z",
  "context": "my-cluster",
  "namespace": null,
  "kinds": ["deploy", "sts", "ds"]
}
```

**Counts**:
```json
{
  "counts": {
    "scannedWorkloads": 10,
    "scannedNamespaces": 3,
    "workloadsWithRecommendations": 8,
    "recordsWithRecommendations": 12,
    "warningRecords": 2
  }
}
```

**Totals** (aggregated across all workloads):
```json
{
  "totals": {
    "cpuRequests": {
      "currentMilli": 5000,
      "recommendedMilli": 3000,
      "deltaMilli": -2000
    },
    "memoryRequests": {
      "currentBytes": 10737418240,
      "recommendedBytes": 5368709120,
      "deltaBytes": -5368709120
    },
    "impact": {
      "cpuRequests": {
        "currentMilli": 15000,
        "recommendedMilli": 9000,
        "deltaMilli": -6000
      }
    }
  }
}
```

**Records** (per-container details):
```json
{
  "records": [
    {
      "kind": "Deployment",
      "namespace": "production",
      "workload": "my-app",
      "pods": 3,
      "container": "app",
      "policy": "production-policy",
      "policyUID": "abc123...",
      "lastRecommendation": "2025-01-28T10:30:00Z",
      "hasRecommendation": true,
      "warnings": [],
      "current": {
        "requests": { "cpu": "1000m", "memory": "2Gi" },
        "limits": { "cpu": "2000m", "memory": "4Gi" }
      },
      "recommended": {
        "requests": { "cpu": "500m", "memory": "1Gi" },
        "limits": null
      },
      "delta": {
        "cpuReqMilli": -500,
        "memReqBytes": -1073741824,
        "impactCpuReqMilli": -1500,
        "impactMemReqBytes": -3221225472,
        "cpuReqDirection": "decrease",
        "memReqDirection": "decrease",
        "cpuReqPercent": -50.00,
        "memReqPercent": -50.00
      }
    }
  ]
}
```

### HTML Output Features

The HTML report provides:

**Summary Dashboard**:
- Workloads optimized percentage (donut chart)
- Total CPU and memory savings
- Requests summary table

**Interactive Filtering**:
- Search by workload, container, or policy name
- Filter by namespace
- Filter by workload type (Deployment, StatefulSet, DaemonSet)
- Filter by change direction (increases, decreases)
- Filter by warnings

**Sorting Options**:
- Largest total decrease (default)
- CPU delta
- Memory delta
- Warnings

**Detailed Table**:
- Per-container current vs recommended resources
- Delta calculations with visual indicators
- Warning badges with tooltips
- Last recommendation timestamp

### Warnings

The script detects and reports potential issues:

**CPU_REQ_EXCEEDS_LIMIT**:
- Recommended CPU request exceeds current CPU limit
- May cause validation errors if applied with `updateRequestsOnly: true`

**MEM_REQ_EXCEEDS_LIMIT**:
- Recommended memory request exceeds current memory limit
- May cause validation errors if applied with `updateRequestsOnly: true`

**UPDATE_REQUESTS_ONLY_CONFLICT**:
- Policy has `updateRequestsOnly: true` but recommendations exceed limits
- Requires either updating limits or changing policy configuration

### Annotation Format

The script reads these annotations from workloads:

**Management annotations**:
- `optipod.io/managed` - Indicates OptiPod management
- `optipod.io/policy` - Policy name
- `optipod.io/policy-uid` - Policy UID
- `optipod.io/last-recommendation` - Timestamp

**Recommendation annotations** (per container):
- `optipod.io/recommendation.<container>.cpu-request`
- `optipod.io/recommendation.<container>.memory-request`
- `optipod.io/recommendation.<container>.cpu-limit`
- `optipod.io/recommendation.<container>.memory-limit`

### Exit Codes

- `0` - Success
- `2` - Invalid arguments or missing dependencies

### Notes

- Requires OptiPod to be installed and generating recommendations
- Only shows workloads with OptiPod annotations
- Impact calculations multiply per-container deltas by pod count
- HTML report is self-contained (no external dependencies)


## create-prometheus-secrets.sh

Creates Kubernetes secrets for Prometheus authentication.

### Synopsis

```bash
scripts/create-prometheus-secrets.sh
```

### Description

Interactive script that creates Kubernetes secrets for authenticating with Prometheus. Supports basic auth, bearer token, and mTLS authentication methods.

### Requirements

- `kubectl` - Kubernetes command-line tool
- Access to a Kubernetes cluster

### Usage

Run the script and follow the interactive prompts:

```bash
./scripts/create-prometheus-secrets.sh
```

The script will:
1. Prompt for authentication method (basic auth, bearer token, or mTLS)
2. Collect required credentials
3. Create the appropriate Kubernetes secret
4. Provide Helm values for using the secret

### Authentication Methods

**Basic Auth**:
- Prompts for username and password
- Creates secret with `username` and `password` keys

**Bearer Token**:
- Prompts for token value
- Creates secret with `token` key

**mTLS**:
- Prompts for client certificate and key file paths
- Creates secret with `tls.crt` and `tls.key` keys

### Example Output

```bash
$ ./scripts/create-prometheus-secrets.sh
Select authentication method:
1) Basic Auth
2) Bearer Token
3) mTLS
Choice: 1

Enter username: admin
Enter password: ********

Creating secret 'prometheus-auth' in namespace 'optipod-system'...
secret/prometheus-auth created

Add this to your Helm values:
  prometheus:
    auth:
      type: basic
      secretName: prometheus-auth
```

## ci-checks-local.sh

Runs local CI checks before committing code.

### Synopsis

```bash
scripts/ci-checks-local.sh
```

### Description

Runs the same checks that CI runs, allowing you to catch issues before pushing code. This includes linting, formatting, tests, and validation.

### Requirements

- Go 1.22+
- golangci-lint
- kubectl
- All project dependencies

### Checks Performed

1. **Go formatting** - Verifies code is formatted with `gofmt`
2. **Go linting** - Runs `golangci-lint`
3. **Go tests** - Runs unit tests
4. **Go vet** - Runs `go vet`
5. **Manifest validation** - Validates Kubernetes manifests
6. **Documentation validation** - Checks documentation links and format

### Usage

```bash
./scripts/ci-checks-local.sh
```

### Exit Codes

- `0` - All checks passed
- `1` - One or more checks failed

## lint-local.sh

Runs linting checks on the codebase.

### Synopsis

```bash
scripts/lint-local.sh
```

### Description

Runs golangci-lint with the project's configuration to check for code quality issues.

### Requirements

- golangci-lint

### Usage

```bash
./scripts/lint-local.sh
```

### Configuration

Uses `.golangci.yml` in the project root for linter configuration.

## setup-pre-commit.sh

Sets up pre-commit hooks for the repository.

### Synopsis

```bash
scripts/setup-pre-commit.sh
```

### Description

Installs and configures pre-commit hooks to automatically run checks before each commit.

### Requirements

- pre-commit (Python package)

### Installation

```bash
# Install pre-commit
pip install pre-commit

# Run setup script
./scripts/setup-pre-commit.sh
```

### Hooks Configured

- Go formatting check
- Go linting
- YAML validation
- Markdown linting
- Secret detection
- Trailing whitespace removal

## validate-docs.sh

Validates documentation for correctness and consistency.

### Synopsis

```bash
scripts/validate-docs.sh
```

### Description

Checks documentation files for:
- Broken internal links
- Invalid markdown syntax
- Missing required sections
- Inconsistent formatting

### Requirements

- markdown-link-check (optional)
- markdownlint (optional)

### Usage

```bash
./scripts/validate-docs.sh
```

### Exit Codes

- `0` - All documentation is valid
- `1` - Validation errors found

## sync-docs.sh

Synchronizes documentation between docs/ and website/.

### Synopsis

```bash
scripts/sync-docs.sh
```

### Description

Ensures documentation is synchronized between the Markdown (`docs/`) and MDX (`website/`) versions.

### Usage

```bash
./scripts/sync-docs.sh
```

### What It Does

1. Compares content between `docs/` and `website/src/content/docs/docs/`
2. Reports any differences
3. Optionally syncs changes

## generate-docs-index.sh

Generates documentation index files.

### Synopsis

```bash
scripts/generate-docs-index.sh
```

### Description

Creates index files that list all documentation pages for easy navigation.

### Usage

```bash
./scripts/generate-docs-index.sh
```

### Output

Generates `docs/INDEX.md` with links to all documentation files.

## build-docs.sh

Builds the documentation website.

### Synopsis

```bash
scripts/build-docs.sh
```

### Description

Builds the Astro-based documentation website for production deployment.

### Requirements

- Node.js 18+
- npm

### Usage

```bash
./scripts/build-docs.sh
```

### Output

Builds website to `website/dist/` directory.

## Common Workflows

### Before Committing Code

```bash
# Run all CI checks locally
./scripts/ci-checks-local.sh

# If checks pass, commit
git add .
git commit -m "feat: add new feature"
```

### Reviewing Recommendations

```bash
# Generate HTML report
./scripts/optipod-recommendation-report.sh --output html --file report.html

# Open in browser
open report.html  # macOS
xdg-open report.html  # Linux
```

### Setting Up Prometheus Authentication

```bash
# Create secret interactively
./scripts/create-prometheus-secrets.sh

# Update Helm values with provided configuration
helm upgrade optipod charts/optipod -f values.yaml
```

### Validating Documentation Changes

```bash
# Validate documentation
./scripts/validate-docs.sh

# Sync docs/ and website/
./scripts/sync-docs.sh

# Build website to test
./scripts/build-docs.sh
```

## Troubleshooting

### Script Not Executable

If you get "Permission denied":

```bash
chmod +x scripts/*.sh
```

### Missing Dependencies

Install required tools:

```bash
# macOS
brew install kubectl jq golangci-lint

# Linux (Ubuntu/Debian)
apt-get install kubectl jq
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### kubectl Not Configured

Ensure kubectl is configured to access your cluster:

```bash
kubectl config current-context
kubectl get nodes
```

## Related Documentation

- [Annotations Reference](annotations.md) - Annotation format used by recommendation report
- [Reviewing Recommendations](../guides/reviewing-recs.md) - Guide to reviewing recommendations
- [CRD Specification](crd-spec.md) - OptimizationPolicy field reference
- [Contributing Guide](../../CONTRIBUTING.md) - Development workflow and guidelines
