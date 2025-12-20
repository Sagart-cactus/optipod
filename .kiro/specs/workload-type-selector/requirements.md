# Requirements Document

## Introduction

This enhancement adds workload type filtering capabilities to OptimizationPolicy's existing workload selector. Currently, OptimizationPolicy discovers and processes all supported workload types (Deployment, StatefulSet, DaemonSet) without the ability to include or exclude specific types. This limits users' ability to adopt OptipPod gradually or apply different optimization strategies based on workload characteristics.

## Glossary

- **OptimizationPolicy**: A Kubernetes custom resource that defines optimization rules and workload selection criteria
- **Workload_Selector**: The existing selector mechanism in OptimizationPolicy that filters workloads by namespace and labels
- **Workload_Type**: The Kubernetes resource kind (Deployment, StatefulSet, DaemonSet) that manages pods
- **Discovery_Engine**: The internal component that finds workloads matching policy selectors
- **Policy_Matcher**: The component that determines which policy applies to a specific workload

## Requirements

### Requirement 1: Workload Type Include Filter

**User Story:** As a platform engineer, I want to create policies that only apply to specific workload types, so that I can start with low-risk workloads like Deployments before expanding to StatefulSets.

#### Acceptance Criteria

1. WHEN a policy specifies workloadTypes.include with one or more types, THE Discovery_Engine SHALL only discover workloads of those specified types
2. WHEN workloadTypes.include contains "Deployment", THE Discovery_Engine SHALL discover only Deployment resources matching other selectors
3. WHEN workloadTypes.include contains multiple types, THE Discovery_Engine SHALL discover workloads matching any of the specified types
4. WHEN workloadTypes.include is empty or not specified, THE Discovery_Engine SHALL discover all supported workload types (backward compatibility)
5. THE Policy_Matcher SHALL validate that workload types match the include filter before applying policies

### Requirement 2: Workload Type Exclude Filter

**User Story:** As a database administrator, I want to exclude StatefulSets from optimization policies, so that I can prevent OptipPod from modifying stateful workloads that require careful resource management.

#### Acceptance Criteria

1. WHEN a policy specifies workloadTypes.exclude with one or more types, THE Discovery_Engine SHALL skip discovery of those specified types
2. WHEN workloadTypes.exclude contains "StatefulSet", THE Discovery_Engine SHALL not discover any StatefulSet resources
3. WHEN workloadTypes.exclude contains multiple types, THE Discovery_Engine SHALL skip discovery of all specified types
4. WHEN workloadTypes.exclude is empty or not specified, THE Discovery_Engine SHALL discover all supported workload types (backward compatibility)
5. THE Policy_Matcher SHALL validate that workload types do not match the exclude filter before applying policies

### Requirement 3: Include and Exclude Precedence

**User Story:** As a system administrator, I want clear precedence rules when both include and exclude filters are specified, so that I can create predictable and safe policies.

#### Acceptance Criteria

1. WHEN both workloadTypes.include and workloadTypes.exclude are specified, THE Discovery_Engine SHALL apply exclude filter first (exclude takes precedence)
2. WHEN a workload type appears in both include and exclude lists, THE Discovery_Engine SHALL exclude that workload type
3. WHEN include and exclude filters result in no valid workload types, THE Discovery_Engine SHALL return an empty result set
4. THE Policy_Matcher SHALL validate precedence rules during policy evaluation

### Requirement 4: Validation and Error Handling

**User Story:** As a DevOps engineer, I want clear validation errors when I misconfigure workload type selectors, so that I can quickly identify and fix policy configuration issues.

#### Acceptance Criteria

1. WHEN workloadTypes.include contains invalid workload type names, THE OptimizationPolicy SHALL reject the configuration with a descriptive error
2. WHEN workloadTypes.exclude contains invalid workload type names, THE OptimizationPolicy SHALL reject the configuration with a descriptive error
3. THE OptimizationPolicy SHALL validate that workload type names are one of: "Deployment", "StatefulSet", "DaemonSet"
4. WHEN workloadTypes configuration results in no discoverable workload types, THE OptimizationPolicy SHALL log a warning but remain valid
5. THE OptimizationPolicy SHALL provide clear error messages indicating which workload type names are invalid

### Requirement 5: Backward Compatibility

**User Story:** As an existing OptipPod user, I want my current policies to continue working unchanged, so that I can upgrade OptipPod without breaking my existing configurations.

#### Acceptance Criteria

1. WHEN workloadTypes field is not specified in existing policies, THE Discovery_Engine SHALL discover all supported workload types as before
2. WHEN existing policies are applied after the upgrade, THE Discovery_Engine SHALL maintain identical behavior to previous versions
3. THE OptimizationPolicy SHALL not require workloadTypes field for validation (optional field)
4. THE Policy_Matcher SHALL process existing policies without workloadTypes filtering
5. THE Discovery_Engine SHALL maintain existing performance characteristics for policies without workloadTypes filtering

### Requirement 6: Policy Status and Observability

**User Story:** As a platform operator, I want to see which workload types are being discovered by each policy, so that I can verify my workload type filters are working correctly.

#### Acceptance Criteria

1. WHEN a policy uses workloadTypes filtering, THE OptimizationPolicy status SHALL report the effective workload types being processed
2. WHEN workload discovery completes, THE OptimizationPolicy status SHALL show counts by workload type (Deployments: X, StatefulSets: Y, DaemonSets: Z)
3. WHEN workloadTypes filtering excludes all workloads, THE OptimizationPolicy status SHALL indicate zero workloads discovered with reason
4. THE OptimizationPolicy status SHALL maintain existing fields for total workloads discovered and processed
5. THE Discovery_Engine SHALL log workload type filtering decisions at appropriate log levels for debugging

### Requirement 7: Multiple Policy Interaction

**User Story:** As a team lead, I want to create multiple policies with different workload type filters that can coexist, so that I can apply different optimization strategies to different workload types.

#### Acceptance Criteria

1. WHEN multiple policies have different workloadTypes filters, THE Policy_Matcher SHALL evaluate each policy's workload type constraints independently
2. WHEN a workload matches multiple policies with compatible workload type filters, THE Policy_Matcher SHALL select the highest weight policy as usual
3. WHEN a workload type is excluded by one policy but included by another, THE Policy_Matcher SHALL only consider policies that include that workload type
4. THE Policy_Matcher SHALL maintain existing weight-based selection logic within the set of applicable policies
5. THE Discovery_Engine SHALL optimize discovery to avoid redundant queries when multiple policies target the same workload types