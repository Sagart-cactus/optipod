package nodesizing

import (
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const ReceiptAnnotation = "optipod.io/node-sizing-receipt"

type Receipt struct {
	PolicySpec       *api.OptimizationPolicySpec            `json:"policySpec,omitempty"`
	WorkloadUID      types.UID                              `json:"workloadUID"`
	PolicyNamespace  string                                 `json:"policyNamespace"`
	PolicyName       string                                 `json:"policyName"`
	PolicyUID        types.UID                              `json:"policyUID"`
	PolicyGeneration int64                                  `json:"policyGeneration"`
	Node             string                                 `json:"node"`
	Before           map[string]corev1.ResourceRequirements `json:"before"`
	Plan             map[string]map[string]any              `json:"plan"`
	Created          time.Time                              `json:"created"`
}
