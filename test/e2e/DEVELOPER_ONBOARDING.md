# OptipPod E2E Testing - Developer Onboarding Guide

Welcome to OptipPod's end-to-end testing framework! This guide will help you get started with understanding, running, and contributing to our comprehensive test suite.

## Table of Contents

- [Overview](#overview)
- [Getting Started](#getting-started)
- [Understanding the Test Architecture](#understanding-the-test-architecture)
- [Your First Test](#your-first-test)
- [Common Patterns](#common-patterns)
- [Best Practices](#best-practices)
- [Contributing Guidelines](#contributing-guidelines)
- [Resources and References](#resources-and-references)

## Overview

OptipPod's e2e test suite is designed to validate the complete functionality of the OptipPod controller in real Kubernetes environments. The tests are built using:

- **Ginkgo v2**: BDD-style testing framework
- **Gomega**: Assertion library
- **Controller-Runtime**: Kubernetes client and testing utilities
- **Kind**: Local Kubernetes cluster for testing

### What Makes Our Tests Special

1. **Property-Based Testing**: We validate universal properties that should hold across all inputs
2. **Comprehensive Coverage**: Tests cover all OptipPod features, edge cases, and error conditions
3. **Real Environment Testing**: Tests run against actual Kubernetes clusters
4. **Parallel Execution**: Tests are designed to run efficiently in parallel
5. **Diagnostic Collection**: Comprehensive failure analysis and debugging support

## Getting Started

### Prerequisites

Before you begin, ensure you have:

1. **Go 1.21+** installed
2. **Docker** running
3. **Kind** installed: `go install sigs.k8s.io/kind@latest`
4. **kubectl** configured
5. **Make** available for build automation

### Initial Setup

1. **Clone the repository**:
   ```bash
   git clone https://github.com/optipod/optipod.git
   cd optipod
   ```

2. **Set up your development environment**:
   ```bash
   # Create Kind cluster
   kind create cluster --name optipod-dev
   
   # Set kubeconfig
   export KUBECONFIG="$(kind get kubeconfig-path --name="optipod-dev")"
   
   # Verify cluster
   kubectl cluster-info
   ```

3. **Build and load the OptipPod image**:
   ```bash
   # Build the image
   make docker-build IMG=example.com/optipod:dev
   
   # Load into Kind
   kind load docker-image example.com/optipod:dev --name optipod-dev
   ```

4. **Run your first test**:
   ```bash
   # Run a simple test to verify setup
   go test -v -tags=e2e ./test/e2e -run "TestBasicController" -timeout=10m
   ```

### Development Environment Configuration

Create a `.env` file in your project root for consistent development settings:

```bash
# .env file for development
export KUBECONFIG="$(kind get kubeconfig-path --name="optipod-dev")"
export E2E_PARALLEL_NODES=2
export E2E_TIMEOUT_MULTIPLIER=2.0
export CERT_MANAGER_INSTALL_SKIP=false
export METRICS_SERVER_INSTALL_SKIP=false
```

Source it in your shell:
```bash
source .env
```

## Understanding the Test Architecture

### Directory Structure Deep Dive

```
test/e2e/
├── e2e_suite_test.go              # Test suite setup and teardown
├── e2e_test.go                    # Basic controller functionality tests
├── policy_modes_test.go           # Policy mode validation (Auto/Recommend/Disabled)
├── resource_bounds_test.go        # Resource bounds enforcement tests
├── rbac_security_test.go          # RBAC and security constraint tests
├── error_handling_test.go         # Error conditions and edge cases
├── workload_types_test.go         # Workload type support tests
├── observability_test.go          # Metrics and logging validation
├── diagnostic_collection_property_test.go  # Diagnostic collection tests
├── helpers/                       # Reusable test utilities
│   ├── policy_helpers.go          # Policy creation and management
│   ├── workload_helpers.go        # Workload creation and management
│   ├── validation_helpers.go      # Common validation functions
│   ├── cleanup_helpers.go         # Resource cleanup utilities
│   └── reporting_helpers.go       # Test reporting and dashboards
├── fixtures/                      # Test data generation
│   └── generators.go              # Programmatic configuration generation
├── parallel_config.go             # Parallel execution configuration
├── performance_config.go          # Performance optimization settings
├── TESTING_GUIDE.md              # Comprehensive testing guide
├── TROUBLESHOOTING.md            # Troubleshooting guide
└── README.md                     # Test suite overview
```

### Key Components

#### 1. Test Suite Setup (`e2e_suite_test.go`)

This file contains the BeforeSuite and AfterSuite hooks that:
- Initialize the Kubernetes client
- Set up the test environment (Kind cluster, CertManager, MetricsServer)
- Configure parallel execution
- Handle cleanup after all tests complete

#### 2. Helper Components (`helpers/`)

These provide reusable utilities for common test operations:

- **PolicyHelper**: Create and manage OptimizationPolicies
- **WorkloadHelper**: Create and manage Kubernetes workloads
- **ValidationHelper**: Validate test results and system state
- **CleanupHelper**: Ensure proper resource cleanup

#### 3. Test Fixtures (`fixtures/`)

Generate test configurations programmatically:
- Policy configurations with various modes and bounds
- Workload configurations for different types
- Test scenarios for comprehensive coverage

#### 4. Configuration Files

- **parallel_config.go**: Manages parallel test execution
- **performance_config.go**: Optimizes test performance

### Test Categories

#### 1. Unit-Style Tests
Test specific functionality with known inputs and expected outputs:

```go
It("should create policy with Auto mode", func() {
    config := helpers.PolicyConfig{
        Name: "test-policy",
        Mode: v1alpha1.ModeAuto,
        // ... configuration
    }
    
    policy, err := policyHelper.CreateOptimizationPolicy(config)
    Expect(err).NotTo(HaveOccurred())
    Expect(policy.Spec.Mode).To(Equal(v1alpha1.ModeAuto))
})
```

#### 2. Property-Based Tests
Validate universal properties across multiple inputs:

```go
DescribeTable("policy mode behavior consistency",
    func(mode v1alpha1.PolicyMode, expectUpdates bool) {
        // Generate random configuration
        config := generator.GenerateRandomPolicyConfig("test", mode)
        
        // Test the property
        policy, err := policyHelper.CreateOptimizationPolicy(config)
        Expect(err).NotTo(HaveOccurred())
        
        // Validate universal property
        if expectUpdates {
            // Verify updates are applied
        } else {
            // Verify no updates occur
        }
    },
    Entry("Auto mode applies updates", v1alpha1.ModeAuto, true),
    Entry("Recommend mode generates recommendations", v1alpha1.ModeRecommend, false),
    Entry("Disabled mode does nothing", v1alpha1.ModeDisabled, false),
)
```

## Your First Test

Let's walk through creating a simple test to understand the patterns:

### Step 1: Create the Test File

Create `test/e2e/my_first_test.go`:

```go
//go:build e2e
// +build e2e

package e2e

import (
    "context"
    "fmt"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    "github.com/optipod/optipod/api/v1alpha1"
    "github.com/optipod/optipod/test/e2e/helpers"
)

var _ = Describe("My First Test", func() {
    var (
        ctx           context.Context
        namespace     string
        cleanupHelper *helpers.CleanupHelper
        policyHelper  *helpers.PolicyHelper
    )

    BeforeEach(func() {
        ctx = context.Background()
        namespace = fmt.Sprintf("my-test-%d", time.Now().Unix())
        
        // Create test namespace
        ns := &corev1.Namespace{
            ObjectMeta: metav1.ObjectMeta{Name: namespace},
        }
        Expect(k8sClient.Create(ctx, ns)).To(Succeed())

        // Initialize helpers
        cleanupHelper = helpers.NewCleanupHelper(k8sClient)
        cleanupHelper.TrackNamespace(namespace)
        
        policyHelper = helpers.NewPolicyHelper(k8sClient, namespace)
    })

    AfterEach(func() {
        if cleanupHelper != nil {
            cleanupHelper.CleanupAll()
        }
    })

    Context("Basic Policy Operations", func() {
        It("should create a policy successfully", func() {
            By("Creating a basic policy configuration")
            config := helpers.PolicyConfig{
                Name: "my-first-policy",
                Mode: v1alpha1.ModeRecommend,
                NamespaceSelector: map[string]string{
                    "environment": "test",
                },
                WorkloadSelector: map[string]string{
                    "optimize": "true",
                },
            }

            By("Creating the OptimizationPolicy")
            policy, err := policyHelper.CreateOptimizationPolicy(config)
            Expect(err).NotTo(HaveOccurred())
            Expect(policy).NotTo(BeNil())

            By("Validating the policy was created correctly")
            Expect(policy.Name).To(Equal("my-first-policy"))
            Expect(policy.Spec.Mode).To(Equal(v1alpha1.ModeRecommend))
            Expect(policy.Namespace).To(Equal(namespace))

            By("Verifying the policy exists in the cluster")
            retrievedPolicy := &v1alpha1.OptimizationPolicy{}
            err = k8sClient.Get(ctx, client.ObjectKey{
                Name:      "my-first-policy",
                Namespace: namespace,
            }, retrievedPolicy)
            Expect(err).NotTo(HaveOccurred())
            Expect(retrievedPolicy.Spec.Mode).To(Equal(v1alpha1.ModeRecommend))
        })
    })
})
```

### Step 2: Run Your Test

```bash
go test -v -tags=e2e ./test/e2e -run "My First Test" -timeout=10m
```

### Step 3: Understand the Output

You should see output like:
```
Running Suite: e2e suite
Random Seed: 1234567890

Will run 1 of X specs

My First Test Basic Policy Operations
  should create a policy successfully
  /path/to/my_first_test.go:XX
•

Ran 1 of X Specs in 0.123 seconds
SUCCESS! -- 1 Passed | 0 Failed | 0 Pending | 0 Skipped
```

## Common Patterns

### 1. Test Structure Pattern

All tests follow this structure:

```go
var _ = Describe("Feature Name", func() {
    var (
        // Test variables
        ctx           context.Context
        namespace     string
        cleanupHelper *helpers.CleanupHelper
    )

    BeforeEach(func() {
        // Setup for each test
    })

    AfterEach(func() {
        // Cleanup after each test
    })

    Context("Test Category", func() {
        It("should validate specific behavior", func() {
            By("Step 1: Setup")
            // Test setup

            By("Step 2: Execute")
            // Test execution

            By("Step 3: Validate")
            // Test validation
        })
    })
})
```

### 2. Helper Usage Pattern

Use helpers for common operations:

```go
// Policy operations
policyHelper := helpers.NewPolicyHelper(k8sClient, namespace)
policy, err := policyHelper.CreateOptimizationPolicy(config)
Expect(err).NotTo(HaveOccurred())

// Workload operations
workloadHelper := helpers.NewWorkloadHelper(k8sClient, namespace)
deployment, err := workloadHelper.CreateDeployment(workloadConfig)
Expect(err).NotTo(HaveOccurred())

// Validation operations
validationHelper := helpers.NewValidationHelper(k8sClient)
err = validationHelper.ValidateResourceBounds(recommendations, bounds)
Expect(err).NotTo(HaveOccurred())

// Cleanup tracking
cleanupHelper.TrackPolicy(policy.Name, policy.Namespace)
cleanupHelper.TrackDeployment(deployment.Name, deployment.Namespace)
```

### 3. Configuration Generation Pattern

Use fixtures for generating test data:

```go
// Generate policy configuration
generator := fixtures.NewPolicyConfigGenerator()
config := generator.GenerateBasicPolicyConfig("test-policy", v1alpha1.ModeAuto)

// Generate workload configuration
workloadGen := fixtures.NewWorkloadConfigGenerator()
workloadConfig := workloadGen.GenerateBasicWorkloadConfig("test-workload", helpers.WorkloadTypeDeployment)

// Generate test scenarios
scenarioGen := fixtures.NewTestScenarioGenerator()
policyConfig, workloadConfig := scenarioGen.GeneratePolicyModeScenario(v1alpha1.ModeRecommend)
```

### 4. Property-Based Testing Pattern

Use table-driven tests for properties:

```go
DescribeTable("property validation scenarios",
    func(scenario TestScenario) {
        By("Setting up the scenario")
        // Setup based on scenario

        By("Executing the test")
        // Execute the operation

        By("Validating the property")
        // Validate the universal property holds
    },
    Entry("scenario 1", TestScenario{...}),
    Entry("scenario 2", TestScenario{...}),
    Entry("scenario 3", TestScenario{...}),
)
```

### 5. Error Handling Pattern

Always handle errors appropriately:

```go
// For operations that should succeed
result, err := someOperation()
Expect(err).NotTo(HaveOccurred())
Expect(result).NotTo(BeNil())

// For operations that should fail
result, err := invalidOperation()
Expect(err).To(HaveOccurred())
Expect(err.Error()).To(ContainSubstring("expected error message"))

// For conditional errors
if shouldSucceed {
    Expect(err).NotTo(HaveOccurred())
} else {
    Expect(err).To(HaveOccurred())
}
```

## Best Practices

### 1. Test Design

- **Test behavior, not implementation**: Focus on what the system should do, not how it does it
- **Use descriptive test names**: Names should explain the expected behavior
- **Keep tests independent**: Each test should be able to run in isolation
- **Use appropriate test granularity**: Balance between too many small tests and too few large tests

### 2. Resource Management

- **Always use cleanup helpers**: Track all created resources for cleanup
- **Use unique names**: Avoid conflicts with concurrent test execution
- **Handle resource conflicts**: Implement retry logic for resource operations
- **Clean up promptly**: Don't leave resources hanging around

### 3. Assertions and Validations

- **Use appropriate matchers**: Choose the most specific Gomega matcher
- **Provide meaningful error messages**: Help future developers understand failures
- **Validate multiple aspects**: Don't just check that operations succeed
- **Use Eventually for async operations**: Handle timing issues properly

```go
// Good: Specific matcher with meaningful message
Expect(policy.Spec.Mode).To(Equal(v1alpha1.ModeAuto), "Policy should be in Auto mode")

// Good: Eventually for async operations
Eventually(func() bool {
    return policyHelper.IsPolicyReady("test-policy")
}).WithTimeout(30*time.Second).WithPolling(1*time.Second).Should(BeTrue())

// Good: Multiple validations
Expect(deployment.Status.Replicas).To(Equal(int32(1)))
Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)))
Expect(deployment.Status.AvailableReplicas).To(Equal(int32(1)))
```

### 4. Performance Considerations

- **Use parallel-safe patterns**: Design tests to run concurrently
- **Minimize resource creation**: Only create what you need for the test
- **Use appropriate timeouts**: Balance between reliability and speed
- **Leverage test fixtures**: Reuse configurations where appropriate

### 5. Documentation

- **Comment complex logic**: Explain non-obvious test behavior
- **Use descriptive By() statements**: Document test steps clearly
- **Update documentation**: Keep guides current with test changes
- **Include examples**: Show how to use new patterns or helpers

## Contributing Guidelines

### 1. Before You Start

- **Check existing tests**: Look for similar functionality before creating new tests
- **Understand the architecture**: Review existing patterns and helpers
- **Plan your approach**: Consider what you're testing and how to test it effectively
- **Discuss complex changes**: Reach out to the team for significant additions

### 2. Writing Tests

- **Follow established patterns**: Use the same structure and style as existing tests
- **Use existing helpers**: Leverage the helper functions rather than duplicating code
- **Add new helpers when needed**: Create reusable utilities for common operations
- **Include both positive and negative cases**: Test success and failure scenarios

### 3. Code Review Process

- **Self-review first**: Check your code against the style guide
- **Include test output**: Show that your tests pass locally
- **Explain complex logic**: Add comments for non-obvious behavior
- **Update documentation**: Include any necessary documentation updates

### 4. Testing Your Changes

Before submitting a PR:

```bash
# Run your specific tests
go test -v -tags=e2e ./test/e2e -run "YourTestName"

# Run related test categories
go test -v -tags=e2e ./test/e2e -run "RelatedCategory"

# Run the full suite (if time permits)
make test-e2e

# Check for resource leaks
kubectl get all -A | grep test
```

### 5. Common Mistakes to Avoid

- **Not cleaning up resources**: Always use cleanup helpers
- **Hard-coding values**: Use generators and configuration helpers
- **Ignoring timing issues**: Use Eventually for async operations
- **Not handling errors**: Always check and handle errors appropriately
- **Creating flaky tests**: Ensure tests are deterministic and reliable

## Resources and References

### Documentation

- [Ginkgo Documentation](https://onsi.github.io/ginkgo/) - BDD testing framework
- [Gomega Documentation](https://onsi.github.io/gomega/) - Assertion library
- [Controller-Runtime Testing](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html) - Kubernetes testing patterns
- [Kind Documentation](https://kind.sigs.k8s.io/) - Local Kubernetes clusters

### OptipPod Specific

- [TESTING_GUIDE.md](./TESTING_GUIDE.md) - Comprehensive testing guide
- [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) - Troubleshooting guide
- [README.md](./README.md) - Test suite overview
- [Architecture Documentation](../../docs/) - OptipPod architecture

### Code Examples

Look at these files for examples of different testing patterns:

- `policy_modes_test.go` - Basic test structure and policy testing
- `resource_bounds_test.go` - Table-driven tests and validation
- `error_handling_test.go` - Error condition testing
- `diagnostic_collection_property_test.go` - Property-based testing
- `helpers/policy_helpers.go` - Helper function patterns

### Getting Help

If you need help:

1. **Check the documentation**: Start with this guide and the testing guide
2. **Look at existing examples**: Find similar tests for reference
3. **Ask questions**: Reach out to the team through appropriate channels
4. **Pair program**: Work with experienced team members on complex tests

### Next Steps

Now that you understand the basics:

1. **Run the existing tests**: Get familiar with the test suite
2. **Explore the helpers**: Understand what utilities are available
3. **Write a simple test**: Start with something basic to get comfortable
4. **Contribute improvements**: Help make the test suite even better

Welcome to the OptipPod testing team! We're excited to have you contribute to our comprehensive test suite.