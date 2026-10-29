package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

func TestAnnotationThrottleDoesNotChasePeriodicWrites(t *testing.T) {
	start := time.Date(2026, time.October, 25, 7, 15, 0, 0, time.UTC)
	current := start
	reads := 0
	condition := annotationThrottleCondition(func(context.Context) (map[string]string, error) {
		reads++
		// Model a controller refreshing the timestamp every 30s.
		writtenAt := start.Add(current.Sub(start).Truncate(30 * time.Second))
		return map[string]string{optipodv1alpha1.AnnotationLastRecommendation: writtenAt.Format(time.RFC3339)}, nil
	}, 32*time.Second, func() time.Time { return current })

	for _, elapsed := range []time.Duration{0, 29 * time.Second, 30 * time.Second, 31 * time.Second, 32 * time.Second} {
		current = start.Add(elapsed)
		done, err := condition(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if want := elapsed >= 32*time.Second; done != want {
			t.Fatalf("at %s: done=%v, want %v", elapsed, done, want)
		}
	}
	if reads != 1 {
		t.Fatalf("read the moving annotation %d times, want one snapshot", reads)
	}
}

func TestAnnotationThrottleHandlesAbsentAndOldAnnotations(t *testing.T) {
	now := time.Now()
	for name, annotations := range map[string]map[string]string{
		"nil":     nil,
		"empty":   {},
		"missing": {"unrelated": "annotation"},
		"expired": {optipodv1alpha1.AnnotationLastRecommendation: now.Add(-time.Minute).Format(time.RFC3339)},
	} {
		t.Run(name, func(t *testing.T) {
			condition := annotationThrottleCondition(func(context.Context) (map[string]string, error) {
				return annotations, nil
			}, 32*time.Second, func() time.Time { return now })
			done, err := condition(context.Background())
			if err != nil || !done {
				t.Fatalf("expected no throttle, got done=%v, err=%v", done, err)
			}
		})
	}
}

func TestAnnotationThrottleRetriesFailedReads(t *testing.T) {
	calls := 0
	now := time.Now()
	condition := annotationThrottleCondition(func(context.Context) (map[string]string, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient read failure")
		}
		return nil, nil
	}, 32*time.Second, func() time.Time { return now })
	if done, err := condition(context.Background()); done || err == nil {
		t.Fatalf("expected failed initial observation, got done=%v, err=%v", done, err)
	}
	if done, err := condition(context.Background()); !done || err != nil {
		t.Fatalf("expected successful retry, got done=%v, err=%v", done, err)
	}
}

func TestAnnotationThrottleRejectsMalformedTimestamp(t *testing.T) {
	condition := annotationThrottleCondition(func(context.Context) (map[string]string, error) {
		return map[string]string{optipodv1alpha1.AnnotationLastRecommendation: "not-a-time"}, nil
	}, 32*time.Second, time.Now)
	if done, err := condition(context.Background()); done || err == nil {
		t.Fatalf("expected invalid timestamp error, got done=%v, err=%v", done, err)
	}
}

func TestWaitForDeploymentAnnotationThrottle(t *testing.T) {
	clients := &Clients{Kube: fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test", Name: "workload",
			Annotations: map[string]string{
				optipodv1alpha1.AnnotationLastRecommendation: time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
		},
	})}
	opts := WaitOptions{Timeout: time.Second, Interval: time.Millisecond}
	if err := WaitForDeploymentAnnotationThrottle(context.Background(), clients, "test", "workload", 32*time.Second, opts); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForDeploymentAnnotationThrottleHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitForDeploymentAnnotationThrottle(ctx, &Clients{Kube: fake.NewSimpleClientset()},
		"test", "missing", 32*time.Second, WaitOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
