package console

import (
	"fmt"
	"net/http"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

type FirstPolicy struct {
	ClusterID  string `json:"clusterID"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	LabelKey   string `json:"labelKey"`
	LabelValue string `json:"labelValue"`
	CPUMin     string `json:"cpuMin"`
	CPUMax     string `json:"cpuMax"`
	MemoryMin  string `json:"memoryMin"`
	MemoryMax  string `json:"memoryMax"`
}

// RecommendPolicy returns an artifact only. It never writes to a cluster.
func RecommendPolicy(p FirstPolicy, r Registration) (string, error) {
	if p.ClusterID != r.ID || len(validation.IsDNS1123Label(p.Name)) > 0 || len(validation.IsDNS1123Label(p.Namespace)) > 0 || p.Namespace == "kube-system" || p.Namespace == "kube-public" || p.Namespace == "kube-node-lease" || (len(r.Namespaces) > 0 && !slices.Contains(r.Namespaces, p.Namespace)) {
		return "", ErrInvalid
	}
	if p.LabelKey == "" || p.LabelValue == "" || len(validation.IsQualifiedName(p.LabelKey)) > 0 || len(validation.IsValidLabelValue(p.LabelValue)) > 0 {
		return "", ErrInvalid
	}
	for _, pair := range [][2]string{{p.CPUMin, p.CPUMax}, {p.MemoryMin, p.MemoryMax}} {
		min, err := resource.ParseQuantity(pair[0])
		if err != nil || min.Sign() <= 0 {
			return "", ErrInvalid
		}
		max, err := resource.ParseQuantity(pair[1])
		if err != nil || max.Cmp(min) < 0 {
			return "", ErrInvalid
		}
	}
	doc := map[string]any{"apiVersion": "optipod.optipod.io/v1alpha1", "kind": "OptimizationPolicy", "metadata": map[string]string{"name": p.Name, "namespace": p.Namespace}, "spec": map[string]any{
		"mode": "Recommend", "selector": map[string]any{"namespaces": map[string]any{"allow": []string{p.Namespace}, "deny": []string{"kube-system", "kube-public", "kube-node-lease"}}, "workloadSelector": map[string]any{"matchLabels": map[string]string{p.LabelKey: p.LabelValue}}},
		"metricsConfig":  map[string]any{"provider": r.MetricsSource, "rollingWindow": "24h", "minimumHistory": "1h", "minCoveragePercent": 80, "minSamples": 12, "percentile": "P95", "safetyFactor": 1.2},
		"resourceBounds": map[string]any{"cpu": map[string]string{"min": p.CPUMin, "max": p.CPUMax}, "memory": map[string]string{"min": p.MemoryMin, "max": p.MemoryMax}},
		"updateStrategy": map[string]bool{"allowInPlaceResize": true, "allowRecreate": false, "updateRequestsOnly": true, "useServerSideApply": true}, "reconciliationInterval": "5m"}}
	data, err := yaml.Marshal(doc)
	return string(data), err
}
func (s *Server) firstPolicy(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) {
		return
	}
	var input FirstPolicy
	if !decodeInput(w, r, &input) {
		return
	}
	registrations, err := s.Store.Registrations(r.Context())
	if err != nil {
		s.onboardingError(w, err)
		return
	}
	for _, reg := range registrations {
		if reg.ID == input.ClusterID {
			artifact, e := RecommendPolicy(input, reg)
			s.adminResult(w, map[string]string{"yaml": artifact, "guidance": "Review and apply this scoped Recommend policy with your own Kubernetes credentials. It never changes workload resources. Wait for sufficient history; keep system namespaces excluded. Metrics provider must match the existing controller."}, e)
			return
		}
	}
	s.onboardingError(w, fmt.Errorf("%w: unknown cluster", ErrInvalid))
}
