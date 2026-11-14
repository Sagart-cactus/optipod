package v1alpha1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BatchConfig bounds the reuse of completed execution observations. Batch
// workloads must be explicitly included in selector.workloadTypes.include.
type BatchConfig struct {
	// Maximum age of completed execution observations, defaults to seven days.
	// +optional
	MaxExecutionAge metav1.Duration `json:"maxExecutionAge,omitempty"`
	// Minimum distinct successful executions; defaults to one. Parallel pods
	// from the same Job count as one execution.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=512
	// +optional
	MinCompletedExecutions *int32 `json:"minCompletedExecutions,omitempty"`
}

func (p *OptimizationPolicy) BatchSettings() (time.Duration, int) {
	age, n := 7*24*time.Hour, 1
	if b := p.Spec.Batch; b != nil {
		if b.MaxExecutionAge.Duration > 0 {
			age = b.MaxExecutionAge.Duration
		}
		if b.MinCompletedExecutions != nil {
			n = int(*b.MinCompletedExecutions)
		}
	}
	return age, n
}
func (p *OptimizationPolicy) validateBatch() error {
	if b := p.Spec.Batch; b != nil {
		if b.MaxExecutionAge.Duration < 0 || b.MaxExecutionAge.Duration > 30*24*time.Hour {
			return fmt.Errorf("batch.maxExecutionAge must be between zero and 720h")
		}
		if b.MinCompletedExecutions != nil && (*b.MinCompletedExecutions < 1 || *b.MinCompletedExecutions > 512) {
			return fmt.Errorf("batch.minCompletedExecutions must be between 1 and 512")
		}
	}
	return nil
}
