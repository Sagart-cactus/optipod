package harness

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/observability"
)

// TestHarness provides a complete testing environment without needing a cluster
type TestHarness struct {
	Client          client.Client
	Clientset       kubernetes.Interface
	MetricsProvider *MockMetricsProvider
	EventRecorder   *observability.EventRecorder
	Clock           *MockClock
	Scheme          *runtime.Scheme
	Context         context.Context
	Cancel          context.CancelFunc
}

// HarnessOption allows customization of TestHarness
type HarnessOption func(*TestHarness)

// NewTestHarness creates a new test harness with fake clients and mocks
func NewTestHarness(opts ...HarnessOption) *TestHarness {
	// 1. Create scheme with OptiPod CRDs
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = optipodv1alpha1.AddToScheme(scheme)

	// 2. Create fake controller-runtime client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&optipodv1alpha1.OptimizationPolicy{}).
		Build()

	// 3. Create fake kubernetes clientset
	fakeClientset := k8sfake.NewSimpleClientset()

	// 4. Create mock metrics provider
	metricsProvider := NewMockMetricsProvider()

	// 5. Create event recorder
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme, corev1.EventSource{Component: "test"})
	eventRecorder := observability.NewEventRecorder(recorder)

	// 6. Create mock clock
	clock := NewMockClock(time.Now())

	ctx, cancel := context.WithCancel(context.Background())

	harness := &TestHarness{
		Client:          fakeClient,
		Clientset:       fakeClientset,
		MetricsProvider: metricsProvider,
		EventRecorder:   eventRecorder,
		Clock:           clock,
		Scheme:          scheme,
		Context:         ctx,
		Cancel:          cancel,
	}

	// Apply options
	for _, opt := range opts {
		opt(harness)
	}

	return harness
}

// WithExistingObjects pre-populates the fake client with objects
func WithExistingObjects(objs ...client.Object) HarnessOption {
	return func(h *TestHarness) {
		for _, obj := range objs {
			_ = h.Client.Create(h.Context, obj)
		}
	}
}

// WithClock sets a custom mock clock
func WithClock(clock *MockClock) HarnessOption {
	return func(h *TestHarness) {
		h.Clock = clock
	}
}

// WithMetricsProvider sets a custom metrics provider
func WithMetricsProvider(provider *MockMetricsProvider) HarnessOption {
	return func(h *TestHarness) {
		h.MetricsProvider = provider
	}
}

// Cleanup releases resources
func (h *TestHarness) Cleanup() {
	if h.Cancel != nil {
		h.Cancel()
	}
}
