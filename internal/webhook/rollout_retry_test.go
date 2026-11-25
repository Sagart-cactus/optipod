package webhook

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Inject a concurrent API write after the controller's first read. The backing
// client, rather than the test, rejects the stale Deployment version.
type rolloutConflictClient struct {
	client.Client
	firstUpdate func(context.Context, *appsv1.Deployment) error
	updates     int
	sawConflict bool
}

func (c *rolloutConflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		return c.Client.Update(ctx, obj, opts...)
	}
	c.updates++
	if c.updates == 1 {
		latest := &appsv1.Deployment{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(dep), latest); err != nil {
			return err
		}
		if err := c.firstUpdate(ctx, latest); err != nil {
			return err
		}
	}
	err := c.Client.Update(ctx, obj, opts...)
	c.sawConflict = c.sawConflict || apierrors.IsConflict(err)
	return err
}

func TestRecommendationRolloutConflict(t *testing.T) {
	ready := int32(1)
	cpu := resource.MustParse("25m")
	recommendations := []ResourceRecommendation{{ContainerName: "app", CPU: &cpu}}
	plan := resourcePlanHash(recommendations)
	const concurrentKey = "fixture.example/change"
	mutateLater := func(writes func(*appsv1.Deployment)) func(context.Context, client.Client, *appsv1.Deployment, *api.OptimizationPolicy) error {
		return func(ctx context.Context, c client.Client, dep *appsv1.Deployment, _ *api.OptimizationPolicy) error {
			writes(dep)
			return c.Update(ctx, dep)
		}
	}
	tests := []struct {
		name         string
		change       func(context.Context, client.Client, *appsv1.Deployment, *api.OptimizationPolicy) error
		wantError    string
		wantApplied  bool
		wantMarker   string
		wantAttempts int
	}{
		{
			name: "preserves concurrent metadata",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[concurrentKey] = "kept"
			}),
			wantApplied: true, wantAttempts: 2,
		},
		{
			name: "already accepted plan is idempotent",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[RolloutHash] = plan
				dep.Spec.Template.Annotations = map[string]string{restartTriggeredAnnotation: "existing"}
			}),
			wantMarker: "existing", wantAttempts: 1,
		},
		{
			name: "superseded plan",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[RecommendationHash] = "superseded"
			}),
			wantError: "recommendation changed", wantAttempts: 1,
		},
		{
			name: "expired plan",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[RecommendationExpiry] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			}),
			wantError: "recommendation changed or expired", wantAttempts: 1,
		},
		{
			name: "renewed plan changes intent",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[RecommendationExpiry] = time.Now().Add(time.Hour).Format(time.RFC3339)
			}),
			wantError: "rollout intent changed", wantAttempts: 1,
		},
		{
			name: "changed resource recommendation",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[api.AnnotationCPURequestPrefix+".app"] = "30m"
			}),
			wantError: "rollout intent changed", wantAttempts: 1,
		},
		{
			name: "changed rollout intent",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[RolloutHash] = "other-plan"
			}),
			wantError: "rollout intent changed", wantAttempts: 1,
		},
		{
			name: "changed workload identity",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.UID = "replacement"
			}),
			wantError: "workload identity changed", wantAttempts: 1,
		},
		{
			name: "workload paused",
			change: mutateLater(func(dep *appsv1.Deployment) {
				dep.Annotations[history.Paused] = "true"
			}),
			wantError: "workload is paused", wantAttempts: 1,
		},
		{
			name: "workload scaled down",
			change: mutateLater(func(dep *appsv1.Deployment) {
				zero := int32(0)
				dep.Spec.Replicas = &zero
			}),
			wantError: "workload spec changed", wantAttempts: 1,
		},
		{
			name: "workload lost readiness",
			change: func(ctx context.Context, c client.Client, dep *appsv1.Deployment, _ *api.OptimizationPolicy) error {
				dep.Status.ReadyReplicas = 0
				return c.Status().Update(ctx, dep)
			},
			wantError: "workload is not fully ready", wantAttempts: 1,
		},
		{
			name: "selected policy advanced",
			change: func(ctx context.Context, c client.Client, dep *appsv1.Deployment, p *api.OptimizationPolicy) error {
				live := &api.OptimizationPolicy{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(p), live); err != nil {
					return err
				}
				live.Generation++
				if err := c.Update(ctx, live); err != nil {
					return err
				}
				dep.Annotations[concurrentKey] = "policy changed too"
				return c.Update(ctx, dep)
			},
			wantError: "selected policy changed", wantAttempts: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{appsv1.AddToScheme, corev1.AddToScheme, api.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			p := &api.OptimizationPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default", Generation: 1, UID: "policy-uid"}, Spec: api.OptimizationPolicySpec{Mode: api.ModeAuto}}
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "default", Generation: 1, UID: "workload-uid", Annotations: map[string]string{
					RecommendationHash: plan, api.AnnotationCPURequestPrefix + ".app": "25m",
					api.AnnotationWebhookEnabled: "true", api.AnnotationStrategy: string(api.StrategyWebhook),
				}},
				Spec: appsv1.DeploymentSpec{Replicas: &ready},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1,
				},
			}
			StampRecommendations(dep.Annotations, p, time.Now())
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dep.Namespace}}
			store := fake.NewClientBuilder().WithScheme(scheme).WithObjects(p, dep, ns).WithStatusSubresource(dep).Build()
			hooked := &rolloutConflictClient{Client: store, firstUpdate: func(ctx context.Context, latest *appsv1.Deployment) error {
				return tt.change(ctx, store, latest, p)
			}}
			applied, err := NewRolloutController(hooked).TriggerRecommendationRollout(ctx, dep, p, recommendations)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("got error %v, want %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if applied != tt.wantApplied || hooked.updates != tt.wantAttempts || !hooked.sawConflict {
				t.Fatalf("applied=%t, writes=%d, API conflict=%t", applied, hooked.updates, hooked.sawConflict)
			}

			var final appsv1.Deployment
			if err := store.Get(ctx, client.ObjectKeyFromObject(dep), &final); err != nil {
				t.Fatal(err)
			}
			marker := final.Spec.Template.Annotations[restartTriggeredAnnotation]
			if tt.wantApplied {
				if marker == "" || final.Annotations[RolloutHash] != plan || final.Annotations[concurrentKey] != "kept" {
					t.Fatalf("restart metadata was not merged: %#v", final.Annotations)
				}
			} else if marker != tt.wantMarker {
				t.Fatalf("got restart marker %q, want %q", marker, tt.wantMarker)
			}
		})
	}
}
