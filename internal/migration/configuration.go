// Package migration translates explicit workload migration configurations.
// It deliberately rejects unknown settings instead of silently dropping behavior.
package migration

import (
	"fmt"
	"strings"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

type Resource struct {
	Target         string   `json:"target"`
	LookBackPeriod string   `json:"lookBackPeriod"`
	Min            string   `json:"min"`
	Max            string   `json:"max"`
	Overhead       *float64 `json:"overhead"`
	Limit          struct {
		Type       string   `json:"type"`
		Multiplier *float64 `json:"multiplier"`
	} `json:"limit"`
}
type Configuration struct {
	ScalingPolicyName string `json:"scalingPolicyName"`
	Vertical          struct {
		Optimization       string   `json:"optimization"`
		ApplyType          string   `json:"applyType"`
		CPU                Resource `json:"cpu"`
		Memory             Resource `json:"memory"`
		ExcludedContainers []string `json:"excludedContainers"`
		Startup            struct {
			Period string `json:"period"`
		} `json:"startup"`
		Confidence *struct {
			Threshold *float64 `json:"threshold"`
			Required  *bool    `json:"required"`
		} `json:"confidence"`
	} `json:"vertical"`
}
type Options struct {
	Namespace, Name string
	// Explicitly supplied replacements. Neither is inferred from source confidence.
	CoveragePercent int32
	MinimumHistory  time.Duration
	// Startup behavior must be explicit: a period alone does not distinguish
	// exclusion from startup boosting in the source configuration.
	StartupMode string
}
type Result struct {
	Policy   *api.OptimizationPolicy
	Warnings []string
}

func Translate(data []byte, opts Options) (*Result, error) {
	var src Configuration
	if err := yaml.UnmarshalStrict(data, &src); err != nil {
		return nil, fmt.Errorf("unsupported or invalid workload migration configuration: %w", err)
	}
	if len(validation.IsDNS1123Label(opts.Namespace)) > 0 || len(validation.IsDNS1123Subdomain(opts.Name)) > 0 || opts.Namespace == "" || opts.Name == "" {
		return nil, fmt.Errorf("explicit valid namespace and policy name are required")
	}
	if opts.CoveragePercent < 0 || opts.CoveragePercent > 100 || opts.MinimumHistory < 0 {
		return nil, fmt.Errorf("invalid replacement history or coverage settings")
	}
	s := src.Vertical
	if s.Optimization != "on" && s.Optimization != "off" {
		return nil, fmt.Errorf("vertical.optimization must explicitly be on or off")
	}
	rollout := "immediate"
	if s.ApplyType == "deferred" {
		rollout = "onNextRestart"
	} else if s.ApplyType != "immediate" {
		return nil, fmt.Errorf("vertical.applyType must explicitly be immediate or deferred")
	}
	strategy := "webhook"
	p := &api.OptimizationPolicy{TypeMeta: metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: "OptimizationPolicy"}, ObjectMeta: metav1.ObjectMeta{Name: opts.Name, Namespace: opts.Namespace}, Spec: api.OptimizationPolicySpec{Mode: api.ModeRecommend, Selector: api.WorkloadSelector{Namespaces: &api.NamespaceFilter{Allow: []string{opts.Namespace}}}, MetricsConfig: api.MetricsConfig{Provider: "prometheus", MinCoveragePercent: opts.CoveragePercent, MinimumHistory: metav1.Duration{Duration: opts.MinimumHistory}}, UpdateStrategy: api.UpdateStrategy{Strategy: &strategy, RolloutStrategy: &rollout, AllowInPlaceResize: true, AllowRecreate: false, LimitConfig: &api.LimitConfig{}}, ResourcePolicy: &api.ResourcePolicy{ExcludedContainers: s.ExcludedContainers}}}
	warnings := []string{"Generated in Recommend mode. Review workload selectors and resource bounds before enabling Auto.", "Recreation remains disabled. Enable it explicitly after testing PDBs and rollout readiness."}
	if s.Optimization == "off" {
		p.Spec.Mode = api.ModeDisabled
	}
	if s.ApplyType == "deferred" {
		warnings = append(warnings, "Deferred maps to onNextRestart; OptiPod waits for a natural restart and does not resize running pods in this mode. Review the source apply behavior before migration.")
	}
	if err := translateResources(p, []Resource{s.CPU, s.Memory}); err != nil {
		return nil, err
	}
	if s.Startup.Period != "" {
		d, err := time.ParseDuration(s.Startup.Period)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("invalid startup period")
		}
		switch opts.StartupMode {
		case "ignore":
			p.Spec.MetricsConfig.StartupIgnore.Duration = d
		case "include":
		default:
			return nil, fmt.Errorf("declare startup-mode ignore or include; startup boosting cannot be translated to exclusion")
		}
	}
	if s.Confidence != nil {
		if s.Confidence.Threshold != nil && (*s.Confidence.Threshold < 0 || *s.Confidence.Threshold > 1) {
			return nil, fmt.Errorf("confidence threshold must be between zero and one")
		}
		warnings = append(warnings, "Source confidence has no exact OptiPod equivalent. The supplied minimumHistory and minCoveragePercent are independent data-quality gates, not a confidence conversion.")
	}
	if src.ScalingPolicyName != "" {
		warnings = append(warnings, "Scaling policy inheritance is not fetched: provide all effective CPU and memory settings explicitly.")
	}
	p.Annotations = map[string]string{"optipod.io/migration-source": "workload-configuration", "optipod.io/migration-review": strings.Join(warnings, " ")}
	if err := p.ValidateCreate(); err != nil {
		return nil, err
	}
	return &Result{Policy: p, Warnings: warnings}, nil
}

func translateResources(p *api.OptimizationPolicy, resources []Resource) error {
	for i, r := range resources {
		name := "cpu"
		if i == 1 {
			name = "memory"
		}
		target := strings.ToUpper(r.Target)
		if target == "MAX" {
			target = "P100"
		}
		switch target {
		case "P50", "P90", "P95", "P99", "P100":
		default:
			return fmt.Errorf("%s target %q has no exact supported percentile; choose an explicit target", name, r.Target)
		}
		window, err := time.ParseDuration(r.LookBackPeriod)
		if err != nil || window <= 0 || window > 30*24*time.Hour {
			return fmt.Errorf("%s requires an explicit valid lookBackPeriod", name)
		}
		if r.Overhead == nil || *r.Overhead < 0 || *r.Overhead > 9 {
			return fmt.Errorf("%s requires an explicit overhead between zero and nine", name)
		}
		factor := 1 + *r.Overhead
		settings := &api.ResourceMetricsConfig{Percentile: target, RollingWindow: metav1.Duration{Duration: window}, SafetyFactor: &factor}
		min, err := resource.ParseQuantity(r.Min)
		if err != nil {
			return fmt.Errorf("%s min: %w", name, err)
		}
		max, err := resource.ParseQuantity(r.Max)
		if err != nil {
			return fmt.Errorf("%s max: %w", name, err)
		}
		bound := api.ResourceBound{Min: min, Max: max}
		mode := ""
		switch r.Limit.Type {
		case "noLimit":
			mode = "None"
		case "keepLimits":
			mode = "Preserve"
		case "automatic":
			if i == 0 {
				return fmt.Errorf("automatic limits are supported only for memory")
			}
			mode = api.LimitAutomatic
		case "multiplier":
			mode = "Multiplier"
			if r.Limit.Multiplier == nil || *r.Limit.Multiplier < 1 || *r.Limit.Multiplier > 10 {
				return fmt.Errorf("%s requires a limit multiplier between one and ten", name)
			}
		default:
			return fmt.Errorf("%s limit type must explicitly be noLimit, keepLimits, multiplier or memory-only automatic", name)
		}
		if i == 0 {
			p.Spec.MetricsConfig.CPU = settings
			p.Spec.ResourceBounds.CPU = bound
			p.Spec.UpdateStrategy.LimitConfig.CPUMode = mode
			p.Spec.UpdateStrategy.LimitConfig.CPULimitMultiplier = r.Limit.Multiplier
		} else {
			p.Spec.MetricsConfig.Memory = settings
			p.Spec.ResourceBounds.Memory = bound
			p.Spec.UpdateStrategy.LimitConfig.MemoryMode = mode
			p.Spec.UpdateStrategy.LimitConfig.MemoryLimitMultiplier = r.Limit.Multiplier
		}
	}
	return nil
}
