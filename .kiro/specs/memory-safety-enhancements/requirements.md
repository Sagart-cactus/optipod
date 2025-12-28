# Memory Safety Enhancements Requirements

## Problem Statement

The current `isUnsafeMemoryDecrease` safety mechanism is blocking auto-application of resource recommendations in scenarios where it should be safe to proceed. The safety check has several limitations:

1. **Incorrect Comparison**: Compares memory recommendations (requests) against current limits instead of considering the final calculated limits
2. **Ignores LimitConfig**: Doesn't account for `memoryLimitMultiplier` which may result in higher limits than current
3. **Ignores UpdateRequestsOnly**: Doesn't consider that when `updateRequestsOnly=true`, limits aren't changed
4. **No Override Option**: No way to disable safety checks for advanced users
5. **No Gradual Decrease**: No option for safe percentage-based memory reduction over time

## Current Behavior

**Test Case 1 - Overutilized Workload:**
- Current: requests=128Mi, limits=1Gi
- Recommendation: 997608Ki (≈974Mi)
- With LimitConfig (1.3x): Final limit would be ≈1.24Gi
- **Issue**: Safety check compares 974Mi < 1Gi and blocks, ignoring that final limit (1.24Gi) > current limit (1Gi)

**Test Case 2 - Underutilized Workload:**
- Current: requests=4Gi, limits=4Gi  
- Recommendation: 32Mi
- With LimitConfig (1.3x): Final limit would be ≈42Mi
- **Issue**: Safety check blocks because 32Mi < 4Gi, but this is exactly the optimization we want

## User Stories

### US1: Enhanced Safety Check Logic
**As a** platform engineer  
**I want** the safety check to consider LimitConfig and updateRequestsOnly settings  
**So that** safe optimizations aren't blocked by overly conservative logic

**Acceptance Criteria:**
- Safety check considers final calculated limits, not just recommendations
- When `updateRequestsOnly=true`, safety check only considers request changes
- When LimitConfig multipliers result in higher limits, allow the change
- Maintain safety for genuine unsafe decreases

### US2: Disable Safety Checks Option
**As a** platform engineer  
**I want** an option to disable memory safety checks  
**So that** I can override conservative behavior when I understand the risks

**Acceptance Criteria:**
- Add `AllowUnsafeMemoryDecrease` boolean field to UpdateStrategy
- When true, skip all memory safety checks
- Default to false for backward compatibility
- Document the risks clearly

### US3: Gradual Memory Decrease
**As a** platform engineer  
**I want** to gradually decrease memory over time by safe percentages  
**So that** I can optimize overprovisioned workloads without risk

**Acceptance Criteria:**
- Add `GradualDecreaseConfig` to UpdateStrategy
- Support configurable decrease percentage per reconciliation
- Support minimum decrease threshold
- Support maximum total decrease limit
- Only apply to memory decreases, not increases

## Technical Requirements

### API Changes
```yaml
updateStrategy:
  # Existing fields...
  allowUnsafeMemoryDecrease: false  # New field
  gradualDecreaseConfig:            # New section
    enabled: false
    memoryDecreasePercentage: 10    # Max 10% decrease per reconciliation
    minimumDecreaseThreshold: "100Mi" # Only apply if decrease > 100Mi
    maximumTotalDecrease: 50        # Max 50% total decrease from original
```

### Implementation Requirements

1. **Enhanced isUnsafeMemoryDecrease Method:**
   - Consider final calculated limits from LimitConfig
   - Respect updateRequestsOnly setting
   - Check AllowUnsafeMemoryDecrease override
   - Implement gradual decrease logic

2. **Backward Compatibility:**
   - All new fields optional with safe defaults
   - Existing behavior preserved when new fields not set
   - No breaking changes to existing APIs

3. **Logging and Observability:**
   - Log safety check decisions with reasoning
   - Metrics for safety check blocks vs allows
   - Clear error messages when blocked

4. **Testing:**
   - Unit tests for all safety check scenarios
   - Integration tests with real workloads
   - Test gradual decrease over multiple reconciliations

## Success Criteria

1. **Test Case 1 Resolution**: Overutilized workload auto-applies when LimitConfig results in higher final limits
2. **Test Case 2 Resolution**: Underutilized workload can be optimized with appropriate safety settings
3. **Gradual Decrease**: Large memory reductions happen safely over multiple reconciliations
4. **Override Capability**: Advanced users can disable safety checks when needed
5. **Backward Compatibility**: Existing policies continue to work unchanged

## Implementation Priority

1. **High Priority**: Enhanced safety check logic (US1)
2. **Medium Priority**: Disable safety checks option (US2)  
3. **Low Priority**: Gradual decrease feature (US3)

## Risks and Mitigations

**Risk**: Disabling safety checks could cause pod evictions  
**Mitigation**: Clear documentation, conservative defaults, detailed logging

**Risk**: Gradual decrease complexity  
**Mitigation**: Start with simple percentage-based approach, iterate based on feedback

**Risk**: Breaking existing behavior  
**Mitigation**: Extensive testing, feature flags, gradual rollout