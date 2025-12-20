# OptipPod E2E Test Plan

## Overview
This document outlines a comprehensive end-to-end test plan for OptipPod based on analysis of the `hack/` folder test scenarios. The tests are organized by functional areas and follow a systematic approach to validate all OptipPod features.

## Test Infrastructure Setup (BeforeSuite)

### 1. Cluster Setup
- **Create Kind Cluster**: Multi-node cluster with control-plane + 2 workers
- **Install Metrics Server**: With Kind-specific patches for TLS and resolution
- **Install OptipPod CRDs**: Custom Resource Definitions
- **Install OptipPod Controller**: Manager deployment with proper RBAC
- **Setup RBAC Permissions**: Service accounts and role bindings
- **Install Test Workloads**: Sample deployments, statefulsets, daemonsets

### 2. Test Workloads (from hack/setup-dev-cluster.sh)
- **nginx-web**: Deployment (3 replicas) - Web server workload
- **redis-cache**: Deployment (2 replicas) - Cache workload  
- **postgres-db**: StatefulSet (2 replicas) - Database workload
- **log-collector**: DaemonSet - Logging workload
- **api-server**: Deployment (4 replicas) - API workload with auto-update
- **batch-worker**: Deployment (2 replicas) - Batch processing workload

## Test Categories

### 1. Policy Mode Tests (`hack/test-policy-modes.sh`)
**File**: `test/e2e/policy_modes_test.go`

#### Test Cases:
- **Auto Mode**: Should generate recommendations AND apply updates
- **Recommend Mode**: Should generate recommendations but NOT apply updates  
- **Disabled Mode**: Should NOT process workloads at all

#### Validation Points:
- Policy mode processing in logs
- Workload annotation presence (`optipod.io/managed`)
- Recommendation generation (`optipod.io/recommendation.*`)
- Actual resource updates (`optipod.io/last-applied`)

### 2. Resource Bounds Tests
**Files**: `test/e2e/resource_bounds_test.go`

#### Test Cases (from hack/test-policy-bounds-*.yaml):
- **Bounds Above Max**: Recommendations should be clamped to maximum values
- **Bounds Below Min**: Recommendations should be clamped to minimum values  
- **Bounds Within Range**: Recommendations should not be clamped
- **Boundary Limits**: Edge cases at exact min/max values

#### Validation Points:
- CPU/Memory recommendations respect bounds
- Clamping behavior works correctly
- Bounds enforcement in different scenarios

### 3. Workload Type Tests
**File**: `test/e2e/workload_types_test.go`

#### Test Cases (from hack/test-workload-*.yaml):
- **Deployment Workloads**: Standard deployment optimization
- **StatefulSet Workloads**: Persistent workload optimization
- **DaemonSet Workloads**: Node-level workload optimization
- **Multi-Container Workloads**: Workloads with multiple containers

#### Validation Points:
- All workload types are discovered and processed
- Container-specific recommendations
- Workload-specific update strategies

### 4. Update Strategy Tests  
**File**: `test/e2e/update_strategies_test.go`

#### Test Cases (from hack/test-update-strategies.sh):
- **In-Place Resize**: Updates using Kubernetes in-place resize
- **Server-Side Apply (SSA)**: Updates using SSA patches
- **Strategic Merge Patch (SMP)**: Updates using SMP patches
- **Requests Only**: Updates only resource requests, not limits
- **Limit Configuration**: Automatic limit calculation from requests

#### Validation Points:
- Correct patch type usage
- Resource update accuracy
- Update strategy compliance
- Rollback capabilities

### 5. Memory Safety Tests
**File**: `test/e2e/memory_safety_test.go`

#### Test Cases (from hack/test-memory-*-safety.sh):
- **Safe Memory Decrease**: Decreases within safety thresholds
- **Unsafe Memory Decrease**: Decreases that should be blocked
- **Memory Increase**: Memory increases should always be allowed
- **Memory Safety Thresholds**: Validation of 50% decrease limit

#### Validation Points:
- Memory decrease safety enforcement
- Warning generation for unsafe decreases
- Safety threshold calculations

### 6. Policy Weight Tests
**File**: `test/e2e/policy_weights_test.go`

#### Test Cases (from hack/test-policy-weight-*.yaml):
- **High Priority Policy**: Weight 200 - should be selected first
- **Default Priority Policy**: Weight 100 - standard priority
- **Low Priority Policy**: Weight 50 - should be selected last
- **Multiple Policy Selection**: Highest weight policy wins

#### Validation Points:
- Policy selection based on weights
- Multiple policy conflict resolution
- Weight-based prioritization logs

### 7. Error Handling Tests
**File**: `test/e2e/error_handling_test.go`

#### Test Cases (from hack/test-error-handling.sh):
- **Invalid Policy Configuration**: Validation errors for bad configs
- **Missing Metrics Provider**: Runtime errors for missing metrics
- **Workload with No Resources**: Handling workloads without resource specs
- **Workload with Invalid Resources**: Malformed resource specifications
- **RBAC Permission Errors**: Insufficient permissions handling

#### Validation Points:
- Proper error messages in logs
- Graceful degradation behavior
- Error recovery mechanisms
- Policy status error conditions

### 8. RBAC Security Tests
**File**: `test/e2e/rbac_test.go`

#### Test Cases (from hack/test-rbac-permissions.sh):
- **Normal RBAC Permissions**: Full permissions should work
- **Restricted RBAC Permissions**: Limited permissions should fail gracefully
- **Metrics Access Permissions**: Access to /metrics endpoint
- **Workload Update Permissions**: Permissions to update workloads

#### Validation Points:
- RBAC error detection in logs
- Permission-based operation blocking
- Service account restrictions
- Metrics endpoint accessibility

### 9. Observability Tests
**File**: `test/e2e/observability_test.go`

#### Test Cases:
- **Metrics Exposure**: Prometheus metrics availability
- **Metrics Content**: OptipPod-specific metrics presence
- **Metrics Format**: Proper Prometheus format compliance
- **Log Format**: Structured logging validation
- **Event Generation**: Kubernetes events for operations

#### Validation Points:
- `/metrics` endpoint accessibility
- Required metrics presence (`optipod_workloads_monitored`, etc.)
- Metric label correctness
- Log message format and content

### 10. Concurrent Modification Tests
**File**: `test/e2e/concurrent_test.go`

#### Test Cases (from hack/test-concurrent-fix.yaml):
- **Concurrent Policy Updates**: Multiple policies updating same workload
- **Concurrent Workload Updates**: External updates during OptipPod processing
- **Resource Conflict Resolution**: Handling update conflicts
- **SSA Conflict Handling**: Server-side apply conflict resolution

#### Validation Points:
- No concurrent modification errors
- Proper conflict resolution
- Resource consistency maintenance
- Update ordering and synchronization

### 11. Integration Tests
**File**: `test/e2e/integration_test.go`

#### Test Cases (from hack/test-comprehensive-final.sh):
- **End-to-End Workflow**: Complete policy → workload → recommendation → update flow
- **Multi-Workload Scenarios**: Multiple workloads with different policies
- **Cross-Namespace Operations**: Policies affecting multiple namespaces
- **Real Metrics Integration**: Using actual metrics-server data

#### Validation Points:
- Complete workflow functionality
- Policy status updates
- Workload discovery and processing
- Recommendation accuracy

### 12. Dry Run Mode Tests
**File**: `test/e2e/dry_run_test.go`

#### Test Cases (from hack/test-dry-run-mode.sh):
- **Dry Run Policy Mode**: Recommendations without updates
- **Dry Run Validation**: What-if analysis functionality
- **Dry Run Logging**: Proper dry-run indication in logs

#### Validation Points:
- No actual workload modifications
- Recommendation generation
- Dry-run mode indication

## Test Execution Strategy

### 1. Test Organization
```
test/e2e/
├── e2e_suite_test.go          # BeforeSuite/AfterSuite setup
├── policy_modes_test.go       # Policy mode functionality
├── resource_bounds_test.go    # Resource bounds enforcement
├── workload_types_test.go     # Different workload types
├── update_strategies_test.go  # Update strategy validation
├── memory_safety_test.go      # Memory safety features
├── policy_weights_test.go     # Policy prioritization
├── error_handling_test.go     # Error scenarios
├── rbac_test.go              # Security and permissions
├── observability_test.go     # Metrics and logging
├── concurrent_test.go        # Concurrent operations
├── integration_test.go       # End-to-end scenarios
├── dry_run_test.go          # Dry run functionality
└── helpers/
    ├── policy_helpers.go     # Policy creation utilities
    ├── workload_helpers.go   # Workload management utilities
    ├── validation_helpers.go # Assertion helpers
    └── cleanup_helpers.go    # Resource cleanup utilities
```

### 2. Test Execution Order
1. **Infrastructure Tests**: RBAC, Observability setup
2. **Core Functionality**: Policy modes, Resource bounds
3. **Advanced Features**: Memory safety, Policy weights
4. **Edge Cases**: Error handling, Concurrent operations
5. **Integration**: End-to-end workflows

### 3. Test Data Management
- Use YAML files from `hack/` directory as test fixtures
- Create helper functions for common test scenarios
- Implement proper cleanup between tests
- Use unique namespaces/names to avoid conflicts

### 4. Validation Approach
- **Log Analysis**: Check OptipPod controller logs for expected messages
- **Resource Inspection**: Validate workload annotations and resource changes
- **Status Verification**: Check policy and workload status conditions
- **Metrics Validation**: Verify Prometheus metrics exposure and content

## Test Utilities and Helpers

### 1. Policy Helpers
- `CreateOptimizationPolicy()`: Create test policies
- `WaitForPolicyReady()`: Wait for policy to be processed
- `GetPolicyStatus()`: Retrieve policy status information

### 2. Workload Helpers  
- `CreateTestWorkload()`: Create test deployments/statefulsets
- `WaitForWorkloadReady()`: Wait for workload availability
- `GetWorkloadAnnotations()`: Retrieve OptipPod annotations
- `GetWorkloadResources()`: Get current resource specifications

### 3. Validation Helpers
- `CheckOptipodLogs()`: Search logs for specific patterns
- `ValidateRecommendations()`: Verify recommendation format and values
- `ValidateResourceBounds()`: Check bounds enforcement
- `ValidateMetricsEndpoint()`: Verify metrics accessibility

### 4. Cleanup Helpers
- `CleanupPolicies()`: Remove test policies
- `CleanupWorkloads()`: Remove test workloads  
- `CleanupNamespaces()`: Remove test namespaces
- `ResetClusterState()`: Reset cluster to clean state

## Environment Variables

```bash
# Cluster configuration
KIND_CLUSTER_NAME=optipod-e2e-test
OPTIPOD_NAMESPACE=optipod-system
TEST_NAMESPACE=optipod-workloads

# Test behavior
KEEP_CLUSTER=false              # Keep cluster after tests
E2E_TEST_TIMEOUT=30m           # Overall test timeout
E2E_PARALLEL_NODES=1           # Parallel test execution
SKIP_CLUSTER_SETUP=false      # Skip cluster creation

# OptipPod configuration
OPTIPOD_IMAGE=optipod:e2e-test # Controller image to test
METRICS_SERVER_WAIT=3m         # Metrics server readiness timeout
```

## Success Criteria

### 1. Functional Requirements
- ✅ All policy modes work correctly (Auto, Recommend, Disabled)
- ✅ Resource bounds are properly enforced
- ✅ All workload types are supported (Deployment, StatefulSet, DaemonSet)
- ✅ Update strategies work as expected
- ✅ Memory safety features prevent unsafe decreases
- ✅ Policy weights enable proper prioritization

### 2. Reliability Requirements  
- ✅ Error handling is robust and informative
- ✅ RBAC security is properly enforced
- ✅ Concurrent operations don't cause conflicts
- ✅ System degrades gracefully under failure conditions

### 3. Observability Requirements
- ✅ Metrics are properly exposed and formatted
- ✅ Logs provide sufficient debugging information
- ✅ Events are generated for important operations
- ✅ Status conditions accurately reflect system state

### 4. Performance Requirements
- ✅ Tests complete within reasonable time limits
- ✅ Controller startup time is acceptable
- ✅ Workload processing latency is reasonable
- ✅ Memory and CPU usage are within bounds

This comprehensive test plan covers all the scenarios identified in the `hack/` folder and provides a solid foundation for validating OptipPod's functionality in a real Kubernetes environment.