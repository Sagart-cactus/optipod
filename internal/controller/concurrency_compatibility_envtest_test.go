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
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// These envtest specs force real API-server resourceVersion conflicts at the
// production reconciler/status and workload-annotation update seams. They also
// prove the selector consumes a policy edit made after Reconcile fetched its
// initial policy, and that an old policy with newer optional fields omitted
// follows persisted defaults through the production processor/application path.
var _ = Describe("Concurrent API edits and legacy policy compatibility", func() {
	It("retries a status resourceVersion conflict without losing another writer's condition", func() {
		ctx := context.Background()
		const (
			namespace  = "concurrent-status-ns"
			policyName = "concurrent-status-policy"
		)
		createModesPolicy(ctx, policyName, namespace, nil)
		defer func() {
			policy := fetchModesPolicy(ctx, policyName)
			_ = k8sClient.Delete(ctx, policy)
		}()

		racingClient := &statusConflictClient{
			Client: k8sClient,
			target: types.NamespacedName{Namespace: "default", Name: policyName},
		}
		reconciler := newModesReconciler(modesProvider())
		reconciler.Client = racingClient

		reconcilePolicy(ctx, reconciler, policyName)

		stored := fetchModesPolicy(ctx, policyName)
		Expect(racingClient.injectionErr).NotTo(HaveOccurred())
		Expect(racingClient.conflictObserved).To(BeTrue(), "the real API server should reject the stale status resourceVersion")
		Expect(racingClient.externalResourceVersion).NotTo(BeEmpty())
		Expect(stored.ResourceVersion).NotTo(Equal(racingClient.externalResourceVersion),
			"the retry should persist the controller's update after the concurrent write")
		Expect(policyCondition(stored, "ConcurrentWriter")).To(Equal("Preserved"),
			"retrying the Ready update must retain the unrelated condition")
		Expect(policyCondition(stored, ConditionTypeReady)).To(Equal("PolicyValid"))
		var readyTransition metav1.Time
		for _, condition := range stored.Status.Conditions {
			if condition.Type == ConditionTypeReady {
				readyTransition = condition.LastTransitionTime
			}
		}
		Expect(stored.Status.WorkloadsDiscovered).To(Equal(0))
		Expect(stored.Status.WorkloadsProcessed).To(Equal(0))
		Expect(stored.Status.LastReconciliation).NotTo(BeNil())
		Expect(stored.Status.LastReconciliation.Time.Before(readyTransition.Time)).To(BeFalse(),
			"the completed reconciliation summary must not precede the preserved Ready transition")
	})

	It("retries a workload conflict and preserves a newer user template edit", func() {
		ctx := context.Background()
		const (
			namespace      = "concurrent-workload-ns"
			deploymentName = "concurrent-workload"
			policyName     = "concurrent-workload-policy"
		)
		cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
		defer cleanup()
		createModesPolicy(ctx, policyName, namespace, func(policy *optipodv1alpha1.OptimizationPolicy) {
			// Leave strategy omitted so the API and production getter select
			// the legacy webhook path.
			policy.Spec.UpdateStrategy.Strategy = nil
		})
		defer func() {
			policy := fetchModesPolicy(ctx, policyName)
			_ = k8sClient.Delete(ctx, policy)
		}()

		racingClient := &workloadConflictClient{
			Client: k8sClient,
			target: types.NamespacedName{Namespace: namespace, Name: deploymentName},
		}
		reconciler := newModesReconciler(modesProvider())
		reconciler.Client = racingClient
		reconciler.WorkloadProcessor.client = racingClient

		reconcilePolicy(ctx, reconciler, policyName)

		deployment := fetchDeployment(ctx, namespace, deploymentName)
		Expect(racingClient.injected).To(BeTrue())
		Expect(racingClient.conflictObserved).To(BeTrue(), "the stale workload update should receive an API conflict")
		Expect(racingClient.updateAttempts).To(BeNumerically("<=", 5),
			"the production annotation retry is bounded to five attempts")
		Expect(racingClient.externalResourceVersion).NotTo(BeEmpty())
		Expect(deployment.ResourceVersion).NotTo(Equal(racingClient.externalResourceVersion),
			"the retry and subsequent recommendation write should advance the concurrent edit")
		Expect(deployment.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("user.example.com/preferred-node", "blue"),
			"annotation retry must start from and preserve the latest user-owned template field")
		Expect(deployment.Annotations).To(HaveKeyWithValue("user.example.com/concurrent-edit", "kept"))
		Expect(deployment.Annotations).To(HaveKeyWithValue(optipodv1alpha1.AnnotationPolicy, policyName))
		Expect(deployment.Annotations).To(HaveKeyWithValue("optipod.io/recommendation.app.cpu-request", "300m"))
		cpu, memory := containerRequest(deployment, "app")
		Expect(cpu).To(Equal("100m"), "the default webhook strategy must not rewrite the template request")
		Expect(memory).To(Equal("128Mi"))

		status := fetchModesPolicy(ctx, policyName)
		Expect(status.Status.WorkloadsDiscovered).To(Equal(1))
		Expect(status.Status.WorkloadsProcessed).To(Equal(1))
	})

	It("selects the current policy version when its mode changes during reconciliation", func() {
		ctx := context.Background()
		const (
			namespace      = "concurrent-policy-ns"
			deploymentName = "concurrent-policy-workload"
			policyName     = "concurrent-current-policy"
		)
		cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
		defer cleanup()
		createModesPolicy(ctx, policyName, namespace, nil)
		defer func() {
			policy := fetchModesPolicy(ctx, policyName)
			_ = k8sClient.Delete(ctx, policy)
		}()

		racingClient := &policyListMutationClient{
			Client: k8sClient,
			target: types.NamespacedName{Namespace: "default", Name: policyName},
			mutate: func(policy *optipodv1alpha1.OptimizationPolicy) {
				policy.Spec.Mode = optipodv1alpha1.ModeRecommend
				if policy.Annotations == nil {
					policy.Annotations = make(map[string]string)
				}
				policy.Annotations["user.example.com/concurrent-edit"] = "recommend"
			},
		}
		reconciler := newModesReconciler(modesProvider())
		reconciler.Client = racingClient

		reconcilePolicy(ctx, reconciler, policyName)

		Expect(racingClient.injectionErr).NotTo(HaveOccurred())
		Expect(racingClient.injected).To(BeTrue(), "policy edit should land between initial fetch and production selection")
		storedPolicy := fetchModesPolicy(ctx, policyName)
		Expect(storedPolicy.Spec.Mode).To(Equal(optipodv1alpha1.ModeRecommend))
		Expect(storedPolicy.Annotations).To(HaveKeyWithValue("user.example.com/concurrent-edit", "recommend"))
		Expect(storedPolicy.Status.WorkloadsProcessed).To(Equal(1))

		deployment := fetchDeployment(ctx, namespace, deploymentName)
		Expect(deployment.Annotations).To(HaveKeyWithValue(optipodv1alpha1.AnnotationPolicy, policyName))
		Expect(deployment.Annotations).To(HaveKeyWithValue("optipod.io/recommendation.app.cpu-request", "300m"))
		cpu, memory := containerRequest(deployment, "app")
		Expect(cpu).To(Equal("100m"), "the selected current Recommend policy must not use the stale Auto mode")
		Expect(memory).To(Equal("128Mi"))
	})

	It("routes a legacy policy with omitted optional fields through persisted defaults", func() {
		ctx := context.Background()
		const (
			namespace      = "legacy-policy-ns"
			deploymentName = "legacy-policy-workload"
			policyName     = "legacy-optional-policy"
		)
		cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
		defer cleanup()

		deployment := fetchDeployment(ctx, namespace, deploymentName)
		deployment.Annotations = map[string]string{"user.example.com/keep": "deployment"}
		deployment.Spec.Template.Annotations = map[string]string{"user.example.com/keep": "template"}
		deployment.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

		legacyPolicy := &optipodv1alpha1.OptimizationPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: "default"},
			Spec: optipodv1alpha1.OptimizationPolicySpec{
				Mode: optipodv1alpha1.ModeAuto,
				Selector: optipodv1alpha1.WorkloadSelector{
					Namespaces: &optipodv1alpha1.NamespaceFilter{Allow: []string{namespace}},
				},
				MetricsConfig: optipodv1alpha1.MetricsConfig{Provider: "prometheus"},
				ResourceBounds: optipodv1alpha1.ResourceBounds{
					CPU:    optipodv1alpha1.ResourceBound{Min: resource.MustParse("50m"), Max: resource.MustParse("2000m")},
					Memory: optipodv1alpha1.ResourceBound{Min: resource.MustParse("64Mi"), Max: resource.MustParse("4Gi")},
				},
				UpdateStrategy: optipodv1alpha1.UpdateStrategy{},
			},
		}
		Expect(k8sClient.Create(ctx, legacyPolicy)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, legacyPolicy) }()

		storedPolicy := fetchModesPolicy(ctx, policyName)
		Expect(storedPolicy.Spec.Weight).NotTo(BeNil())
		Expect(*storedPolicy.Spec.Weight).To(Equal(int32(100)))
		Expect(storedPolicy.Spec.Selector.WorkloadTypes).To(BeNil(), "omitted type filters remain compatible with all workload kinds")
		Expect(storedPolicy.Spec.UpdateStrategy.Strategy).NotTo(BeNil())
		Expect(*storedPolicy.Spec.UpdateStrategy.Strategy).To(Equal(string(optipodv1alpha1.StrategyWebhook)))
		Expect(storedPolicy.GetStrategy()).To(Equal(optipodv1alpha1.StrategyWebhook))
		Expect(storedPolicy.Spec.UpdateStrategy.RolloutStrategy).NotTo(BeNil())
		Expect(storedPolicy.GetRolloutStrategy()).To(Equal(optipodv1alpha1.RolloutOnNextRestart))
		Expect(storedPolicy.Spec.UpdateStrategy.UpdateRequestsOnly).To(BeTrue())
		Expect(storedPolicy.Spec.UpdateStrategy.LimitConfig).To(BeNil())
		Expect(storedPolicy.Spec.UpdateStrategy.GradualDecreaseConfig).To(BeNil())
		Expect(storedPolicy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease).NotTo(BeNil())
		Expect(*storedPolicy.Spec.UpdateStrategy.AllowUnsafeMemoryDecrease).To(BeFalse())
		Expect(storedPolicy.GetWeight()).To(Equal(int32(100)))

		reconciler := newModesReconciler(modesProvider())
		reconcilePolicy(ctx, reconciler, policyName)

		result := fetchDeployment(ctx, namespace, deploymentName)
		status := fetchModesPolicy(ctx, policyName)
		Expect(status.Status.WorkloadsDiscovered).To(Equal(1))
		Expect(status.Status.WorkloadsProcessed).To(Equal(1))
		Expect(result.Annotations).To(HaveKeyWithValue("user.example.com/keep", "deployment"))
		Expect(result.Spec.Template.Annotations).To(HaveKeyWithValue("user.example.com/keep", "template"))
		Expect(result.Annotations).To(HaveKeyWithValue(optipodv1alpha1.AnnotationStrategy, string(optipodv1alpha1.StrategyWebhook)))
		Expect(result.Annotations).To(HaveKeyWithValue("optipod.io/recommendation.app.cpu-request", "360m"),
			"the schema-persisted default safety factor is consumed by the production recommendation path")
		cpu, memory := containerRequest(result, "app")
		Expect(cpu).To(Equal("100m"), "legacy default webhook routing must not alter existing requests")
		Expect(memory).To(Equal("128Mi"))
		container := result.Spec.Template.Spec.Containers[0]
		Expect(container.Resources.Limits.Cpu().String()).To(Equal("500m"))
		Expect(container.Resources.Limits.Memory().String()).To(Equal("1Gi"))
	})
})

func policyCondition(policy *optipodv1alpha1.OptimizationPolicy, conditionType string) string {
	for _, condition := range policy.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Reason
		}
	}
	return ""
}

type statusConflictClient struct {
	client.Client
	target                  types.NamespacedName
	mu                      sync.Mutex
	injected                bool
	conflictObserved        bool
	externalResourceVersion string
	injectionErr            error
}

func (c *statusConflictClient) Status() client.SubResourceWriter {
	return &statusConflictWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

type statusConflictWriter struct {
	client.SubResourceWriter
	owner *statusConflictClient
}

func (w *statusConflictWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	owner := w.owner
	owner.mu.Lock()
	shouldInject := !owner.injected && client.ObjectKeyFromObject(obj) == owner.target
	if shouldInject {
		owner.injected = true
	}
	owner.mu.Unlock()

	if !shouldInject {
		return w.SubResourceWriter.Update(ctx, obj, opts...)
	}

	latest := &optipodv1alpha1.OptimizationPolicy{}
	if err := owner.Get(ctx, owner.target, latest); err != nil {
		owner.setInjectionError(err)
		return err
	}
	latest.Status.Conditions = append(latest.Status.Conditions, metav1.Condition{
		Type:               "ConcurrentWriter",
		Status:             metav1.ConditionTrue,
		Reason:             "Preserved",
		Message:            "written by an independent status editor",
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	if err := owner.Client.Status().Update(ctx, latest); err != nil {
		owner.setInjectionError(err)
		return err
	}
	owner.mu.Lock()
	owner.externalResourceVersion = latest.ResourceVersion
	owner.mu.Unlock()

	err := w.SubResourceWriter.Update(ctx, obj, opts...)
	if apierrors.IsConflict(err) {
		owner.mu.Lock()
		owner.conflictObserved = true
		owner.mu.Unlock()
	}
	return err
}

func (c *statusConflictClient) setInjectionError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.injectionErr = err
}

type workloadConflictClient struct {
	client.Client
	target                  types.NamespacedName
	mu                      sync.Mutex
	injected                bool
	conflictObserved        bool
	updateAttempts          int
	externalResourceVersion string
}

func (c *workloadConflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if deployment, ok := obj.(*appsv1.Deployment); ok &&
		client.ObjectKeyFromObject(deployment) == c.target {
		c.mu.Lock()
		c.updateAttempts++
		shouldInject := !c.injected
		if shouldInject {
			c.injected = true
		}
		c.mu.Unlock()

		if shouldInject {
			latest := &appsv1.Deployment{}
			if err := c.Get(ctx, c.target, latest); err != nil {
				return err
			}
			latest.Spec.Template.Spec.NodeSelector = map[string]string{"user.example.com/preferred-node": "blue"}
			if latest.Annotations == nil {
				latest.Annotations = make(map[string]string)
			}
			latest.Annotations["user.example.com/concurrent-edit"] = "kept"
			if err := c.Client.Update(ctx, latest); err != nil {
				return err
			}
			c.mu.Lock()
			c.externalResourceVersion = latest.ResourceVersion
			c.mu.Unlock()
		}
	}

	err := c.Client.Update(ctx, obj, opts...)
	if apierrors.IsConflict(err) {
		if deployment, ok := obj.(*appsv1.Deployment); ok &&
			client.ObjectKeyFromObject(deployment) == c.target {
			c.mu.Lock()
			c.conflictObserved = true
			c.mu.Unlock()
		}
	}
	return err
}

type policyListMutationClient struct {
	client.Client
	target       types.NamespacedName
	mutate       func(*optipodv1alpha1.OptimizationPolicy)
	injected     bool
	injectionErr error
}

func (c *policyListMutationClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, isPolicyList := list.(*optipodv1alpha1.OptimizationPolicyList); isPolicyList && !c.injected {
		c.injected = true
		policy := &optipodv1alpha1.OptimizationPolicy{}
		if err := c.Get(ctx, c.target, policy); err != nil {
			c.injectionErr = err
			return err
		}
		c.mutate(policy)
		if err := c.Update(ctx, policy); err != nil {
			c.injectionErr = err
			return err
		}
	}
	return c.Client.List(ctx, list, opts...)
}
