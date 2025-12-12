# E2E Test Enhancement Design Document

## Overview

This design outlines the enhancement of OptipPod's end-to-end test suite by converting comprehensive shell-based test scenarios from the hack folder into a robust Go-based testing framework using Ginkgo. The enhanced test suite will provide comprehensive validation of OptipPod's behavior across all features, edge cases, and error conditions in real Kubernetes environments.

## Architecture

### Current State
- Basic e2e tests in `test/e2e/e2e_test.go` covering controller deployment and basic functionality
- Comprehensive shell-based tests in `hack/` folder covering specific scenarios
- Manual test execution and validation

### Target State
- Unified Go-based e2e test suite using Ginkgo framework
- Automated test execution in CI/CD pipeline
- Comprehensive coverage of all OptipPod features and edge cases
- Structured test organization with reusable components

### Test Suite Architecture

```
test/e2e/
├── e2e_suite_test.go          # Test suite setup and configuration
├── e2e_test.go                # Basic controller and metrics tests (existing)
├── policy_modes_test.go       # Auto, Recommend, Disabled mode tests
├── resource_bounds_test.go    # Resource bounds enforcement tests
├── rbac_security_test.go      # RBAC and security constraint tests
├── error_handling_test.go     # Error conditions and edge cases
├── workload_types_test.go     # Different workload types and update strategies
├── observability_test.go      # Metrics and logging validation
├── helpers/
│   ├── policy_helpers.go      # Policy creation and validation helpers
│   ├── workload_helpers.go    # Workload creation and management helpers
│   ├── validation_helpers.go  # Common validation functions
│   └── cleanup_helpers.go     # Resource cleanup utilities
└── fixtures/
    └── generators.go          # Programmatic YAML generation
```

## Components and Interfaces

### Test Helper Components

#### PolicyHelper
```go
type PolicyHelper struct {
    client client.Client
    namespace string
}

// CreateOptimizationPolicy creates a policy with specified configuration
func (h *PolicyHelper) CreateOptimizationPolicy(config PolicyConfig) (*v1alpha1.OptimizationPolicy, error)

// WaitForPolicyReady waits for policy to reach Ready condition
func (h *PolicyHelper) WaitForPolicyReady(policyName string, timeout time.Duration) error

// ValidatePolicyBehavior validates policy behavior based on mode
func (h *PolicyHelper) ValidatePolicyBehavior(policyName string, expectedMode PolicyMode) error
```

#### WorkloadHelper
```go
type WorkloadHelper struct {
    client client.Client
    namespace string
}

// CreateDeployment creates a deployment with specified resources
func (h *WorkloadHelper) CreateDeployment(config WorkloadConfig) (*appsv1.Deployment, error)

// CreateStatefulSet creates a statefulset with specified resources
func (h *WorkloadHelper) CreateStatefulSet(config WorkloadConfig) (*appsv1.StatefulSet, error)

// WaitForWorkloadReady waits for workload to be ready
func (h *WorkloadHelper) WaitForWorkloadReady(workloadName string, timeout time.Duration) error

// GetWorkloadAnnotations retrieves OptipPod annotations from workload
func (h *WorkloadHelper) GetWorkloadAnnotations(workloadName string) (map[string]string, error)
```

#### ValidationHelper
```go
type ValidationHelper struct {
    client client.Client
}

// ValidateResourceBounds checks if recommendations respect bounds
func (h *ValidationHelper) ValidateResourceBounds(recommendations map[string]string, bounds ResourceBounds) error

// ValidateRecommendations verifies recommendation format and values
func (h *ValidationHelper) ValidateRecommendations(annotations map[string]string) error

// ValidateWorkloadUpdate checks if workload was updated according to policy mode
func (h *ValidationHelper) ValidateWorkloadUpdate(workloadName string, mode PolicyMode) error

// ValidateMetrics verifies OptipPod metrics are exposed correctly
func (h *ValidationHelper) ValidateMetrics(expectedMetrics []string) error
```

#### CleanupHelper
```go
type CleanupHelper struct {
    client client.Client
    resources []ResourceRef
}

// TrackResource adds a resource to cleanup list
func (h *CleanupHelper) TrackResource(resource ResourceRef)

// CleanupAll removes all tracked resources
func (h *CleanupHelper) CleanupAll() error

// CleanupNamespace removes all resources in a namespace
func (h *CleanupHelper) CleanupNamespace(namespace string) error
```

## Data Models

### Test Configuration Models

```go
type PolicyConfig struct {
    Name                    string
    Mode                    PolicyMode
    NamespaceSelector       map[string]string
    WorkloadSelector        map[string]string
    ResourceBounds          ResourceBounds
    MetricsConfig          MetricsConfig
    UpdateStrategy         UpdateStrategy
    ReconciliationInterval time.Duration
}

type WorkloadConfig struct {
    Name         string
    Namespace    string
    Type         WorkloadType // Deployment, StatefulSet, DaemonSet
    Labels       map[string]string
    Resources    ResourceRequirements
    Replicas     int32
}

type ResourceBounds struct {
    CPU    ResourceBound
    Memory ResourceBound
}

type ResourceBound struct {
    Min string
    Max string
}

type PolicyMode string
const (
    PolicyModeAuto      PolicyMode = "Auto"
    PolicyModeRecommend PolicyMode = "Recommend"
    PolicyModeDisabled  PolicyMode = "Disabled"
)

type WorkloadType string
const (
    WorkloadTypeDeployment  WorkloadType = "Deployment"
    WorkloadTypeStatefulSet WorkloadType = "StatefulSet"
    WorkloadTypeDaemonSet   WorkloadType = "DaemonSet"
)
```

### Test Scenario Models

```go
type TestScenario struct {
    Name        string
    Description string
    Setup       func() error
    Execute     func() error
    Validate    func() error
    Cleanup     func() error
}

type BoundsTestCase struct {
    Name             string
    PolicyBounds     ResourceBounds
    WorkloadResources ResourceRequirements
    ExpectedBehavior BoundsExpectation
}

type BoundsExpectation string
const (
    BoundsWithin      BoundsExpectation = "within"
    BoundsClampedMin  BoundsExpectation = "clamped-to-min"
    BoundsClampedMax  BoundsExpectation = "clamped-to-max"
)
```

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system-essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property Reflection

After reviewing all properties identified in the prework, several can be consolidated:
- Properties 1.1, 1.2, and 1.3 can be combined into a comprehensive policy mode validation property
- Properties 2.2, 2.3, and 2.4 can be combined into a single bounds enforcement property
- Properties 3.1, 3.2, and 3.5 can be combined into an RBAC lifecycle property
- Properties 4.1, 4.2, and 4.3 can be combined into a comprehensive error handling property

Property 1: Policy mode behavior consistency
*For any* optimization policy configuration and workload, the policy mode should consistently determine whether recommendations are generated and whether updates are applied across all workload types
**Validates: Requirements 1.1, 1.2, 1.3**

Property 2: Resource bounds enforcement
*For any* optimization policy with resource bounds and any workload, recommendations should always respect the configured minimum and maximum limits, clamping values when necessary
**Validates: Requirements 2.2, 2.3, 2.4**

Property 3: Resource quantity parsing consistency
*For any* resource specification with different units (m, Mi, Gi, etc.), the parsing and comparison logic should correctly handle unit conversions and maintain ordering relationships
**Validates: Requirements 2.5**

Property 4: RBAC lifecycle management
*For any* RBAC test scenario, service accounts and role bindings should be created with correct permissions, tested for expected behavior, and completely cleaned up afterward
**Validates: Requirements 3.1, 3.2, 3.5**

Property 5: Security constraint compliance
*For any* security policy configuration, OptipPod should respect pod security policies and report clear error messages when constraints are violated
**Validates: Requirements 3.3, 3.4**

Property 6: Error handling robustness
*For any* invalid configuration or error condition, OptipPod should handle the error gracefully, provide clear error messages, and maintain system stability
**Validates: Requirements 4.1, 4.2, 4.3**

Property 7: Concurrent modification safety
*For any* concurrent modification scenario, OptipPod should handle resource conflicts correctly without data corruption or inconsistent state
**Validates: Requirements 4.4**

Property 8: Memory decrease safety
*For any* workload with memory decrease recommendations, unsafe decreases should be prevented or flagged according to safety policies
**Validates: Requirements 4.5**

Property 9: Workload type consistency
*For any* supported workload type (Deployment, StatefulSet, DaemonSet), OptipPod behavior should be consistent in terms of discovery, recommendation generation, and update application
**Validates: Requirements 5.1, 5.3**

Property 10: Update strategy compliance
*For any* configured update strategy, OptipPod should apply updates using only the specified method (in-place resize, recreation, requests-only)
**Validates: Requirements 5.2, 5.4**

Property 11: Status reporting accuracy
*For any* workload processing operation, the workload status should accurately reflect the current state and any applied changes
**Validates: Requirements 5.5**

Property 12: CI integration reliability
*For any* test execution in CI environment, the test suite should provide appropriate exit codes and handle failures consistently
**Validates: Requirements 6.3**

Property 13: Test artifact generation
*For any* test execution, appropriate reports and debugging artifacts should be generated for analysis
**Validates: Requirements 6.5**

Property 14: Programmatic configuration generation
*For any* test scenario requiring configuration, YAML should be generated programmatically rather than loaded from static files
**Validates: Requirements 7.3**

Property 15: Metrics exposure correctness
*For any* OptipPod operation, appropriate Prometheus metrics should be exposed with values that accurately reflect system state
**Validates: Requirements 8.1, 8.3**

Property 16: Log content validation
*For any* OptipPod operation, controller logs should contain expected information for debugging and monitoring
**Validates: Requirements 8.2**

Property 17: Monitoring system integration
*For any* monitoring configuration, alerts and health checks should respond correctly to system conditions
**Validates: Requirements 8.4**

Property 18: Metrics endpoint security
*For any* metrics endpoint access, security constraints should be enforced while maintaining accessibility for authorized users
**Validates: Requirements 8.5**

Property 19: Test cleanup completeness
*For any* test execution, all created resources should be automatically cleaned up without leaving orphaned resources
**Validates: Requirements 1.4**

Property 20: Diagnostic information collection
*For any* test failure, comprehensive diagnostic information including logs and resource states should be collected and reported
**Validates: Requirements 1.5**

## Error Handling

### Test Failure Handling
- Comprehensive diagnostic collection on test failures
- Resource state snapshots for debugging
- Controller log collection with appropriate filtering
- Kubernetes event collection for context

### Resource Cleanup
- Automatic cleanup of all test resources on completion
- Cleanup on test failure to prevent resource leaks
- Namespace-based isolation for test scenarios
- Graceful handling of cleanup failures

### Timeout Management
- Configurable timeouts for different operations
- Progressive timeout strategies for complex scenarios
- Proper timeout handling to prevent test hangs
- Clear timeout error messages

## Testing Strategy

### Dual Testing Approach

The e2e test enhancement will use both unit testing principles and property-based testing concepts:

**Unit Testing Approach:**
- Specific test scenarios that validate concrete behavior
- Edge case testing with known inputs and expected outputs
- Integration testing between OptipPod components
- Regression testing for known issues

**Property-Based Testing Approach:**
- Use Ginkgo's table-driven tests to validate properties across multiple inputs
- Generate test configurations programmatically to cover various scenarios
- Validate universal properties that should hold across all valid inputs
- Each property-based test should run with multiple generated test cases

**Property-Based Testing Requirements:**
- Use Ginkgo's `DescribeTable` and `Entry` functions for property-based scenarios
- Configure each property-based test to run with at least 10 different generated configurations
- Tag each property-based test with comments referencing the design document property
- Use this exact format: `**Feature: e2e-test-enhancement, Property {number}: {property_text}**`
- Each correctness property must be implemented by a single property-based test

**Testing Framework:**
- Primary framework: Ginkgo v2 for BDD-style test organization
- Assertion library: Gomega for expressive assertions
- Kubernetes testing: controller-runtime's envtest for integration scenarios
- Property generation: Custom generators for OptipPod-specific configurations

**Test Organization:**
- Group related tests into logical contexts using Ginkgo's `Context` blocks
- Use `BeforeEach` and `AfterEach` for test setup and cleanup
- Implement helper functions for common operations
- Use table-driven tests for scenarios with multiple similar test cases

**Test Execution:**
- Tests should be runnable both locally and in CI environments
- Support for parallel test execution where appropriate
- Configurable test timeouts based on environment
- Proper test isolation to prevent interference between tests