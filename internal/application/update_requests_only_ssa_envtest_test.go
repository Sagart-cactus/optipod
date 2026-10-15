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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// TestApplyUpdateRequestsOnlySSAOutcome is the M3 application-outcome proof for
// S059 (coverage-matrix B04, VAL-FAST-005): it applies a recommendation through
// the production Apply path with an explicit strategy: ssa policy against a
// real API server and reads the workload's resources.requests AND
// resources.limits before and after. Under updateRequestsOnly=true the requests
// change to the recommended values while the limits stay byte-identical; with
// updateRequestsOnly=false the limits change to the exact multiplier-derived
// values. The pre-existing property tests only check the generated patch JSON,
// not an applied outcome.
func TestApplyUpdateRequestsOnlySSAOutcome(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping envtest in short mode")
	}

	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Skipf("Failed to start test environment: %v", err)
		return
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("Failed to stop test environment: %v", err)
		}
	}()

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create dynamic client: %v", err)
	}
	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create discovery client: %v", err)
	}

	ctx := context.Background()
	engine := NewEngine(k8sClient, dynamicClient, discoveryClient, false)

	ssaStrategy := string(optipodv1alpha1.StrategySSA)
	newPolicy := func(updateRequestsOnly bool) *optipodv1alpha1.OptimizationPolicy {
		return &optipodv1alpha1.OptimizationPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "requests-only-ssa-policy", Namespace: "default"},
			Spec: optipodv1alpha1.OptimizationPolicySpec{
				Mode: optipodv1alpha1.ModeAuto,
				UpdateStrategy: optipodv1alpha1.UpdateStrategy{
					Strategy:           &ssaStrategy,
					UpdateRequestsOnly: updateRequestsOnly,
				},
			},
		}
	}

	tests := []struct {
		name                string
		namespace           string
		updateRequestsOnly  bool
		wantCPURequest      int64 // milli
		wantMemoryRequest   int64 // bytes
		wantCPULimit        int64 // milli
		wantMemoryLimit     int64 // bytes
		wantLimitsUnchanged bool
	}{
		{
			name:                "updateRequestsOnly=true changes requests and leaves limits byte-identical",
			namespace:           "requests-only-ssa-on",
			updateRequestsOnly:  true,
			wantCPURequest:      250,
			wantMemoryRequest:   250 * 1024 * 1024,
			wantLimitsUnchanged: true,
		},
		{
			name:               "updateRequestsOnly=false changes requests and applies exact default-multiplier limits",
			namespace:          "requests-only-ssa-off",
			updateRequestsOnly: false,
			wantCPURequest:     250,
			wantMemoryRequest:  250 * 1024 * 1024,
			wantCPULimit:       375,               // 250m * 1.5
			wantMemoryLimit:    325 * 1024 * 1024, // 250Mi * 1.3 = 325Mi
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tt.namespace}}
			if err := k8sClient.Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			defer func() {
				if err := k8sClient.Delete(ctx, ns); err != nil {
					t.Logf("Failed to delete namespace: %v", err)
				}
			}()

			deployment := requestsOnlyDeployment("requests-only-deploy", ns.Name)
			if err := k8sClient.Create(ctx, deployment); err != nil {
				t.Fatalf("Failed to create deployment: %v", err)
			}

			key := types.NamespacedName{Namespace: ns.Name, Name: deployment.Name}
			before := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, key, before); err != nil {
				t.Fatalf("Failed to fetch deployment before apply: %v", err)
			}
			beforeContainer := before.Spec.Template.Spec.Containers[0]

			if _, err := engine.Apply(ctx, toApplicationWorkload(t, before), singleContainerApplySet(), newPolicy(tt.updateRequestsOnly)); err != nil {
				t.Fatalf("Apply failed: %v", err)
			}

			after := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, key, after); err != nil {
				t.Fatalf("Failed to fetch deployment after apply: %v", err)
			}
			afterContainer := after.Spec.Template.Spec.Containers[0]

			// Requests must differ from the original values and carry the
			// recommended ones.
			if got := afterContainer.Resources.Requests.Cpu().MilliValue(); got != tt.wantCPURequest {
				t.Errorf("CPU request = %d milli, want %d milli", got, tt.wantCPURequest)
			}
			if got := afterContainer.Resources.Requests.Memory().Value(); got != tt.wantMemoryRequest {
				t.Errorf("memory request = %d bytes, want %d bytes", got, tt.wantMemoryRequest)
			}
			if afterContainer.Resources.Requests.Cpu().Equal(*beforeContainer.Resources.Requests.Cpu()) &&
				afterContainer.Resources.Requests.Memory().Equal(*beforeContainer.Resources.Requests.Memory()) {
				t.Error("requests must change before/after the apply")
			}

			if tt.wantLimitsUnchanged {
				// Limits must be byte-identical before/after.
				if !afterContainer.Resources.Limits.Cpu().Equal(*beforeContainer.Resources.Limits.Cpu()) {
					t.Errorf("CPU limit changed under updateRequestsOnly: before %s after %s",
						beforeContainer.Resources.Limits.Cpu().String(), afterContainer.Resources.Limits.Cpu().String())
				}
				if !afterContainer.Resources.Limits.Memory().Equal(*beforeContainer.Resources.Limits.Memory()) {
					t.Errorf("memory limit changed under updateRequestsOnly: before %s after %s",
						beforeContainer.Resources.Limits.Memory().String(), afterContainer.Resources.Limits.Memory().String())
				}
			} else {
				if got := afterContainer.Resources.Limits.Cpu().MilliValue(); got != tt.wantCPULimit {
					t.Errorf("CPU limit = %d milli, want %d milli", got, tt.wantCPULimit)
				}
				if got := afterContainer.Resources.Limits.Memory().Value(); got != tt.wantMemoryLimit {
					t.Errorf("memory limit = %d bytes, want %d bytes", got, tt.wantMemoryLimit)
				}
			}
		})
	}
}
