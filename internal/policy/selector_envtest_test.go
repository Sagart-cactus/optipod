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

package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
)

// These tests prove the production SelectBestPolicy/GetMatchingPolicies entry
// points against a real ephemeral API server with the checked-in CRD: highest
// supported weight, the API-persisted omitted-weight default, deterministic
// tie resolution by name, Disabled and nonmatching exclusion, namespace
// isolation of selection, and the absence of unsupported selector-specificity
// precedence (VAL-FAST-004). They run under -short because the approved fast
// gate provides the pinned envtest assets through KUBEBUILDER_ASSETS. The
// fake-client property tests in selector_test.go remain unit-level harness
// checks and are not API-list proof.

var selectionClient client.Client

func TestMain(m *testing.M) {
	if err := optipodv1alpha1.AddToScheme(clientgoscheme.Scheme); err != nil {
		fmt.Fprintln(os.Stderr, "failed to register OptimizationPolicy scheme:", err)
		os.Exit(1)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start envtest (KUBEBUILDER_ASSETS must point at the pinned binaries):", err)
		os.Exit(1)
	}

	c, err := client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create envtest client:", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}
	selectionClient = c

	code := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to stop envtest:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// createSelectionNamespace creates a namespace with a Deployment labelled
// app=web that can be used as a selection target.
func createSelectionNamespace(ctx context.Context, t *testing.T, namespace string) *discovery.Workload {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if err := selectionClient.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace %s: %v", namespace, err)
	}

	labels := map[string]string{"app": "web"}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.21"}}},
			},
		},
	}
	if err := selectionClient.Create(ctx, deployment); err != nil {
		t.Fatalf("failed to create deployment %s/web: %v", namespace, err)
	}
	return workloadFromDeployment(deployment)
}

func workloadFromDeployment(d *appsv1.Deployment) *discovery.Workload {
	return &discovery.Workload{
		Kind:      "Deployment",
		Namespace: d.Namespace,
		Name:      d.Name,
		Labels:    d.Labels,
		Object:    d,
	}
}

// createSelectionPolicy persists a schema- and runtime-valid policy through the
// API. Every policy gets an allow-list so selection stays isolated per case;
// extra selector/mode knobs come from mutate. Code that needs the API-fetched
// object calls fetchSelectionPolicy explicitly.
func createSelectionPolicy(ctx context.Context, t *testing.T, name string, mutate func(*optipodv1alpha1.OptimizationPolicy)) {
	t.Helper()
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeRecommend,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{},
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{Provider: "prometheus"},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU:    optipodv1alpha1.ResourceBound{Min: resource.MustParse("50m"), Max: resource.MustParse("2000m")},
				Memory: optipodv1alpha1.ResourceBound{Min: resource.MustParse("64Mi"), Max: resource.MustParse("4Gi")},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{},
		},
	}
	if mutate != nil {
		mutate(policy)
	}
	if err := selectionClient.Create(ctx, policy); err != nil {
		t.Fatalf("failed to create policy %s: %v", name, err)
	}
}

// fetchSelectionPolicy reads a persisted policy back from the API.
func fetchSelectionPolicy(ctx context.Context, t *testing.T, name string) *optipodv1alpha1.OptimizationPolicy {
	t.Helper()
	policy := &optipodv1alpha1.OptimizationPolicy{}
	if err := selectionClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, policy); err != nil {
		t.Fatalf("failed to fetch policy %s from the API: %v", name, err)
	}
	return policy
}

func selectBest(ctx context.Context, t *testing.T, workload *discovery.Workload) (*optipodv1alpha1.OptimizationPolicy, error) {
	t.Helper()
	selector := NewPolicySelector(selectionClient)
	return selector.SelectBestPolicy(ctx, workload)
}

func matchingNames(ctx context.Context, t *testing.T, workload *discovery.Workload) []string {
	t.Helper()
	selector := NewPolicySelector(selectionClient)
	matches, err := selector.GetMatchingPolicies(ctx, workload)
	if err != nil {
		t.Fatalf("GetMatchingPolicies failed: %v", err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.Policy.Name)
	}
	return names
}

func requireWinner(t *testing.T, winner *optipodv1alpha1.OptimizationPolicy, err error, wantName string) {
	t.Helper()
	if err != nil {
		t.Fatalf("SelectBestPolicy failed: %v", err)
	}
	if winner.Name != wantName {
		t.Fatalf("SelectBestPolicy picked %q, expected %q", winner.Name, wantName)
	}
}

func weightPtr(v int32) *int32 { return &v }

// TestSelectBestPolicyAgainstRealAPI covers the supported selection rules
// with policies persisted through the real API.
func TestSelectBestPolicyAgainstRealAPI(t *testing.T) {
	ctx := context.Background()

	t.Run("the highest supported weight wins", func(t *testing.T) {
		ns := "sel-hw-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		createSelectionPolicy(ctx, t, "p-hw-low", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(10)
		})
		createSelectionPolicy(ctx, t, "p-hw-high", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(500)
		})

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-hw-high")

		if got := matchingNames(ctx, t, workload); len(got) != 2 || got[0] != "p-hw-high" || got[1] != "p-hw-low" {
			t.Errorf("GetMatchingPolicies order = %v, expected [p-hw-high p-hw-low]", got)
		}
	})

	t.Run("an omitted weight defaults to 100 through the API and participates in selection", func(t *testing.T) {
		ns := "sel-ow-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		createSelectionPolicy(ctx, t, "p-ow-explicit-50", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(50)
		})
		createSelectionPolicy(ctx, t, "p-ow-default", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			// Weight deliberately omitted: the API persists the schema default.
		})

		persisted := fetchSelectionPolicy(ctx, t, "p-ow-default")
		if persisted.Spec.Weight == nil || *persisted.Spec.Weight != 100 {
			t.Fatalf("API-persisted weight = %v, expected the schema default 100", persisted.Spec.Weight)
		}

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-ow-default")
		if winner.GetWeight() != 100 {
			t.Errorf("winner GetWeight() = %d, expected the API-persisted default 100", winner.GetWeight())
		}
	})

	t.Run("equal weights resolve deterministically by name", func(t *testing.T) {
		ns := "sel-tie-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		createSelectionPolicy(ctx, t, "p-tie-zzz", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
		})
		createSelectionPolicy(ctx, t, "p-tie-aaa", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
		})

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-tie-aaa")

		if got := matchingNames(ctx, t, workload); len(got) != 2 || got[0] != "p-tie-aaa" || got[1] != "p-tie-zzz" {
			t.Errorf("GetMatchingPolicies order = %v, expected [p-tie-aaa p-tie-zzz]", got)
		}
	})

	t.Run("a disabled policy is excluded even with the highest weight", func(t *testing.T) {
		ns := "sel-dis-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		createSelectionPolicy(ctx, t, "p-off", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(900)
			p.Spec.Mode = optipodv1alpha1.ModeDisabled
		})
		createSelectionPolicy(ctx, t, "p-on", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(10)
		})

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-on")

		if got := matchingNames(ctx, t, workload); len(got) != 1 || got[0] != "p-on" {
			t.Errorf("GetMatchingPolicies = %v, expected the disabled policy to be excluded", got)
		}
	})

	t.Run("a nonmatching selector excludes the policy regardless of weight", func(t *testing.T) {
		ns := "sel-nm-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		createSelectionPolicy(ctx, t, "p-nomatch", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(999)
			p.Spec.Selector.WorkloadSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "no-such-app"},
			}
		})
		createSelectionPolicy(ctx, t, "p-match", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Weight = weightPtr(10)
		})

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-match")
		if got := matchingNames(ctx, t, workload); len(got) != 1 || got[0] != "p-match" {
			t.Errorf("GetMatchingPolicies = %v, expected only the matching policy", got)
		}
	})

	t.Run("selection is isolated per workload namespace", func(t *testing.T) {
		workloadOne := createSelectionNamespace(ctx, t, "sel-iso-one-ns")
		workloadTwo := createSelectionNamespace(ctx, t, "sel-iso-two-ns")
		createSelectionPolicy(ctx, t, "p-iso-one", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{"sel-iso-one-ns"}
			p.Spec.Weight = weightPtr(10)
		})
		createSelectionPolicy(ctx, t, "p-iso-two", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{"sel-iso-two-ns"}
			p.Spec.Weight = weightPtr(20)
		})

		winnerOne, err := selectBest(ctx, t, workloadOne)
		requireWinner(t, winnerOne, err, "p-iso-one")
		winnerTwo, err := selectBest(ctx, t, workloadTwo)
		requireWinner(t, winnerTwo, err, "p-iso-two")
	})

	t.Run("selector specificity does not override the weight/name rule", func(t *testing.T) {
		ns := "sel-spec-ns"
		workload := createSelectionNamespace(ctx, t, ns)
		// A more specific selector loses to an equally weighted, wider policy
		// whose name sorts first: only weight and name are precedence inputs.
		createSelectionPolicy(ctx, t, "p-z-specific", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
			p.Spec.Selector.WorkloadSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			}
		})
		createSelectionPolicy(ctx, t, "p-a-wide", func(p *optipodv1alpha1.OptimizationPolicy) {
			p.Spec.Selector.Namespaces.Allow = []string{ns}
		})

		winner, err := selectBest(ctx, t, workload)
		requireWinner(t, winner, err, "p-a-wide")
	})

	t.Run("a workload with no matching policies yields an explicit error", func(t *testing.T) {
		workload := createSelectionNamespace(ctx, t, "sel-none-ns")

		_, err := selectBest(ctx, t, workload)
		if err == nil {
			t.Fatalf("SelectBestPolicy succeeded, expected an explicit no-match error")
		}
		if !strings.Contains(err.Error(), "no matching policies found") {
			t.Errorf("SelectBestPolicy error %q does not identify the no-match condition", err.Error())
		}
	})
}
