package v1alpha1

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// AssignmentRule matches all specified criteria. Alternatives within a field are ORed.
type AssignmentRule struct {
	NamespacePattern string                `json:"namespacePattern,omitempty"`
	Kinds            []string              `json:"kinds,omitempty"`
	Labels           *metav1.LabelSelector `json:"labels,omitempty"`
	// LabelValueContains matches a literal substring of any workload label value.
	LabelValueContains []string `json:"labelValueContains,omitempty"`
}

func (r AssignmentRule) validate() error {
	if r.NamespacePattern == "" && len(r.Kinds) == 0 && r.Labels == nil && len(r.LabelValueContains) == 0 {
		return fmt.Errorf("assignment rule requires at least one criterion")
	}
	if _, err := regexp.Compile(r.NamespacePattern); err != nil {
		return err
	}
	if r.Labels != nil {
		if _, err := metav1.LabelSelectorAsSelector(r.Labels); err != nil {
			return err
		}
	}
	if slices.Contains(r.LabelValueContains, "") || slices.Contains(r.Kinds, "") {
		return fmt.Errorf("assignment values cannot be empty")
	}
	return nil
}

func (r AssignmentRule) matches(ns, kind string, values map[string]string) bool {
	if len(r.Kinds) > 0 && !slices.Contains(r.Kinds, kind) {
		return false
	}
	if r.NamespacePattern != "" {
		ok, err := regexp.MatchString(r.NamespacePattern, ns)
		if err != nil || !ok {
			return false
		}
	}
	if r.Labels != nil {
		sel, err := metav1.LabelSelectorAsSelector(r.Labels)
		if err != nil || !sel.Matches(labels.Set(values)) {
			return false
		}
	}
	if len(r.LabelValueContains) > 0 {
		for _, part := range r.LabelValueContains {
			for _, v := range values {
				if part != "" && strings.Contains(v, part) {
					return true
				}
			}
		}
		return false
	}
	return r.NamespacePattern != "" || len(r.Kinds) > 0 || r.Labels != nil
}

// MatchesAssignment tests the additional rule disjunction after ordinary selectors.
func (s WorkloadSelector) MatchesAssignment(ns, kind string, values map[string]string) bool {
	if len(s.AnyOf) == 0 {
		return true
	}
	for _, r := range s.AnyOf {
		if r.matches(ns, kind, values) {
			return true
		}
	}
	return false
}
