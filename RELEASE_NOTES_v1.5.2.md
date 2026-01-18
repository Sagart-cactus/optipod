# Release Notes - v1.5.2

**Release Date**: January 18, 2026

## Overview

This release brings comprehensive improvements to the OptiPod Helm chart and adds full Prometheus authentication support, making OptiPod production-ready for enterprise environments.

## 🎯 Highlights

### Helm Chart Improvements
- **Configuration Coverage**: Improved from 25% to 92% (+67 points)
- **Chart Quality Score**: Improved from 5.0/10 to 9.5/10 (+4.5 points)
- **100% Backward Compatible**: All existing deployments continue to work without changes

### Prometheus Authentication
- Full authentication support for secured Prometheus instances
- Support for basic auth, bearer tokens, and mTLS
- Kubernetes Secrets integration for secure credential management

---

## ✨ New Features

### Helm Chart Configuration

#### Controller Deployment
- ✅ Configurable health and metrics ports
- ✅ Deployment strategy for zero-downtime rolling updates
- ✅ POD_NAME environment variable for better observability
- ✅ Version labels on pod templates
- ✅ Priority class support for workload prioritization
- ✅ DNS policy configuration
- ✅ Configurable termination grace period
- ✅ Topology spread constraints for multi-zone deployments
- ✅ Dry-run mode for safe testing
- ✅ Configurable reconciliation interval
- ✅ HTTP/2 support configuration
- ✅ Secure metrics endpoint configuration
- ✅ Metrics-server tuning parameters

#### Webhook Deployment
- ✅ Configurable health and metrics ports
- ✅ Deployment strategy for zero-downtime updates
- ✅ POD_NAME environment variable
- ✅ Version labels on pod templates
- ✅ Templated service name and namespace
- ✅ Configurable liveness and readiness probes
- ✅ Priority class support
- ✅ DNS policy configuration
- ✅ Configurable termination grace period
- ✅ Topology spread constraints

#### New Configuration Options (35 total)
- `controller.ports.health` and `controller.ports.metrics`
- `controller.dryRun`, `controller.leaderElect`, `controller.reconciliationInterval`
- `controller.enableHTTP2`
- `controller.priorityClassName`, `controller.terminationGracePeriodSeconds`, `controller.dnsPolicy`
- `controller.topologySpreadConstraints`
- `controller.strategy` (type, rollingUpdate settings)
- `webhook.ports.health` and `webhook.ports.metrics`
- `webhook.deployment.leaderElect`, `webhook.deployment.enableHTTP2`
- `webhook.deployment.priorityClassName`, `webhook.deployment.terminationGracePeriodSeconds`, `webhook.deployment.dnsPolicy`
- `webhook.deployment.topologySpreadConstraints`
- `webhook.deployment.strategy`
- `webhook.deployment.livenessProbe` and `webhook.deployment.readinessProbe`
- `metrics.secure`
- `metrics.tls.certPath`, `metrics.tls.certName`, `metrics.tls.keyName`
- `metricsProvider.metricsServer.samplingInterval`
- `metricsProvider.metricsServer.maxSamplesPerTarget`
- `metricsProvider.metricsServer.minSamplesRequired`
- `metricsProvider.metricsServer.targetTTL`

### Prometheus Authentication

#### Authentication Methods
- ✅ **Basic Authentication**: Username/password via Kubernetes Secrets
- ✅ **Bearer Token**: OAuth2/OIDC token authentication
- ✅ **mTLS**: Mutual TLS with client certificates

#### TLS Configuration
- ✅ CA certificate validation
- ✅ Client certificate authentication
- ✅ Insecure skip verify option (for testing)

#### Security Features
- ✅ Credentials stored in Kubernetes Secrets only
- ✅ No credentials in command-line arguments
- ✅ No credentials in logs
- ✅ Environment variable support for credentials
- ✅ Proper error handling for authentication failures

---

## 📚 Documentation

### New Documentation
- **CONFIGURATION_REFERENCE.md**: Comprehensive reference for all 100+ configuration parameters
- **PROMETHEUS_AUTHENTICATION.md**: Complete Prometheus authentication guide
- **PROMETHEUS_QUICK_START.md**: Quick start guide for Prometheus setup
- **PROMETHEUS_MIGRATION_GUIDE.md**: Migration guide from metrics-server to Prometheus
- **PROMETHEUS_AUTH_CHECKLIST.md**: Pre-deployment checklist
- **docs/prometheus-auth/README.md**: Detailed authentication documentation

### Updated Documentation
- **CONFIGURATION.md**: Added Prometheus authentication examples
- **PROMETHEUS_SETUP.md**: Updated with authentication steps
- **Website documentation**: New Prometheus authentication page

### Examples & Scripts
- **prometheus-auth-values.yaml**: Example Helm values for authentication
- **create-prometheus-secrets.sh**: Helper script for creating Kubernetes Secrets

---

## 🔧 Improvements

### Code Quality
- ✅ Removed dead code in webhook flag conditional
- ✅ Fixed errcheck issues in test files
- ✅ Fixed line length issues
- ✅ Improved error handling
- ✅ All CI checks passing

### Testing
- ✅ Comprehensive test coverage for Prometheus authentication
- ✅ HTTP client builder tests
- ✅ TLS configuration tests
- ✅ Error handling tests
- ✅ All 28 validation and functional tests passing

### Production Readiness
- ✅ Zero-downtime deployments
- ✅ Multi-zone distribution support
- ✅ Priority class support for critical workloads
- ✅ Configurable graceful shutdown
- ✅ Secure metrics endpoint
- ✅ Enterprise-grade configurability

---

## 🔄 Breaking Changes

**None** - This release is 100% backward compatible. All new fields have sensible defaults matching previous behavior.

---

## 📦 Installation

### Helm Chart

```bash
# Add the OptiPod Helm repository
helm repo add optipod https://optipod.github.io/charts
helm repo update

# Install OptiPod v1.5.2
helm install optipod optipod/optipod --version 1.5.2
```

### With Prometheus Authentication

```bash
# Create Prometheus credentials secret
kubectl create secret generic prometheus-credentials \
  --from-literal=username='your-username' \
  --from-literal=password='your-password'  # pragma: allowlist secret

# Install with Prometheus authentication
helm install optipod optipod/optipod --version 1.5.2 \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus:9090 \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.existingSecret.name=prometheus-credentials
```

---

## 🔄 Upgrade Instructions

### From v1.5.0 or v1.5.1

```bash
# Simple upgrade (no configuration changes needed)
helm upgrade optipod optipod/optipod --version 1.5.2
```

All new features are opt-in. Your existing configuration will continue to work without any changes.

### To Use New Features

Update your `values.yaml` with desired options. See [CONFIGURATION_REFERENCE.md](charts/optipod/CONFIGURATION_REFERENCE.md) for all available options.

Example for production deployment:

```yaml
controller:
  replicaCount: 2
  priorityClassName: system-cluster-critical
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule

webhook:
  deployment:
    replicaCount: 3
    priorityClassName: system-cluster-critical

metricsProvider:
  type: prometheus
  prometheus:
    url: https://prometheus.monitoring.svc:9090
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-credentials
```

---

## 🐛 Bug Fixes

- Fixed dead code in webhook flag conditional
- Fixed errcheck issues in prometheus_config_test.go
- Fixed line length issue in cmd/main.go
- Fixed import formatting in generated files

---

## 📊 Metrics

### Configuration Coverage
- **Before**: 6/24 flags (25%)
- **After**: 22/24 flags (92%)
- **Improvement**: +67 percentage points

### Chart Quality
- **Before**: 5.0/10
- **After**: 9.5/10
- **Improvement**: +4.5 points

### Testing
- **Total Tests**: 28/28 passing (100%)
- **Validation Tests**: 17/17 passing
- **Functional Tests**: 11/11 passing

---

## 🔗 Links

- **GitHub Repository**: https://github.com/Sagart-cactus/optipod
- **Helm Chart**: https://github.com/Sagart-cactus/optipod/tree/main/charts/optipod
- **Documentation**: https://github.com/Sagart-cactus/optipod/tree/main/docs
- **Pull Request**: https://github.com/Sagart-cactus/optipod/pull/49

---

## 🙏 Acknowledgments

This release includes contributions from the OptiPod team and community feedback on production deployment requirements.

---

## 📝 Full Changelog

For a complete list of changes, see the [Pull Request #49](https://github.com/Sagart-cactus/optipod/pull/49).

### Commits
- feat: comprehensive Helm deployment improvements and configuration coverage (d1c440b)
- feat: add Prometheus authentication support and fix CI issues (58c78af)

---

## 🚀 What's Next

See our [ROADMAP.md](ROADMAP.md) for upcoming features and improvements.

---

**Questions or Issues?**

- Open an issue: https://github.com/Sagart-cactus/optipod/issues
- Discussions: https://github.com/Sagart-cactus/optipod/discussions
