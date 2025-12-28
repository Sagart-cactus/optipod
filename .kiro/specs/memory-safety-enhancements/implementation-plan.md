# Memory Safety Enhancements Implementation Plan

## Overview

This document outlines the implementation plan for enhancing OptipPod's memory safety mechanisms to address blocking issues in auto-application mode.

## Phase 1: Enhanced Safety Check Logic (High Priority)

### 1.1 API Schema Updates

**File**: `api/v1alpha1/optimizationpolicy_types.go`

Add new fields to `UpdateStrategy`:

```go
type UpdateStrategy struct {
    // Existing fields...
    
    // AllowUnsafeMemoryDecrease disables memory safety checks when true
    // +optional
    AllowUnsafeMemoryDecrease *bool `json:"allowUnsafeMemoryDecrease,omitempty"`
    
    // GradualDecreaseConfig configures gradual memory reduction
    // +optional
    GradualDecreaseConfig *GradualDecreaseConfig `json:"gradualDecreaseConfig,omitempty"`
}

// GradualDecreaseConfig defines parameters for gradual memory reduction
type GradualDecreaseConfig struct {
    // Enabled activates gradual decrease functionality
    Enabled bool `json:"enabled"`
    
    // MemoryDecreasePercentage is the maximum percentage to decrease memory per reconciliation (1-50)
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=50
    MemoryDecreasePercentage int `json:"memoryDecreasePercentage"`
    
    // MinimumDecreaseThreshold is the minimum decrease amount to trigger gradual reduction
    // +optional
    MinimumDecreaseThreshold *resource.Quantity `json:"minimumDecreaseThreshold,omitempty"`
    
    // MaximumTotalDecrease is the maximum total percentage decrease from original (1-90)
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=90
    // +optional
    MaximumTotalDecrease *int `json:"maximumTotalDecrease,omitempty"`
}
```

### 1.2 Engine Logic Updates

**File**: `internal/application/engine.go`

#### 1.2.1 Enhanced isUnsafeMemoryDecrease Method

```go
// isUnsafeMemoryDecrease checks if a memory decrease could be unsafe
func (e *Engine) isUnsafeMemoryDecrease(
    currentResources map[string]corev1.ResourceRequirements,
    containerName string,
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) bool {
    log := ctrl.LoggerFrom(context.Background())
    
    // Check if safety checks are disabled
    if policy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease != nil && 
       *policy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease {
        log.Info("Memory safety checks disabled by policy", 
            "container", containerName,
            "recommendation", rec.Memory.String())
        return false
    }
    
    // Get current resources for the specific container
    currentReqs, exists := currentResources[containerName]
    if !exists {
        log.Info("Container not found in current resources, allowing change",
            "container", containerName)
        return false
    }
    
    // If updateRequestsOnly is true, only check request changes
    if policy.Spec.UpdateStrategy.UpdateRequestsOnly {
        return e.isUnsafeRequestDecrease(currentReqs, rec, containerName)
    }
    
    // Calculate what the final limits would be after applying LimitConfig
    _, finalMemoryLimit := e.calculateLimits(rec, policy)
    
    // Get current memory limit
    currentMemoryLimit, hasCurrentLimit := currentReqs.Limits[corev1.ResourceMemory]
    
    if !hasCurrentLimit {
        // No current limit, so setting one is safe
        log.Info("No current memory limit, allowing change",
            "container", containerName,
            "finalLimit", finalMemoryLimit.String())
        return false
    }
    
    // Check if final limit would be significantly lower than current limit
    // Allow small decreases (< 10%) but block large ones
    decreaseThreshold := multiplyQuantity(currentMemoryLimit, 0.9) // 90% of current
    
    if finalMemoryLimit.Cmp(decreaseThreshold) < 0 {
        log.Info("Memory limit decrease blocked by safety check",
            "container", containerName,
            "currentLimit", currentMemoryLimit.String(),
            "finalLimit", finalMemoryLimit.String(),
            "threshold", decreaseThreshold.String())
        return true
    }
    
    log.Info("Memory limit change allowed by safety check",
        "container", containerName,
        "currentLimit", currentMemoryLimit.String(),
        "finalLimit", finalMemoryLimit.String())
    return false
}

// isUnsafeRequestDecrease checks if a memory request decrease could be unsafe
func (e *Engine) isUnsafeRequestDecrease(
    currentReqs corev1.ResourceRequirements,
    rec *recommendation.Recommendation,
    containerName string,
) bool {
    log := ctrl.LoggerFrom(context.Background())
    
    currentMemoryRequest, hasCurrentRequest := currentReqs.Requests[corev1.ResourceMemory]
    if !hasCurrentRequest {
        // No current request, so setting one is safe
        return false
    }
    
    // Allow request decreases that are reasonable (> 50% of current)
    // This prevents extreme decreases that could cause immediate OOM
    minimumRequest := multiplyQuantity(currentMemoryRequest, 0.5) // 50% of current
    
    if rec.Memory.Cmp(minimumRequest) < 0 {
        log.Info("Memory request decrease blocked by safety check",
            "container", containerName,
            "currentRequest", currentMemoryRequest.String(),
            "recommendation", rec.Memory.String(),
            "minimum", minimumRequest.String())
        return true
    }
    
    return false
}
```

#### 1.2.2 Update CanApply Method

```go
// Update the CanApply method to pass containerName to safety check
func (e *Engine) CanApply(
    ctx context.Context,
    workload *Workload,
    containerName string, // Add this parameter
    rec *recommendation.Recommendation,
    policy *optipodv1alpha1.OptimizationPolicy,
) (*ApplyDecision, error) {
    // ... existing checks ...
    
    // Get current container resources
    currentResources, err := e.getCurrentResources(workload)
    if err != nil {
        return nil, fmt.Errorf("failed to get current resources: %w", err)
    }
    
    // Check for memory decrease safety with container-specific logic
    if e.isUnsafeMemoryDecrease(currentResources, containerName, rec, policy) {
        return &ApplyDecision{
            CanApply: false,
            Method:   Skip,
            Reason:   "Memory decrease could cause pod eviction or OOM",
        }, nil
    }
    
    // ... rest of method ...
}
```

### 1.3 Controller Updates

**File**: `internal/controller/optimizationpolicy_controller.go`

Update the controller to pass container name to CanApply:

```go
// In the workload processing logic
decision, err := engine.CanApply(ctx, workload, containerName, recommendation, policy)
if err != nil {
    return fmt.Errorf("failed to check if changes can be applied: %w", err)
}

if !decision.CanApply {
    log.Info("Skipping workload update", 
        "reason", decision.Reason,
        "container", containerName)
    return nil
}
```

## Phase 2: Gradual Decrease Implementation (Medium Priority)

### 2.1 Gradual Decrease Logic

Add to `engine.go`:

```go
// applyGradualDecrease applies gradual memory reduction if configured
func (e *Engine) applyGradualDecrease(
    currentMemory resource.Quantity,
    targetMemory resource.Quantity,
    policy *optipodv1alpha1.OptimizationPolicy,
) resource.Quantity {
    gradualConfig := policy.Spec.UpdateStrategy.GradualDecreaseConfig
    if gradualConfig == nil || !gradualConfig.Enabled {
        return targetMemory
    }
    
    // Only apply to decreases
    if targetMemory.Cmp(currentMemory) >= 0 {
        return targetMemory
    }
    
    // Check minimum decrease threshold
    if gradualConfig.MinimumDecreaseThreshold != nil {
        decrease := currentMemory.DeepCopy()
        decrease.Sub(targetMemory)
        if decrease.Cmp(*gradualConfig.MinimumDecreaseThreshold) < 0 {
            return targetMemory // Decrease too small, apply directly
        }
    }
    
    // Calculate gradual decrease amount
    decreasePercentage := float64(gradualConfig.MemoryDecreasePercentage) / 100.0
    maxDecrease := multiplyQuantity(currentMemory, decreasePercentage)
    
    // Calculate new memory value
    newMemory := currentMemory.DeepCopy()
    newMemory.Sub(maxDecrease)
    
    // Don't go below target
    if newMemory.Cmp(targetMemory) < 0 {
        newMemory = targetMemory
    }
    
    return newMemory
}
```

## Phase 3: Testing Strategy

### 3.1 Unit Tests

**File**: `internal/application/engine_safety_test.go`

```go
func TestIsUnsafeMemoryDecrease(t *testing.T) {
    tests := []struct {
        name           string
        currentLimits  string
        recommendation string
        limitMultiplier float64
        updateRequestsOnly bool
        allowUnsafe    bool
        expected       bool
    }{
        {
            name: "safe decrease with limit multiplier",
            currentLimits: "1Gi",
            recommendation: "974Mi", 
            limitMultiplier: 1.3,
            expected: false, // 974Mi * 1.3 = 1.24Gi > 1Gi
        },
        {
            name: "unsafe decrease blocked",
            currentLimits: "1Gi",
            recommendation: "100Mi",
            limitMultiplier: 1.1,
            expected: true, // 100Mi * 1.1 = 110Mi < 1Gi
        },
        {
            name: "unsafe decrease allowed with override",
            currentLimits: "1Gi", 
            recommendation: "100Mi",
            limitMultiplier: 1.1,
            allowUnsafe: true,
            expected: false,
        },
        // ... more test cases
    }
    
    // Test implementation...
}
```

### 3.2 Integration Tests

Test with real workloads in the cluster to verify:
1. Overutilized workload gets optimized
2. Underutilized workload gets optimized with appropriate settings
3. Safety checks work as expected
4. Gradual decrease works over multiple reconciliations

## Phase 4: Documentation Updates

### 4.1 API Documentation

Update CRD documentation with new fields and examples.

### 4.2 User Guide

Add section on memory safety configuration with examples:

```yaml
# Example: Allow aggressive optimization
updateStrategy:
  allowUnsafeMemoryDecrease: true
  
# Example: Gradual decrease for safety  
updateStrategy:
  gradualDecreaseConfig:
    enabled: true
    memoryDecreasePercentage: 15
    minimumDecreaseThreshold: "200Mi"
    maximumTotalDecrease: 70
```

## Implementation Timeline

- **Week 1**: Phase 1.1-1.2 (API changes and enhanced safety logic)
- **Week 2**: Phase 1.3 and testing (Controller updates and unit tests)
- **Week 3**: Phase 2 (Gradual decrease implementation)
- **Week 4**: Phase 3-4 (Integration testing and documentation)

## Success Metrics

1. **Functional**: Test workloads auto-apply successfully
2. **Safety**: No pod evictions in production clusters
3. **Adoption**: Users can configure policies for their risk tolerance
4. **Performance**: No significant impact on reconciliation time