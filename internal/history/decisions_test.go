package history

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDecisionTransitionsRetainSettingsWithoutWorkloadMutation(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test", UID: "workload"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(d).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			obj.SetCreationTimestamp(metav1.Now())
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	w := &discovery.Workload{Kind: "Deployment", Name: d.Name, Namespace: d.Namespace, Object: d}
	p := &api.OptimizationPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "test", UID: "policy", Generation: 1}, Spec: api.OptimizationPolicySpec{Mode: api.ModeDisabled}}
	status := &api.WorkloadStatus{Status: "Skipped", Reason: "Policy is disabled"}
	for _, reason := range []string{"Policy is disabled", "Policy is disabled", "Missing metrics", "Policy is disabled"} {
		status.Reason = reason
		if err := ObserveDecision(ctx, c, w, p, status, nil); err != nil {
			t.Fatal(err)
		}
	}
	list := &corev1.ConfigMapList{}
	if err := c.List(ctx, list, client.MatchingLabels{decisionLabel: labelTrue}); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected three transitions, got %d", len(list.Items))
	}
	p.Spec.Mode = api.ModeAuto
	for _, cm := range list.Items {
		r, err := Read(ctx, c, cm.Namespace, cm.Name)
		if err != nil {
			t.Fatal(err)
		}
		if r.PolicySpec == nil || r.PolicySpec.Mode != api.ModeDisabled || r.Decision.Reason == "" || cm.Immutable == nil || !*cm.Immutable {
			t.Fatal("decision lost immutable settings or reason")
		}
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(d), d); err != nil {
		t.Fatal(err)
	}
	if len(d.Annotations) != 0 {
		t.Fatal("decision logging changed a Disabled workload")
	}
}

func TestPruningKeepsPendingRollbackAndSeparateRecordBudgets(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	latest := &Record{Name: "latest", Workload: Reference{Namespace: "test", UID: "workload"}}
	for i := 0; i < 43; i++ {
		name, phase, created := fmt.Sprintf("record-%02d", i), "Applied", time.Now()
		labels := map[string]string{Label: labelTrue, WorkloadLabel: "workload"}
		if i < 40 {
			labels[decisionLabel] = labelTrue
			phase = ""
		}
		if i == 40 {
			name = "latest"
		}
		if i == 41 {
			name = "previous"
		}
		if i == 42 {
			name = "pending"
			phase = "RollbackPending"
			created = created.Add(-40 * 24 * time.Hour)
		}
		data, err := json.Marshal(Record{Created: created})
		if err != nil {
			t.Fatal(err)
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", Labels: labels, CreationTimestamp: metav1.NewTime(created), Annotations: map[string]string{"optipod.io/history-phase": phase}}, Data: map[string]string{DataKey: string(data)}}
		if err := c.Create(ctx, cm); err != nil {
			t.Fatal(err)
		}
	}
	if err := prune(ctx, c, latest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"latest", "previous", "pending"} {
		if err := c.Get(ctx, client.ObjectKey{Namespace: "test", Name: name}, &corev1.ConfigMap{}); err != nil {
			t.Fatalf("lost protected %s: %v", name, err)
		}
	}
}
