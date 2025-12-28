# Requirements Document

## Introduction

This specification defines a contract-based E2E testing model for OptiPod that focuses on minimal, deterministic, and fast validation of user-visible behavior. The goal is to create stable, reproducible tests that validate only essential user contracts without testing internal controller behavior, timing-sensitive assertions, or exact resource values.

## Glossary

- **Contract_E2E_Tests**: Minimal end-to-end tests that validate only user-visible behavior contracts
- **User_Contract**: The essential behavior that users depend on (installation, CR acceptance, status convergence)
- **OptiPod_Controller**: The Kubernetes controller that manages optimization policies
- **Custom_Resource**: OptiPod's OptimizationPolicy custom resource definition
- **Ready_Condition**: Kubernetes condition indicating successful resource processing
- **Kind_Cluster**: Lightweight Kubernetes cluster for testing
- **Release_Critical_Path**: CI/CD pipeline steps that must pass for releases
- **Hermetic_Test**: Test that runs in complete isolation without external dependencies

## Requirements

### Requirement 1

**User Story:** As a release engineer, I want fast and stable contract E2E tests, so that releases are not blocked by flaky or slow comprehensive tests.

#### Acceptance Criteria

1. WHEN contract E2E tests execute, THE Test_Suite SHALL complete within 10 minutes maximum
2. WHEN tests run in CI, THE Test_Suite SHALL create its own Kind cluster for complete isolation
3. WHEN tests complete, THE Test_Suite SHALL clean up the cluster automatically
4. WHEN contract tests fail, THE Test_Suite SHALL provide actionable error messages without internal details
5. WHEN running locally or in CI, THE Test_Suite SHALL produce identical results

### Requirement 2

**User Story:** As a developer, I want contract E2E tests that validate only user-visible behavior, so that internal refactoring doesn't break tests unnecessarily.

#### Acceptance Criteria

1. WHEN validating controller installation, THE Test_Suite SHALL only verify the controller pod becomes Ready
2. WHEN testing CR application, THE Test_Suite SHALL only verify the CR is accepted by the API server
3. WHEN checking status convergence, THE Test_Suite SHALL only verify a Ready=True condition appears
4. THE Test_Suite SHALL NOT assert reconcile counts, timing thresholds, or exact CPU/memory values
5. THE Test_Suite SHALL NOT depend on controller logs or internal controller fields

### Requirement 3

**User Story:** As a CI/CD engineer, I want contract E2E tests separated from the release critical path, so that comprehensive tests can run without blocking releases.

#### Acceptance Criteria

1. WHEN releases are triggered, THE Release_Pipeline SHALL NOT include contract E2E tests as blocking steps
2. WHEN contract E2E tests are needed, THE Test_Suite SHALL run on workflow dispatch or scheduled basis
3. WHEN contract tests fail, THE Failure SHALL NOT prevent release deployment
4. WHEN running contract tests, THE Test_Suite SHALL use separate GitHub Actions workflow
5. WHEN test results are needed, THE Test_Suite SHALL provide clear pass/fail status

### Requirement 4

**User Story:** As a developer, I want contract E2E tests that install OptiPod successfully, so that I can verify the basic deployment works.

#### Acceptance Criteria

1. WHEN installing OptiPod, THE Test_Suite SHALL apply manifests to a clean Kind cluster
2. WHEN controller deployment occurs, THE Test_Suite SHALL wait for controller pod to become Ready
3. WHEN installation completes, THE Test_Suite SHALL verify no crashloop or restart events
4. THE Test_Suite SHALL NOT assert specific resource requests, limits, or configuration details
5. THE Test_Suite SHALL NOT validate internal controller initialization steps

### Requirement 5

**User Story:** As a developer, I want contract E2E tests that validate minimal CR application, so that I can verify the API accepts valid configurations.

#### Acceptance Criteria

1. WHEN applying a minimal OptiPod CR, THE Test_Suite SHALL use only required fields
2. WHEN CR is submitted, THE Test_Suite SHALL verify acceptance by the API server
3. WHEN validation occurs, THE Test_Suite SHALL verify no validation errors are returned
4. THE Test_Suite SHALL NOT include optional fields in the minimal CR
5. THE Test_Suite SHALL NOT assert specific field values or defaults

### Requirement 6

**User Story:** As a developer, I want contract E2E tests that validate status convergence, so that I can verify OptiPod processes resources correctly.

#### Acceptance Criteria

1. WHEN polling CR status, THE Test_Suite SHALL use Eventually with 2-minute timeout maximum
2. WHEN checking status, THE Test_Suite SHALL poll at 5-second intervals minimum
3. WHEN status converges, THE Test_Suite SHALL verify Ready=True condition appears
4. THE Test_Suite SHALL NOT assert specific status messages or detailed conditions
5. THE Test_Suite SHALL NOT validate timing of status updates

### Requirement 7

**User Story:** As a developer, I want contract E2E tests with proper Ginkgo usage, so that tests are maintainable and follow best practices.

#### Acceptance Criteria

1. WHEN writing test assertions, THE Test_Suite SHALL use one assertion per It block
2. WHEN testing async behavior, THE Test_Suite SHALL use Eventually, never Expect
3. WHEN using Eventually, THE Test_Suite SHALL NOT nest multiple Eventually calls
4. THE Test_Suite SHALL NOT share state between different test cases
5. THE Test_Suite SHALL use descriptive test names that explain the contract being validated

### Requirement 8

**User Story:** As a developer, I want new Makefile targets for contract testing, so that I can run contract tests independently.

#### Acceptance Criteria

1. WHEN running make test-e2e-contract, THE Build_System SHALL execute only contract E2E tests
2. WHEN creating test cluster, THE Build_System SHALL provide make e2e-cluster target
3. WHEN e2e-cluster runs, THE Build_System SHALL delete existing cluster and create fresh one
4. THE Build_System SHALL NOT include manifests, generate, fmt, or vet dependencies in contract target
5. THE Build_System SHALL maintain existing test-e2e target for comprehensive tests

### Requirement 9

**User Story:** As a developer, I want contract E2E tests organized in a dedicated directory, so that they are clearly separated from comprehensive tests.

#### Acceptance Criteria

1. WHEN organizing contract tests, THE Test_Suite SHALL use test/e2e/contract/ directory structure
2. WHEN creating test files, THE Test_Suite SHALL include install_test.go, minimal_cr_test.go, and status_ready_test.go
3. WHEN documenting tests, THE Test_Suite SHALL provide README.md explaining contract testing approach
4. THE Test_Suite SHALL NOT mix contract tests with existing comprehensive e2e tests
5. THE Test_Suite SHALL maintain clear separation between test types

### Requirement 10

**User Story:** As a developer, I want local contract E2E execution that matches CI behavior, so that I can debug issues locally.

#### Acceptance Criteria

1. WHEN running make test-e2e-contract locally, THE Test_Suite SHALL create its own Kind cluster
2. WHEN tests execute locally, THE Test_Suite SHALL produce same results as CI
3. WHEN tests complete locally, THE Test_Suite SHALL clean up automatically
4. THE Test_Suite SHALL NOT require manual cluster setup or configuration
5. THE Test_Suite SHALL provide same timeout and polling behavior as CI