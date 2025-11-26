# Implementation Plan

- [x] 1. Initialize project structure and dependencies
  - Create Go module with kubebuilder/controller-runtime dependencies
  - Set up project layout following Kubernetes operator conventions
  - Initialize kubebuilder project with API group optipod.io
  - Configure Go modules with required dependencies (controller-runtime, client-go, gopter for property testing)
  - _Requirements: All_

- [x] 2. Define CRD and core data models
  - Create OptimizationPolicy CRD with spec and status fields
  - Define Go types for PolicyMode, ResourceBounds, MetricsConfig, UpdateStrategy
  - Implement CRD validation markers (kubebuilder annotations)
  - Generate CRD manifests using controller-gen
  - _Requirements: 6.1, 6.2, 16.1, 16.2_

- [x] 3. Implement metrics provider interface and implementations
- [x] 3.1 Create metrics provider interface
  - Define MetricsProvider interface with GetContainerMetrics and HealthCheck methods
  - Define ContainerMetrics and ResourceMetrics types
  - _Requirements: 5.1, 5.2, 15.1_

- [x] 3.2 Implement metrics-server provider
  - Create MetricsServerProvider implementing MetricsProvider interface
  - Query metrics.k8s.io API for pod metrics
  - Compute percentiles (P50, P90, P99) from time-series data
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

- [x] 3.3 Write property test for percentile computation
  - **Property 13: Percentile computation**
  - **Validates: Requirements 5.4, 5.5**

- [x] 3.4 Implement Prometheus provider
  - Create PrometheusProvider implementing MetricsProvider interface
  - Query Prometheus using PromQL for container CPU and memory usage
  - Compute percentiles from query results over rolling window
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

- [x] 3.5 Implement provider factory and configuration
  - Create factory function to instantiate providers based on config
  - Handle provider initialization errors with fallback
  - _Requirements: 15.2, 15.3_

- [x] 3.6 Write property test for metrics provider configurability
  - **Property 30: Metrics provider configurability**
  - **Validates: Requirements 15.2, 15.3**

- [x] 4. Implement recommendation engine
- [x] 4.1 Create recommendation engine core logic
  - Implement ComputeRecommendation function
  - Select percentile based on policy configuration (P50, P90, or P99)
  - Apply safety factor to selected percentile
  - Clamp recommendations to min/max bounds
  - Generate explanation strings for recommendations
  - _Requirements: 5.6, 5.7, 5.8, 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 4.2 Write property test for bounds enforcement
  - **Property 4: Bounds enforcement**
  - **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

- [x] 4.3 Write property test for safety factor application
  - **Property 14: Strategy application**
  - **Validates: Requirements 5.6, 5.7, 5.8**

- [x] 5. Implement application engine
- [x] 5.1 Create application decision logic
  - Implement CanApply function to determine if changes can be applied
  - Detect Kubernetes version and in-place resize capability
  - Check policy mode (Auto, Recommend, Disabled)
  - Check global dry-run configuration
  - Evaluate memory decrease safety
  - Determine apply method (InPlace, Recreate, Skip)
  - _Requirements: 3.1, 3.2, 3.3, 8.1, 8.2, 8.3, 8.4, 8.5_

- [x] 5.2 Write property test for feature gate detection
  - **Property 20: Feature gate detection**
  - **Validates: Requirements 8.1**

- [x] 5.3 Write property test for in-place resize preference
  - **Property 21: In-place resize preference**
  - **Validates: Requirements 8.2, 8.3**

- [x] 5.4 Implement workload patching logic
  - Create Apply function to patch workload resources
  - Build JSON patch for resource requests
  - Preserve resource limits unless configured otherwise
  - Handle RBAC errors gracefully
  - _Requirements: 1.4, 13.1, 13.2_

- [x] 5.5 Write property test for updates preserve limits
  - **Property 3: Updates preserve limits**
  - **Validates: Requirements 1.4**

- [x] 5.6 Write property test for RBAC respect
  - **Property 28: RBAC respect**
  - **Validates: Requirements 13.1, 13.2, 13.3, 13.4**

- [x] 6. Implement policy controller
- [x] 6.1 Create policy validation logic
  - Implement ValidatePolicy function
  - Validate required fields (mode, selectors, bounds)
  - Validate bound ranges (min < max)
  - Validate selector syntax
  - Return descriptive error messages
  - _Requirements: 6.1, 6.2_

- [x] 6.2 Write property test for policy validation
  - **Property 15: Policy validation**
  - **Validates: Requirements 6.1, 6.2**

- [x] 6.3 Implement policy controller reconciliation
  - Set up controller-runtime reconciler for OptimizationPolicy
  - Watch OptimizationPolicy CRD events
  - Call validation on create/update
  - Update policy status conditions
  - Emit Kubernetes events for validation errors
  - _Requirements: 6.1, 6.2, 11.4_

- [x] 7. Implement workload discovery
- [x] 7.1 Create workload discovery logic
  - Implement DiscoverWorkloads function
  - Query Deployments, StatefulSets, DaemonSets matching label selectors
  - Filter by namespace selectors
  - Apply allow/deny namespace lists with deny precedence
  - _Requirements: 6.3, 6.4, 6.5, 12.1, 12.2, 12.3, 12.4, 12.5_

- [x] 7.2 Write property test for workload discovery
  - **Property 16: Workload discovery**
  - **Validates: Requirements 6.3, 6.4, 6.5**

- [x] 7.3 Write property test for multi-tenant scoping
  - **Property 27: Multi-tenant scoping**
  - **Validates: Requirements 12.1, 12.2, 12.3, 12.4, 12.5**

- [-] 8. Implement workload controller
- [x] 8.1 Create workload processing pipeline
  - Implement ProcessWorkload function
  - Coordinate metrics collection via MetricsProvider
  - Call RecommendationEngine to compute recommendations
  - Call ApplicationEngine to determine and apply changes
  - Handle missing metrics errors
  - _Requirements: 1.1, 1.2, 1.3, 3.4, 3.5_

- [x] 8.2 Write property test for monitoring initiates metrics collection
  - **Property 1: Monitoring initiates metrics collection**
  - **Validates: Requirements 1.2**

- [x] 8.3 Write property test for missing metrics prevent changes
  - **Property 7: Missing metrics prevent changes**
  - **Validates: Requirements 3.4, 3.5**

- [x] 8.2 Implement mode-specific behavior
  - Implement Auto mode: apply recommendations
  - Implement Recommend mode: store recommendations only, no patches
  - Implement Disabled mode: skip processing, preserve status
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 4.1, 4.2, 4.3_

- [x] 8.5 Write property test for recommend mode prevents modifications
  - **Property 8: Recommend mode prevents modifications**
  - **Validates: Requirements 4.1, 4.2, 7.4**

- [x] 8.6 Write property test for auto mode applies changes
  - **Property 17: Auto mode applies changes**
  - **Validates: Requirements 7.1, 7.2**

- [x] 8.7 Write property test for disabled mode stops processing
  - **Property 19: Disabled mode stops processing**
  - **Validates: Requirements 7.5, 7.6, 7.7**

- [x] 8.8 Implement workload controller reconciliation
  - Set up controller-runtime reconciler
  - Trigger on OptimizationPolicy changes
  - Discover and process matching workloads
  - Handle reconciliation intervals and requeueing
  - _Requirements: 1.1, 1.3_

- [x] 9. Implement status management
- [x] 9.1 Create status update logic
  - Update policy status with per-workload results
  - Record last recommendation timestamp
  - Record last applied timestamp
  - Record skip reasons
  - Store recommendations in structured format
  - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 4.4_

- [x] 9.2 Write property test for status timestamp tracking
  - **Property 23: Status timestamp tracking**
  - **Validates: Requirements 9.1, 9.2**

- [x] 9.3 Write property test for recommendation format completeness
  - **Property 10: Recommendation format completeness**
  - **Validates: Requirements 4.4, 9.4, 9.5, 9.6**

- [x] 10. Implement observability
- [x] 10.1 Create Prometheus metrics
  - Define and register Prometheus metrics (gauges, histograms, counters)
  - Instrument reconciliation loops with duration metrics
  - Track workloads monitored, updated, skipped
  - Track errors by type
  - Expose metrics endpoint
  - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

- [x] 10.2 Write property test for Prometheus metrics exposure
  - **Property 25: Prometheus metrics exposure**
  - **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5**

- [x] 10.3 Implement Kubernetes event creation
  - Create events on workloads for update success/failure
  - Create events on policies for validation errors
  - Create events for metrics collection errors
  - Create events for RBAC errors
  - Include actionable messages and suggestions
  - _Requirements: 11.1, 11.2, 11.3, 11.4, 13.4_

- [x] 10.4 Write property test for failure event creation
  - **Property 26: Failure event creation**
  - **Validates: Requirements 11.1, 11.2, 11.3, 11.4**

- [x] 11. Implement global configuration
- [x] 11.1 Create operator configuration
  - Define OperatorConfig struct
  - Load configuration from flags and ConfigMap
  - Implement global dry-run mode
  - Configure default metrics provider
  - Configure leader election
  - _Requirements: 14.1, 14.2, 14.3, 14.4_

- [x] 11.2 Write property test for global dry-run mode
  - **Property 29: Global dry-run mode**
  - **Validates: Requirements 14.1, 14.2, 14.3, 14.4**

- [x] 12. Implement caching and performance optimizations
- [x] 12.1 Add workload caching
  - Cache discovered workloads per policy
  - Implement cache invalidation on workload changes
  - _Requirements: 17.5_

- [x] 12.2 Add metrics caching
  - Cache metrics with short TTL (1 minute)
  - Avoid overwhelming metrics backends
  - _Requirements: 17.5_

- [x] 12.3 Write property test for API call efficiency
  - **Property 32: API call efficiency**
  - **Validates: Requirements 17.5**

- [x] 13. Create deployment manifests
- [x] 13.1 Generate RBAC manifests
  - Create ServiceAccount, Role, RoleBinding, ClusterRole, ClusterRoleBinding
  - Define required permissions for workloads, CRDs, events, metrics
  - _Requirements: 13.1, 13.2_

- [x] 13.2 Create operator deployment manifest
  - Define Deployment with single replica and leader election
  - Configure resource requests and limits
  - Set command-line flags
  - _Requirements: 17.3, 17.4_

- [x] 13.3 Create ConfigMap for operator configuration
  - Define default configuration values
  - Include metrics provider settings
  - _Requirements: 15.2_

- [x] 14. Checkpoint - Ensure all tests pass
  - Run all unit tests and property-based tests
  - Verify all properties pass with 100+ iterations
  - Fix any failing tests
  - Ask the user if questions arise

- [x] 15. Create integration tests
  - Set up envtest framework
  - Test full reconciliation loops with mock API server
  - Test CRD status updates
  - Test workload discovery across namespaces
  - Test RBAC enforcement
  - Test metrics provider integration with mocks
  - _Requirements: All_

- [x] 16. Create end-to-end tests
  - Set up kind cluster for E2E testing
  - Deploy OptiPod operator
  - Create sample OptimizationPolicy resources
  - Deploy sample workloads
  - Verify resource requests are updated
  - Test in-place resize on supported clusters
  - Verify Prometheus metrics
  - _Requirements: All_

- [x] 17. Create documentation
  - Write README with installation instructions
  - Document OptimizationPolicy CRD fields
  - Provide example policies for common use cases
  - Document metrics provider configuration
  - Create troubleshooting guide
  - _Requirements: All_

- [x] 18. Final checkpoint - Ensure all tests pass
  - Run all unit tests, property tests, integration tests
  - Verify operator builds successfully
  - Verify CRD manifests are valid
  - Ask the user if questions arise
