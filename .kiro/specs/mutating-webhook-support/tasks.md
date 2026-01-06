# Implementation Plan: Mutating Webhook Support

## Overview

This implementation plan converts the mutating webhook support design into discrete coding tasks. The approach focuses on incremental development, starting with core API changes, then webhook infrastructure, and finally integration with the existing OptipPod system. Each task builds on previous work and includes comprehensive testing.

## Tasks

- [x] 1. Extend OptimizationPolicy API with webhook strategy fields
  - Add `strategy` and `rolloutStrategy` fields to `UpdateStrategy` struct in `api/v1alpha1/optimizationpolicy_types.go`
  - Add validation logic for new fields with proper enum constraints
  - Update CRD generation and validation webhooks
  - Add helper methods for strategy detection and defaults
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

- [x] 1.1 Write property test for strategy configuration
  - **Property 1: Strategy configuration and routing**
  - **Validates: Requirements 1.1, 1.2, 1.3, 1.4**

- [x] 1.2 Write property test for strategy validation
  - **Property 2: Strategy validation**
  - **Validates: Requirements 1.5**

- [x] 2. Implement webhook server infrastructure
  - [x] 2.1 Create webhook server component in `internal/webhook/server.go`
    - Implement HTTP server with TLS support
    - Add admission request/response handling
    - Implement certificate loading and validation
    - Add graceful shutdown and health checks
    - _Requirements: 6.1, 6.2, 6.5_

  - [x] 2.2 Implement pod mutation logic in `internal/webhook/mutator.go`
    - Create pod mutation handler for admission requests
    - Add annotation parsing and resource modification logic
    - Implement policy selector matching for pods
    - Add error handling and logging for mutation failures
    - _Requirements: 2.1, 2.2, 2.4, 2.5_

  - [x] 2.3 Write property tests for webhook behavior
    - **Property 3: Webhook pod modification behavior**
    - **Property 4: Webhook non-interference**
    - **Property 5: Webhook strategy control**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

- [x] 3. Implement annotation management system
  - [x] 3.1 Create annotation manager in `internal/webhook/annotations.go`
    - Define standardized annotation keys and formats
    - Implement annotation storage for resource recommendations
    - Add annotation parsing and validation logic
    - Support both container-specific and pod-level annotations
    - _Requirements: 3.1, 3.2, 3.3, 3.5_

  - [x] 3.2 Add annotation error handling
    - Implement graceful handling of invalid annotation values
    - Add comprehensive error logging for annotation issues
    - Ensure pod creation proceeds even with annotation errors
    - _Requirements: 3.4, 7.4_

  - [x] 3.3 Write property tests for annotation handling
    - **Property 6: Annotation storage and format**
    - **Property 7: Annotation error handling**
    - **Property 8: Annotation scope handling**
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 7.4**

- [x] 4. Implement rolling restart controller
  - [x] 4.1 Create rollout controller in `internal/webhook/rollout.go`
    - Implement rolling restart logic for supported workload types
    - Add workload type validation (Deployments, StatefulSets, DaemonSets)
    - Implement pod template update mechanism to trigger restarts
    - Add rollout strategy configuration handling
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x] 4.2 Write property tests for rollout behavior
    - **Property 9: Rollout strategy behavior**
    - **Property 10: Rolling restart workload validation**
    - **Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5**

- [x] 5. Checkpoint - Core webhook components complete
  - Ensure all webhook infrastructure tests pass, ask the user if questions arise.

- [x] 6. Integrate webhook strategy with application engine
  - [x] 6.1 Extend application engine in `internal/application/engine.go`
    - Add `ApplyWithWebhook` method to support webhook strategy
    - Implement strategy routing logic in existing `Apply` method
    - Add webhook-specific result tracking and metrics
    - Preserve all existing SSA functionality unchanged
    - _Requirements: 5.1, 5.3, 5.4_

  - [x] 6.2 Update workload processor integration
    - Modify workload processor to use strategy-based routing
    - Add annotation storage calls for webhook strategy
    - Implement rollout strategy handling in workload processing
    - Ensure backward compatibility for policies without strategy field
    - _Requirements: 5.2, 5.5_

  - [x] 6.3 Write property tests for SSA compatibility
    - **Property 11: SSA compatibility preservation**
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**

- [-] 7. Implement webhook lifecycle management
  - [x] 7.1 Add webhook registration to controller startup
    - Create mutating webhook configuration during controller initialization
    - Configure appropriate failure policies and namespace selectors
    - Add webhook cleanup during controller shutdown
    - Implement certificate management and rotation support
    - _Requirements: 6.1, 6.2, 6.3, 6.4_

  - [ ] 7.2 Write property tests for webhook lifecycle
    - **Property 12: Webhook lifecycle management**
    - **Property 13: Webhook health monitoring**
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.5**

- [x] 8. Add comprehensive observability
  - [x] 8.1 Implement webhook metrics and logging
    - Add metrics for webhook admission requests, successes, and failures
    - Implement detailed logging for webhook operations and errors
    - Add debugging endpoints for webhook configuration inspection
    - Integrate with existing OptipPod observability infrastructure
    - _Requirements: 7.1, 7.2, 7.3, 7.5_

  - [x] 8.2 Write property tests for observability
    - **Property 14: Webhook observability**
    - **Property 15: Webhook debugging support**
    - **Validates: Requirements 7.1, 7.2, 7.3, 7.5**

- [x] 9. Create webhook deployment manifests
  - [x] 9.1 Add webhook service and deployment configurations
    - Create Kubernetes service for webhook endpoint
    - Add webhook deployment with proper resource limits
    - Configure TLS certificates and secret management
    - Add RBAC permissions for webhook operations
    - _Requirements: 6.1, 6.2, 6.4_

  - [x] 9.2 Update installation documentation
    - Document webhook strategy configuration options
    - Add migration guide from SSA to webhook strategy
    - Include troubleshooting guide for common webhook issues
    - Update examples with webhook strategy configurations

- [x] 10. Integration testing and validation
  - [x] 10.1 Create end-to-end integration tests
    - Test complete workflow from policy creation to pod mutation
    - Validate mixed SSA/webhook environments
    - Test rolling restart behavior with different strategies
    - Verify backward compatibility with existing policies
    - _Requirements: All requirements_

  - [x] 10.2 Write comprehensive integration tests
    - Test webhook server startup and shutdown
    - Test certificate rotation and renewal
    - Test failure policy behavior under various conditions
    - Test performance under high pod creation load

- [x] 11. Final checkpoint - Complete system validation
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties
- Unit tests validate specific examples and edge cases
- The implementation preserves all existing SSA functionality
- Webhook strategy becomes the new default for better ArgoCD compatibility