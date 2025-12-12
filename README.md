# OptiPod

[![CI](https://github.com/Sagart-cactus/optipod/actions/workflows/ci.yml/badge.svg)](https://github.com/Sagart-cactus/optipod/actions/workflows/ci.yml)
[![Lint](https://github.com/Sagart-cactus/optipod/actions/workflows/lint.yml/badge.svg)](https://github.com/Sagart-cactus/optipod/actions/workflows/lint.yml)
[![Tests](https://github.com/Sagart-cactus/optipod/actions/workflows/test.yml/badge.svg)](https://github.com/Sagart-cactus/optipod/actions/workflows/test.yml)
[![E2E Tests](https://github.com/Sagart-cactus/optipod/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/Sagart-cactus/optipod/actions/workflows/test-e2e.yml)
[![Release](https://github.com/Sagart-cactus/optipod/actions/workflows/release.yml/badge.svg)](https://github.com/Sagart-cactus/optipod/actions/workflows/release.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Sagart-cactus/optipod)](https://github.com/Sagart-cactus/optipod)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

OptiPod is an open-source, Kubernetes-native operator that automatically rightsizes CPU and memory requests for your workloads based on real-time and historical usage patterns. It helps reduce cloud costs by eliminating over-provisioning while maintaining safety margins to prevent performance issues.

## Features

- **Automatic Resource Optimization**: Continuously monitors workload resource usage and adjusts CPU and memory requests based on actual consumption
- **Server-Side Apply (SSA)**: Field-level ownership tracking prevents conflicts with GitOps tools like ArgoCD - OptiPod owns only resource requests/limits while other tools manage different fields
- **Multiple Operational Modes**: Choose between Auto (automatic application), Recommend (review before applying), or Disabled modes
- **Safety-First Approach**: Configurable safety factors, min/max bounds, and intelligent handling of in-place resize vs pod recreation
- **Pluggable Metrics Backends**: Support for Prometheus, metrics-server, and custom metrics providers
- **Multi-Tenant Ready**: Namespace and label-based workload selection with allow/deny lists
- **Comprehensive Observability**: Prometheus metrics, Kubernetes events, and detailed status reporting
- **Property-Based Tested**: Extensive test coverage including property-based tests for correctness guarantees

## Quick Start

### Prerequisites

- Kubernetes cluster (1.29+)
- kubectl configured to access your cluster
- Metrics source (Prometheus or metrics-server)

### Installation

1. **Install using the release manifest**:
```bash
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install.yaml
```

Or install a specific version:
```bash
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/download/v1.0.0/install.yaml
```

2. **Verify the installation**:
```bash
kubectl get pods -n optipod-system
kubectl logs -n optipod-system deployment/optipod-manager
```

### Create Your First Policy

Create a file named `my-policy.yaml`:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-workloads
  namespace: default
spec:
  mode: Recommend  # Start with Recommend mode to review suggestions
  
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
  
  reconciliationInterval: 5m
```

Apply the policy:
```bash
kubectl apply -f my-policy.yaml
```

Label your workloads to enable optimization:
```bash
kubectl label deployment my-app optimize=true
```

Check the recommendations:
```bash
kubectl describe optimizationpolicy production-workloads
```

## Documentation

- [Installation Guide](docs/INSTALLATION.md) - Detailed installation instructions
- [CRD Reference](docs/CRD_REFERENCE.md) - Complete OptimizationPolicy field documentation
- [Example Policies](docs/EXAMPLES.md) - Common use case examples
- [ArgoCD Integration](docs/ARGOCD_INTEGRATION.md) - GitOps compatibility guide
- [CI/CD Testing Guide](docs/ci-cd-testing.md) - How to test and validate workflows
- [CI/CD Implementation](docs/ci-cd-implementation-summary.md) - CI/CD pipeline details

## How It Works

1. **Discovery**: OptiPod discovers workloads matching your policy selectors
2. **Metrics Collection**: Collects CPU and memory usage data from your configured metrics provider
3. **Analysis**: Computes percentiles (P50, P90, P99) over a rolling window
4. **Recommendation**: Applies safety factors and enforces min/max bounds
5. **Application**: Updates workload resource requests (in Auto mode) or stores recommendations (in Recommend mode)

## Operational Modes

- **Auto**: Automatically applies resource recommendations to workloads
- **Recommend**: Computes and stores recommendations without applying them (review first)
- **Disabled**: Stops processing workloads while preserving historical data

## Safety Features

- **Bounds Enforcement**: All recommendations respect configured min/max limits
- **Safety Factors**: Multiply usage percentiles by configurable factors (default 1.2x)
- **In-Place Resize Support**: Prefers in-place updates when available (Kubernetes 1.29+)
- **Graceful Fallback**: Skips changes that require pod recreation unless explicitly allowed
- **RBAC Respect**: Never attempts operations without proper permissions
- **Global Dry-Run**: Test across entire cluster without making changes

## Building from Source

```bash
# Clone the repository
git clone https://github.com/Sagart-cactus/optipod.git
cd optipod

# Build the operator
make build

# Run tests
make test

# Run E2E tests
make test-e2e

# Build and push Docker image
make docker-build docker-push IMG=your-registry/optipod:tag
```

## Development

```bash
# Install CRDs into your cluster
make install

# Run the operator locally (against your current kubeconfig)
make run

# Generate manifests after API changes
make manifests

# Run linter
make lint
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

- **Issues**: Report bugs and request features via [GitHub Issues](https://github.com/Sagart-cactus/optipod/issues)
- **Discussions**: Ask questions in [GitHub Discussions](https://github.com/Sagart-cactus/optipod/discussions)
- **Documentation**: Full documentation at [docs/](docs/)

## Acknowledgments

Built with:
- [Kubebuilder](https://book.kubebuilder.io/) - Kubernetes operator framework
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) - Kubernetes controller library
- [gopter](https://github.com/leanovate/gopter) - Property-based testing for Go
