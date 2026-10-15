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

package webhook

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// These tests replace the independent clusterless webhook simulator assertions
// (inventory S142-S150) with deterministic checks against the PRODUCTION
// mutator (internal/webhook/mutator.go): exact decoded admission patch
// paths/quantities for every container, no patches for unmatched containers,
// and safe handling of partial or invalid recommendation annotations
// (VAL-FAST-006). Each case goes through MutatePod, the production admission
// entry point, with a fake client holding the matching policy.

// mutatorTestPolicy returns a matching webhook-strategy Auto policy for the
// given namespace.
func mutatorTestPolicy(namespace string) *optipodv1alpha1.OptimizationPolicy {
	return &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mutator-test-policy",
			Namespace: "default",
		},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{
					Allow: []string{namespace},
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				// Strategy omitted: production default is webhook.
				AllowInPlaceResize: true,
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("10m"),
					Max: resource.MustParse("2"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("16Mi"),
					Max: resource.MustParse("8Gi"),
				},
			},
		},
	}
}

func newMutatorTestPod(name, namespace string, annotations map[string]string, containers []corev1.Container) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{Containers: containers},
	}
}

// decodePatchDocument marshals the response patches the way a real admission
// response would carry them and returns the decoded JSON array.
func decodePatchDocument(t *testing.T, response *AdmissionResponse) []map[string]interface{} {
	t.Helper()

	if response.Patches == nil {
		return nil
	}
	raw, err := json.Marshal(response.Patches)
	if err != nil {
		t.Fatalf("failed to marshal patches: %v", err)
	}
	var decoded []map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("failed to decode the admission patch document: %v", err)
	}
	return decoded
}

// assertPatchDocument compares the response's decoded admission patch document
// with the exact expected JSON (both normalized through encode/decode so the
// comparison is structural, not formatting-based).
func assertPatchDocument(t *testing.T, response *AdmissionResponse, wantJSON string) {
	t.Helper()

	var want interface{}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("test bug: expected document is not valid JSON: %v", err)
	}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("test bug: failed to re-marshal expected document: %v", err)
	}

	gotRaw, err := json.Marshal(response.Patches)
	if err != nil {
		t.Fatalf("failed to marshal patches: %v", err)
	}
	var got interface{}
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("patch document is not valid JSON: %v", err)
	}
	gotNormalized, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("failed to re-marshal patch document: %v", err)
	}

	if string(gotNormalized) != string(wantRaw) {
		t.Errorf("admission patch document mismatch:\ngot:  %s\nwant: %s", gotNormalized, wantRaw)
	}
}

func TestMutatePodExactPatchPathsAndQuantities(t *testing.T) {
	namespace := "mutator-patches-ns"
	k8sClient := createFakeClient(mutatorTestPolicy(namespace))
	mutator := NewMutator(k8sClient, nil)

	// Two containers with existing requests/limits and complete annotations
	// for both (frontend also carries limit annotations; sidecar only
	// requests, proving partial limit data per container is honored).
	pod := newMutatorTestPod("multi-pod", namespace, map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled:                    "true",
		optipodv1alpha1.AnnotationStrategy:                          string(optipodv1alpha1.StrategyWebhook),
		optipodv1alpha1.AnnotationCPURequestPrefix + ".frontend":    "200m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".frontend": "256Mi",
		optipodv1alpha1.AnnotationCPULimitPrefix + ".frontend":      "300m",
		optipodv1alpha1.AnnotationMemoryLimitPrefix + ".frontend":   "384Mi",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".sidecar":     "50m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".sidecar":  "64Mi",
	}, []corev1.Container{
		{
			Name: "frontend",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
		{
			Name: "sidecar",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("25m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
			},
		},
	})

	response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

	if !response.Allowed {
		t.Fatalf("admission must be allowed, message: %q", response.Message)
	}
	assertPatchDocument(t, response, `[
		{"op":"add","path":"/spec/containers/0/resources/requests/cpu","value":"200m"},
		{"op":"add","path":"/spec/containers/0/resources/requests/memory","value":"256Mi"},
		{"op":"add","path":"/spec/containers/0/resources/limits/cpu","value":"300m"},
		{"op":"add","path":"/spec/containers/0/resources/limits/memory","value":"384Mi"},
		{"op":"add","path":"/spec/containers/1/resources/requests/cpu","value":"50m"},
		{"op":"add","path":"/spec/containers/1/resources/requests/memory","value":"64Mi"}
	]`)
	if response.Message != "Applied 6 resource patches using policy mutator-test-policy" {
		t.Errorf("unexpected response message: %q", response.Message)
	}
}

func TestMutatePodAddsRequestsMapForBareContainers(t *testing.T) {
	namespace := "mutator-bare-ns"
	k8sClient := createFakeClient(mutatorTestPolicy(namespace))
	mutator := NewMutator(k8sClient, nil)

	// A bare container with no resources at all must receive the requests
	// map first, then the exact quantities.
	pod := newMutatorTestPod("bare-pod", namespace, map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled:               "true",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "250m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "256Mi",
	}, []corev1.Container{{Name: "app"}})

	response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

	if !response.Allowed {
		t.Fatalf("admission must be allowed, message: %q", response.Message)
	}
	assertPatchDocument(t, response, `[
		{"op":"add","path":"/spec/containers/0/resources/requests","value":{}},
		{"op":"add","path":"/spec/containers/0/resources/requests/cpu","value":"250m"},
		{"op":"add","path":"/spec/containers/0/resources/requests/memory","value":"256Mi"}
	]`)
}

// TestMutatePodDryRunProducesIdenticalPatch pins the production mutator's
// dry-run semantics (inventory S150): the mutator is side-effect-free (it
// only computes the admission patch and never writes cluster state), so a
// dry-run request must produce the exact same patch document as a live one
// and must leave the submitted pod untouched.
func TestMutatePodDryRunProducesIdenticalPatch(t *testing.T) {
	namespace := "mutator-dryrun-ns"
	k8sClient := createFakeClient(mutatorTestPolicy(namespace))
	mutator := NewMutator(k8sClient, nil)

	annotations := map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled:               "true",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "250m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "256Mi",
	}
	containers := []corev1.Container{{
		Name: "app",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}}

	live := mutator.MutatePod(&AdmissionRequest{Pod: newMutatorTestPod("live-pod", namespace, annotations, containers)})
	dryRun := mutator.MutatePod(&AdmissionRequest{
		Pod:    newMutatorTestPod("dry-run-pod", namespace, annotations, containers),
		DryRun: true,
	})

	if !live.Allowed || !dryRun.Allowed {
		t.Fatalf("both live and dry-run admissions must be allowed, messages: %q / %q", live.Message, dryRun.Message)
	}

	liveDoc, err := json.Marshal(live.Patches)
	if err != nil {
		t.Fatalf("failed to marshal live patches: %v", err)
	}
	dryRunDoc, err := json.Marshal(dryRun.Patches)
	if err != nil {
		t.Fatalf("failed to marshal dry-run patches: %v", err)
	}
	if string(liveDoc) != string(dryRunDoc) {
		t.Errorf("dry-run patch document must be identical to the live patch document:\nlive:   %s\ndryRun: %s", liveDoc, dryRunDoc)
	}
	if dryRun.Message != live.Message {
		t.Errorf("dry-run response message must match the live one: %q vs %q", dryRun.Message, live.Message)
	}

	// The submitted pod object itself must never be mutated in place.
	submitted := newMutatorTestPod("submitted-pod", namespace, annotations, containers)
	_ = mutator.MutatePod(&AdmissionRequest{Pod: submitted, DryRun: true})
	if cpu := submitted.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]; !cpu.Equal(resource.MustParse("100m")) {
		t.Errorf("the mutator must not modify the submitted pod in place, cpu request became %s", cpu.String())
	}
}

func TestMutatePodLeavesUnmatchedContainersUnchanged(t *testing.T) {
	namespace := "mutator-unmatched-ns"
	k8sClient := createFakeClient(mutatorTestPolicy(namespace))
	mutator := NewMutator(k8sClient, nil)

	// Only "app" carries recommendations; the sidecar and proxy containers
	// must not be touched at all.
	pod := newMutatorTestPod("unmatched-pod", namespace, map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled:               "true",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "300m",
		optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "512Mi",
		optipodv1alpha1.AnnotationCPULimitPrefix + ".app":      "450m",
		optipodv1alpha1.AnnotationMemoryLimitPrefix + ".app":   "640Mi",
	}, []corev1.Container{
		{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
		{Name: "sidecar", Image: "envoy:1.30"},
		{Name: "proxy", Image: "istio-proxy:1.22"},
	})

	response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

	if !response.Allowed {
		t.Fatalf("admission must be allowed, message: %q", response.Message)
	}
	document := decodePatchDocument(t, response)
	if len(document) != 4 {
		t.Fatalf("expected exactly 4 patches for the matched container, got %d: %s", len(document), mustReMarshal(t, document))
	}
	for i, patch := range document {
		path, _ := patch["path"].(string)
		if path == "" {
			t.Fatalf("patch %d has no path: %v", i, patch)
		}
		if !strings.HasPrefix(path, "/spec/containers/0/") {
			t.Errorf("patch %d path %q must only target the matched container at index 0", i, path)
		}
	}
}

func TestMutatePodPartialAnnotationsApplyOnlyPresentKeys(t *testing.T) {
	namespace := "mutator-partial-ns"
	k8sClient := createFakeClient(mutatorTestPolicy(namespace))
	mutator := NewMutator(k8sClient, nil)

	// Only a CPU request annotation exists: exactly one CPU patch must be
	// produced and the memory request must be left alone.
	pod := newMutatorTestPod("partial-pod", namespace, map[string]string{
		optipodv1alpha1.AnnotationWebhookEnabled:            "true",
		optipodv1alpha1.AnnotationCPURequestPrefix + ".app": "250m",
	}, []corev1.Container{
		{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	})

	response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

	if !response.Allowed {
		t.Fatalf("admission must be allowed, message: %q", response.Message)
	}
	assertPatchDocument(t, response, `[
		{"op":"add","path":"/spec/containers/0/resources/requests/cpu","value":"250m"}
	]`)
}

func TestMutatePodInvalidAnnotationValuesAreSkippedSafely(t *testing.T) {
	namespace := "mutator-invalid-ns"

	t.Run("invalid quantity annotations are skipped and valid ones still apply", func(t *testing.T) {
		k8sClient := createFakeClient(mutatorTestPolicy(namespace))
		mutator := NewMutator(k8sClient, nil)

		pod := newMutatorTestPod("invalid-pod", namespace, map[string]string{
			optipodv1alpha1.AnnotationWebhookEnabled:               "true",
			optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "eight-cores", // invalid quantity
			optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "256Mi",
		}, []corev1.Container{
			{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		})

		response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

		if !response.Allowed {
			t.Fatalf("an invalid annotation must never reject admission, message: %q", response.Message)
		}
		assertPatchDocument(t, response, `[
			{"op":"add","path":"/spec/containers/0/resources/requests/memory","value":"256Mi"}
		]`)
	})

	t.Run("only invalid annotations yield no patches but still admit the pod", func(t *testing.T) {
		k8sClient := createFakeClient(mutatorTestPolicy(namespace))
		mutator := NewMutator(k8sClient, nil)

		pod := newMutatorTestPod("all-invalid-pod", namespace, map[string]string{
			optipodv1alpha1.AnnotationWebhookEnabled:               "true",
			optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "not-a-quantity",
			optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "also not a quantity",
		}, []corev1.Container{{Name: "app"}})

		response := mutator.MutatePod(&AdmissionRequest{Pod: pod})

		if !response.Allowed {
			t.Fatalf("admission must be allowed, message: %q", response.Message)
		}
		if len(response.Patches) != 0 {
			t.Errorf("expected no patches for fully invalid annotations, got %d", len(response.Patches))
		}
		if response.Message != "No resource recommendations found" {
			t.Errorf("unexpected message: %q", response.Message)
		}
	})
}

func TestMutatePodNoPatchCases(t *testing.T) {
	namespace := "mutator-nopatch-ns"

	newPod := func(annotations map[string]string) *corev1.Pod {
		return newMutatorTestPod("nopatch-pod", namespace, annotations, []corev1.Container{
			{Name: "app", Image: "nginx:1.21"},
		})
	}

	t.Run("pod without the webhook-enabling annotations is admitted unmutated", func(t *testing.T) {
		mutator := NewMutator(createFakeClient(mutatorTestPolicy(namespace)), nil)
		response := mutator.MutatePod(&AdmissionRequest{Pod: newPod(nil)})
		if !response.Allowed {
			t.Fatalf("admission must be allowed, message: %q", response.Message)
		}
		if len(response.Patches) != 0 {
			t.Errorf("expected no patches, got %d", len(response.Patches))
		}
		if response.Message != "Webhook strategy not enabled" {
			t.Errorf("unexpected message: %q", response.Message)
		}
	})

	t.Run("eligible pod without recommendation annotations is admitted unmutated", func(t *testing.T) {
		mutator := NewMutator(createFakeClient(mutatorTestPolicy(namespace)), nil)
		response := mutator.MutatePod(&AdmissionRequest{Pod: newPod(map[string]string{
			optipodv1alpha1.AnnotationWebhookEnabled: "true",
		})})
		if !response.Allowed {
			t.Fatalf("admission must be allowed, message: %q", response.Message)
		}
		if len(response.Patches) != 0 {
			t.Errorf("expected no patches, got %d", len(response.Patches))
		}
		if response.Message != "No resource recommendations found" {
			t.Errorf("unexpected message: %q", response.Message)
		}
	})

	t.Run("pod in a namespace the policy does not select is admitted unmutated", func(t *testing.T) {
		otherNamespace := "mutator-other-ns"
		mutator := NewMutator(createFakeClient(mutatorTestPolicy(otherNamespace)), nil)

		pod := newMutatorTestPod("other-ns-pod", namespace, map[string]string{
			optipodv1alpha1.AnnotationWebhookEnabled:               "true",
			optipodv1alpha1.AnnotationCPURequestPrefix + ".app":    "250m",
			optipodv1alpha1.AnnotationMemoryRequestPrefix + ".app": "256Mi",
		}, []corev1.Container{{Name: "app"}})

		response := mutator.MutatePod(&AdmissionRequest{Pod: pod})
		if !response.Allowed {
			t.Fatalf("admission must be allowed, message: %q", response.Message)
		}
		if len(response.Patches) != 0 {
			t.Errorf("expected no patches, got %d", len(response.Patches))
		}
		if response.Message != "No matching policies found" {
			t.Errorf("unexpected message: %q", response.Message)
		}
	})
}

func mustReMarshal(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return string(raw)
}
