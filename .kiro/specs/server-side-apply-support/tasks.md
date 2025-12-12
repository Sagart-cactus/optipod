# Implementation Plan: Server-Side Apply Support

- [x] 1. Update CRD with SSA configuration field
  - Add `UseServerSideApply` field to `UpdateStrategy` in `api/v1alpha1/optimizationpolicy_types.go`
  - Set default value to `true` using kubebuilder marker
  - Add field to `WorkloadStatus` for tracking apply method
  - Regenerate CRD manifests with `make manifests`
  - _Requirements: 2.1, 2.4_

- [x] 1.1 Write property test for CRD field acceptance
  - **Property 1: Configuration determines patch method**
  - **Validates: Requirements 2.2, 2.3**

- [x] 1.2 Write property test for default SSA behavior
  - **Property 2: Default to SSA when unspecified**
  - **Validates: Requirements 2.4**

- [x] 2. Implement SSA patch builder
  - Create `buildSSAPatch()` method in `internal/application/engine.go`
  - Build minimal patch with only apiVersion, kind, metadata, and resource fields
  - Implement container identification by name
  - Handle `updateRequestsOnly` flag for conditional limits inclusion
  - Add `getAPIVersionAndKind()` helper method
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 2.1 Write property test for SSA patch structure
  - **Property 3: SSA patch contains only resource fields**
  - **Validates: Requirements 1.2, 3.1, 3.2**

- [x] 2.2 Write property test for container identification
  - **Property 4: Container identification in patch**
  - **Validates: Requirements 3.3**

- [x] 2.3 Write property test for conditional limits
  - **Property 5: Conditional limits inclusion**
  - **Validates: Requirements 3.4, 3.5**

- [x] 2.4 Write property test for JSON serialization
  - **Property 6: Valid JSON serialization**
  - **Validates: Requirements 3.6**

- [x] 3. Implement SSA apply method
  - Create `ApplyWithSSA()` method in `internal/application/engine.go`
  - Use `types.ApplyPatchType` for patch type
  - Set `FieldManager: "optipod"` in PatchOptions
  - Set `Force: true` in PatchOptions
  - Call `dynamicClient.Patch()` with SSA configuration
  - _Requirements: 1.1, 1.3, 4.1_

- [x] 3.1 Write property test for field manager
  - **Property 7: SSA uses correct field manager**
  - **Validates: Requirements 1.1, 4.1**

- [x] 3.2 Write property test for Force flag
  - **Property 8: Force flag is set for SSA**
  - **Validates: Requirements 1.3**

- [x] 4. Implement SSA error handling
  - Create `handleSSAError()` method in `internal/application/engine.go`
  - Handle conflict errors with descriptive messages
  - Handle RBAC errors
  - Handle invalid patch errors
  - Return errors with field manager information
  - _Requirements: 1.5, 6.1, 6.3, 6.4_

- [x] 5. Update Apply method to route between SSA and Strategic Merge
  - Modify `Apply()` method to check `useServerSideApply` configuration
  - Route to `ApplyWithSSA()` when SSA is enabled
  - Route to `ApplyWithStrategicMerge()` (renamed existing method) when disabled
  - Handle nil value for `useServerSideApply` (default to true)
  - _Requirements: 2.2, 2.3, 2.4_

- [x] 6. Add SSA logging
  - Add structured logging to `ApplyWithSSA()` method
  - Log fieldManager name and Force setting
  - Log fields being updated on success
  - Log errors with context
  - _Requirements: 7.1, 7.3_

- [x] 6.1 Write property test for logging
  - **Property 9: Logging includes field manager**
  - **Validates: Requirements 7.1**

- [x] 7. Add SSA events
  - Create `RecordSSAOwnershipTaken()` in `internal/observability/events.go`
  - Add `EventReasonSSAOwnershipTaken` constant
  - Add `EventReasonSSAConflict` constant
  - Emit events when taking field ownership
  - Emit events on conflicts
  - _Requirements: 6.5, 7.2_

- [x] 8. Add SSA Prometheus metrics
  - Add `ssaPatchTotal` counter in `internal/observability/metrics.go`
  - Include labels for policy, namespace, workload, kind, status, patch_type
  - Register metric in `RegisterMetrics()`
  - Create `RecordSSAPatch()` helper function
  - Call metric recording in Application Engine
  - _Requirements: 7.5_

- [x] 8.1 Write property test for metrics
  - **Property 10: Metrics track patch type**
  - **Validates: Requirements 7.5**

- [x] 9. Update workload status with SSA information
  - Add `LastApplyMethod` field to `WorkloadStatus` in CRD
  - Add `FieldOwnership` field to `WorkloadStatus` in CRD
  - Update status after applying changes in workload processor
  - Set `lastApplyMethod` to "ServerSideApply" or "StrategicMergePatch"
  - Set `fieldOwnership` to true when SSA is used
  - _Requirements: 7.4_

- [x] 9.1 Write property test for status tracking
  - **Property 11: Status tracks apply method**
  - **Validates: Requirements 7.4**

- [x] 10. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. Write integration tests for SSA
  - Create `internal/application/ssa_integration_test.go`
  - Test SSA patch is accepted by Kubernetes
  - Test managedFields shows "optipod" as owner
  - Test only resource fields are modified
  - Test field ownership with multiple managers
  - _Requirements: 4.2, 4.5, 9.2_

- [x] 11.1 Write property test for field ownership
  - **Property 12: Field ownership tracking**
  - **Validates: Requirements 4.2, 4.5**

- [x] 11.2 Write property test for consistent field manager
  - **Property 13: Consistent field manager across policies**
  - **Validates: Requirements 4.3**

- [x] 11.3 Write property test for Apply operation type
  - **Property 14: Apply operation type**
  - **Validates: Requirements 4.4**

- [x] 12. Write E2E tests for SSA
  - Create `test/e2e/ssa_test.go`
  - Test end-to-end SSA flow with real workloads
  - Test that resources are updated correctly
  - Test that managedFields shows OptipPod ownership
  - Test that other fields remain unchanged
  - _Requirements: 9.2, 9.4_

- [x] 13. Write ArgoCD compatibility E2E test
  - Add test to `test/e2e/ssa_test.go`
  - Deploy app via ArgoCD (simulated or real)
  - Apply OptipPod policy with SSA
  - Verify ArgoCD doesn't show OutOfSync
  - Verify ArgoCD sync doesn't revert OptipPod changes
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 9.3_

- [x] 14. Update documentation
  - Update README.md with SSA benefits
  - Create ArgoCD integration guide in `docs/ARGOCD_INTEGRATION.md`
  - Add SSA configuration examples to `docs/EXAMPLES.md`
  - Document `useServerSideApply` field in `docs/CRD_REFERENCE.md`
  - Add troubleshooting section for field ownership conflicts
  - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5_

- [x] 15. Update sample policies
  - Update `config/samples/optipod_v1alpha1_optimizationpolicy.yaml` to include SSA configuration
  - Add example with SSA enabled
  - Add example with SSA disabled
  - Add comments explaining when to use each
  - _Requirements: 8.1_

- [x] 16. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.
