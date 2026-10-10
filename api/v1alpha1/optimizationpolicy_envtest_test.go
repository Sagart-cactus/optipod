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

package v1alpha1

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// These tests run a real ephemeral API server with the checked-in CRD so the
// CRD schema, server-side defaulting, the status subresource and list
// semantics are observed through the API instead of Go structs (VAL-FAST-001).
// They run under -short because the approved fast gate provides the pinned
// envtest assets through KUBEBUILDER_ASSETS.

var envtestClient client.Client

func TestMain(m *testing.M) {
	if err := AddToScheme(clientgoscheme.Scheme); err != nil {
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
	envtestClient = c

	code := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to stop envtest:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// minimalValidSpec is a schema- and runtime-valid policy spec expressed as raw
// JSON so omitted optional fields stay absent from the request and the
// server-side schema defaults can be observed on read-back.
func minimalValidSpec(namespace string) map[string]interface{} {
	return map[string]interface{}{
		"mode": "Auto",
		"selector": map[string]interface{}{
			"namespaces": map[string]interface{}{
				"allow": []interface{}{namespace},
			},
		},
		"metricsConfig": map[string]interface{}{
			"provider": "prometheus",
		},
		"resourceBounds": map[string]interface{}{
			"cpu":    map[string]interface{}{"min": "100m", "max": "4000m"},
			"memory": map[string]interface{}{"min": "128Mi", "max": "8Gi"},
		},
		"updateStrategy": map[string]interface{}{},
	}
}

// fetchPolicy reads the policy back from the "default" namespace through the
// typed API client.
func fetchPolicy(ctx context.Context, t *testing.T, name string) *OptimizationPolicy {
	t.Helper()
	policy := &OptimizationPolicy{}
	if err := envtestClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, policy); err != nil {
		t.Fatalf("failed to get policy default/%s from the API: %v", name, err)
	}
	return policy
}

// requireSchemaInvalid asserts the API rejected the request as Invalid with an
// actionable cause naming the offending field.
func requireSchemaInvalid(t *testing.T, err error, wantField, wantFragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected the API to reject the request with an Invalid error, got nil")
	}
	if !apierrors.IsInvalid(err) {
		t.Fatalf("expected an Invalid API error, got %v", err)
	}
	statusErr, ok := err.(*apierrors.StatusError)
	if !ok {
		t.Fatalf("expected *apierrors.StatusError, got %T", err)
	}
	details := statusErr.Status().Details
	if details == nil {
		t.Fatalf("Invalid error %q has no details/cause", err.Error())
	}
	for _, cause := range details.Causes {
		if cause.Field == wantField {
			if wantFragment != "" && !strings.Contains(cause.Message, wantFragment) && !strings.Contains(err.Error(), wantFragment) {
				t.Errorf("Invalid cause for %s = %q does not mention %q", wantField, cause.Message, wantFragment)
			}
			return
		}
	}
	t.Errorf("Invalid error %q has no cause for field %q (causes: %+v)", err.Error(), wantField, details.Causes)
}

// TestEnvtestCRDSchemaRejectsInvalidValues proves malformed schema values are
// rejected by the real CRD schema with an actionable API cause, on create and
// on update, and that a rejected update leaves the stored object unchanged.
func TestEnvtestCRDSchemaRejectsInvalidValues(t *testing.T) {
	ctx := context.Background()

	t.Run("create with an unsupported mode is rejected with a spec.mode cause", func(t *testing.T) {
		policy := validValidationPolicy()
		policy.Name = "schema-bad-mode"
		policy.Spec.Mode = PolicyMode("bogus")
		err := envtestClient.Create(ctx, policy)
		requireSchemaInvalid(t, err, "spec.mode", "Auto")
	})

	t.Run("create with a zero weight is rejected by the schema minimum", func(t *testing.T) {
		policy := validValidationPolicy()
		policy.Name = "schema-zero-weight"
		zero := int32(0)
		policy.Spec.Weight = &zero
		err := envtestClient.Create(ctx, policy)
		requireSchemaInvalid(t, err, "spec.weight", "")
	})

	t.Run("create with an unsupported metrics provider is rejected", func(t *testing.T) {
		policy := validValidationPolicy()
		policy.Name = "schema-bad-provider"
		policy.Spec.MetricsConfig.Provider = "graphite"
		err := envtestClient.Create(ctx, policy)
		requireSchemaInvalid(t, err, "spec.metricsConfig.provider", "prometheus")
	})

	t.Run("create with a malformed quantity is rejected by the schema pattern", func(t *testing.T) {
		spec := minimalValidSpec("default")
		spec["resourceBounds"].(map[string]interface{})["cpu"].(map[string]interface{})["min"] = "not-a-quantity"

		u := &unstructured.Unstructured{}
		u.SetAPIVersion("optipod.optipod.io/v1alpha1")
		u.SetKind("OptimizationPolicy")
		u.SetName("schema-bad-quantity")
		u.SetNamespace("default")
		u.Object["spec"] = spec
		requireSchemaInvalid(t, envtestClient.Create(ctx, u), "spec.resourceBounds.cpu.min", "")
	})

	t.Run("update to an unsupported mode is rejected and the stored spec is unchanged", func(t *testing.T) {
		policy := validValidationPolicy()
		policy.Name = "schema-update-guard"
		if err := envtestClient.Create(ctx, policy); err != nil {
			t.Fatalf("create: %v", err)
		}

		stored := fetchPolicy(ctx, t, policy.Name)
		stored.Spec.Mode = PolicyMode("bogus")
		requireSchemaInvalid(t, envtestClient.Update(ctx, stored), "spec.mode", "")

		after := fetchPolicy(ctx, t, policy.Name)
		if after.Spec.Mode != ModeAuto {
			t.Errorf("rejected update leaked into the stored object: mode = %q", after.Spec.Mode)
		}
	})
}

// TestEnvtestSchemaAdmittedButRuntimeInvalid proves the boundary between CRD
// schema validation and the runtime policy validator: inverted CPU bounds pass
// the schema, persist through the API, and are rejected by ValidateCreate on
// the API-fetched object. The reconciler's handling of the same object (Ready
// condition and no-mutation) is proven in internal/controller.
func TestEnvtestSchemaAdmittedButRuntimeInvalid(t *testing.T) {
	ctx := context.Background()

	policy := validValidationPolicy()
	policy.Name = "runtime-invalid-bounds"
	policy.Spec.ResourceBounds.CPU.Min = resource.MustParse("4000m")
	policy.Spec.ResourceBounds.CPU.Max = resource.MustParse("100m")

	if err := envtestClient.Create(ctx, policy); err != nil {
		t.Fatalf("the schema must admit the runtime-invalid policy, got create error %v", err)
	}

	stored := fetchPolicy(ctx, t, policy.Name)
	if stored.UID == "" {
		t.Fatalf("the API-admitted policy has no UID; persistence was not observed")
	}
	if stored.Spec.ResourceBounds.CPU.Min.MilliValue() != 4000 || stored.Spec.ResourceBounds.CPU.Max.MilliValue() != 100 {
		t.Fatalf("persisted bounds %s..%s differ from the submitted spec", stored.Spec.ResourceBounds.CPU.Min.String(), stored.Spec.ResourceBounds.CPU.Max.String())
	}

	validateErr := stored.ValidateCreate()
	if validateErr == nil {
		t.Fatalf("ValidateCreate accepted the schema-admitted but runtime-invalid policy")
	}
	if !strings.Contains(validateErr.Error(), "CPU min") {
		t.Errorf("runtime validation error %q does not name the CPU bounds", validateErr.Error())
	}

	if updateErr := stored.ValidateUpdate(validValidationPolicy()); updateErr == nil {
		t.Errorf("ValidateUpdate accepted the runtime-invalid policy")
	}
}

// TestEnvtestCRDDefaultsPersisted proves server-side defaulting is persisted
// and read back through the API rather than inferred from Go zero values, and
// that the production getters consume those persisted defaults.
func TestEnvtestCRDDefaultsPersisted(t *testing.T) {
	ctx := context.Background()

	name := "defaults-readback"
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("optipod.optipod.io/v1alpha1")
	u.SetKind("OptimizationPolicy")
	u.SetName(name)
	u.SetNamespace("default")
	u.Object["spec"] = minimalValidSpec("default")
	if err := envtestClient.Create(ctx, u); err != nil {
		t.Fatalf("failed to create the minimal policy through the raw API: %v", err)
	}

	stored := fetchPolicy(ctx, t, name)

	if stored.Spec.Weight == nil || *stored.Spec.Weight != 100 {
		t.Errorf("persisted weight = %v, expected the schema default 100", stored.Spec.Weight)
	}
	if stored.Spec.MetricsConfig.Percentile != "P90" {
		t.Errorf("persisted percentile = %q, expected schema default P90", stored.Spec.MetricsConfig.Percentile)
	}
	if stored.Spec.MetricsConfig.SafetyFactor == nil || *stored.Spec.MetricsConfig.SafetyFactor != 1.2 {
		t.Errorf("persisted safetyFactor = %v, expected schema default 1.2", stored.Spec.MetricsConfig.SafetyFactor)
	}
	if stored.Spec.MetricsConfig.RollingWindow.Duration != 24*time.Hour {
		t.Errorf("persisted rollingWindow = %s, expected schema default 24h", stored.Spec.MetricsConfig.RollingWindow.Duration)
	}
	if stored.Spec.ReconciliationInterval.Duration != 5*time.Minute {
		t.Errorf("persisted reconciliationInterval = %s, expected schema default 5m", stored.Spec.ReconciliationInterval.Duration)
	}
	if stored.Spec.UpdateStrategy.Strategy == nil || *stored.Spec.UpdateStrategy.Strategy != string(StrategyWebhook) {
		t.Errorf("persisted strategy = %v, expected schema default webhook", stored.Spec.UpdateStrategy.Strategy)
	}
	if stored.Spec.UpdateStrategy.RolloutStrategy == nil || *stored.Spec.UpdateStrategy.RolloutStrategy != string(RolloutOnNextRestart) {
		t.Errorf("persisted rolloutStrategy = %v, expected schema default onNextRestart", stored.Spec.UpdateStrategy.RolloutStrategy)
	}
	if !stored.Spec.UpdateStrategy.AllowInPlaceResize {
		t.Error("persisted allowInPlaceResize = false, expected schema default true")
	}
	if stored.Spec.UpdateStrategy.AllowRecreate {
		t.Error("persisted allowRecreate = true, expected schema default false")
	}
	if !stored.Spec.UpdateStrategy.UpdateRequestsOnly {
		t.Error("persisted updateRequestsOnly = false, expected schema default true")
	}
	if stored.Spec.UpdateStrategy.UseServerSideApply == nil || !*stored.Spec.UpdateStrategy.UseServerSideApply {
		t.Errorf("persisted useServerSideApply = %v, expected schema default true", stored.Spec.UpdateStrategy.UseServerSideApply)
	}

	// The production getters must consume the persisted defaults.
	if stored.GetStrategy() != StrategyWebhook {
		t.Errorf("GetStrategy() = %q, expected webhook from the persisted default", stored.GetStrategy())
	}
	if stored.GetRolloutStrategy() != RolloutOnNextRestart {
		t.Errorf("GetRolloutStrategy() = %q, expected onNextRestart from the persisted default", stored.GetRolloutStrategy())
	}
	if stored.GetWeight() != 100 {
		t.Errorf("GetWeight() = %d, expected 100 from the persisted default", stored.GetWeight())
	}

	// A fully defaulted policy must also satisfy the runtime validator.
	if err := stored.ValidateCreate(); err != nil {
		t.Errorf("ValidateCreate rejected the fully defaulted policy: %v", err)
	}
}

// TestEnvtestStatusSubresourceAndListSemantics proves the status subresource
// and namespace-scoped list semantics against the real API: a main-resource
// update ignores status changes, Status().Update persists them, and list
// returns the expected identities per namespace.
func TestEnvtestStatusSubresourceAndListSemantics(t *testing.T) {
	ctx := context.Background()

	policy := validValidationPolicy()
	policy.Name = "status-subresource-policy"
	policy.Namespace = "default"
	if err := envtestClient.Create(ctx, policy); err != nil {
		t.Fatalf("create: %v", err)
	}

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Test",
		Message:            "status written through the subresource",
		LastTransitionTime: metav1.Now(),
	}

	t.Run("a main-resource update ignores status", func(t *testing.T) {
		stored := fetchPolicy(ctx, t, policy.Name)
		stored.Status.Conditions = []metav1.Condition{condition}
		if err := envtestClient.Update(ctx, stored); err != nil {
			t.Fatalf("main-resource update: %v", err)
		}

		after := fetchPolicy(ctx, t, policy.Name)
		if len(after.Status.Conditions) != 0 {
			t.Errorf("main-resource update persisted %d conditions, expected the status subresource to ignore them", len(after.Status.Conditions))
		}
	})

	t.Run("Status().Update persists conditions readably", func(t *testing.T) {
		stored := fetchPolicy(ctx, t, policy.Name)
		stored.Status.Conditions = []metav1.Condition{condition}
		if err := envtestClient.Status().Update(ctx, stored); err != nil {
			t.Fatalf("status update: %v", err)
		}

		after := fetchPolicy(ctx, t, policy.Name)
		if len(after.Status.Conditions) != 1 {
			t.Fatalf("read back %d conditions, expected 1", len(after.Status.Conditions))
		}
		if after.Status.Conditions[0].Reason != condition.Reason || after.Status.Conditions[0].Type != condition.Type {
			t.Errorf("read-back condition %+v does not match the written condition", after.Status.Conditions[0])
		}
	})

	t.Run("namespace-scoped list returns the expected identities", func(t *testing.T) {
		listNamespace := "defaults-list-ns"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: listNamespace}}
		if err := envtestClient.Create(ctx, ns); err != nil {
			t.Fatalf("create list namespace: %v", err)
		}

		scoped := validValidationPolicy()
		scoped.Name = "list-scoped-policy"
		scoped.Namespace = listNamespace
		if err := envtestClient.Create(ctx, scoped); err != nil {
			t.Fatalf("create scoped policy: %v", err)
		}

		inNS := &OptimizationPolicyList{}
		if err := envtestClient.List(ctx, inNS, client.InNamespace(listNamespace)); err != nil {
			t.Fatalf("namespace-scoped list: %v", err)
		}
		if len(inNS.Items) != 1 || inNS.Items[0].Name != scoped.Name {
			t.Fatalf("namespace-scoped list returned %v, expected exactly [%s]", itemNames(inNS), scoped.Name)
		}

		clusterWide := &OptimizationPolicyList{}
		if err := envtestClient.List(ctx, clusterWide); err != nil {
			t.Fatalf("cluster-wide list: %v", err)
		}
		names := itemNames(clusterWide)
		for _, want := range []string{policy.Name, scoped.Name} {
			if !containsName(names, want) {
				t.Errorf("cluster-wide list %v is missing %q", names, want)
			}
		}
	})
}

func itemNames(list *OptimizationPolicyList) []string {
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
