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

package discovery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// These tests prove the production DiscoverWorkloads entry point against a
// real ephemeral API server: exact included and excluded workload identities
// for all three workload kinds, include/exclude precedence, omitted-filter
// compatibility, namespace and label selectors, deny-list precedence and
// identity changes after label edits (VAL-FAST-003). They run under -short
// because the approved fast gate provides the pinned envtest assets through
// KUBEBUILDER_ASSETS. The fake-client property tests in discovery_test.go
// remain unit-level harness checks and are not API-list proof.

var discoveryClient client.Client

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{}
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
	discoveryClient = c

	code := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to stop envtest:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// Fixture namespaces and workloads (created through the real API):
//
//	ns-prod  (team=platform): d-web Deployment(app=web), s-web StatefulSet(app=web),
//	                          ds-web DaemonSet(app=web), d-db Deployment(app=db)
//	ns-other (team=other):    d-other Deployment(app=web)
//	ns-denied(team=platform): d-denied Deployment(app=web)
const (
	fixNsProd   = "disc-prod-ns"
	fixNsOther  = "disc-other-ns"
	fixNsDenied = "disc-denied-ns"
)

// setupDiscoveryFixture creates the namespaces and workloads used by the
// subtests below through the real API.
func setupDiscoveryFixture(ctx context.Context, t *testing.T) {
	t.Helper()
	createNamespace(ctx, t, fixNsProd, map[string]string{"team": "platform"})
	createNamespace(ctx, t, fixNsOther, map[string]string{"team": "other"})
	createNamespace(ctx, t, fixNsDenied, map[string]string{"team": "platform"})

	createDeployment(ctx, t, "d-web", fixNsProd, map[string]string{"app": "web"})
	createStatefulSet(ctx, t, "s-web", fixNsProd, map[string]string{"app": "web"})
	createDaemonSet(ctx, t, "ds-web", fixNsProd, map[string]string{"app": "web"})
	createDeployment(ctx, t, "d-db", fixNsProd, map[string]string{"app": "db"})
	createDeployment(ctx, t, "d-other", fixNsOther, map[string]string{"app": "web"})
	createDeployment(ctx, t, "d-denied", fixNsDenied, map[string]string{"app": "web"})
}

func createNamespace(ctx context.Context, t *testing.T, name string, labels map[string]string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	if err := discoveryClient.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace %s: %v", name, err)
	}
}

func podTemplate(labels map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:1.21"}},
		},
	}
}

func createDeployment(ctx context.Context, t *testing.T, name, namespace string, labels map[string]string) {
	t.Helper()
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: podTemplate(labels),
		},
	}
	if err := discoveryClient.Create(ctx, d); err != nil {
		t.Fatalf("failed to create deployment %s/%s: %v", namespace, name, err)
	}
}

func createStatefulSet(ctx context.Context, t *testing.T, name, namespace string, labels map[string]string) {
	t.Helper()
	s := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template:    podTemplate(labels),
		},
	}
	if err := discoveryClient.Create(ctx, s); err != nil {
		t.Fatalf("failed to create statefulset %s/%s: %v", namespace, name, err)
	}
}

func createDaemonSet(ctx context.Context, t *testing.T, name, namespace string, labels map[string]string) {
	t.Helper()
	d := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: podTemplate(labels),
		},
	}
	if err := discoveryClient.Create(ctx, d); err != nil {
		t.Fatalf("failed to create daemonset %s/%s: %v", namespace, name, err)
	}
}

// discoverySelector builds a policy selector with the given knobs.
func discoverySelector(namespaceAllow []string, namespaceDeny []string, namespaceSelector *metav1.LabelSelector, workloadSelector *metav1.LabelSelector, workloadTypes *optipodv1alpha1.WorkloadTypeFilter) optipodv1alpha1.WorkloadSelector {
	selector := optipodv1alpha1.WorkloadSelector{}
	if len(namespaceAllow) > 0 || len(namespaceDeny) > 0 {
		selector.Namespaces = &optipodv1alpha1.NamespaceFilter{Allow: namespaceAllow, Deny: namespaceDeny}
	}
	selector.NamespaceSelector = namespaceSelector
	selector.WorkloadSelector = workloadSelector
	selector.WorkloadTypes = workloadTypes
	return selector
}

// discoveredIdentities runs the production discovery against the real API and
// returns sorted "Kind/namespace/name" identity strings.
func discoveredIdentities(ctx context.Context, t *testing.T, selector optipodv1alpha1.WorkloadSelector) []string {
	t.Helper()
	policy := &optipodv1alpha1.OptimizationPolicy{
		Spec: optipodv1alpha1.OptimizationPolicySpec{Selector: selector},
	}
	workloads, err := DiscoverWorkloads(ctx, discoveryClient, policy)
	if err != nil {
		t.Fatalf("DiscoverWorkloads failed: %v", err)
	}
	identities := make([]string, 0, len(workloads))
	for _, w := range workloads {
		identities = append(identities, fmt.Sprintf("%s/%s/%s", w.Kind, w.Namespace, w.Name))
	}
	sort.Strings(identities)
	return identities
}

func expectIdentities(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("discovered identities %v differ from expected %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discovered identities %v differ from expected %v", got, want)
		}
	}
}

// webAppSelector matches labels app=web.
func webAppSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
}

// TestDiscoverWorkloadsAgainstRealAPI covers three-kind discovery, label
// positives/negatives, namespace selectors, allow/deny precedence,
// include/exclude precedence, omitted-filter compatibility and identity
// changes after a real label edit.
func TestDiscoverWorkloadsAgainstRealAPI(t *testing.T) {
	ctx := context.Background()
	setupDiscoveryFixture(ctx, t)

	nsProdWeb := []string{
		"DaemonSet/" + fixNsProd + "/ds-web",
		"Deployment/" + fixNsProd + "/d-web",
		"StatefulSet/" + fixNsProd + "/s-web",
	}

	t.Run("discovers exactly the three matching kinds in the allowed namespace", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd}, nil, nil, webAppSelector(), nil)
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, nsProdWeb)
	})

	t.Run("nonmatching workload labels and unselected namespaces are excluded", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd, fixNsOther}, nil, nil, webAppSelector(), nil)
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, []string{
			"DaemonSet/" + fixNsProd + "/ds-web",
			"Deployment/" + fixNsOther + "/d-other",
			"Deployment/" + fixNsProd + "/d-web",
			"StatefulSet/" + fixNsProd + "/s-web",
		})
	})

	t.Run("include filter limits discovery to the included kinds", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd}, nil, nil, webAppSelector(),
			&optipodv1alpha1.WorkloadTypeFilter{Include: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment}})
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, []string{"Deployment/" + fixNsProd + "/d-web"})
	})

	t.Run("exclude takes precedence over include", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd}, nil, nil, webAppSelector(),
			&optipodv1alpha1.WorkloadTypeFilter{
				Include: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment, optipodv1alpha1.WorkloadTypeStatefulSet},
				Exclude: []optipodv1alpha1.WorkloadType{optipodv1alpha1.WorkloadTypeDeployment},
			})
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, []string{"StatefulSet/" + fixNsProd + "/s-web"})
	})

	t.Run("an empty workload-type filter keeps all kinds (backward compatibility)", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd}, nil, nil, webAppSelector(),
			&optipodv1alpha1.WorkloadTypeFilter{})
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, nsProdWeb)
	})

	t.Run("namespace label selector includes matching and excludes nonmatching namespaces", func(t *testing.T) {
		selector := discoverySelector(nil, nil,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}},
			webAppSelector(), nil)
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, []string{
			"DaemonSet/" + fixNsProd + "/ds-web",
			"Deployment/" + fixNsDenied + "/d-denied",
			"Deployment/" + fixNsProd + "/d-web",
			"StatefulSet/" + fixNsProd + "/s-web",
		})
	})

	t.Run("deny list takes precedence over allow list", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd, fixNsDenied}, []string{fixNsDenied}, nil, webAppSelector(), nil)
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, nsProdWeb)
	})

	t.Run("a nonmatching workload-selector yields an empty result", func(t *testing.T) {
		selector := discoverySelector([]string{fixNsProd}, nil, nil,
			&metav1.LabelSelector{MatchLabels: map[string]string{"app": "no-such-app"}}, nil)
		got := discoveredIdentities(ctx, t, selector)
		expectIdentities(t, got, []string{})
	})

	t.Run("identities change after a real workload label edit", func(t *testing.T) {
		dbAppSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}

		// Before the edit only d-db matches app=db.
		selector := discoverySelector([]string{fixNsProd}, nil, nil, dbAppSelector, nil)
		expectIdentities(t, discoveredIdentities(ctx, t, selector), []string{"Deployment/" + fixNsProd + "/d-db"})

		// Relabel d-db to app=web through the API.
		db := &appsv1.Deployment{}
		if err := discoveryClient.Get(ctx, types.NamespacedName{Name: "d-db", Namespace: fixNsProd}, db); err != nil {
			t.Fatalf("failed to fetch d-db for relabeling: %v", err)
		}
		db.Labels["app"] = "web"
		if err := discoveryClient.Update(ctx, db); err != nil {
			t.Fatalf("failed to relabel d-db: %v", err)
		}

		// The app=db selector no longer discovers any workload.
		expectIdentities(t, discoveredIdentities(ctx, t, selector), []string{})

		// The app=web selector now includes the relabeled deployment too.
		webSelector := discoverySelector([]string{fixNsProd}, nil, nil, webAppSelector(), nil)
		expectIdentities(t, discoveredIdentities(ctx, t, webSelector), []string{
			"DaemonSet/" + fixNsProd + "/ds-web",
			"Deployment/" + fixNsProd + "/d-db",
			"Deployment/" + fixNsProd + "/d-web",
			"StatefulSet/" + fixNsProd + "/s-web",
		})
	})
}
