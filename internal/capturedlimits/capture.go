// Package capturedlimits describes the fixed CPU and memory limits observed
// before a workload changes optimizer. Only policy writers can opt into them.
package capturedlimits

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const Annotation = "optipod.io/captured-limits"

// ForOwner returns a UID-specific capture first, with backward compatibility
// for policies created with one capture.
func ForOwner(annotations map[string]string, uid types.UID) string {
	if uid != "" {
		if raw, ok := annotations["optipod.io/capture-"+string(uid)]; ok {
			return raw
		}
	}
	return annotations[Annotation]
}

type Snapshot struct {
	WorkloadUID           types.UID                       `json:"workloadUID"`
	Containers            map[string]Container            `json:"containers,omitempty"`
	Ordinals              map[string]map[string]Container `json:"ordinals,omitempty"`
	Nodes                 map[string]NodeCapture          `json:"nodes,omitempty"`
	FallbackRequestsUntil *time.Time                      `json:"fallbackRequestsUntil,omitempty"`
	HoldRequestsUntil     *time.Time                      `json:"holdRequestsUntil,omitempty"`
}

type NodeCapture struct {
	UID        types.UID            `json:"uid"`
	Containers map[string]Container `json:"containers"`
}

type Container struct {
	RunningRequests *corev1.ResourceList `json:"runningRequests,omitempty"`
	Field           string               `json:"field"`
	// Nil identifies an injected container. An empty map means the template had no limits.
	Template corev1.ResourceList `json:"template"`
	Limits   corev1.ResourceList `json:"limits"`
	// Only utilization-owned dimensions are captured. Missing template keys
	// distinguish a previously absent request from an explicit quantity.
	HPARequests      corev1.ResourceList `json:"hpaRequests,omitempty"`
	TemplateRequests corev1.ResourceList `json:"templateRequests,omitempty"`
}

func Parse(raw string) (*Snapshot, error) {
	var s Snapshot
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing capture data")
	}
	formats := 0
	for _, n := range []int{len(s.Containers), len(s.Nodes), len(s.Ordinals)} {
		if n > 0 {
			formats++
		}
	}
	if s.WorkloadUID == "" || formats != 1 {
		return nil, fmt.Errorf("expected workload UID and exactly one capture format")
	}
	if len(s.Ordinals) != 0 {
		if s.HoldRequestsUntil == nil || s.FallbackRequestsUntil != nil {
			return nil, fmt.Errorf("ordinal capture requires a request hold")
		}
		for key, containers := range s.Ordinals {
			index, err := strconv.ParseUint(key, 10, 31)
			if err != nil || strconv.FormatUint(index, 10) != key || len(containers) == 0 {
				return nil, fmt.Errorf("invalid StatefulSet ordinal %q", key)
			}
			if err := validateContainers(containers, true); err != nil {
				return nil, err
			}
		}
		return &s, nil
	}
	if len(s.Nodes) == 0 {
		if err := validateContainers(s.Containers, s.FallbackRequestsUntil != nil || s.HoldRequestsUntil != nil); err != nil {
			return nil, err
		}
		return &s, nil
	}
	if s.FallbackRequestsUntil != nil {
		return nil, fmt.Errorf("request fallback requires a workload capture")
	}
	seen := map[types.UID]bool{}
	for name, n := range s.Nodes {
		if name == "" || n.UID == "" || seen[n.UID] || len(n.Containers) == 0 {
			return nil, fmt.Errorf("invalid node capture")
		}
		seen[n.UID] = true
		if err := validateContainers(n.Containers, true); err != nil {
			return nil, err
		}
	}
	return &s, nil
}
func validateContainers(containers map[string]Container, withRunningRequests bool) error {
	for name, c := range containers {
		if (withRunningRequests != (c.RunningRequests != nil)) || name == "" || c.Limits == nil || (c.Field != "containers" && c.Field != "initContainers") {
			return fmt.Errorf("invalid captured container %q", name)
		}
		for k := range c.TemplateRequests {
			if _, captured := c.HPARequests[k]; (!withRunningRequests && !captured) || c.Template == nil {
				return fmt.Errorf("unexpected template request %s for %s", k, name)
			}
		}
		// Template quantities are comparison baselines. Kubernetes accepts
		// an explicit zero request, which must stay distinct from an absent key.
		for _, baseline := range []corev1.ResourceList{c.Template, c.TemplateRequests} {
			for k, q := range baseline {
				if (k != corev1.ResourceCPU && k != corev1.ResourceMemory) || q.Sign() < 0 {
					return fmt.Errorf("invalid template %s quantity on %s", k, name)
				}
			}
		}
		resourcesToCheck := []corev1.ResourceList{c.Limits, c.HPARequests}
		if c.RunningRequests != nil {
			resourcesToCheck = append(resourcesToCheck, *c.RunningRequests)
		}
		for _, resources := range resourcesToCheck {
			for k, q := range resources {
				if (k != corev1.ResourceCPU && k != corev1.ResourceMemory) || q.Sign() <= 0 {
					return fmt.Errorf("invalid %s quantity on %s", k, name)
				}
			}
		}
	}
	return nil
}

func Same(a, b corev1.ResourceList, key corev1.ResourceName) bool {
	x, ok := a[key]
	y, found := b[key]
	return ok == found && x.Cmp(y) == 0
}
