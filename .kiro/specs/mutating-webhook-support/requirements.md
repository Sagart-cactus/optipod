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
- **CertManager**: Kubernetes certificate management operator for automated TLS certificate provisioning
- **CABundle**: Certificate Authority bundle used to validate TLS connections to the webhook
- **MutatingAdmissionWebhook**: Kubernetes resource that configures admission webhook behavior
- **SelfSignedCertificate**: TLS certificate signed by its own private key, used for testing and development
- **WebhookConfiguration**: Kubernetes configuration that defines how admission webhooks are called

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

### Requirement 8: Certificate Management and CA Bundle Injection

**User Story:** As a cluster administrator, I want automatic certificate management and CA bundle injection for the mutating webhook, so that the webhook can establish trusted TLS connections with the Kubernetes API server.

#### Acceptance Criteria

1. WHEN cert-manager is available, THE System SHALL use cert-manager to generate and manage webhook certificates
2. WHEN cert-manager is not available, THE System SHALL support manual certificate provisioning
3. WHEN certificates are generated or updated, THE System SHALL automatically inject the CA bundle into the MutatingWebhookConfiguration
4. THE System SHALL define a proper Issuer resource for cert-manager certificate generation
5. THE System SHALL define a Certificate resource with appropriate DNS names and certificate properties
6. WHEN using manual certificates, THE install script SHALL extract the CA bundle and update the webhook configuration
7. THE System SHALL validate certificate expiration and provide warnings for certificates expiring within 30 days

### Requirement 9: Webhook Infrastructure Configuration

**User Story:** As a platform administrator, I want proper Kubernetes webhook infrastructure configuration, so that the mutating webhook integrates correctly with the Kubernetes admission control system.

#### Acceptance Criteria

1. THE System SHALL define a MutatingAdmissionWebhook configuration with proper service references
2. THE System SHALL configure appropriate namespace and object selectors to target only relevant pods
3. THE System SHALL set reasonable timeout values and failure policies for webhook operations
4. THE System SHALL include the webhook infrastructure in the webhook-enabled kustomization overlay
5. THE System SHALL ensure the webhook service points to the correct controller deployment
6. WHEN using cert-manager, THE System SHALL configure automatic CA bundle injection annotations
7. THE System SHALL validate that all webhook configuration references resolve correctly

### Requirement 10: Installation Script Enhancements

**User Story:** As a system operator, I want an enhanced installation script that properly handles certificate management for both cert-manager and manual modes, so that webhook installation is reliable and automated.

#### Acceptance Criteria

1. WHEN cert-manager is detected, THE install script SHALL use cert-manager for certificate management
2. WHEN cert-manager is not available, THE install script SHALL generate self-signed certificates
3. WHEN generating manual certificates, THE install script SHALL create certificates with proper DNS names and extensions
4. THE install script SHALL extract the CA bundle from generated certificates and inject it into the MutatingWebhookConfiguration
5. THE install script SHALL verify that the webhook configuration is properly updated with the CA bundle
6. THE install script SHALL validate that all required resources are created and ready
7. WHEN installation fails, THE install script SHALL provide clear error messages and cleanup instructions

### Requirement 11: Webhook Startup Validation and Fail-Fast Behavior

**User Story:** As a system operator, I want the webhook server to validate its configuration and fail fast if requirements are not met, so that I can quickly identify and resolve configuration issues.

#### Acceptance Criteria

1. WHEN the webhook server starts, THE System SHALL validate that required certificates exist and are valid
2. WHEN certificate validation fails, THE System SHALL fail to start and log detailed error information
3. WHEN the webhook service is unreachable, THE System SHALL detect this condition and report it
4. THE System SHALL validate that the MutatingWebhookConfiguration exists and is properly configured
5. THE System SHALL perform a self-test by making a test admission request during startup
6. WHEN any startup validation fails, THE System SHALL exit with a non-zero status code
7. THE System SHALL provide clear diagnostic information for each validation failure