# Requirements Document: Server-Side Apply Support for OptipPod

## Introduction

This feature adds Server-Side Apply (SSA) support to OptipPod to enable field-level ownership of resource requests and limits. This solves the conflict problem with GitOps tools like ArgoCD by allowing OptipPod to own specific fields (resource requests/limits) while ArgoCD owns other fields (image, replicas, etc.).

## Glossary

- **Server-Side Apply (SSA)**: A Kubernetes feature that tracks field-level ownership using managedFields metadata
- **fieldManager**: An identifier that declares which controller/tool owns specific fields in a resource
- **managedFields**: Metadata in Kubernetes resources that tracks which manager owns which fields
- **Strategic Merge Patch**: The current patch method used by OptipPod (being replaced)
- **Apply Patch**: The SSA patch type that declares field ownership
- **ArgoCD**: A GitOps continuous delivery tool for Kubernetes
- **OptimizationPolicy**: The OptipPod CRD that defines optimization behavior
- **Application Engine**: The OptipPod component that applies resource changes to workloads

## Requirements

### Requirement 1: Server-Side Apply Implementation

**User Story:** As an OptipPod operator, I want OptipPod to use Server-Side Apply when updating workload resources, so that field ownership is properly tracked in Kubernetes.

#### Acceptance Criteria

1. WHEN OptipPod applies resource recommendations THEN the system SHALL use Server-Side Apply with fieldManager set to "optipod"
2. WHEN OptipPod patches a workload THEN the system SHALL only include resource request and limit fields in the apply patch
3. WHEN OptipPod applies changes THEN the system SHALL set Force to true to take ownership of resource fields if needed
4. WHEN a workload is updated via SSA THEN the system SHALL preserve all other fields managed by different field managers
5. WHEN SSA patch fails THEN the system SHALL return a descriptive error indicating the conflict and field manager involved

### Requirement 2: Configuration

**User Story:** As an OptipPod administrator, I want to control whether SSA is used, so that I can choose the appropriate patch strategy for my environment.

#### Acceptance Criteria

1. WHEN an OptimizationPolicy is created THEN the system SHALL support a useServerSideApply boolean field in updateStrategy
2. WHEN useServerSideApply is true THEN the system SHALL use SSA for applying changes
3. WHEN useServerSideApply is false THEN the system SHALL use Strategic Merge Patch method
4. WHEN useServerSideApply is not specified THEN the system SHALL default to true (SSA is the recommended approach)
5. WHEN the policy is validated THEN the system SHALL accept both SSA and non-SSA configurations as valid

### Requirement 3: Patch Construction for SSA

**User Story:** As a developer, I want the SSA patch to be correctly formatted, so that Kubernetes accepts it and properly tracks field ownership.

#### Acceptance Criteria

1. WHEN building an SSA patch THEN the system SHALL include apiVersion, kind, metadata, and spec fields
2. WHEN building an SSA patch THEN the system SHALL include only the container resources being updated
3. WHEN building an SSA patch THEN the system SHALL identify containers by name in the patch
4. WHEN updateRequestsOnly is true THEN the system SHALL include only requests in the patch
5. WHEN updateRequestsOnly is false THEN the system SHALL include both requests and limits in the patch
6. WHEN the patch is constructed THEN the system SHALL serialize it as valid JSON

### Requirement 4: Field Manager Identification

**User Story:** As a Kubernetes administrator, I want to identify which fields OptipPod manages, so that I can audit and troubleshoot field ownership conflicts.

#### Acceptance Criteria

1. WHEN OptipPod applies changes THEN the system SHALL use "optipod" as the fieldManager identifier
2. WHEN viewing a workload's managedFields THEN the system SHALL show "optipod" as the manager for resource request and limit fields
3. WHEN multiple OptipPod policies target the same workload THEN the system SHALL use the same "optipod" fieldManager to avoid conflicts
4. WHEN OptipPod takes ownership of a field THEN the system SHALL record the operation as "Apply" in managedFields
5. WHEN querying managedFields THEN the system SHALL show which specific resource fields OptipPod owns

### Requirement 5: ArgoCD Compatibility

**User Story:** As a platform engineer using ArgoCD, I want OptipPod to coexist with ArgoCD without sync conflicts, so that both tools can manage different aspects of my workloads.

#### Acceptance Criteria

1. WHEN ArgoCD manages a workload AND OptipPod uses SSA THEN the system SHALL allow both to manage different fields without conflicts
2. WHEN ArgoCD syncs a workload THEN the system SHALL not revert OptipPod's resource changes if SSA is enabled
3. WHEN OptipPod updates resources THEN the system SHALL not cause ArgoCD to mark the application as OutOfSync for resource fields
4. WHEN both ArgoCD and OptipPod use SSA THEN the system SHALL respect field ownership boundaries
5. WHEN ArgoCD attempts to manage resource fields owned by OptipPod THEN the system SHALL preserve OptipPod's values if Force is true

### Requirement 6: Error Handling and Conflict Resolution

**User Story:** As an OptipPod operator, I want clear error messages when field ownership conflicts occur, so that I can resolve them quickly.

#### Acceptance Criteria

1. WHEN a field ownership conflict occurs THEN the system SHALL return an error identifying the conflicting field manager
2. WHEN Force is true and a conflict exists THEN the system SHALL take ownership and log a warning about the previous owner
3. WHEN Force is false and a conflict exists THEN the system SHALL fail with an error and not apply changes
4. WHEN SSA patch fails due to invalid format THEN the system SHALL return an error with the validation failure details
5. WHEN a conflict is resolved THEN the system SHALL emit a Kubernetes event documenting the ownership change

### Requirement 7: Observability and Logging

**User Story:** As an OptipPod operator, I want to observe when SSA is used and track field ownership changes, so that I can monitor the system's behavior.

#### Acceptance Criteria

1. WHEN SSA is used to apply changes THEN the system SHALL log the fieldManager name and Force setting
2. WHEN OptipPod takes ownership of fields THEN the system SHALL emit a Kubernetes event indicating the ownership change
3. WHEN SSA patch succeeds THEN the system SHALL log the fields that were updated
4. WHEN viewing OptimizationPolicy status THEN the system SHALL indicate whether SSA was used for the last update
5. WHEN Prometheus metrics are collected THEN the system SHALL track SSA vs non-SSA patch operations separately

### Requirement 8: Documentation and Examples

**User Story:** As an OptipPod user, I want clear documentation on SSA support, so that I can configure it correctly for my use case.

#### Acceptance Criteria

1. WHEN reading documentation THEN the system SHALL provide examples of OptimizationPolicy with SSA enabled
2. WHEN reading documentation THEN the system SHALL explain the benefits of SSA over Strategic Merge Patch
3. WHEN reading documentation THEN the system SHALL provide ArgoCD configuration examples for SSA compatibility
4. WHEN reading documentation THEN the system SHALL explain when to use SSA vs Strategic Merge Patch
5. WHEN reading documentation THEN the system SHALL explain how to troubleshoot field ownership conflicts

### Requirement 9: Testing and Validation

**User Story:** As a developer, I want comprehensive tests for SSA functionality, so that I can ensure it works correctly across different scenarios.

#### Acceptance Criteria

1. WHEN running unit tests THEN the system SHALL verify SSA patch construction is correct
2. WHEN running integration tests THEN the system SHALL verify SSA patches are accepted by Kubernetes
3. WHEN running E2E tests THEN the system SHALL verify OptipPod and ArgoCD can coexist without conflicts
4. WHEN running tests THEN the system SHALL verify field ownership is correctly tracked in managedFields
5. WHEN running tests THEN the system SHALL verify both SSA and Strategic Merge Patch modes work correctly
