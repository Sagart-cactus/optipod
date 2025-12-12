# Design Document: Server-Side Apply Support for OptipPod

## Overview

This design document describes the implementation of Server-Side Apply (SSA) support in OptipPod. SSA is a Kubernetes feature that enables field-level ownership tracking through the `managedFields` metadata. By implementing SSA, OptipPod can own specific fields (resource requests and limits) while allowing other tools like ArgoCD to own different fields (image, replicas, environment variables, etc.), eliminating sync conflicts in GitOps workflows.

### Key Benefits

- **Field-Level Ownership**: OptipPod owns only resource requests/limits, not entire workload specs
- **ArgoCD Compatibility**: No sync conflicts when both tools manage the same workload
- **Audit Trail**: `managedFields` provides clear visibility into who owns which fields
- **Conflict Resolution**: Built-in conflict detection and resolution with Force flag
- **Kubernetes Native**: Uses standard Kubernetes SSA mechanism (GA since 1.22)

## Architecture

### Current Architecture (Strategic Merge Patch)

```
┌─────────────────────────────────────────────────────┐
│  Application Engine                                 │
│                                                     │
│  1. Build Strategic Merge Patch                    │
│     - Includes full container spec                 │
│     - No field ownership tracking                  │
│                                                     │
│  2. Apply via dynamicClient.Patch()                │
│     - PatchType: StrategicMergePatchType           │
│     - No fieldManager specified                    │
│                                                     │
│  3. Kubernetes merges entire patch                 │
│     - Overwrites all specified fields              │
│     - No ownership metadata                        │
└─────────────────────────────────────────────────────┘
```

### New Architecture (Server-Side Apply)

```
┌─────────────────────────────────────────────────────┐
│  Application Engine                                 │
│                                                     │
│  1. Build SSA Patch                                │
│     - Includes ONLY resource requests/limits       │
│     - Minimal patch structure                      │
│     - Full resource identification (GVK + name)    │
│                                                     │
│  2. Apply via dynamicClient.Patch()                │
│     - PatchType: ApplyPatchType                    │
│     - FieldManager: "optipod"                      │
│     - Force: true                                  │
│                                                     │
│  3. Kubernetes tracks field ownership              │
│     - Updates managedFields metadata               │
│     - Only modifies OptipPod-owned fields          │
│     - Preserves other managers' fields             │
└─────────────────────────────────────────────────────┘
```

### Field Ownership Model

```
Deployment: my-app
├── spec.replicas                    [Owned by: argocd]
├── spec.template.metadata.labels    [Owned by: argocd]
├── spec.template.spec.containers[0]
│   ├── name                         [Owned by: argocd]
│   ├── image                        [Owned by: argocd]
│   ├── env                          [Owned by: argocd]
│   └── resources
│       ├── requests
│       │   ├── cpu                  [Owned by: optipod] ✅
│       │   └── memory               [Owned by: optipod] ✅
│       └── limits
│           ├── cpu                  [Owned by: optipod] ✅
│           └── memory               [Owned by: optipod] ✅
└── spec.strategy                    [Owned by: argocd]
```

## Components

### 1. API Changes (CRD Update)

**File**: `api/v1alpha1/optimizationpolicy_types.go`

Add `UseServerSideApply` field to `UpdateStrategy`:

```go
type UpdateStrategy struct {
    // AllowInPlaceResize enables in-place pod resource resize (Kubernetes 1.29+)
    AllowInPlaceResize bool `json:"allowInPlaceResize,omitempty"`
    
    // AllowRecreate allows pod recreation for resource changes
    AllowRecreate bool `json:"allowRecreate,omitempty"`
    
    // UpdateRequestsOnly updates only requests, leaving limits unchanged
    UpdateRequestsOnly bool `json:"updateRequestsOnly,omitempty"`
    
    // UseServerSideApply enables Server-Side Apply for field-level ownership
    // +kubebuilder:default=true
    // +optional
    UseServerSideApply *bool `json:"useServerSideApply,omitempty"`
}
```

**Default Behavior**: When `UseServerSideApply` is nil, it defaults to `true`.

### 2. Application Engine Updates

**File**: `internal/application/engine.go`

#### 2.1 New SSA Apply Method

```go
// ApplyWithSSA applies resource recommendations using Server-Side Apply
func (e *Engine) ApplyWithSSA(
    ctx context.Context,
    workload *Workload,
    containerName string,
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) error {
    // Build SSA patch
    patch, err := e.buildSSAPatch(workload, containerName, rec, policy)
    if err != nil {
        return fmt.Errorf("failed to build SSA patch: %w", err)
    }
    
    // Get GVR for workload type
    gvr, err := e.getGVR(workload.Kind)
    if err != nil {
        return fmt.Errorf("failed to get GVR: %w", err)
    }
    
    // Apply using Server-Side Apply
    _, err = e.dynamicClient.Resource(gvr).Namespace(workload.Namespace).Patch(
        ctx,
        workload.Name,
        types.ApplyPatchType,
        patch,
        metav1.PatchOptions{
            FieldManager: "optipod",
            Force:        boolPtr(true),
        },
    )
    
    if err != nil {
        return e.handleSSAError(err)
    }
    
    return nil
}
```

#### 2.2 SSA Patch Builder

```go
// buildSSAPatch constructs a Server-Side Apply patch containing only resource fields
func (e *Engine) buildSSAPatch(
    workload *Workload,
    containerName string,
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) ([]byte, error) {
    // Determine API version and kind
    apiVersion, kind := e.getAPIVersionAndKind(workload.Kind)
    
    // Build resources map
    resources := map[string]interface{}{
        "requests": map[string]interface{}{
            "cpu":    rec.CPU.String(),
            "memory": rec.Memory.String(),
        },
    }
    
    // Include limits if configured
    if !policy.Spec.UpdateStrategy.UpdateRequestsOnly {
        resources["limits"] = map[string]interface{}{
            "cpu":    rec.CPU.String(),
            "memory": rec.Memory.String(),
        }
    }
    
    // Build minimal patch with only resource fields
    patch := map[string]interface{}{
        "apiVersion": apiVersion,
        "kind":       kind,
        "metadata": map[string]interface{}{
            "name":      workload.Name,
            "namespace": workload.Namespace,
        },
        "spec": map[string]interface{}{
            "template": map[string]interface{}{
                "spec": map[string]interface{}{
                    "containers": []map[string]interface{}{
                        {
                            "name":      containerName,
                            "resources": resources,
                        },
                    },
                },
            },
        },
    }
    
    // Serialize to JSON
    patchBytes, err := json.Marshal(patch)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal SSA patch: %w", err)
    }
    
    return patchBytes, nil
}

// getAPIVersionAndKind returns the API version and kind for a workload type
func (e *Engine) getAPIVersionAndKind(workloadKind string) (string, string) {
    switch workloadKind {
    case kindDeployment, kindStatefulSet, kindDaemonSet:
        return "apps/v1", workloadKind
    default:
        return "apps/v1", workloadKind
    }
}
```

#### 2.3 Error Handling

```go
// handleSSAError processes SSA-specific errors and provides helpful messages
func (e *Engine) handleSSAError(err error) error {
    if errors.IsConflict(err) {
        return fmt.Errorf("SSA conflict: another field manager owns these fields. "+
            "This may indicate a configuration issue. Error: %w", err)
    }
    
    if errors.IsForbidden(err) {
        return fmt.Errorf("RBAC: insufficient permissions for Server-Side Apply: %w", err)
    }
    
    if errors.IsInvalid(err) {
        return fmt.Errorf("SSA patch validation failed: %w", err)
    }
    
    return fmt.Errorf("SSA patch failed: %w", err)
}
```

#### 2.4 Updated Apply Method (Router)

```go
// Apply applies resource recommendations using the configured patch strategy
func (e *Engine) Apply(
    ctx context.Context,
    workload *Workload,
    containerName string,
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) error {
    // Determine if SSA should be used
    useSSA := true // default
    if policy.Spec.UpdateStrategy.UseServerSideApply != nil {
        useSSA = *policy.Spec.UpdateStrategy.UseServerSideApply
    }
    
    if useSSA {
        return e.ApplyWithSSA(ctx, workload, containerName, rec, policy)
    }
    
    // Fall back to Strategic Merge Patch
    return e.ApplyWithStrategicMerge(ctx, workload, containerName, rec, policy)
}

// ApplyWithStrategicMerge is the existing implementation (renamed)
func (e *Engine) ApplyWithStrategicMerge(
    ctx context.Context,
    workload *Workload,
    containerName string,
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) error {
    // Existing implementation (current Apply method)
    // ... (keep existing code)
}
```

### 3. Observability Updates

#### 3.1 Logging

**File**: `internal/application/engine.go`

Add structured logging for SSA operations:

```go
func (e *Engine) ApplyWithSSA(...) error {
    log := ctrl.LoggerFrom(ctx)
    
    log.Info("Applying resource changes using Server-Side Apply",
        "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
        "kind", workload.Kind,
        "container", containerName,
        "fieldManager", "optipod",
        "force", true,
        "cpu", rec.CPU.String(),
        "memory", rec.Memory.String(),
    )
    
    // ... apply logic ...
    
    if err != nil {
        log.Error(err, "Server-Side Apply failed",
            "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
        )
        return e.handleSSAError(err)
    }
    
    log.Info("Successfully applied resource changes via SSA",
        "workload", fmt.Sprintf("%s/%s", workload.Namespace, workload.Name),
    )
    
    return nil
}
```

#### 3.2 Events

**File**: `internal/observability/events.go`

Add new event types for SSA:

```go
const (
    // ... existing events ...
    
    // EventReasonSSAOwnershipTaken indicates OptipPod took ownership of resource fields
    EventReasonSSAOwnershipTaken = "SSAOwnershipTaken"
    
    // EventReasonSSAConflict indicates a field ownership conflict
    EventReasonSSAConflict = "SSAConflict"
)

// RecordSSAOwnershipTaken records an event when OptipPod takes field ownership
func (r *EventRecorder) RecordSSAOwnershipTaken(
    policy *optipodv1alpha1.OptimizationPolicy,
    workload string,
    previousOwner string,
) {
    message := fmt.Sprintf(
        "Took ownership of resource fields for %s (previous owner: %s)",
        workload,
        previousOwner,
    )
    r.recorder.Event(policy, corev1.EventTypeNormal, EventReasonSSAOwnershipTaken, message)
}
```

#### 3.3 Prometheus Metrics

**File**: `internal/observability/metrics.go`

Add metrics for SSA operations:

```go
var (
    // ... existing metrics ...
    
    // SSA patch operations counter
    ssaPatchTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "optipod_ssa_patch_total",
            Help: "Total number of Server-Side Apply patch operations",
        },
        []string{"policy", "namespace", "workload", "kind", "status", "patch_type"},
    )
)

func RegisterMetrics() {
    // ... existing registrations ...
    metrics.Registry.MustRegister(ssaPatchTotal)
}

// RecordSSAPatch records an SSA patch operation
func RecordSSAPatch(policy, namespace, workload, kind, status, patchType string) {
    ssaPatchTotal.WithLabelValues(policy, namespace, workload, kind, status, patchType).Inc()
}
```

### 4. Status Updates

**File**: `api/v1alpha1/optimizationpolicy_types.go`

Add SSA information to workload status:

```go
type WorkloadStatus struct {
    // ... existing fields ...
    
    // LastApplyMethod indicates the patch method used for the last update
    // +optional
    LastApplyMethod string `json:"lastApplyMethod,omitempty"`
    
    // FieldOwnership indicates if OptipPod owns resource fields via SSA
    // +optional
    FieldOwnership bool `json:"fieldOwnership,omitempty"`
}
```

Update status after applying changes:

```go
// In workload processor
workloadStatus.LastApplyMethod = "ServerSideApply"  // or "StrategicMergePatch"
workloadStatus.FieldOwnership = true  // if SSA was used
```

## Data Models

### SSA Patch Structure

```json
{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "metadata": {
    "name": "my-app",
    "namespace": "production"
  },
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "app",
            "resources": {
              "requests": {
                "cpu": "300m",
                "memory": "512Mi"
              },
              "limits": {
                "cpu": "300m",
                "memory": "512Mi"
              }
            }
          }
        ]
      }
    }
  }
}
```

### ManagedFields After SSA

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  managedFields:
  - manager: argocd
    operation: Apply
    apiVersion: apps/v1
    time: "2025-11-29T10:00:00Z"
    fieldsType: FieldsV1
    fieldsV1:
      f:spec:
        f:replicas: {}
        f:template:
          f:metadata:
            f:labels: {}
          f:spec:
            f:containers:
              k:{"name":"app"}:
                f:name: {}
                f:image: {}
                f:env: {}
  
  - manager: optipod
    operation: Apply
    apiVersion: apps/v1
    time: "2025-11-29T10:05:00Z"
    fieldsType: FieldsV1
    fieldsV1:
      f:spec:
        f:template:
          f:spec:
            f:containers:
              k:{"name":"app"}:
                f:resources:
                  f:requests:
                    f:cpu: {}
                    f:memory: {}
                  f:limits:
                    f:cpu: {}
                    f:memory: {}
```

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system-essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: SSA uses correct field manager

*For any* workload and recommendation, when SSA is enabled, the patch operation should use "optipod" as the fieldManager
**Validates: Requirements 1.1, 4.1**

### Property 2: SSA patch contains only resource fields

*For any* workload and recommendation, the SSA patch should contain only resource requests and limits, not other fields like image or replicas
**Validates: Requirements 1.2, 3.1, 3.2**

### Property 3: Force flag is set for SSA

*For any* SSA patch operation, the Force option should be set to true
**Validates: Requirements 1.3**

### Property 4: Configuration determines patch method

*For any* policy with useServerSideApply=true, the system should use ApplyPatchType; for useServerSideApply=false, it should use StrategicMergePatchType
**Validates: Requirements 2.2, 2.3**

### Property 5: Default to SSA when unspecified

*For any* policy where useServerSideApply is nil, the system should behave as if useServerSideApply=true
**Validates: Requirements 2.4**

### Property 6: Container identification in patch

*For any* container name, the SSA patch should correctly identify the container by name in the containers array
**Validates: Requirements 3.3**

### Property 7: Conditional limits inclusion

*For any* recommendation, if updateRequestsOnly=true, the patch should not include limits; if false, it should include both requests and limits
**Validates: Requirements 3.4, 3.5**

### Property 8: Valid JSON serialization

*For any* constructed SSA patch, it should serialize to valid JSON that can be parsed back
**Validates: Requirements 3.6**

### Property 9: Field ownership tracking

*For any* workload updated via SSA, querying managedFields should show "optipod" as the manager for resource request and limit fields
**Validates: Requirements 4.2, 4.5**

### Property 10: Consistent field manager across policies

*For any* workload targeted by multiple policies, all SSA operations should use the same "optipod" fieldManager
**Validates: Requirements 4.3**

### Property 11: Apply operation type

*For any* SSA operation, the managedFields should record the operation as "Apply" not "Update"
**Validates: Requirements 4.4**

### Property 12: Logging includes field manager

*For any* SSA operation, the log output should contain the fieldManager name and Force setting
**Validates: Requirements 7.1**

### Property 13: Status tracks apply method

*For any* workload processed with SSA, the workload status should indicate lastApplyMethod as "ServerSideApply"
**Validates: Requirements 7.4**

### Property 14: Metrics track patch type

*For any* patch operation, Prometheus metrics should distinguish between SSA and Strategic Merge Patch operations
**Validates: Requirements 7.5**

## Error Handling

### SSA-Specific Errors

1. **Field Ownership Conflict**
   - **Cause**: Another manager owns the resource fields and Force=false
   - **Handling**: Return error with conflicting manager name
   - **User Action**: Review field ownership, consider Force=true

2. **Invalid Patch Format**
   - **Cause**: Malformed SSA patch (missing required fields)
   - **Handling**: Return validation error with details
   - **User Action**: Report bug (should not happen in production)

3. **RBAC Insufficient Permissions**
   - **Cause**: Service account lacks SSA permissions
   - **Handling**: Return RBAC error
   - **User Action**: Update RBAC to allow Patch with ApplyPatchType

4. **API Server Doesn't Support SSA**
   - **Cause**: Kubernetes version < 1.22
   - **Handling**: Fall back to Strategic Merge Patch with warning
   - **User Action**: Upgrade Kubernetes or disable SSA

## Testing Strategy

### Unit Tests

**File**: `internal/application/engine_test.go`

1. **Test SSA Patch Construction**
   ```go
   func TestBuildSSAPatch(t *testing.T) {
       // Test that patch contains only resource fields
       // Test that patch includes correct apiVersion, kind, metadata
       // Test that container is identified by name
       // Test conditional limits inclusion based on updateRequestsOnly
   }
   ```

2. **Test Patch Method Selection**
   ```go
   func TestApplyMethodSelection(t *testing.T) {
       // Test SSA is used when useServerSideApply=true
       // Test Strategic Merge is used when useServerSideApply=false
       // Test default behavior when useServerSideApply=nil
   }
   ```

3. **Test Error Handling**
   ```go
   func TestSSAErrorHandling(t *testing.T) {
       // Test conflict error handling
       // Test RBAC error handling
       // Test invalid patch error handling
   }
   ```

### Integration Tests

**File**: `internal/controller/integration_test.go`

1. **Test SSA with Real Kubernetes**
   ```go
   func TestSSAIntegration(t *testing.T) {
       // Create deployment
       // Apply SSA patch via OptipPod
       // Verify managedFields shows "optipod" as owner
       // Verify only resource fields are modified
   }
   ```

2. **Test Field Ownership**
   ```go
   func TestFieldOwnership(t *testing.T) {
       // Apply deployment via kubectl (manager: kubectl)
       // Apply SSA patch via OptipPod (manager: optipod)
       // Verify both managers coexist
       // Verify each owns their respective fields
   }
   ```

### E2E Tests

**File**: `test/e2e/ssa_test.go`

1. **Test ArgoCD Compatibility**
   ```go
   func TestArgoCDCompatibility(t *testing.T) {
       // Deploy app via ArgoCD
       // Apply OptipPod policy with SSA
       // Verify ArgoCD doesn't show OutOfSync
       // Verify ArgoCD sync doesn't revert OptipPod changes
   }
   ```

2. **Test End-to-End SSA Flow**
   ```go
   func TestSSAEndToEnd(t *testing.T) {
       // Create policy with SSA enabled
       // Deploy workload
       // Wait for OptipPod to optimize
       // Verify resources are updated
       // Verify managedFields shows OptipPod ownership
       // Verify other fields unchanged
   }
   ```

### Property-Based Tests

**File**: `internal/application/ssa_gopter_test.go`

1. **Property: SSA Patch Structure**
   ```go
   func TestSSAPatchStructureProperty(t *testing.T) {
       // Generate random workloads and recommendations
       // Build SSA patches
       // Verify all patches have required fields
       // Verify all patches contain only resource fields
   }
   ```

2. **Property: Field Manager Consistency**
   ```go
   func TestFieldManagerConsistencyProperty(t *testing.T) {
       // Generate random policies and workloads
       // Apply SSA patches
       // Verify all use "optipod" as fieldManager
   }
   ```

## ArgoCD Integration Guide

### ArgoCD Configuration

#### Option 1: ArgoCD 2.5+ (Automatic)

ArgoCD 2.5+ automatically respects SSA field ownership. No configuration needed.

#### Option 2: Explicit Ignore Differences

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  ignoreDifferences:
  - group: apps
    kind: Deployment
    managedFieldsManagers:
    - optipod
```

#### Option 3: Server-Side Apply for ArgoCD

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  syncPolicy:
    syncOptions:
    - ServerSideApply=true
    - RespectIgnoreDifferences=true
```

### Verification

After deploying with both ArgoCD and OptipPod:

```bash
# Check managedFields
kubectl get deployment my-app -o yaml | grep -A 20 managedFields

# Verify ArgoCD sync status
argocd app get my-app

# Check OptipPod status
kubectl get optimizationpolicy -o yaml
```

## Migration and Rollout

Since OptipPod is not yet live, SSA will be the default from the start:

1. **Default Configuration**: `useServerSideApply` defaults to `true`
2. **Opt-Out Available**: Users can set `useServerSideApply: false` if needed
3. **Documentation**: Clearly document SSA as the recommended approach
4. **Examples**: All examples use SSA by default

## Performance Considerations

### SSA vs Strategic Merge Patch

- **SSA**: Slightly more overhead due to field tracking, but negligible in practice
- **Strategic Merge**: Simpler but no field ownership tracking
- **Recommendation**: Use SSA for all production deployments

### Patch Size

SSA patches are typically **smaller** than Strategic Merge patches because they only include the fields being managed:

- **Strategic Merge**: ~500 bytes (includes full container spec)
- **SSA**: ~200 bytes (only resource fields)

## Security Considerations

### RBAC Requirements

OptipPod needs additional RBAC permissions for SSA:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-controller-manager
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "patch", "update"]
  # Note: "patch" verb covers both Strategic Merge and SSA
```

No additional permissions needed - the `patch` verb covers SSA.

### Force Flag Security

Setting `Force: true` allows OptipPod to take ownership even if another manager owns the fields. This is intentional and safe because:

1. OptipPod only manages resource requests/limits
2. Other managers (ArgoCD) manage different fields
3. Field ownership is tracked and auditable
4. Conflicts are logged and emit events

## Documentation Updates

### User Documentation

1. **Getting Started Guide**: Add SSA explanation and benefits
2. **Configuration Reference**: Document `useServerSideApply` field
3. **ArgoCD Integration**: Provide setup guide and examples
4. **Troubleshooting**: Add section on field ownership conflicts

### Developer Documentation

1. **Architecture**: Update diagrams to show SSA flow
2. **API Reference**: Document SSA-related fields and status
3. **Testing Guide**: Explain how to test SSA functionality

## Future Enhancements

1. **Configurable Field Manager Name**: Allow users to customize the fieldManager identifier
2. **Selective Force**: Option to use Force=false and handle conflicts gracefully
3. **Field Ownership Reporting**: CLI tool to visualize field ownership across cluster
4. **Multi-Manager Coordination**: Coordinate with other optimization tools using SSA

## References

- [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
- [ArgoCD Server-Side Apply Support](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#server-side-apply)
- [Managed Fields Specification](https://kubernetes.io/docs/reference/using-api/server-side-apply/#field-management)
