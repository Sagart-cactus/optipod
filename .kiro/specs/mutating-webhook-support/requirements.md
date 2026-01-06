# Requirements Document

## Introduction

This feature adds mutating webhook support as an alternative to Server Side Apply (SSA) for applying resource recommendations in environments where SSA is not available by default (such as ArgoCD). The mutating webhook will intercept pod creation and modify resource requests/limits based on annotations, providing a seamless way to apply OptipPod recommendations without requiring SSA capabilities.

## Glossary

- **OptimizationPolicy**: The custom resource that defines optimization behavior and configuration
- **MutatingWebhook**: A Kubernetes admission controller that can modify resources during creation
- **SSA**: Server Side Apply - Kubernetes feature for declarative resource management
- **ResourceRecommendation**: Calculated CPU/memory requests and limits for workloads
- **RollingRestart**: Process of restarting workload pods to apply new configurations
- **AnnotationBasedRecommendation**: Resource recommendations stored as pod annotations

## Requirements

### Requirement 1: Webhook Strategy Configuration

**User Story:** As a platform administrator, I want to configure whether OptipPod uses SSA or mutating webhooks for applying recommendations, so that I can choose the approach that works best with my cluster setup.

#### Acceptance Criteria

1. WHEN an OptimizationPolicy is created, THE System SHALL default to mutating webhook strategy
2. WHEN the strategy field is set to "ssa", THE System SHALL use Server Side Apply for resource updates
3. WHEN the strategy field is set to "webhook", THE System SHALL use mutating webhook for resource modifications
4. WHEN the strategy field is omitted, THE System SHALL default to "webhook" strategy
5. THE System SHALL validate that only "ssa" or "webhook" values are accepted for the strategy field

### Requirement 2: Mutating Webhook Implementation

**User Story:** As a developer, I want the mutating webhook to automatically apply resource recommendations when pods are created, so that my workloads get optimized resources without manual intervention.

#### Acceptance Criteria

1. WHEN a pod is created with optimization annotations, THE MutatingWebhook SHALL modify the pod's resource requests and limits
2. WHEN a pod lacks optimization annotations, THE MutatingWebhook SHALL leave the pod unchanged
3. WHEN the webhook strategy is disabled, THE MutatingWebhook SHALL not modify any pods
4. THE MutatingWebhook SHALL only modify pods that match the OptimizationPolicy selector
5. THE MutatingWebhook SHALL preserve existing resource specifications when no recommendations are available

### Requirement 3: Annotation-Based Recommendations

**User Story:** As a system operator, I want resource recommendations stored as pod annotations, so that the mutating webhook can apply them during pod creation.

#### Acceptance Criteria

1. WHEN recommendations are calculated, THE System SHALL store them as pod template annotations
2. THE System SHALL use standardized annotation keys for CPU and memory requests and limits
3. WHEN annotations are present, THE MutatingWebhook SHALL parse and apply the resource values
4. WHEN annotation values are invalid, THE MutatingWebhook SHALL log an error and leave resources unchanged
5. THE System SHALL handle both container-specific and pod-level resource annotations

### Requirement 4: Rolling Restart Configuration

**User Story:** As a workload owner, I want to control when resource changes take effect, so that I can manage the impact on my running applications.

#### Acceptance Criteria

1. WHEN rolloutStrategy is set to "immediate", THE System SHALL trigger a rolling restart after applying annotations
2. WHEN rolloutStrategy is set to "onNextRestart", THE System SHALL only apply annotations without triggering restarts
3. WHEN rolloutStrategy is omitted, THE System SHALL default to "onNextRestart"
4. THE System SHALL only perform rolling restarts for workloads that support them (Deployments, StatefulSets, DaemonSets)
5. WHEN immediate rollout is requested, THE System SHALL update the workload's pod template to trigger the restart

### Requirement 5: SSA Compatibility Preservation

**User Story:** As an existing OptipPod user, I want my current SSA-based configurations to continue working unchanged, so that I don't need to modify my existing policies.

#### Acceptance Criteria

1. WHEN strategy is set to "ssa", THE System SHALL use the existing SSA implementation
2. WHEN existing OptimizationPolicies lack a strategy field, THE System SHALL continue using SSA for backward compatibility during migration
3. THE System SHALL maintain all existing SSA functionality and behavior
4. WHEN SSA strategy is used, THE System SHALL ignore webhook-specific configuration options
5. THE System SHALL support mixed environments where some policies use SSA and others use webhooks

### Requirement 6: Webhook Registration and Management

**User Story:** As a cluster administrator, I want the mutating webhook to be properly registered and managed, so that it integrates seamlessly with the Kubernetes admission control system.

#### Acceptance Criteria

1. WHEN the controller starts, THE System SHALL register the mutating webhook configuration
2. THE System SHALL configure appropriate failure policies and namespace selectors for the webhook
3. WHEN the controller shuts down, THE System SHALL clean up webhook registrations
4. THE System SHALL handle webhook certificate management and rotation
5. THE System SHALL provide health checks and readiness probes for the webhook endpoint

### Requirement 7: Error Handling and Observability

**User Story:** As a system operator, I want comprehensive logging and error handling for webhook operations, so that I can troubleshoot issues and monitor system behavior.

#### Acceptance Criteria

1. WHEN webhook operations succeed, THE System SHALL log successful resource modifications
2. WHEN webhook operations fail, THE System SHALL log detailed error information
3. THE System SHALL emit metrics for webhook admission requests, successes, and failures
4. WHEN annotation parsing fails, THE System SHALL log the error and allow pod creation to proceed
5. THE System SHALL provide debugging information for webhook configuration and operation