package v1alpha1

import (
	"fmt"
	"math"
	"regexp"

	"k8s.io/apimachinery/pkg/api/resource"
)

// MetricSource identifies a Prometheus gauge or recording rule. OptiPod always
// adds exact namespace, pod and container selectors; arbitrary PromQL is not accepted.
type MetricSource struct {
	// +kubebuilder:validation:Pattern=`^[a-zA-Z_:][a-zA-Z0-9_:]*$`
	Name string `json:"name"`
	// Additional exact-match labels (namespace, pod and container cannot be overridden).
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Converts the gauge to CPU cores or memory bytes. Defaults to 1.
	// +optional
	Multiplier *float64 `json:"multiplier,omitempty"`
}

// JVMConfig protects Java heap capacity, non-heap memory and a native reserve.
// It reads existing instrumentation and never edits JVM flags or injects an agent.
type JVMConfig struct {
	HeapUsed    MetricSource `json:"heapUsed"`
	HeapMax     MetricSource `json:"heapMax"`
	NonHeapUsed MetricSource `json:"nonHeapUsed"`
	// Reserve for native allocations, thread stacks and direct buffers. Required.
	NativeReserve resource.Quantity `json:"nativeReserve"`
	// Desired fraction of heap capacity used; defaults to 0.7.
	// +optional
	TargetHeapUtilization *float64 `json:"targetHeapUtilization,omitempty"`
}

var metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
var metricLabelPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func ValidateMetricSource(s *MetricSource) error {
	if s == nil {
		return nil
	}
	if !metricNamePattern.MatchString(s.Name) {
		return fmt.Errorf("invalid Prometheus metric name")
	}
	for k := range s.Labels {
		if !metricLabelPattern.MatchString(k) || k == "namespace" || k == "pod" || k == "container" || k == "__name__" {
			return fmt.Errorf("metric label %s is invalid or reserved", k)
		}
	}
	if s.Multiplier != nil && (math.IsNaN(*s.Multiplier) || math.IsInf(*s.Multiplier, 0) || *s.Multiplier <= 0 || *s.Multiplier > 1e6) {
		return fmt.Errorf("metric multiplier must be positive and at most 1000000")
	}
	return nil
}
func (p *OptimizationPolicy) validateSources() error {
	custom := false
	for _, s := range []*ResourceMetricsConfig{p.Spec.MetricsConfig.CPU, p.Spec.MetricsConfig.Memory} {
		if s != nil && s.Source != nil {
			custom = true
			if err := ValidateMetricSource(s.Source); err != nil {
				return err
			}
		}
	}
	if j := p.Spec.MetricsConfig.JVM; j != nil {
		custom = true
		if p.Spec.MetricsConfig.Memory != nil && p.Spec.MetricsConfig.Memory.Source != nil {
			return fmt.Errorf("JVM and custom memory source cannot be combined")
		}
		for _, s := range []*MetricSource{&j.HeapUsed, &j.HeapMax, &j.NonHeapUsed} {
			if err := ValidateMetricSource(s); err != nil {
				return err
			}
		}
		if j.NativeReserve.Sign() <= 0 {
			return fmt.Errorf("JVM nativeReserve must be positive")
		}
		if j.TargetHeapUtilization != nil && (*j.TargetHeapUtilization < .1 || *j.TargetHeapUtilization > 1) {
			return fmt.Errorf("JVM targetHeapUtilization must be between 0.1 and 1")
		}
	}
	if custom && p.Spec.MetricsConfig.Provider != "prometheus" {
		return fmt.Errorf("custom and JVM metrics require Prometheus")
	}
	return nil
}
