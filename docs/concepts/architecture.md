# Architecture

OptiPod is a Kubernetes operator that provides explainable resource recommendations for CPU and memory requests/limits. This document describes the system architecture and core components.

## System Overview

OptiPod follows a standard Kubernetes operator pattern with optional webhook support for GitOps-safe resource application.

```
┌─────────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                      │
│                                                              │
│  ┌────────────────┐         ┌──────────────────┐           │
│  │  Controller    │◄────────┤ OptimizationPolicy│           │
│  │  (Required)    │         │      CRDs         │           │
│  └────────┬───────┘         └──────────────────┘           │
│           │                                                  │
│           │ Discovers & Processes                           │
│           ▼                                                  │
│  ┌────────────────┐                                         │
│  │   Workloads    │                                         │
│  │ (Deployments,  │                                         │
│  │ StatefulSets,  │                                         │
│  │  DaemonSets)   │                                         │
│  └────────┬───────┘                                         │
│           │                                                  │
│           │ Reads Metrics                                   │
│           ▼                                                  │
│  ┌────────────────┐         ┌──────────────────┐           │
│  │ Metrics Source │         │  Webhook Server  │           │
│  │ (metrics-server│         │   (Optional)     │           │
│  │  or Prometheus)│         └──────────────────┘           │
│  └────────────────┘                                         │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Controller (Required)

The controller is the heart of OptiPod. It runs as a Deployment in your cluster and performs the following functions:

**Responsibilities:**
- Watches `OptimizationPolicy` CRDs
- Discovers workloads matching policy selectors
- Fetches metrics from configured providers
- Computes resource recommendations
- Applies changes (in Auto mode) or stores recommendations (in Recommend mode)
- Updates policy status with aggregate statistics

**Key Features:**
- Leader election support for high availability
- Adaptive reconciliation intervals based on workload activity
- Policy weight-based prioritization when multiple policies match
- Comprehensive observability via Prometheus metrics and Kubernetes events

**Configuration:**
- Runs with minimal privileges (non-root, read-only filesystem)
- Configurable resource limits (default: 500m CPU, 512Mi memory)
- Health and readiness probes on port 8081
- Metrics endpoint on port 8080

### 2. Webhook Server (Optional)

The webhook server enables GitOps-safe resource application through a mutating admission webhook.

**Responsibilities:**
- Intercepts pod creation requests
- Reads recommendations from workload annotations
- Injects resource requests/limits into pod specs
- Supports immediate or on-next-restart rollout strategies

**When to Use:**
- ArgoCD or Flux GitOps workflows
- Environments where SSA conflicts with GitOps controllers
- When you want recommendations applied at pod creation time

**Requirements:**
- cert-manager for TLS certificate management
- Network connectivity from API server to webhook service
- Proper RBAC permissions for mutating webhooks

**Configuration:**
- Runs as a separate Deployment (2 replicas by default)
- Pod Disruption Budget for high availability
- Network policies for security
- Configurable failure policy (Ignore or Fail)

### 3. Custom Resource Definitions (CRDs)

OptiPod defines one primary CRD:

**OptimizationPolicy** (`optimizationpolicies.optipod.optipod.io`)
- Defines optimization behavior for selected workloads
- Configures metrics collection and processing
- Sets resource bounds and safety constraints
- Specifies update strategies and rollout behavior

## Component Interactions

### Discovery Flow

1. Controller watches for `OptimizationPolicy` resources
2. For each policy, controller discovers matching workloads using:
   - Namespace selectors (label-based)
   - Workload selectors (label-based)
   - Namespace allow/deny lists
   - Workload type filters (Deployment, StatefulSet, DaemonSet)
3. Multiple policies can match the same workload; highest weight wins

### Metrics Collection Flow

1. Controller identifies containers in discovered workloads
2. Queries metrics provider for CPU and memory usage:
   - **metrics-server**: Background sampler collects data at regular intervals
   - **Prometheus**: Queries historical data using PromQL
3. Aggregates metrics over configured rolling window
4. Calculates percentiles (P50, P90, P99) for recommendation engine

### Recommendation Flow

1. Recommendation engine receives container metrics and policy configuration
2. Selects appropriate percentile based on policy (default: P90)
3. Applies safety factor (default: 1.2x)
4. Clamps values to policy-defined resource bounds
5. Generates explainable recommendation with reasoning

### Application Flow

The application flow differs based on the configured strategy:

**Webhook Strategy (Default):**
1. Controller stores recommendations as workload annotations
2. If rollout strategy is "immediate", triggers rolling restart
3. Webhook intercepts pod creation during restart
4. Webhook reads annotations and injects resource values
5. Pod starts with optimized resources

**SSA Strategy:**
1. Controller builds Server-Side Apply patch
2. Applies patch with "optipod" field manager
3. Kubernetes handles pod restart based on workload type
4. Pods restart with new resource values

## Data Flow

```
Policy CRD → Controller → Workload Discovery
                ↓
         Metrics Provider
                ↓
      Recommendation Engine
                ↓
         Application Engine
                ↓
    ┌───────────┴───────────┐
    │                       │
Webhook Strategy      SSA Strategy
    │                       │
Annotations          Direct Patch
    │                       │
Webhook Server       Kubernetes API
    │                       │
    └───────────┬───────────┘
                ↓
         Updated Workloads
```

## Deployment Topologies

### Minimal Deployment (Controller Only)

Suitable for:
- Non-GitOps environments
- Direct Kubernetes API access
- SSA strategy

Components:
- Controller Deployment (1 replica)
- Service Account with RBAC
- Metrics Service (optional)

### Full Deployment (Controller + Webhook)

Suitable for:
- GitOps workflows (ArgoCD, Flux)
- Production environments
- Webhook strategy

Components:
- Controller Deployment (1 replica)
- Webhook Deployment (2+ replicas)
- cert-manager (for certificates)
- Webhook Service
- MutatingWebhookConfiguration
- Network Policies
- Pod Disruption Budget

## Scalability Considerations

### Controller Scaling

- Single replica with leader election (default)
- Handles hundreds of policies and thousands of workloads
- Adaptive reconciliation reduces unnecessary processing
- Policy weight system prevents duplicate work

### Webhook Scaling

- Multiple replicas for high availability (default: 2)
- Horizontal Pod Autoscaler can be configured
- Stateless design allows easy scaling
- Pod Disruption Budget ensures availability during updates

### Metrics Provider Scaling

**metrics-server:**
- Background sampler runs in controller process
- Caches samples in memory (configurable limits)
- Automatic target eviction for inactive workloads

**Prometheus:**
- External service, scales independently
- Query load depends on policy count and reconciliation frequency
- Consider Prometheus federation for large clusters

## Security Model

### Controller Security

- Runs as non-root user (UID 65532)
- Read-only root filesystem
- Drops all Linux capabilities
- Minimal RBAC permissions:
  - Read: Namespaces, Pods, Workloads
  - Write: Workloads (for SSA), Events
  - Read: Metrics (metrics.k8s.io API)

### Webhook Security

- TLS-only communication (cert-manager managed)
- Certificate rotation handled automatically
- Network policies restrict ingress
- Same security context as controller
- Additional RBAC for MutatingWebhookConfiguration

### Secrets Management

- Prometheus credentials stored in Kubernetes Secrets
- TLS certificates managed by cert-manager
- No secrets in environment variables or logs
- Supports external secret management (e.g., Vault)

## High Availability

### Controller HA

- Leader election prevents split-brain
- Graceful shutdown on termination
- Health checks ensure quick failover
- Stateless design (state in Kubernetes API)

### Webhook HA

- Multiple replicas (default: 2)
- Pod anti-affinity spreads across nodes
- Pod Disruption Budget (minAvailable: 1)
- Failure policy configurable (Ignore/Fail)

## Observability

### Metrics

Exposed on port 8080 (controller and webhook):
- Reconciliation duration and errors
- Workload discovery and processing counts
- Optimization success/failure rates
- Resource change magnitudes
- Webhook admission request metrics

### Events

Kubernetes events for:
- Policy validation errors
- Workload discovery issues
- Recommendation generation
- Application success/failure
- Webhook errors

### Logs

Structured JSON logging with:
- Request tracing
- Error details with context
- Performance metrics
- Debug information (configurable verbosity)

## Performance Characteristics

### Controller Performance

- Reconciliation: ~100-500ms per policy (depends on workload count)
- Metrics queries: ~50-200ms (depends on provider)
- Recommendation computation: <10ms per container
- SSA application: ~50-100ms per workload

### Webhook Performance

- Admission latency: <50ms (typical)
- Timeout: 10s (configurable)
- Concurrent requests: Limited by replica count
- No external dependencies during admission

## Failure Modes

### Controller Failures

- **Metrics provider unavailable**: Skips recommendations, retries on next reconciliation
- **API server errors**: Exponential backoff with retries
- **Invalid policy**: Marks policy as not ready, emits event
- **RBAC errors**: Logs error, continues with other workloads

### Webhook Failures

- **Certificate issues**: Health checks fail, pods not admitted
- **Timeout**: Falls back to failure policy (Ignore/Fail)
- **Annotation parsing errors**: Allows pod creation, logs error
- **Network issues**: Kubernetes retries admission request

## Related Documentation

- [Modes](modes.md) - Operational modes (Recommend, Auto, Disabled)
- [Safety Model](safety-model.md) - Safety guarantees and constraints
- [Update Strategies](update-strategies.md) - SSA vs Webhook strategies
- [Installation Guide](../getting-started/installation.md) - Deployment instructions
