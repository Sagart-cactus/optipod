# Implementation Plan: Contract E2E Testing

## Overview

This implementation plan creates a contract-based E2E testing model for OptiPod that validates only user-visible behavior, is deterministic, fast (<10 minutes), hermetic, and non-blocking for releases.

## Tasks

- [x] 1. Create new Makefile targets for contract E2E testing
  - Add test-e2e-contract target that runs contract tests with 10-minute timeout
  - Add e2e-cluster target that creates fresh Kind cluster
  - Ensure targets do NOT include manifests, generate, fmt, or vet dependencies
  - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [x] 2. Create contract test directory structure
  - Create test/e2e/contract/ directory
  - Set up proper Go module structure for contract tests
  - Ensure clear separation from existing comprehensive e2e tests
  - _Requirements: 9.1, 9.2, 9.4, 9.5_

- [x] 3. Implement installation contract test
  - [x] 3.1 Create install_test.go with controller installation validation
    - Apply OptiPod manifests to clean Kind cluster
    - Wait for controller pod to become Ready
    - Verify no crashloop or restart events
    - Use only allowed assertions (Pod Ready == true, Restart count == 0)
    - _Requirements: 4.1, 4.2, 4.3_

  - [x] 3.2 Write property test for installation validation
    - **Property 7: Installation validation**
    - **Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5**

- [-] 4. Implement minimal CR acceptance test
  - [x] 4.1 Create minimal_cr_test.go with CR acceptance validation
    - Create CR with only required fields (no optional fields)
    - Apply CR to cluster and verify API server acceptance
    - Verify no validation errors are returned
    - Do not assert specific field values or defaults
    - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 4.2 Write property test for minimal CR validation
    - **Property 8: Minimal CR validation**
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**

- [-] 5. Implement status convergence test
  - [x] 5.1 Create status_ready_test.go with status convergence validation
    - Poll CR status using Eventually with 2-minute timeout maximum
    - Use 5-second polling intervals minimum
    - Verify Ready=True condition appears
    - Do not assert specific status messages or timing details
    - _Requirements: 6.1, 6.2, 6.3_

  - [ ]* 5.2 Write property test for status convergence validation
    - **Property 9: Status convergence validation**
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5**

- [-] 6. Implement Ginkgo best practices and test framework compliance
  - [x] 6.1 Ensure all tests follow Ginkgo best practices
    - Use one assertion per It block
    - Use Eventually for async behavior, never Expect
    - Avoid nested Eventually calls
    - Maintain test isolation with no shared state
    - Use descriptive test names explaining contracts
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [ ]* 6.2 Write property test for Ginkgo best practices
    - **Property 10: Ginkgo best practices**
    - **Validates: Requirements 7.1, 7.2, 7.3, 7.4, 7.5**

- [x] 7. Implement contract validation scope enforcement
  - [x] 7.1 Ensure tests validate only user-visible behavior
    - Verify tests only check pod readiness, CR acceptance, Ready condition
    - Ensure no assertions on reconcile counts, logs, exact resource values
    - Validate no dependencies on internal controller fields
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [ ]* 7.2 Write property test for contract validation scope
    - **Property 2: Contract validation scope**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

- [-] 8. Implement test execution lifecycle management
  - [x] 8.1 Create test suite setup and teardown
    - Implement cluster creation and cleanup logic
    - Ensure tests complete within 10 minutes maximum
    - Implement automatic cleanup even on test failures
    - _Requirements: 1.1, 1.2, 1.3_

  - [ ]* 8.2 Write property test for test execution lifecycle
    - **Property 1: Test execution lifecycle**
    - **Validates: Requirements 1.1, 1.2, 1.3**

- [-] 9. Implement error handling and reporting
  - [x] 9.1 Create actionable error messages
    - Ensure error messages do not contain internal implementation details
    - Provide clear pass/fail status reporting
    - Implement proper error handling for test failures
    - _Requirements: 1.4, 3.5_

  - [ ]* 9.2 Write property test for error message quality
    - **Property 3: Error message quality**
    - **Validates: Requirements 1.4**

  - [ ]* 9.3 Write property test for test result reporting
    - **Property 6: Test result reporting**
    - **Validates: Requirements 3.5**

- [-] 10. Ensure test determinism and consistency
  - [x] 10.1 Implement local and CI consistency
    - Ensure tests produce identical results locally and in CI
    - Implement consistent timeout and polling behavior
    - Ensure no manual setup required for local execution
    - _Requirements: 1.5, 10.1, 10.2, 10.3, 10.4, 10.5_

  - [ ]* 10.2 Write property test for test determinism
    - **Property 4: Test determinism**
    - **Validates: Requirements 1.5**

  - [ ]* 10.3 Write property test for local execution consistency
    - **Property 13: Local execution consistency**
    - **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5**

- [x] 11. Create GitHub Actions workflow for contract E2E tests
  - [x] 11.1 Create .github/workflows/e2e.yml
    - Set up workflow with workflow_dispatch and scheduled triggers
    - Install required tools (Kind, kubectl)
    - Run contract E2E tests using make test-e2e-contract
    - Ensure workflow is separate from release pipeline
    - _Requirements: 3.2, 3.4_

  - [x] 11.2 Ensure release pipeline separation
    - Verify contract test failures do not block releases
    - Ensure release.yml does not reference contract tests
    - _Requirements: 3.1, 3.3_

  - [ ]* 11.3 Write property test for release pipeline separation
    - **Property 5: Release pipeline separation**
    - **Validates: Requirements 3.3**

- [x] 12. Implement build system behavior validation
  - [x] 12.1 Validate build target behavior
    - Ensure test-e2e-contract runs only contract tests
    - Verify e2e-cluster creates fresh clusters
    - Ensure no code generation dependencies in contract target
    - Preserve existing comprehensive test targets
    - _Requirements: 8.1, 8.3, 8.4, 8.5_

  - [ ]* 12.2 Write property test for build system behavior
    - **Property 11: Build system behavior**
    - **Validates: Requirements 8.1, 8.3, 8.4, 8.5**

- [-] 13. Create contract test documentation
  - [ ] 13.1 Create test/e2e/contract/README.md
    - Explain what contract E2E testing is
    - Document what it explicitly does NOT test
    - Provide instructions for local execution
    - Explain why failures are meaningful
    - _Requirements: 9.3_

  - [ ]* 13.2 Write property test for directory organization
    - **Property 12: Directory organization**
    - **Validates: Requirements 9.4, 9.5**

- [x] 14. Final validation and integration testing
  - [x] 14.1 Validate all acceptance criteria
    - Verify make test-e2e-contract works locally with no setup
    - Ensure CI and local results match
    - Confirm no codegen, fmt, or vet runs
    - Verify cluster cleanup always occurs
    - Validate runtime is under 10 minutes
    - _Requirements: 1.1, 1.5, 8.4, 1.3_

  - [x] 14.2 Test complete contract validation workflow
    - Run full contract test suite locally
    - Verify all contract properties are validated
    - Ensure tests are deterministic and stable
    - Validate error handling and cleanup
    - _Requirements: 1.4, 1.5, 2.1, 2.2, 2.3_

- [x] 15. Final checkpoint - Ensure all contract tests pass
  - Ensure all contract tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional property-based tests that validate correctness properties
- Each task references specific requirements for traceability
- Contract tests must be boring, stable, and fast (under 10 minutes total)
- No assertions on internal behavior, only user-visible contracts
- Tests must work identically locally and in CI with no manual setup