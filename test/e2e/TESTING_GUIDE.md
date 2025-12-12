# OptipPod E2E Testing Guide

This guide provides comprehensive information about OptipPod's end-to-end (e2e) testing framework, including test execution, configuration, troubleshooting, and development practices.

## Table of Contents

- [Overview](#overview)
- [Test Architecture](#test-architecture)
- [Prerequisites](#prerequisites)
- [Running Tests](#running-tests)
- [Test Configuration](#test-configuration)
- [Test Categories](#test-categories)
- [Troubleshooting](#troubleshooting)
- [Development Guidelines](#development-guidelines)
- [CI/CD Integration](#cicd-integration)

## Overview

OptipPod's e2e test suite validates the complete functionality of the OptipPod controller in real Kubernetes environments. The tests cover:

- Policy mode validation (Auto, Recommend, Disabled)
- Resource bounds enforcement
- RBAC and security constraints
- Error handling and edge cases
- Workload type support (Deployments, StatefulSets, DaemonSets)
- Observability and metrics
- Diagnostic collection

## Test Architecture

### Directory Structure

```
test/e2e/
├── e2e_suite_test.go              # Test suite setup and configuration
├── e2e_test.go                    # Basic controller and metrics tests
├── policy_modes_test.go           # Policy mode validation tests
├── resource_bounds_test.go        # Resource bounds enforcement tests
├── rbac_security_test.go          # RBAC and security tests
├── error_handling_test.go         # Error conditions and edge cases
├── workload_types_test.go         # Workload type support tests
├── observability_test.go          # Metrics and logging validation
├── diagnostic_collection_property_test.go  # Diagnostic collection tests
├── helpers/                       # Test helper components
│   ├── policy_helpers.go          # Policy creation and validation
│   ├── workload_helpers.go        # Workload management utilities
│   ├── validation_helpers.go      # Common validation functions
│   └── cleanup_helpers.go         # Resource cleanup utilities
├── fixtures/                      # Test data generators
│   └── generators.go              # Programmatic configuration generation
├── parallel_config.go             # Parallel execution configuration
├── performance_config.go          # Performance optimization settings
└── README.md                      # Test suite overview
```

### Test Framework Components

- **Ginkgo v2**: BDD-style test framework for structured test organization
- **Gomega**: Assertion library for expressive test validations
- **Controller-Runtime**: Kubernetes client and testing utilities
- **Kind**: Local Kubernetes cluster for test execution

## Prerequisites

### Required Software

1. **Go 1.21+**: Required for running tests
2. **Docker**: Required for building container images
3. **Kind**: Required for local Kubernetes cluster
4. **kubectl**: Required for cluster interaction
5. **Make**: Required for build automation

### Installation Commands

```bash
# Install Kind
go install sigs.k8s.io/kind@latest

# Create Kind cluster
kind create cluster --name optipod-test

# Verify cluster
kubectl cluster-info --context kind-optipod-test
```

### Environment Setup

```bash
# Set required environment variables
export KUBECONFIG="$(kind get kubeconfig-path --name="optipod-test")"
export E2E_PARALLEL_NODES=4
export E2E_TIMEOUT_MULTIPLIER=1.0

# Optional: Skip component installation if already present
export CERT_MANAGER_INSTALL_SKIP=false
export METRICS_SERVER_INSTALL_SKIP=false
```

## Running Tests

### Full Test Suite

```bash
# Run all e2e tests
make test-e2e

# Run with verbose output
make test-e2e ARGS="-v"

# Run with specific timeout
make test-e2e ARGS="-timeout=30m"
```

### Specific Test Categories

```bash
# Run policy mode tests only
go test -v -tags=e2e ./test/e2e -run "Policy.*Mode"

# Run resource bounds tests only
go test -v -tags=e2e ./test/e2e -run "Resource.*Bounds"

# Run RBAC security tests only
go test -v -tags=e2e ./test/e2e -run "RBAC.*Security"

# Run error handling tests only
go test -v -tags=e2e ./test/e2e -run "Error.*Handling"

# Run observability tests only
go test -v -tags=e2e ./test/e2e -run "Observability"
```

### Property-Based Tests

```bash
# Run all property-based tests
go test -v -tags=e2e ./test/e2e -run "Property.*"

# Run specific property test
go test -v -tags=e2e ./test/e2e -run "Property.*1.*Policy.*mode"

# Run diagnostic collection property test
go test -v -tags=e2e ./test/e2e -run "Property.*20.*Diagnostic"
```

### Parallel Execution

```bash
# Run tests in parallel (default: 4 nodes)
go test -v -tags=e2e ./test/e2e -ginkgo.procs=4

# Run with custom parallel configuration
E2E_PARALLEL_NODES=8 go test -v -tags=e2e ./test/e2e -ginkgo.procs=8

# Run with timeout multiplier for parallel execution
E2E_TIMEOUT_MULTIPLIER=2.0 go test -v -tags=e2e ./test/e2e -ginkgo.procs=4
```

## Test Configuration

### Environment Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `E2E_PARALLEL_NODES` | Number of parallel test nodes | 4 | 8 |
| `E2E_TIMEOUT_MULTIPLIER` | Timeout multiplier for parallel execution | 1.0 | 2.0 |
| `CERT_MANAGER_INSTALL_SKIP` | Skip CertManager installation | false | true |
| `METRICS_SERVER_INSTALL_SKIP` | Skip MetricsServer installation | false | true |
| `KUBECONFIG` | Kubernetes configuration file | ~/.kube/config | /tmp/kubeconfig |

### Performance Configuration

The test suite includes performance optimizations for different execution environments:

```go
// Performance settings in performance_config.go
type PerformanceConfig struct {
    DefaultTimeout    time.Duration  // Default operation timeout
    MaxRetries        int           // Maximum retry attempts
    RetryInterval     time.Duration  // Interval between retries
    ParallelSafety    bool          // Enable parallel execution safety
}
```

### Parallel Execution Configuration

```go
// Parallel settings in parallel_config.go
type ParallelConfig struct {
    Enabled           bool          // Enable parallel execution
    NodeCount         int           // Number of parallel nodes
    TimeoutMultiplier float64       // Timeout adjustment for parallel runs
    IsolationMode     string        // Namespace isolation strategy
}
```

## Test Categories

### 1. Policy Mode Tests

**Location**: `policy_modes_test.go`

**Purpose**: Validate OptipPod behavior across different policy modes

**Test Cases**:
- Auto mode: Verify recommendations are applied automatically
- Recommend mode: Verify recommendations are generated but not applied
- Disabled mode: Verify no processing occurs

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Policy.*Mode.*Auto"
```

### 2. Resource Bounds Tests

**Location**: `resource_bounds_test.go`

**Purpose**: Validate resource bounds enforcement and clamping

**Test Cases**:
- Within bounds: Verify recommendations respect configured limits
- Below minimum: Verify recommendations are clamped to minimum values
- Above maximum: Verify recommendations are clamped to maximum values

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Resource.*Bounds.*Clamping"
```

### 3. RBAC Security Tests

**Location**: `rbac_security_test.go`

**Purpose**: Validate RBAC permissions and security constraints

**Test Cases**:
- Restricted permissions: Verify appropriate error handling
- Security policies: Verify compliance with pod security policies
- Permission escalation: Verify prevention of privilege escalation

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "RBAC.*Restricted"
```

### 4. Error Handling Tests

**Location**: `error_handling_test.go`

**Purpose**: Validate error conditions and edge cases

**Test Cases**:
- Invalid configurations: Verify rejection and error messages
- Missing metrics: Verify graceful degradation
- Concurrent modifications: Verify conflict resolution

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Error.*Invalid.*Configuration"
```

### 5. Workload Type Tests

**Location**: `workload_types_test.go`

**Purpose**: Validate support for different Kubernetes workload types

**Test Cases**:
- Deployments: Verify recommendation and update behavior
- StatefulSets: Verify ordered update handling
- DaemonSets: Verify node-based update behavior

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Workload.*StatefulSet"
```

### 6. Observability Tests

**Location**: `observability_test.go`

**Purpose**: Validate metrics exposure and logging

**Test Cases**:
- Prometheus metrics: Verify metric exposure and accuracy
- Controller logs: Verify log content and formatting
- Monitoring integration: Verify alert and health check functionality

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Observability.*Metrics"
```

### 7. Property-Based Tests

**Location**: Various `*_property_test.go` files

**Purpose**: Validate universal properties across multiple inputs

**Test Cases**:
- Policy mode consistency: Verify behavior across all policy modes
- Resource bounds enforcement: Verify bounds respect across all configurations
- Diagnostic collection: Verify comprehensive diagnostic information collection

**Example**:
```bash
go test -v -tags=e2e ./test/e2e -run "Property.*1.*Policy"
```

## Troubleshooting

### Common Issues

#### 1. Kind Cluster Not Found

**Error**: `ERROR: no nodes found for cluster "kind"`

**Solution**:
```bash
# Create Kind cluster
kind create cluster --name kind

# Verify cluster exists
kind get clusters
```

#### 2. Image Load Failures

**Error**: `Failed to load the manager(Operator) image into Kind`

**Solution**:
```bash
# Build image first
make docker-build IMG=example.com/optipod:v0.0.1

# Load image manually
kind load docker-image example.com/optipod:v0.0.1 --name kind
```

#### 3. CertManager Installation Issues

**Error**: `Failed to install CertManager`

**Solution**:
```bash
# Skip CertManager installation if already present
export CERT_MANAGER_INSTALL_SKIP=true

# Or install manually
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

#### 4. Test Timeouts

**Error**: `Test exceeded timeout`

**Solution**:
```bash
# Increase timeout
go test -v -tags=e2e ./test/e2e -timeout=60m

# Or adjust timeout multiplier for parallel execution
E2E_TIMEOUT_MULTIPLIER=3.0 go test -v -tags=e2e ./test/e2e
```

#### 5. Resource Cleanup Issues

**Error**: `Failed to cleanup test resources`

**Solution**:
```bash
# Manual cleanup
kubectl delete namespace --all --selector=test-namespace=true

# Force cleanup
kubectl patch namespace test-namespace -p '{"metadata":{"finalizers":[]}}' --type=merge
kubectl delete namespace test-namespace --force --grace-period=0
```

### Debug Mode

Enable debug mode for detailed test execution information:

```bash
# Enable Ginkgo debug output
go test -v -tags=e2e ./test/e2e -ginkgo.v -ginkgo.trace

# Enable controller-runtime debug logging
KUBEBUILDER_ASSETS=$(setup-envtest use --use-env -p path) \
go test -v -tags=e2e ./test/e2e -ginkgo.v
```

### Log Collection

Collect logs for troubleshooting:

```bash
# Collect controller logs
kubectl logs -n optipod-system deployment/optipod-controller-manager

# Collect test artifacts
ls -la /tmp/optipod-diagnostics/

# Collect Kind cluster logs
kind export logs /tmp/kind-logs --name kind
```

## Development Guidelines

### Writing New Tests

#### 1. Test Structure

Follow the established patterns for test organization:

```go
var _ = Describe("Feature Name", func() {
    var (
        ctx           context.Context
        namespace     string
        cleanupHelper *helpers.CleanupHelper
    )

    BeforeEach(func() {
        ctx = context.Background()
        namespace = fmt.Sprintf("test-%d", time.Now().Unix())
        
        // Create test namespace
        ns := &corev1.Namespace{
            ObjectMeta: metav1.ObjectMeta{Name: namespace},
        }
        Expect(k8sClient.Create(ctx, ns)).To(Succeed())

        cleanupHelper = helpers.NewCleanupHelper(k8sClient)
        cleanupHelper.TrackNamespace(namespace)
    })

    AfterEach(func() {
        if cleanupHelper != nil {
            cleanupHelper.CleanupAll()
        }
    })

    Context("Test Category", func() {
        It("should validate specific behavior", func() {
            // Test implementation
        })
    })
})
```

#### 2. Property-Based Tests

For property-based tests, use table-driven approaches:

```go
DescribeTable("property validation scenarios",
    func(scenario TestScenario) {
        // Test implementation using scenario
    },
    Entry("scenario 1", TestScenario{...}),
    Entry("scenario 2", TestScenario{...}),
)
```

#### 3. Helper Usage

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

// Cleanup operations
cleanupHelper.TrackDeployment(deployment.Name, deployment.Namespace)
```

#### 4. Configuration Generation

Use fixtures for generating test configurations:

```go
// Policy configuration
generator := fixtures.NewPolicyConfigGenerator()
config := generator.GenerateBasicPolicyConfig("test-policy", v1alpha1.ModeAuto)

// Workload configuration
workloadGen := fixtures.NewWorkloadConfigGenerator()
workloadConfig := workloadGen.GenerateBasicWorkloadConfig("test-workload", helpers.WorkloadTypeDeployment)

// Test scenarios
scenarioGen := fixtures.NewTestScenarioGenerator()
policyConfig, workloadConfig := scenarioGen.GeneratePolicyModeScenario(v1alpha1.ModeRecommend)
```

### Code Quality Standards

#### 1. Test Naming

- Use descriptive test names that explain the behavior being tested
- Follow the pattern: "should [expected behavior] when [condition]"
- Use consistent naming for similar test categories

#### 2. Error Handling

- Always check for errors in test setup and teardown
- Use appropriate Gomega matchers for error validation
- Provide meaningful error messages in assertions

#### 3. Resource Management

- Always track created resources for cleanup
- Use appropriate timeouts for resource operations
- Handle resource conflicts gracefully

#### 4. Documentation

- Add comments for complex test logic
- Document any special setup requirements
- Include examples in helper function documentation

### Performance Considerations

#### 1. Test Isolation

- Use unique namespaces for each test
- Avoid shared resources between tests
- Clean up resources promptly after test completion

#### 2. Parallel Execution

- Design tests to be parallel-safe
- Avoid dependencies between test cases
- Use appropriate timeouts for parallel execution

#### 3. Resource Optimization

- Minimize resource creation in test setup
- Reuse configurations where appropriate
- Use efficient cleanup strategies

## CI/CD Integration

### GitHub Actions Configuration

The e2e tests are integrated into the CI/CD pipeline using GitHub Actions:

```yaml
name: E2E Tests
on:
  pull_request:
    branches: [ main ]
  push:
    branches: [ main ]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    - name: Create Kind cluster
      run: |
        kind create cluster --name kind
        kubectl cluster-info
    - name: Run E2E tests
      run: make test-e2e
    - name: Collect test artifacts
      if: failure()
      run: |
        mkdir -p artifacts
        cp -r /tmp/optipod-diagnostics artifacts/ || true
        kind export logs artifacts/kind-logs || true
    - name: Upload artifacts
      if: failure()
      uses: actions/upload-artifact@v3
      with:
        name: e2e-test-artifacts
        path: artifacts/
```

### Test Reporting

The test suite generates structured reports for CI integration:

- **JUnit XML**: Compatible with most CI systems
- **Test artifacts**: Diagnostic information for failed tests
- **Coverage reports**: Code coverage analysis
- **Performance metrics**: Test execution timing

### Quality Gates

The CI pipeline enforces quality gates based on test results:

- **Test success rate**: All tests must pass
- **Coverage threshold**: Minimum coverage requirements
- **Performance benchmarks**: Test execution time limits
- **Resource cleanup**: Verification of proper cleanup

## Best Practices

### 1. Test Design

- Write tests that validate behavior, not implementation
- Use property-based testing for universal properties
- Include both positive and negative test cases
- Test edge cases and error conditions

### 2. Maintenance

- Keep tests up-to-date with code changes
- Refactor tests when patterns emerge
- Remove obsolete tests promptly
- Update documentation with test changes

### 3. Debugging

- Use descriptive test names and error messages
- Include relevant context in test failures
- Collect diagnostic information for complex failures
- Use appropriate logging levels for different scenarios

### 4. Collaboration

- Follow established patterns and conventions
- Review test changes carefully
- Share knowledge about test utilities and helpers
- Document any special requirements or considerations

## Additional Resources

- [Ginkgo Documentation](https://onsi.github.io/ginkgo/)
- [Gomega Documentation](https://onsi.github.io/gomega/)
- [Controller-Runtime Testing](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html)
- [Kind Documentation](https://kind.sigs.k8s.io/)
- [OptipPod Architecture Documentation](../../docs/)

For questions or issues with the test suite, please:

1. Check this guide for common solutions
2. Review existing test patterns for examples
3. Consult the troubleshooting section
4. Open an issue with detailed information about the problem