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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/metrics"
	"github.com/optipod/optipod/internal/observability"
)

// These specs drive the production OptimizationPolicyReconciler end to end on
// the suite's envtest API server with deterministic per-container metrics:
// Disabled/Recommend/Auto outcomes, mode transitions, reconciler status
// (validation/discovered/processed), and invalid-policy no-mutation plus
// corrected-policy recovery (VAL-FAST-002). The workload annotations and
// applied requests are read back through the API, not from fixtures.

const modesTimeout = 10 * time.Second

// newModesReconciler wires the production workload processor (deterministic
// provider, production recommendation and application engines) into the
// production reconciler.
func newModesReconciler(provider *perContainerMetricsProvider) *OptimizationPolicyReconciler {
	return &OptimizationPolicyReconciler{
		Client:            k8sClient,
		Scheme:            k8sClient.Scheme(),
		Recorder:          record.NewFakeRecorder(200),
		WorkloadProcessor: buildConvergenceProcessor(provider),
		EventRecorder:     observability.NewEventRecorder(record.NewFakeRecorder(200)),
	}
}

// modesProvider returns deterministic P90 metrics for the "app" container:
// 300m CPU / 512Mi memory (with safety factor 1.0 these become the exact
// recommendation values).
func modesProvider() *perContainerMetricsProvider {
	return &perContainerMetricsProvider{
		metricsByContainer: map[string]*metrics.ContainerMetrics{
			"app": {
				CPU: metrics.ResourceMetrics{
					P50:     resource.MustParse("200m"),
					P90:     resource.MustParse("300m"),
					P99:     resource.MustParse("400m"),
					Samples: 50,
				},
				Memory: metrics.ResourceMetrics{
					P50:     resource.MustParse("384Mi"),
					P90:     resource.MustParse("512Mi"),
					P99:     resource.MustParse("640Mi"),
					Samples: 50,
				},
			},
		},
	}
}

// createModesWorkloadFixture creates a namespace, a one-container Deployment
// (requests 100m/128Mi) and a pod matching its selector, returning cleanup.
func createModesWorkloadFixture(ctx context.Context, namespace, deploymentName string) func() {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels:    map[string]string{"app": deploymentName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": deploymentName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": deploymentName},
				},
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
							},
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName + "-pod",
			Namespace: namespace,
			Labels:    map[string]string{"app": deploymentName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx:1.21"}},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

	return func() {
		_ = k8sClient.Delete(ctx, pod)
		_ = k8sClient.Delete(ctx, deployment)
		_ = k8sClient.Delete(ctx, ns)
	}
}

// createModesPolicy persists an Auto-mode policy with the given knobs through
// the API. Spec code that needs the API-fetched object (with schema defaults
// applied) calls fetchModesPolicy explicitly.
func createModesPolicy(ctx context.Context, name, namespace string, mutate func(*optipodv1alpha1.OptimizationPolicy)) {
	safetyFactor := 1.0
	ssaStrategy := string(optipodv1alpha1.StrategySSA)
	policy := &optipodv1alpha1.OptimizationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: optipodv1alpha1.OptimizationPolicySpec{
			Mode: optipodv1alpha1.ModeAuto,
			Selector: optipodv1alpha1.WorkloadSelector{
				Namespaces: &optipodv1alpha1.NamespaceFilter{Allow: []string{namespace}},
			},
			MetricsConfig: optipodv1alpha1.MetricsConfig{
				Provider:     "prometheus",
				Percentile:   "P90",
				SafetyFactor: &safetyFactor,
			},
			ResourceBounds: optipodv1alpha1.ResourceBounds{
				CPU: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("50m"),
					Max: resource.MustParse("2000m"),
				},
				Memory: optipodv1alpha1.ResourceBound{
					Min: resource.MustParse("64Mi"),
					Max: resource.MustParse("4Gi"),
				},
			},
			UpdateStrategy: optipodv1alpha1.UpdateStrategy{
				Strategy:           &ssaStrategy,
				UpdateRequestsOnly: true,
			},
		},
	}
	if mutate != nil {
		mutate(policy)
	}
	Expect(k8sClient.Create(ctx, policy)).To(Succeed())
}

// reconcilePolicy runs the production Reconcile for the named policy through
// the given reconciler.
func reconcilePolicy(ctx context.Context, reconciler *OptimizationPolicyReconciler, name string) {
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
	Expect(err).NotTo(HaveOccurred())
}

// fetchModesPolicy reads the policy back from the API.
func fetchModesPolicy(ctx context.Context, name string) *optipodv1alpha1.OptimizationPolicy {
	policy := &optipodv1alpha1.OptimizationPolicy{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, policy)).To(Succeed())
	return policy
}

// expectPolicyStatusEventually waits for the policy status to satisfy expect.
func expectPolicyStatusEventually(ctx context.Context, name string, expect func(*optipodv1alpha1.OptimizationPolicy) bool, description string) {
	Eventually(func() bool {
		policy := fetchModesPolicy(ctx, name)
		return expect(policy)
	}, modesTimeout, 250*time.Millisecond).Should(BeTrue(), description)
}

// requireCondition asserts the Ready condition state with the given reason.
func requireCondition(policy *optipodv1alpha1.OptimizationPolicy, wantStatus metav1.ConditionStatus, wantReason, wantMessagePart string) {
	for _, condition := range policy.Status.Conditions {
		if condition.Type == ConditionTypeReady {
			Expect(condition.Status).To(Equal(wantStatus))
			Expect(condition.Reason).To(Equal(wantReason))
			if wantMessagePart != "" {
				Expect(condition.Message).To(ContainSubstring(wantMessagePart))
			}
			return
		}
	}
	Fail("policy has no Ready condition")
}

var _ = Describe("Production mode handling through the reconciler", func() {
	Context("Disabled mode", func() {
		const (
			namespace      = "modes-disabled-ns"
			deploymentName = "modes-disabled-deploy"
			policyName     = "modes-disabled-policy"
		)

		It("reconciles to Ready but does not process the matching workload", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			createModesPolicy(ctx, policyName, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				p.Spec.Mode = optipodv1alpha1.ModeDisabled
			})

			reconcilePolicy(ctx, reconciler, policyName)

			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				if len(p.Status.Conditions) == 0 {
					return false
				}
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 0
			}, "disabled policy should report the discovered workload as unprocessed")

			policy := fetchModesPolicy(ctx, policyName)
			requireCondition(policy, metav1.ConditionTrue, "PolicyValid", "")
			Expect(policy.Status.WorkloadsByType).NotTo(BeNil())
			Expect(policy.Status.WorkloadsByType.Deployments).To(Equal(1))
			Expect(policy.Status.LastReconciliation).NotTo(BeNil())

			deployment := fetchDeployment(ctx, namespace, deploymentName)
			Expect(deployment.Annotations).To(BeEmpty(), "a Disabled policy must not annotate the workload")
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("100m"))
			Expect(memory).To(Equal("128Mi"))
		})
	})

	Context("Recommend mode", func() {
		const (
			namespace      = "modes-recommend-ns"
			deploymentName = "modes-recommend-deploy"
			policyName     = "modes-recommend-policy"
		)

		It("records exact recommendations without applying them", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			createModesPolicy(ctx, policyName, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				p.Spec.Mode = optipodv1alpha1.ModeRecommend
			})

			reconcilePolicy(ctx, reconciler, policyName)

			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 1
			}, "recommend policy should report the workload as discovered and processed")

			policy := fetchModesPolicy(ctx, policyName)
			requireCondition(policy, metav1.ConditionTrue, "PolicyValid", "")

			deployment := fetchDeployment(ctx, namespace, deploymentName)
			Expect(deployment.Annotations).NotTo(BeNil())
			Expect(deployment.Annotations[optipodv1alpha1.AnnotationManaged]).To(Equal("true"))
			Expect(deployment.Annotations[optipodv1alpha1.AnnotationPolicy]).To(Equal(policyName))
			Expect(deployment.Annotations[optipodv1alpha1.AnnotationLastRecommendation]).NotTo(BeEmpty())
			Expect(deployment.Annotations["optipod.io/recommendation.app.cpu-request"]).To(Equal("300m"))
			Expect(deployment.Annotations["optipod.io/recommendation.app.memory-request"]).To(Equal("512Mi"))

			// Recommend must not apply: requests stay at the original values.
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("100m"))
			Expect(memory).To(Equal("128Mi"))
		})
	})

	Context("Mode transitions", func() {
		const (
			namespace      = "modes-transition-ns"
			deploymentName = "modes-transition-deploy"
			policyName     = "modes-transition-policy"
		)

		It("affect subsequent processing from Recommend to Auto to Disabled", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			createModesPolicy(ctx, policyName, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				p.Spec.Mode = optipodv1alpha1.ModeRecommend
			})

			By("Recommend: recommendations recorded, nothing applied")
			reconcilePolicy(ctx, reconciler, policyName)
			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 1
			}, "recommend stage should process the workload")
			deployment := fetchDeployment(ctx, namespace, deploymentName)
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("100m"))
			Expect(memory).To(Equal("128Mi"))

			By("Auto: the recorded recommendation is applied to the workload")
			policy := fetchModesPolicy(ctx, policyName)
			policy.Spec.Mode = optipodv1alpha1.ModeAuto
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			reconcilePolicy(ctx, reconciler, policyName)
			Eventually(func() string {
				cpu, _ := containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
				return cpu
			}, modesTimeout, 250*time.Millisecond).Should(Equal("300m"), "Auto mode must apply the recommendation")
			deployment = fetchDeployment(ctx, namespace, deploymentName)
			_, memory = containerRequest(deployment, "app")
			Expect(memory).To(Equal("512Mi"))

			By("Disabled: changed metric input produces no further effect")
			provider.metricsByContainer["app"] = &metrics.ContainerMetrics{
				CPU: metrics.ResourceMetrics{
					P50:     resource.MustParse("400m"),
					P90:     resource.MustParse("600m"),
					P99:     resource.MustParse("800m"),
					Samples: 50,
				},
				Memory: metrics.ResourceMetrics{
					P50:     resource.MustParse("576Mi"),
					P90:     resource.MustParse("700Mi"),
					P99:     resource.MustParse("900Mi"),
					Samples: 50,
				},
			}
			policy = fetchModesPolicy(ctx, policyName)
			policy.Spec.Mode = optipodv1alpha1.ModeDisabled
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			reconcilePolicy(ctx, reconciler, policyName)

			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 0
			}, "disabled stage must stop processing the workload")

			deployment = fetchDeployment(ctx, namespace, deploymentName)
			cpu, memory = containerRequest(deployment, "app")
			Expect(cpu).To(Equal("300m"), "Disabled must not apply the changed recommendation")
			Expect(memory).To(Equal("512Mi"))
		})
	})

	Context("Invalid runtime policy", func() {
		const (
			namespace      = "modes-invalid-ns"
			deploymentName = "modes-invalid-deploy"
			policyName     = "modes-invalid-policy"
		)

		It("does not mutate workloads and recovers after correction", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			// Inverted CPU bounds pass the CRD schema but fail the runtime
			// validator, so the API admits the policy.
			createModesPolicy(ctx, policyName, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				p.Spec.ResourceBounds.CPU.Min = resource.MustParse("2000m")
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("1000m")
			})

			reconcilePolicy(ctx, reconciler, policyName)

			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				for _, condition := range p.Status.Conditions {
					if condition.Type == ConditionTypeReady && condition.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, "the runtime-invalid policy must be flagged")

			policy := fetchModesPolicy(ctx, policyName)
			requireCondition(policy, metav1.ConditionFalse, "ValidationFailed", "CPU min")
			Expect(policy.Status.WorkloadsDiscovered).To(Equal(0))
			Expect(policy.Status.WorkloadsProcessed).To(Equal(0))

			deployment := fetchDeployment(ctx, namespace, deploymentName)
			Expect(deployment.Annotations).To(BeEmpty(), "an invalid policy must not annotate the workload")
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("100m"))
			Expect(memory).To(Equal("128Mi"))

			By("correcting the policy restores processing")
			policy = fetchModesPolicy(ctx, policyName)
			policy.Spec.ResourceBounds.CPU.Min = resource.MustParse("50m")
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			reconcilePolicy(ctx, reconciler, policyName)

			expectPolicyStatusEventually(ctx, policyName, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 1
			}, "the corrected policy must process the workload")

			requireCondition(fetchModesPolicy(ctx, policyName), metav1.ConditionTrue, "PolicyValid", "")

			Eventually(func() string {
				cpu, _ := containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
				return cpu
			}, modesTimeout, 250*time.Millisecond).Should(Equal("300m"), "the corrected policy must apply")
		})
	})
})
