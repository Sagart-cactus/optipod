# OptipPod E2E Test Suite

This directory contains OptipPod's comprehensive end-to-end (e2e) test suite that validates the complete functionality of the OptipPod controller in real Kubernetes environments.

## 🎯 Overview

The e2e test suite provides comprehensive validation of OptipPod's functionality through:

- **Policy Mode Validation**: Auto, Recommend, and Disabled mode behavior
- **Resource Bounds Enforcement**: CPU and memory limit validation and clamping
- **RBAC and Security**: Permission validation and security constraint compliance
- **Error Handling**: Edge cases, invalid configurations, and failure scenarios
- **Workload Support**: Deployments, StatefulSets, and DaemonSets
- **Observability**: Metrics exposure, logging, and monitoring integration
- **Property-Based Testing**: Universal properties validated across multiple inputs
- **Diagnostic Collection**: Comprehensive failure analysis and debugging

## 🏗️ Architecture

### Test Framework Stack

- **Ginkgo v2**: BDD-style test framework for structured test organization
- **Gomega**: Expressive assertion library for test validations
- **Controller-Runtime**: Kubernetes client and testing utilities
- **Kind**: Local Kubernetes cluster for test execution

### Directory Structure

```
test/e2e/
├── 📁 Core Test Files
│   ├── e2e_suite_test.go              # Test suite setup and configuration
│   ├── e2e_test.go                    # Basic controller functionality tests
│   ├── policy_modes_test.go           # Policy mode validation tests
│   ├── resource_bounds_test.go        # Resource bounds enforcement tests
│   ├── rbac_security_test.go          # RBAC and security constraint tests
│   ├── error_handling_test.go         # Error conditions and edge cases
│   ├── workload_types_test.go         # Workload type support tests
│   ├── observability_test.go          # Metrics and logging validation
│   └── diagnostic_collection_property_test.go  # Diagnostic collection tests
├── 📁 helpers/                        # Reusable test utilities
│   ├── policy_helpers.go              # Policy creation and management
│   ├── workload_helpers.go            # Workload creation and management
│   ├── validation_helpers.go          # Common validation functions
│   ├── cleanup_helpers.go             # Resource cleanup utilities
│   └── reporting_helpers.go           # Test reporting and dashboards
├── 📁 fixtures/                       # Test data generation
│   └── generators.go                  # Programmatic configuration generation
├── 📁 Configuration
│   ├── parallel_config.go             # Parallel execution configuration
│   └── performance_config.go          # Performance optimization settings
└── 📁 Documentation
    ├── README.md                      # This file
    ├── TESTING_GUIDE.md              # Comprehensive testing guide
    ├── TROUBLESHOOTING.md            # Troubleshooting guide
    └── DEVELOPER_ONBOARDING.md       # Developer onboarding guide
```

## 🚀 Quick Start

### Prerequisites

Ensure you have the following installed:

- **Go 1.21+**: `go version`
- **Docker**: `docker --version`
- **Kind**: `go install sigs.k8s.io/kind@latest`
- **kubectl**: `kubectl version --client`
- **Make**: `make --version`

### Setup and Run

1. **Create Kind cluster**:
   ```bash
   kind create cluster --name optipod-test
   export KUBECONFIG="$(kind get kubeconfig-path --name="optipod-test")"
   ```

2. **Build and load OptipPod image**:
   ```bash
   make docker-build IMG=example.com/optipod:v0.0.1
   kind load docker-image example.com/optipod:v0.0.1 --name optipod-test
   ```

3. **Run tests**:
   ```bash
   # Run all e2e tests
   make test-e2e
   
   # Run with verbose output
   make test-e2e ARGS="-v"
   
   # Run specific test
   go test -v -tags=e2e ./test/e2e -run "TestBasicController"
   ```

## 🧪 Test Categories

### 1. Policy Mode Tests (`policy_modes_test.go`)
Validates OptipPod behavior across different policy modes:
- **Auto Mode**: Verifies recommendations are applied automatically
- **Recommend Mode**: Verifies recommendations are generated but not applied
- **Disabled Mode**: Verifies no workload processing occurs

```bash
go test -v -tags=e2e ./test/e2e -run "Policy.*Mode"
```

### 2. Resource Bounds Tests (`resource_bounds_test.go`)
Validates resource bounds enforcement and clamping:
- **Within Bounds**: Recommendations respect configured limits
- **Below Minimum**: Recommendations clamped to minimum values
- **Above Maximum**: Recommendations clamped to maximum values

```bash
go test -v -tags=e2e ./test/e2e -run "Resource.*Bounds"
```

### 3. RBAC Security Tests (`rbac_security_test.go`)
Validates RBAC permissions and security constraints:
- **Restricted Permissions**: Appropriate error handling
- **Security Policies**: Pod security policy compliance
- **Permission Escalation**: Prevention of privilege escalation

```bash
go test -v -tags=e2e ./test/e2e -run "RBAC.*Security"
```

### 4. Error Handling Tests (`error_handling_test.go`)
Validates error conditions and edge cases:
- **Invalid Configurations**: Rejection and error messages
- **Missing Metrics**: Graceful degradation
- **Concurrent Modifications**: Conflict resolution

```bash
go test -v -tags=e2e ./test/e2e -run "Error.*Handling"
```

### 5. Workload Type Tests (`workload_types_test.go`)
Validates support for different Kubernetes workload types:
- **Deployments**: Recommendation and update behavior
- **StatefulSets**: Ordered update handling
- **DaemonSets**: Node-based update behavior

```bash
go test -v -tags=e2e ./test/e2e -run "Workload.*Types"
```

### 6. Observability Tests (`observability_test.go`)
Validates metrics exposure and logging:
- **Prometheus Metrics**: Metric exposure and accuracy
- **Controller Logs**: Log content and formatting
- **Monitoring Integration**: Alert and health check functionality

```bash
go test -v -tags=e2e ./test/e2e -run "Observability"
```

### 7. Property-Based Tests
Validates universal properties across multiple inputs:
- **Policy Mode Consistency**: Behavior across all policy modes
- **Resource Bounds Enforcement**: Bounds respect across all configurations
- **Diagnostic Collection**: Comprehensive diagnostic information collection

```bash
go test -v -tags=e2e ./test/e2e -run "Property.*"
```

## ⚙️ Configuration

### Environment Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `E2E_PARALLEL_NODES` | Number of parallel test nodes | 4 | 8 |
| `E2E_TIMEOUT_MULTIPLIER` | Timeout multiplier for parallel execution | 1.0 | 2.0 |
| `CERT_MANAGER_INSTALL_SKIP` | Skip CertManager installation | false | true |
| `METRICS_SERVER_INSTALL_SKIP` | Skip MetricsServer installation | false | true |

### Parallel Execution

Run tests in parallel for faster execution:

```bash
# Default parallel execution (4 nodes)
go test -v -tags=e2e ./test/e2e -ginkgo.procs=4

# Custom parallel configuration
E2E_PARALLEL_NODES=8 go test -v -tags=e2e ./test/e2e -ginkgo.procs=8

# With timeout adjustment
E2E_TIMEOUT_MULTIPLIER=2.0 go test -v -tags=e2e ./test/e2e -ginkgo.procs=4
```

### Performance Optimization

For faster test execution:

```bash
# Skip component installation if already present
export CERT_MANAGER_INSTALL_SKIP=true
export METRICS_SERVER_INSTALL_SKIP=true

# Use performance mode
export E2E_PERFORMANCE_MODE=true

# Reduce parallel load for resource-constrained environments
export E2E_PARALLEL_NODES=2
```

## 🔧 Development

### Writing New Tests

1. **Follow established patterns**: Use the same structure as existing tests
2. **Use helper functions**: Leverage existing utilities in `helpers/`
3. **Generate test data**: Use fixtures in `fixtures/` for configuration generation
4. **Handle cleanup**: Always use cleanup helpers to track resources
5. **Include documentation**: Add comments for complex test logic

Example test structure:

```go
var _ = Describe("Feature Name", func() {
    var (
        ctx           context.Context
        namespace     string
        cleanupHelper *helpers.CleanupHelper
    )

    BeforeEach(func() {
        // Test setup
    })

    AfterEach(func() {
        // Test cleanup
    })

    Context("Test Category", func() {
        It("should validate specific behavior", func() {
            By("Step 1: Setup")
            // Test implementation
        })
    })
})
```

### Helper Usage

Use existing helpers for common operations:

```go
// Policy operations
policyHelper := helpers.NewPolicyHelper(k8sClient, namespace)
policy, err := policyHelper.CreateOptimizationPolicy(config)

// Workload operations
workloadHelper := helpers.NewWorkloadHelper(k8sClient, namespace)
deployment, err := workloadHelper.CreateDeployment(config)

// Validation operations
validationHelper := helpers.NewValidationHelper(k8sClient)
err := validationHelper.ValidateResourceBounds(recommendations, bounds)

// Cleanup tracking
cleanupHelper.TrackPolicy(policy.Name, policy.Namespace)
```

## 🐛 Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| `no nodes found for cluster "kind"` | `kind create cluster` |
| `connection refused` | Check KUBECONFIG setting |
| `image not found` | Build and load image: `make docker-build && kind load docker-image` |
| `timeout exceeded` | Increase timeout: `-timeout=30m` |
| `resource already exists` | Clean up: `kubectl delete ns --all --selector=test-namespace=true` |

### Debug Mode

Enable detailed logging for troubleshooting:

```bash
# Verbose Ginkgo output
go test -v -tags=e2e ./test/e2e -ginkgo.v -ginkgo.trace

# Controller-runtime debug logging
KUBEBUILDER_ASSETS=$(setup-envtest use --use-env -p path) \
go test -v -tags=e2e ./test/e2e -ginkgo.v
```

### Log Collection

Collect diagnostic information:

```bash
# Controller logs
kubectl logs -n optipod-system deployment/optipod-controller-manager

# Test artifacts
ls -la /tmp/optipod-diagnostics/

# Kind cluster logs
kind export logs /tmp/kind-logs --name optipod-test
```

## 📚 Documentation

### Comprehensive Guides

- **[TESTING_GUIDE.md](./TESTING_GUIDE.md)**: Complete guide to running and configuring tests
- **[TROUBLESHOOTING.md](./TROUBLESHOOTING.md)**: Detailed troubleshooting for common issues
- **[DEVELOPER_ONBOARDING.md](./DEVELOPER_ONBOARDING.md)**: Guide for new contributors

### External Resources

- [Ginkgo Documentation](https://onsi.github.io/ginkgo/)
- [Gomega Documentation](https://onsi.github.io/gomega/)
- [Controller-Runtime Testing](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html)
- [Kind Documentation](https://kind.sigs.k8s.io/)

## 🤝 Contributing

We welcome contributions to improve the test suite! Please:

1. **Read the guides**: Start with [DEVELOPER_ONBOARDING.md](./DEVELOPER_ONBOARDING.md)
2. **Follow patterns**: Use established test structures and helpers
3. **Test your changes**: Ensure tests pass locally before submitting
4. **Update documentation**: Include any necessary documentation updates

### Before Submitting

```bash
# Run your specific tests
go test -v -tags=e2e ./test/e2e -run "YourTestName"

# Run related test categories
go test -v -tags=e2e ./test/e2e -run "RelatedCategory"

# Check for resource leaks
kubectl get all -A | grep test
```

## 📊 Test Metrics

The test suite provides comprehensive metrics and reporting:

- **Coverage Analysis**: Validates all requirements and properties are tested
- **Performance Metrics**: Tracks test execution time and resource usage
- **Failure Analysis**: Collects diagnostic information for failed tests
- **CI Integration**: Structured reporting for continuous integration

## 🎯 Quality Assurance

Our test suite ensures OptipPod quality through:

- **Comprehensive Coverage**: All features, edge cases, and error conditions
- **Property-Based Testing**: Universal properties validated across inputs
- **Real Environment Testing**: Tests run against actual Kubernetes clusters
- **Parallel Execution**: Efficient test execution for faster feedback
- **Diagnostic Collection**: Detailed failure analysis for quick resolution

---

For questions or issues with the test suite, please check the troubleshooting guide or open an issue with detailed information about the problem.