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

package application

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/recommendation"
)

// This file proves VAL-FAST-005's request/limit consistency and
// updateRequestsOnly behavior against production output: exact limit
// arithmetic through CalculateLimitsWithDefaults, and the production Apply
// path (default webhook strategy) persisting request annotations only — or
// request plus limit annotations when limits are configured to update.

// TestCalculateLimitsWithDefaultsExactArithmetic pins the exact production
// limit arithmetic for every configuration shape (VAL-FAST-005): default
// multipliers, explicit overrides, partial configurations, zero requests and
// out-of-range multipliers.
func TestCalculateLimitsWithDefaultsExactArithmetic(t *testing.T) {
	fiveTenths := 0.5
	two := 2.0
	onePointOne := 1.1
	eleven := 11.0

	tests := []struct {
		name            string
		cpuRequest      string
		memoryRequest   string
		limitConfig     *optipodv1alpha1.LimitConfig
		wantCPULimit    int64 // milli, -1 = absent
		wantMemoryLimit int64 // bytes, -1 = absent
		wantErr         string
	}{
		{
			name:            "default multipliers apply 1.5x CPU and 1.3x memory",
			cpuRequest:      "250m",
			memoryRequest:   "250Mi",
			limitConfig:     nil,
			wantCPULimit:    375,       // 250m * 1.5
			wantMemoryLimit: 340787200, // 250Mi * 1.3 = 325Mi exactly
		},
		{
			name:          "explicit multipliers override the defaults",
			cpuRequest:    "250m",
			memoryRequest: "250Mi",
			limitConfig: &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier:    &two,
				MemoryLimitMultiplier: &onePointOne,
			},
			wantCPULimit:    500,       // 250m * 2.0
			wantMemoryLimit: 288358400, // 250Mi * 1.1 = 275Mi exactly
		},
		{
			name:          "partial configuration uses the explicit CPU multiplier and the default memory multiplier",
			cpuRequest:    "250m",
			memoryRequest: "250Mi",
			limitConfig: &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier: &two,
			},
			wantCPULimit:    500,
			wantMemoryLimit: 340787200, // default 1.3
		},
		{
			name:            "zero requests produce no limit entries",
			cpuRequest:      "0m",
			memoryRequest:   "0Mi",
			limitConfig:     nil,
			wantCPULimit:    -1,
			wantMemoryLimit: -1,
		},
		{
			name:          "CPU multiplier below the range is rejected",
			cpuRequest:    "250m",
			memoryRequest: "250Mi",
			limitConfig: &optipodv1alpha1.LimitConfig{
				CPULimitMultiplier: &fiveTenths,
			},
			wantErr: "CPU multiplier 0.50 is out of range [1.0, 10.0]",
		},
		{
			name:          "memory multiplier above the range is rejected",
			cpuRequest:    "250m",
			memoryRequest: "250Mi",
			limitConfig: &optipodv1alpha1.LimitConfig{
				MemoryLimitMultiplier: &eleven,
			},
			wantErr: "memory multiplier 11.00 is out of range [1.0, 10.0]",
		},
	}

	engine := &Engine{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(tt.cpuRequest),
				corev1.ResourceMemory: resource.MustParse(tt.memoryRequest),
			}
			limits, err := engine.CalculateLimitsWithDefaults(requests, tt.limitConfig)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CalculateLimitsWithDefaults returned an error: %v", err)
			}

			if tt.wantCPULimit == -1 {
				if limit, exists := limits[corev1.ResourceCPU]; exists {
					t.Errorf("expected no CPU limit entry, got %s", limit.String())
				}
			} else if limit, exists := limits[corev1.ResourceCPU]; !exists {
				t.Errorf("expected a CPU limit entry, got none")
			} else if got := limit.MilliValue(); got != tt.wantCPULimit {
				t.Errorf("CPU limit = %d milli, want %d milli", got, tt.wantCPULimit)
			}

			if tt.wantMemoryLimit == -1 {
				if limit, exists := limits[corev1.ResourceMemory]; exists {
					t.Errorf("expected no memory limit entry, got %s", limit.String())
				}
			} else if limit, exists := limits[corev1.ResourceMemory]; !exists {
				t.Errorf("expected a memory limit entry, got none")
			} else if got := limit.Value(); got != tt.wantMemoryLimit {
				t.Errorf("memory limit = %d bytes, want %d bytes", got, tt.wantMemoryLimit)
			}
		})
	}
}

// requestsOnlyDeployment builds a one-container Deployment carrying explicit
// requests and limits so a requests-only apply must leave the limits intact.
// The limits are deliberately above the recommended requests so raising the
// requests while keeping the limits unchanged remains valid for the API server.
func requestsOnlyDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

// toApplicationWorkload converts a typed Deployment into the production
// application.Workload input (same conversion the controller uses).
func toApplicationWorkload(t *testing.T, deployment *appsv1.Deployment) *Workload {
	t.Helper()

	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		t.Fatalf("failed to convert to unstructured: %v", err)
	}
	return &Workload{
		Kind:      "Deployment",
		Namespace: deployment.GetNamespace(),
		Name:      deployment.GetName(),
		Object:    &unstructured.Unstructured{Object: unstructuredObj},
	}
}

// fetchAppliedDeployment reads the workload back through the client.
func fetchAppliedDeployment(t *testing.T, k8sClient client.Client, name string) *appsv1.Deployment {
	t.Helper()

	fetched := &appsv1.Deployment{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, fetched); err != nil {
		t.Fatalf("failed to fetch deployment: %v", err)
	}
	return fetched
}

func applyTestEngine(k8sClient client.Client) *Engine {
	// The default webhook strategy never touches the dynamic client; the
	// discovery client is only read by CanApply's in-place detection, for
	// which a fixed 1.37 server version is sufficient.
	return NewEngine(k8sClient, nil, &mockDiscoveryClient{
		serverVersion: &version.Info{Major: "1", Minor: "37"},
	}, false)
}

func autoWebhookPolicy(updateRequestsOnly bool) *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "requests-only-policy", Namespace: "default"},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			// Strategy omitted: production default is webhook.
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				AllowInPlaceResize: true,
				UpdateRequestsOnly: updateRequestsOnly,
			},
		},
	}
}

func singleContainerApplySet() []ContainerRecommendation {
	return []ContainerRecommendation{
		{
			Container: "app",
			Recommendation: &recommendation.Recommendation{
				CPU:    resource.MustParse("250m"),
				Memory: resource.MustParse("250Mi"),
			},
		},
	}
}

// TestApplyUpdateRequestsOnlyWebhookOutcome proves the S059 outcome through
// the production Apply path under the default webhook strategy (VAL-FAST-005):
// with updateRequestsOnly=true only request annotations are stored for the pod
// mutator (limits are never written, so admitted pods keep their limits), and
// with updateRequestsOnly=false the stored limit annotations match the exact
// limit arithmetic. The workload's own template requests/limits are never
// edited by the webhook strategy (rollout happens on the next pod admission).
func TestApplyUpdateRequestsOnlyWebhookOutcome(t *testing.T) {
	ctx := context.Background()

	t.Run("updateRequestsOnly=true stores requests and never limit annotations", func(t *testing.T) {
		k8sClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(requestsOnlyDeployment("requests-only-deploy", "default")).Build()
		deployment := fetchAppliedDeployment(t, k8sClient, "requests-only-deploy")
		before := deployment.DeepCopy()

		engine := applyTestEngine(k8sClient)
		result, err := engine.Apply(ctx, toApplicationWorkload(t, deployment), singleContainerApplySet(), autoWebhookPolicy(true))
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}
		if result.Method != webhookMethod {
			t.Fatalf("apply method = %q, want the default webhook strategy", result.Method)
		}
		if result.FieldOwnership {
			t.Fatal("webhook strategy must not claim field ownership")
		}

		after := fetchAppliedDeployment(t, k8sClient, "requests-only-deploy")

		// Request annotations are stored with the exact recommended values.
		annotations := after.GetAnnotations()
		wantAnnotations := map[string]string{
			optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "250m",
			optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "250Mi",
			optipodv1alpha1.AnnotationWebhookEnabled:               "true",
			optipodv1alpha1.AnnotationStrategy:                     string(optipodv1alpha1.StrategyWebhook),
		}
		for key, want := range wantAnnotations {
			if got := annotations[key]; got != want {
				t.Errorf("annotation %q = %q, want %q", key, got, want)
			}
		}

		// No limit annotation may exist, so the pod mutator cannot touch limits.
		for _, key := range []string{
			optipodv1alpha1.AnnotationCPULimitPrefix + ".app",
			optipodv1alpha1.AnnotationMemoryLimitPrefix + ".app",
		} {
			if _, exists := annotations[key]; exists {
				t.Errorf("limit annotation %q must not be stored under updateRequestsOnly", key)
			}
		}

		// The webhook strategy never edits the workload template itself.
		container := after.Spec.Template.Spec.Containers[0]
		if cpu := container.Resources.Requests.Cpu().MilliValue(); cpu != before.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() {
			t.Errorf("template CPU request changed under webhook strategy: %d milli", cpu)
		}
		if mem := container.Resources.Requests.Memory().Value(); mem != before.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() {
			t.Errorf("template memory request changed under webhook strategy: %d bytes", mem)
		}
		if cpu := container.Resources.Limits.Cpu().MilliValue(); cpu != 500 {
			t.Errorf("template CPU limit = %d milli, want unchanged 500 milli", cpu)
		}
		if mem := container.Resources.Limits.Memory().Value(); mem != 512*1024*1024 {
			t.Errorf("template memory limit = %d bytes, want unchanged 512Mi", mem)
		}
	})

	t.Run("updateRequestsOnly=false stores request and limit annotations with exact arithmetic", func(t *testing.T) {
		k8sClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(requestsOnlyDeployment("requests-and-limits-deploy", "default")).Build()
		deployment := fetchAppliedDeployment(t, k8sClient, "requests-and-limits-deploy")

		engine := applyTestEngine(k8sClient)
		if _, err := engine.Apply(ctx, toApplicationWorkload(t, deployment), singleContainerApplySet(), autoWebhookPolicy(false)); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		annotations := fetchAppliedDeployment(t, k8sClient, "requests-and-limits-deploy").GetAnnotations()
		wantAnnotations := map[string]string{
			optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "250m",
			optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "250Mi",
			// Default multipliers: CPU 1.5x -> 375m, memory 1.3x -> 325Mi.
			optipodv1alpha1.AnnotationCPULimitPrefix + ".app":    "375m",
			optipodv1alpha1.AnnotationMemoryLimitPrefix + ".app": "325Mi",
		}
		for key, want := range wantAnnotations {
			if got := annotations[key]; got != want {
				t.Errorf("annotation %q = %q, want %q", key, got, want)
			}
		}
	})
}

// TestApplyMemoryDecreaseCharacterization pins the CURRENT production behavior
// for memory decreases in Auto mode so the gap between the API documentation
// and the engine is executable evidence rather than a doc claim.
//
// CONFLICT DISCLOSURE (VAL-FAST-005): the approved validation contract states
// "Unsafe memory decreases are blocked under the configured default" and
// "explicitly enabled gradual decrease produces bounded steps", and the API
// documents AllowUnsafeMemoryDecrease=false (the schema default) as blocking
// memory decreases while GradualDecreaseConfig bounds decrease steps. However
// commit 9dfc868 ("feat: implement memory safety enhancements") deliberately
// REMOVED the engine's isUnsafeMemoryDecrease guard, and no production code
// consumes AllowUnsafeMemoryDecrease or GradualDecreaseConfig today (only the
// deepcopy). The engine's own property test asserts the opposite ("applied
// regardless of memory decrease magnitude").
//
// This characterization asserts what production actually does: an Auto apply
// with a decreased memory recommendation is NOT blocked with the default
// AllowUnsafeMemoryDecrease=false, and the decrease is stored for the pod
// mutator. When decrease-safety behavior is (re-)authorized, replace this
// test with blocked-decrease and gradual-step assertions.
func TestApplyMemoryDecreaseCharacterization(t *testing.T) {
	ctx := context.Background()

	k8sClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(requestsOnlyDeployment("memory-decrease-deploy", "default")).Build()
	deployment := fetchAppliedDeployment(t, k8sClient, "memory-decrease-deploy")

	engine := applyTestEngine(k8sClient)

	// A decrease from the current 128Mi request down to 64Mi.
	decreased := []ContainerRecommendation{{
		Container: "app",
		Recommendation: &recommendation.Recommendation{
			CPU:    resource.MustParse("100m"),
			Memory: resource.MustParse("64Mi"),
		},
	}}
	policy := autoWebhookPolicy(true)
	// Explicitly leave AllowUnsafeMemoryDecrease nil: the API documents the
	// default (false) as blocking unsafe decreases.
	if policy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease != nil {
		t.Fatal("test setup must leave AllowUnsafeMemoryDecrease unset")
	}

	result, err := engine.Apply(ctx, toApplicationWorkload(t, deployment), decreased, policy)
	if err != nil {
		t.Fatalf("Apply failed (current production behavior must not error): %v", err)
	}
	if !result.FieldOwnership {
		// sanity: routed through the webhook strategy
		if result.Method != webhookMethod {
			t.Fatalf("apply method = %q, want webhook", result.Method)
		}
	}

	annotations := fetchAppliedDeployment(t, k8sClient, "memory-decrease-deploy").GetAnnotations()
	if got := annotations[optipodv1alpha1.AnnotationMemoryRequestPrefix+".app"]; got != "64Mi" {
		t.Errorf("decreased memory request annotation = %q, want the applied 64Mi under current unguarded behavior", got)
	}

	// The decision path has no memory-safety dimension at all.
	decision, err := engine.CanApply(ctx, toApplicationWorkload(t, deployment), "app", decreased[0].Recommendation, policy)
	if err != nil {
		t.Fatalf("CanApply returned an error: %v", err)
	}
	if !decision.CanApply {
		t.Fatalf("CanApply = false with reason %q; current production has no memory-decrease guard", decision.Reason)
	}
	lowerReason := strings.ToLower(decision.Reason)
	for _, forbidden := range []string{"memory", "safety", "unsafe", "decrease", "oom", "eviction"} {
		if strings.Contains(lowerReason, forbidden) {
			t.Errorf("decision reason %q unexpectedly mentions %q", decision.Reason, forbidden)
		}
	}
}
