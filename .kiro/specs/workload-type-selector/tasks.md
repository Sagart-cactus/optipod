# Implementation Plan: Workload Type Selector

## Overview

This implementation adds workload type filtering capabilities to OptimizationPolicy's existing workload selector. The implementation follows a phased approach: API types first, then discovery logic, policy matching, validation, status reporting, and finally comprehensive testing.

## Tasks

- [x] 1. Extend API types with workload type filtering
  - Add WorkloadTypeFilter and WorkloadType to api/v1alpha1/optimizationpolicy_types.go
  - Add WorkloadTypes field to WorkloadSelector struct
  - Add kubebuilder validation annotations for WorkloadType enum
  - _Requirements: 1.1, 2.1, 4.3_

- [x] 1.1 Write property test for API type validation
  - **Property 8: Workload Type Validation**
  - **Validates: Requirements 4.1, 4.2, 4.3**

- [x] 2. Implement workload type filtering logic
  - [x] 2.1 Create WorkloadTypeSet utility type and methods
    - Implement NewWorkloadTypeSet, Contains, Remove methods
    - Add helper functions for set operations
    - _Requirements: 3.1, 3.2_

  - [x] 2.2 Write unit tests for WorkloadTypeSet utility
    - Test set operations (add, remove, contains)
    - Test edge cases (empty sets, duplicate additions)
    - _Requirements: 3.1, 3.2_

  - [x] 2.3 Implement getActiveWorkloadTypes function
    - Handle include/exclude precedence rules
    - Support backward compatibility (nil filter = all types)
    - _Requirements: 1.4, 3.1, 5.1_

  - [x] 2.4 Write property test for getActiveWorkloadTypes
    - **Property 6: Exclude Precedence Over Include**
    - **Validates: Requirements 3.1, 3.2**

- [x] 3. Enhance discovery engine with workload type filtering
  - [x] 3.1 Modify DiscoverWorkloads function in internal/discovery/discovery.go
    - Add workload type filtering before discovery calls
    - Skip discovery calls for excluded workload types
    - Maintain backward compatibility for policies without workloadTypes
    - _Requirements: 1.1, 2.1, 5.1_

  - [x] 3.2 Write property test for discovery filtering
    - **Property 1: Include Filter Behavior**
    - **Validates: Requirements 1.1, 1.2, 1.3**

  - [x] 3.3 Write property test for exclude filtering
    - **Property 4: Exclude Filter Behavior**
    - **Validates: Requirements 2.1, 2.2, 2.3**

  - [x] 3.4 Write property test for backward compatibility
    - **Property 2: Backward Compatibility for Missing Filters**
    - **Validates: Requirements 1.4, 5.1**

- [x] 4. Checkpoint - Ensure discovery tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Enhance policy matcher with workload type validation
  - [x] 5.1 Add workloadTypeMatches function to internal/policy/selector.go
    - Implement workload type matching logic
    - Handle nil filter cases for backward compatibility
    - _Requirements: 1.5, 2.5_

  - [x] 5.2 Modify policyMatchesWorkload function
    - Add workload type validation as first check
    - Maintain existing namespace and label selector logic
    - _Requirements: 1.5, 2.5_

  - [x] 5.3 Write property test for policy matcher include validation
    - **Property 3: Policy Matcher Include Validation**
    - **Validates: Requirements 1.5**

  - [x] 5.4 Write property test for policy matcher exclude validation
    - **Property 5: Policy Matcher Exclude Validation**
    - **Validates: Requirements 2.5**

- [x] 6. Add comprehensive validation to OptimizationPolicy
  - [x] 6.1 Extend validateOptimizationPolicy function
    - Add workload type validation logic
    - Validate include and exclude lists contain only valid types
    - Ensure field remains optional for backward compatibility
    - _Requirements: 4.1, 4.2, 4.3, 5.3_

  - [x] 6.2 Write property test for validation logic
    - **Property 10: Optional Field Validation**
    - **Validates: Requirements 5.3**

  - [x] 6.3 Write property test for empty result set handling
    - **Property 9: Empty Filter Configuration Validity**
    - **Validates: Requirements 4.4**

- [x] 7. Enhance status reporting with workload type breakdown
  - [x] 7.1 Add WorkloadTypeStatus to OptimizationPolicyStatus
    - Extend status struct with workload type counts
    - Add helper methods for updating workload type counts
    - _Requirements: 6.1, 6.2, 6.4_

  - [x] 7.2 Update status reporting logic in controllers
    - Modify reconciliation logic to populate workload type counts
    - Ensure existing status fields remain unchanged
    - _Requirements: 6.2, 6.4_

  - [x] 7.3 Write property test for status reporting
    - **Property 11: Status Workload Type Reporting**
    - **Validates: Requirements 6.1, 6.2**

  - [x] 7.4 Write property test for status backward compatibility
    - **Property 12: Status Backward Compatibility**
    - **Validates: Requirements 6.4**

- [x] 8. Implement multiple policy interaction logic
  - [x] 8.1 Enhance SelectBestPolicy function
    - Ensure workload type filtering works with multiple policies
    - Maintain weight-based selection within applicable policies
    - _Requirements: 7.1, 7.2, 7.3_

  - [x] 8.2 Write property test for multiple policy independence
    - **Property 13: Multiple Policy Independence**
    - **Validates: Requirements 7.1**

  - [x] 8.3 Write property test for weight-based selection with filtering
    - **Property 14: Weight-Based Selection with Type Filtering**
    - **Validates: Requirements 7.2, 7.3**

- [x] 9. Add comprehensive integration tests
  - [x] 9.1 Create end-to-end test scenarios
    - Test complete workflow from policy creation to workload discovery
    - Test multiple policies with different workload type filters
    - Test backward compatibility with existing policies
    - _Requirements: All requirements_

  - [x] 9.2 Write property test for empty result set handling
    - **Property 7: Empty Result Set Handling**
    - **Validates: Requirements 3.3**

- [x] 10. Update CRD generation and documentation
  - [x] 10.1 Generate updated CRDs with new fields
    - Run make manifests to update CRD definitions
    - Verify kubebuilder annotations are correctly applied
    - _Requirements: 4.3_

  - [x] 10.2 Update API documentation
    - Add examples of workload type filtering to config/samples
    - Update CRD reference documentation
    - _Requirements: All requirements_

- [x] 11. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties
- Unit tests validate specific examples and edge cases
- The implementation maintains full backward compatibility
- All existing functionality remains unchanged for policies without workloadTypes field