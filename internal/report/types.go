// Package report provides read-only fleet and journal views from Kubernetes.
package report

import (
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	corev1 "k8s.io/api/core/v1"
)

type Container struct {
	Name        string                      `json:"name"`
	Current     corev1.ResourceRequirements `json:"current"`
	Recommended map[string]string           `json:"recommended,omitempty"`
}
type Pod struct {
	Name       string                                 `json:"name"`
	UID        string                                 `json:"uid"`
	Node       string                                 `json:"node"`
	Ready      bool                                   `json:"ready"`
	Restarts   int32                                  `json:"restarts"`
	Containers map[string]corev1.ResourceRequirements `json:"containers"`
}
type Workload struct {
	Kind                  string         `json:"kind"`
	Namespace             string         `json:"namespace"`
	Name                  string         `json:"name"`
	UID                   string         `json:"uid"`
	Policy                string         `json:"policy"`
	Mode                  api.PolicyMode `json:"mode"`
	Provider              string         `json:"provider"`
	Paused                bool           `json:"paused"`
	RecommendationExpires string         `json:"recommendationExpires,omitempty"`
	LatestRecord          string         `json:"latestRecord,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	Containers            []Container    `json:"containers"`
	Pods                  []Pod          `json:"pods"`
	Scalers               []string       `json:"scalers"`
	NodePlans             map[string]any `json:"nodePlans,omitempty"`
	RequestedMilliCPU     int64          `json:"requestedMilliCPU"`
	RequestedMemoryBytes  int64          `json:"requestedMemoryBytes"`
}
type Fleet struct {
	Generated time.Time  `json:"generated"`
	Workloads []Workload `json:"workloads"`
	Policies  int        `json:"policies"`
	Warnings  []string   `json:"warnings,omitempty"`
}
type Journal struct {
	Generated time.Time         `json:"generated"`
	Records   []*history.Record `json:"records"`
}
