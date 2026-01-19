---
inclusion: always
---

# OptiPod Project Context

## What is OptiPod?

OptiPod is an open-source Kubernetes operator that provides **explainable recommendations** for CPU and memory requests/limits. It's designed to be GitOps-safe, policy-driven, and safe by default.

## Core Principles

1. **Recommend First, Apply When Ready**
   - Default mode is "Recommend" - no mutations
   - Requires explicit opt-in (`mode: Auto`) for automatic application
   - Safe to try in production

2. **GitOps Compatible**
   - Works seamlessly with ArgoCD and Flux
   - Stores recommendations in workload metadata (not pod templates)
   - No fighting with GitOps controllers

3. **Explainable Recommendations**
   - No black box algorithms
   - Clear reasoning for each recommendation
   - Based on actual metrics (Prometheus)

4. **Policy-Driven**
   - Define optimization policies via CRDs
   - Target specific workloads with label selectors
   - Flexible configuration per policy

## Architecture

### Components

1. **Controller** (Required)
   - Reconciles OptimizationPolicy CRDs
   - Fetches metrics from Prometheus
   - Generates recommendations
   - Applies changes in Auto mode

2. **Webhook** (Optional)
   - Mutating admission webhook
   - Injects resource requests/limits at pod creation
   - Reads recommendations from workload metadata
   - Requires cert-manager for TLS

3. **CRDs**
   - `OptimizationPolicy` - Defines optimization rules

### Modes

- **Recommend**: Safe mode, only annotates workloads
- **Auto**: Applies recommendations automatically (requires opt-in)

## Tech Stack

- **Language**: Go 1.22+
- **Framework**: Kubebuilder, controller-runtime
- **Metrics**: Prometheus (required for recommendations)
- **Deployment**: Helm charts
- **Certificates**: cert-manager (for webhook)

## Key Features

### Current (v1.5.x)
- ✅ Recommend and Auto modes
- ✅ Prometheus metrics integration
- ✅ Prometheus authentication (basic auth, bearer token, mTLS)
- ✅ GitOps-safe webhook strategy
- ✅ ArgoCD compatibility
- ✅ Helm chart with cert-manager auto-detection
- ✅ Support for Deployments, StatefulSets, DaemonSets

### Roadmap
- 🔄 Additional metrics providers (VPA, custom metrics)
- 🔄 Advanced recommendation algorithms
- 🔄 Cost optimization insights
- 🔄 Multi-cluster support

## Development Workflow

### Testing Strategy
- Unit tests for all packages
- E2E tests for end-to-end scenarios
- Local CI checks before commit
- GitHub Actions for CI/CD

### Release Process
- Semantic versioning (vX.Y.Z)
- Automated releases via GitHub Actions
- Multi-arch images (amd64, arm64)
- Helm chart published to OCI registry

## Important Constraints

### What OptiPod Will NOT Do
- ❌ Mutate workloads without explicit opt-in
- ❌ Override GitOps ownership
- ❌ Blindly reduce memory (risk of OOMKills)
- ❌ Require a SaaS backend
- ❌ Make unexplainable recommendations

### Safety Guarantees
- Default to recommend mode
- Require explicit policy configuration
- Respect GitOps patterns
- Provide clear audit trail
- Allow gradual rollout

## Common Use Cases

1. **Initial Assessment** (Recommend mode)
   - Deploy OptiPod with default settings
   - Review recommendations on workloads
   - Understand optimization potential

2. **Gradual Optimization** (Auto mode)
   - Start with non-critical workloads
   - Enable Auto mode per policy
   - Monitor impact
   - Expand to more workloads

3. **GitOps Integration**
   - Use webhook strategy
   - Let ArgoCD/Flux manage workload specs
   - OptiPod manages metadata annotations
   - Webhook applies at runtime

## Key Files and Directories

- `api/v1alpha1/` - CRD definitions
- `internal/controller/` - Controller logic
- `internal/webhook/` - Webhook implementation
- `internal/metrics/` - Metrics provider implementations
- `charts/optipod/` - Helm chart
- `config/` - Kubernetes manifests (kustomize)
- `test/e2e/` - E2E test suite
- `scripts/` - Build and utility scripts

## Metrics and Observability

- Controller exposes Prometheus metrics on `:8080/metrics`
- Webhook exposes metrics on `:8443/metrics`
- ServiceMonitor for Prometheus Operator integration
- Health probes on `:8081/healthz` and `:8081/readyz`

## When Working on OptiPod

### Always Consider
- GitOps compatibility
- Safety and explainability
- Backward compatibility
- Helm chart configuration coverage
- Documentation updates

### Before Committing
- Run `./scripts/ci-checks-local.sh`
- Ensure tests pass
- Update documentation if needed
- Follow conventional commit format

### When Adding Features
- Add corresponding Helm values
- Update ROADMAP.md if significant
- Add examples in `charts/optipod/examples/`
- Consider backward compatibility
