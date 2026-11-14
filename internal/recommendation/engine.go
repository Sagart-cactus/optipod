/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package recommendation

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
)

// Recommendation represents a computed resource recommendation for a container
type Recommendation struct {
	// ResourceOverride is internal-only and used for an explicitly requested journal rollback.
	ResourceOverride   map[string]any
	PreserveCPU        bool
	PreserveMemory     bool
	Emergency          bool
	MinimumMemoryLimit *resource.Quantity
	ValidUntil         time.Time
	CPU                resource.Quantity
	Memory             resource.Quantity
	Explanation        string
}

// Engine computes resource recommendations based on metrics and policy configuration
type Engine struct{}

// NewEngine creates a new recommendation engine
func NewEngine() *Engine {
	return &Engine{}
}

// ComputeRecommendation computes optimal resource requests based on usage metrics and policy
func (e *Engine) ComputeRecommendation(
	containerMetrics *metrics.ContainerMetrics,
	policy *optipodv1alpha1.OptimizationPolicy,
) (*Recommendation, error) {
	if containerMetrics == nil {
		return nil, fmt.Errorf("container metrics cannot be nil")
	}
	if policy == nil {
		return nil, fmt.Errorf("policy cannot be nil")
	}

	// Select percentile based on policy configuration
	cpuConfig := policy.Spec.MetricsConfig.ResourceSettings(true)
	memoryConfig := policy.Spec.MetricsConfig.ResourceSettings(false)
	cpuPercentile := selectPercentile(containerMetrics.CPU, cpuConfig.Percentile)
	memoryPercentile := selectPercentile(containerMetrics.Memory, memoryConfig.Percentile)

	// Apply safety factor
	safetyFactor := 1.2 // default
	if policy.Spec.MetricsConfig.SafetyFactor != nil {
		safetyFactor = *policy.Spec.MetricsConfig.SafetyFactor
	}

	cpuWithSafety := multiplyQuantity(cpuPercentile, *cpuConfig.SafetyFactor)
	memoryWithSafety := multiplyQuantity(memoryPercentile, *memoryConfig.SafetyFactor)

	if policy.Spec.MetricsConfig.JVM != nil && policy.ControlsResource("memory") {
		floor := containerMetrics.Memory.P100
		if floor.Cmp(policy.Spec.ResourceBounds.Memory.Max) > 0 {
			return nil, fmt.Errorf("JVM memory floor exceeds policy maximum; raise the bound or review JVM heap/native configuration")
		}
		if memoryWithSafety.Cmp(floor) < 0 {
			memoryWithSafety = floor.DeepCopy()
		}
	}
	// Clamp to bounds
	cpuRecommendation := clampToBounds(cpuWithSafety, policy.Spec.ResourceBounds.CPU)
	memoryRecommendation := clampToBounds(memoryWithSafety, policy.Spec.ResourceBounds.Memory)

	cpuRecommendation, forecastExplanation := predictiveCPU(containerMetrics.CPU.Points, cpuRecommendation, policy)

	// Generate explanation
	percentileStr := policy.Spec.MetricsConfig.Percentile
	if percentileStr == "" {
		percentileStr = "P90"
	}
	explanation := fmt.Sprintf(
		"Computed from %s percentile (CPU: %s, Memory: %s) with safety factor %.2f, clamped to bounds (CPU: %s-%s, Memory: %s-%s)",
		percentileStr,
		cpuPercentile.String(),
		memoryPercentile.String(),
		safetyFactor,
		policy.Spec.ResourceBounds.CPU.Min.String(),
		policy.Spec.ResourceBounds.CPU.Max.String(),
		policy.Spec.ResourceBounds.Memory.Min.String(),
		policy.Spec.ResourceBounds.Memory.Max.String(),
	)
	if policy.Spec.MetricsConfig.CPU != nil || policy.Spec.MetricsConfig.Memory != nil {
		explanation = fmt.Sprintf("CPU %s over %s x %.2f; memory %s over %s x %.2f; clamped to policy bounds",
			cpuConfig.Percentile, cpuConfig.RollingWindow.Duration, *cpuConfig.SafetyFactor,
			memoryConfig.Percentile, memoryConfig.RollingWindow.Duration, *memoryConfig.SafetyFactor)
	}

	if policy.Spec.MetricsConfig.JVM != nil {
		explanation += "; JVM maximum heap, non-heap, native reserve and observed working set protected"
	}
	if policy.Spec.MetricsConfig.CPU != nil && policy.Spec.MetricsConfig.CPU.Source != nil {
		explanation += "; custom CPU gauge: " + policy.Spec.MetricsConfig.CPU.Source.Name
	}
	if policy.Spec.MetricsConfig.Memory != nil && policy.Spec.MetricsConfig.Memory.Source != nil {
		explanation += "; custom memory gauge: " + policy.Spec.MetricsConfig.Memory.Source.Name
	}
	if forecastExplanation != "" {
		explanation += "; " + forecastExplanation
	}
	return &Recommendation{
		ValidUntil:  containerMetrics.ValidUntil,
		CPU:         cpuRecommendation,
		Memory:      memoryRecommendation,
		Explanation: explanation,
	}, nil
}

// selectPercentile selects the appropriate percentile value based on configuration
func selectPercentile(resourceMetrics metrics.ResourceMetrics, percentile string) resource.Quantity {
	switch percentile {
	case "P50":
		return resourceMetrics.P50
	case "P99":
		return resourceMetrics.P99
	case "P95":
		return resourceMetrics.P95
	case "P100":
		return resourceMetrics.P100
	case "P90", "":
		return resourceMetrics.P90
	default:
		return resourceMetrics.P90
	}
}

// multiplyQuantity multiplies a resource quantity by a factor
func multiplyQuantity(q resource.Quantity, factor float64) resource.Quantity {
	// For CPU quantities (DecimalSI format), work with millivalue to preserve millicores
	// For Memory quantities (BinarySI format), work with value (bytes)
	if q.Format == resource.DecimalSI {
		// CPU quantity - use millivalue to preserve millicores
		milliValue := q.MilliValue()
		result := int64(float64(milliValue) * factor)
		newQuantity := resource.NewMilliQuantity(result, q.Format)
		return *newQuantity
	} else {
		// Memory quantity - use value (bytes)
		value := q.Value()
		result := int64(float64(value) * factor)
		newQuantity := resource.NewQuantity(result, q.Format)
		return *newQuantity
	}
}

// clampToBounds ensures a value is within the specified min/max bounds
func clampToBounds(value resource.Quantity, bounds optipodv1alpha1.ResourceBound) resource.Quantity {
	// If value < min, return min
	if value.Cmp(bounds.Min) < 0 {
		return bounds.Min.DeepCopy()
	}
	// If value > max, return max
	if value.Cmp(bounds.Max) > 0 {
		return bounds.Max.DeepCopy()
	}
	// Otherwise return value
	return value.DeepCopy()
}
