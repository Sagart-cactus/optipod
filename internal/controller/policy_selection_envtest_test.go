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
	"k8s.io/apimachinery/pkg/api/resource"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// These specs prove winning-policy selection and conflicts through the
// production reconciler on the suite's envtest API server (VAL-FAST-004): the
// observed winner's effect on the workload (applied request values and the
// per-policy processed counts) before and after weight edits and winner
// deletion, including a Recommend-vs-Auto mode conflict. The winner identity
// is observed through its resulting workload effect, not only stored fixture
// fields.

var _ = Describe("Winning policy selection and conflicts through the reconciler", func() {
	Context("Two Auto policies with distinct bounds", func() {
		const (
			namespace      = "winner-weight-ns"
			deploymentName = "winner-weight-deploy"
			highPolicy     = "winner-high"
			lowPolicy      = "winner-low"
		)

		It("applies the highest-weight policy and flips after weight edit and winner deletion", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			// winner-high allows the raw P90 (300m); winner-low clamps to 250m.
			createModesPolicy(ctx, highPolicy, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				w := int32(500)
				p.Spec.Weight = &w
			})
			createModesPolicy(ctx, lowPolicy, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				w := int32(10)
				p.Spec.Weight = &w
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("250m")
			})

			By("the highest-weight policy wins and its effect is applied")
			reconcilePolicy(ctx, reconciler, highPolicy)

			expectPolicyStatusEventually(ctx, highPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 1
			}, "the winner should process the workload")

			deployment := fetchDeployment(ctx, namespace, deploymentName)
			Expect(deployment.Annotations).NotTo(BeNil())
			Expect(deployment.Annotations[optipodv1alpha1.AnnotationPolicy]).To(Equal(highPolicy))
			Expect(deployment.Annotations["optipod.io/recommendation.app.cpu-request"]).To(Equal("300m"))
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("300m"))
			Expect(memory).To(Equal("512Mi"))

			By("the losing policy's reconcile observes the winner and processes nothing")
			reconcilePolicy(ctx, reconciler, lowPolicy)
			expectPolicyStatusEventually(ctx, lowPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 0
			}, "the loser must leave the workload to the winner")
			cpu, _ = containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
			Expect(cpu).To(Equal("300m"), "the losing policy must not apply its value")

			By("editing the winner's weight flips the applied effect")
			policy := fetchModesPolicy(ctx, highPolicy)
			newWeight := int32(5)
			policy.Spec.Weight = &newWeight
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			reconcilePolicy(ctx, reconciler, lowPolicy)
			Eventually(func() string {
				cpu, _ := containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
				return cpu
			}, modesTimeout, 250*time.Millisecond).Should(Equal("250m"), "the new winner must apply its own clamped value")
			expectPolicyStatusEventually(ctx, lowPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsProcessed == 1
			}, "the new winner should process the workload")

			By("deleting the current winner flips the effect back to the remaining policy")
			low := fetchModesPolicy(ctx, lowPolicy)
			Expect(k8sClient.Delete(ctx, low)).To(Succeed())

			reconcilePolicy(ctx, reconciler, highPolicy)
			Eventually(func() string {
				cpu, _ := containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
				return cpu
			}, modesTimeout, 250*time.Millisecond).Should(Equal("300m"), "the remaining policy must win after the winner is deleted")
			expectPolicyStatusEventually(ctx, highPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsProcessed == 1
			}, "the remaining policy should process the workload")
		})
	})

	Context("A Recommend policy versus an Auto policy", func() {
		const (
			namespace       = "winner-conflict-ns"
			deploymentName  = "winner-conflict-deploy"
			recommendPolicy = "conflict-recommend"
			autoPolicy      = "conflict-auto"
		)

		It("the higher-weight Recommend policy blocks application until weights change", func() {
			ctx := context.Background()
			provider := modesProvider()
			reconciler := newModesReconciler(provider)
			cleanup := createModesWorkloadFixture(ctx, namespace, deploymentName)
			defer cleanup()

			// conflict-recommend annotates the raw P90 (300m) but never applies;
			// conflict-auto clamps to 180m and applies under an explicit SSA
			// strategy.
			createModesPolicy(ctx, recommendPolicy, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				w := int32(900)
				p.Spec.Weight = &w
				p.Spec.Mode = optipodv1alpha1.ModeRecommend
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("350m")
			})
			createModesPolicy(ctx, autoPolicy, namespace, func(p *optipodv1alpha1.OptimizationPolicy) {
				w := int32(10)
				p.Spec.Weight = &w
				p.Spec.ResourceBounds.CPU.Max = resource.MustParse("180m")
			})

			By("the Recommend winner records recommendations without applying")
			reconcilePolicy(ctx, reconciler, recommendPolicy)
			expectPolicyStatusEventually(ctx, recommendPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 1
			}, "the Recommend winner should process the workload")

			deployment := fetchDeployment(ctx, namespace, deploymentName)
			Expect(deployment.Annotations).NotTo(BeNil())
			Expect(deployment.Annotations[optipodv1alpha1.AnnotationPolicy]).To(Equal(recommendPolicy))
			Expect(deployment.Annotations["optipod.io/recommendation.app.cpu-request"]).To(Equal("300m"))
			cpu, memory := containerRequest(deployment, "app")
			Expect(cpu).To(Equal("100m"), "Recommend mode must not apply")
			Expect(memory).To(Equal("128Mi"))

			By("the Auto loser's reconcile is blocked by the Recommend winner")
			reconcilePolicy(ctx, reconciler, autoPolicy)
			expectPolicyStatusEventually(ctx, autoPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsDiscovered == 1 && p.Status.WorkloadsProcessed == 0
			}, "the Auto loser must not process the winner's workload")
			cpu, _ = containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
			Expect(cpu).To(Equal("100m"), "the blocked Auto policy must not apply")

			By("changing the weights lets Auto win and its effect appears")
			policy := fetchModesPolicy(ctx, recommendPolicy)
			newWeight := int32(5)
			policy.Spec.Weight = &newWeight
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			reconcilePolicy(ctx, reconciler, autoPolicy)
			Eventually(func() string {
				cpu, _ := containerRequest(fetchDeployment(ctx, namespace, deploymentName), "app")
				return cpu
			}, modesTimeout, 250*time.Millisecond).Should(Equal("180m"), "the Auto policy must apply its clamped value once it wins")
			expectPolicyStatusEventually(ctx, autoPolicy, func(p *optipodv1alpha1.OptimizationPolicy) bool {
				return p.Status.WorkloadsProcessed == 1
			}, "the Auto policy should process the workload after winning")
		})
	})
})
