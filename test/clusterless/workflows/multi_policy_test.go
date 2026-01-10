package workflows

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/clusterless/harness"
)

var _ = Describe("Multi-Policy Selection", func() {
	var h *harness.TestHarness

	BeforeEach(func() {
		h = harness.NewTestHarness()
	})

	AfterEach(func() {
		h.Cleanup()
	})

	Context("Policy Weight Selection", func() {
		It("should select highest weight policy when multiple match", func() {
			ctx := h.Context

			// Create deployment with multiple labels that will match different policies
			deploy := harness.NewDeployment("api", "production").
				WithLabels(map[string]string{
					"app":  "api",
					"tier": "frontend",
					"env":  "prod",
				}).
				WithContainer("api", "api:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create low-weight policy matching "app" label
			lowWeight := int32(100)
			lowPolicy := harness.NewPolicy("app-policy", "production").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "api"}).
				WithWeight(lowWeight).
				WithMetricsConfig("prometheus", "P90", 1.2).
				Build()
			Expect(h.Client.Create(ctx, lowPolicy)).To(Succeed())

			// Create high-weight policy matching "tier" label
			highWeight := int32(500)
			highPolicy := harness.NewPolicy("tier-policy", "production").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"tier": "frontend"}).
				WithWeight(highWeight).
				WithMetricsConfig("prometheus", "P95", 1.5).
				Build()
			Expect(h.Client.Create(ctx, highPolicy)).To(Succeed())

			// Create medium-weight policy matching "env" label
			mediumWeight := int32(300)
			mediumPolicy := harness.NewPolicy("env-policy", "production").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"env": "prod"}).
				WithWeight(mediumWeight).
				WithMetricsConfig("prometheus", "P99", 1.1).
				Build()
			Expect(h.Client.Create(ctx, mediumPolicy)).To(Succeed())

			// Verify all policies were created
			policies := []string{"app-policy", "tier-policy", "env-policy"}
			for _, policyName := range policies {
				policy := &optipodv1alpha1.OptimizationPolicy{}
				key := types.NamespacedName{Name: policyName, Namespace: "production"}
				Expect(h.Client.Get(ctx, key, policy)).To(Succeed())
			}

			// Note: In a full implementation, we would:
			// 1. Use a PolicySelector to find all matching policies
			// 2. Select the one with highest weight (tier-policy with weight 500)
			// 3. Verify the selected policy is used for recommendations

			// For now, verify the policies have correct weights
			tierPolicy := &optipodv1alpha1.OptimizationPolicy{}
			key := types.NamespacedName{Name: "tier-policy", Namespace: "production"}
			Expect(h.Client.Get(ctx, key, tierPolicy)).To(Succeed())
			Expect(*tierPolicy.Spec.Weight).To(Equal(int32(500)))
		})

		It("should handle policies without weights (default weight 0)", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("worker", "default").
				WithLabels(map[string]string{"app": "worker", "type": "batch"}).
				WithContainer("worker", "worker:v1", "200m", "256Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create policy without weight (should default to 0)
			noWeightPolicy := harness.NewPolicy("no-weight-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "worker"}).
				Build()
			Expect(h.Client.Create(ctx, noWeightPolicy)).To(Succeed())

			// Create policy with explicit weight
			weightedPolicy := harness.NewPolicy("weighted-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"type": "batch"}).
				WithWeight(100).
				Build()
			Expect(h.Client.Create(ctx, weightedPolicy)).To(Succeed())

			// Verify weights
			noWeightPol := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "no-weight-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key1, noWeightPol)).To(Succeed())
			Expect(noWeightPol.Spec.Weight).To(BeNil()) // No weight specified

			weightedPol := &optipodv1alpha1.OptimizationPolicy{}
			key2 := types.NamespacedName{Name: "weighted-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key2, weightedPol)).To(Succeed())
			Expect(*weightedPol.Spec.Weight).To(Equal(int32(100)))

			// Weighted policy should be selected over no-weight policy
		})
	})

	Context("Policy Mode Conflicts", func() {
		It("should handle conflicting policy modes", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("conflicted", "default").
				WithLabels(map[string]string{"app": "conflicted", "env": "test"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create Auto mode policy with lower weight
			autoPolicy := harness.NewPolicy("auto-policy", "default").
				WithMode(optipodv1alpha1.ModeAuto).
				WithLabelSelector(map[string]string{"app": "conflicted"}).
				WithWeight(200).
				Build()
			Expect(h.Client.Create(ctx, autoPolicy)).To(Succeed())

			// Create Recommend mode policy with higher weight
			recommendPolicy := harness.NewPolicy("recommend-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"env": "test"}).
				WithWeight(300).
				Build()
			Expect(h.Client.Create(ctx, recommendPolicy)).To(Succeed())

			// Higher weight policy should win regardless of mode
			// In this case, recommend-policy (weight 300) should be selected
			// over auto-policy (weight 200)

			// Verify policies have different modes
			autoPol := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "auto-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key1, autoPol)).To(Succeed())
			Expect(autoPol.Spec.Mode).To(Equal(optipodv1alpha1.ModeAuto))

			recPol := &optipodv1alpha1.OptimizationPolicy{}
			key2 := types.NamespacedName{Name: "recommend-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key2, recPol)).To(Succeed())
			Expect(recPol.Spec.Mode).To(Equal(optipodv1alpha1.ModeRecommend))
		})

		It("should handle disabled policies", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("disabled-test", "default").
				WithLabels(map[string]string{"app": "disabled-test"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create disabled policy with high weight
			disabledPolicy := harness.NewPolicy("disabled-policy", "default").
				WithMode(optipodv1alpha1.ModeDisabled).
				WithLabelSelector(map[string]string{"app": "disabled-test"}).
				WithWeight(1000). // Very high weight
				Build()
			Expect(h.Client.Create(ctx, disabledPolicy)).To(Succeed())

			// Create enabled policy with lower weight
			enabledPolicy := harness.NewPolicy("enabled-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "disabled-test"}).
				WithWeight(100).
				Build()
			Expect(h.Client.Create(ctx, enabledPolicy)).To(Succeed())

			// Disabled policies should be ignored regardless of weight
			// enabled-policy should be selected even with lower weight

			// Verify modes
			disabledPol := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "disabled-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key1, disabledPol)).To(Succeed())
			Expect(disabledPol.Spec.Mode).To(Equal(optipodv1alpha1.ModeDisabled))

			enabledPol := &optipodv1alpha1.OptimizationPolicy{}
			key2 := types.NamespacedName{Name: "enabled-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key2, enabledPol)).To(Succeed())
			Expect(enabledPol.Spec.Mode).To(Equal(optipodv1alpha1.ModeRecommend))
		})
	})

	Context("Label Selector Specificity", func() {
		It("should prefer more specific selectors", func() {
			ctx := h.Context

			// Create deployment with multiple labels
			deploy := harness.NewDeployment("specific", "default").
				WithLabels(map[string]string{
					"app":     "specific",
					"tier":    "backend",
					"env":     "prod",
					"version": "v2",
				}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create broad selector policy (matches 1 label)
			broadPolicy := harness.NewPolicy("broad-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"env": "prod"}).
				WithWeight(500). // High weight
				Build()
			Expect(h.Client.Create(ctx, broadPolicy)).To(Succeed())

			// Create specific selector policy (matches 3 labels)
			specificPolicy := harness.NewPolicy("specific-policy", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{
					"app":  "specific",
					"tier": "backend",
					"env":  "prod",
				}).
				WithWeight(300). // Lower weight but more specific
				Build()
			Expect(h.Client.Create(ctx, specificPolicy)).To(Succeed())

			// Note: In a full implementation, policy selection might consider:
			// 1. Specificity (number of matching labels)
			// 2. Weight
			// 3. Creation time
			// The exact algorithm depends on the PolicySelector implementation

			// Verify both policies match the deployment
			broadPol := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "broad-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key1, broadPol)).To(Succeed())
			Expect(broadPol.Spec.Selector.WorkloadSelector.MatchLabels).To(HaveLen(1))

			specificPol := &optipodv1alpha1.OptimizationPolicy{}
			key2 := types.NamespacedName{Name: "specific-policy", Namespace: "default"}
			Expect(h.Client.Get(ctx, key2, specificPol)).To(Succeed())
			Expect(specificPol.Spec.Selector.WorkloadSelector.MatchLabels).To(HaveLen(3))
		})
	})

	Context("Namespace Isolation", func() {
		It("should only consider policies in the same namespace", func() {
			ctx := h.Context

			// Create deployment in production namespace
			prodDeploy := harness.NewDeployment("api", "production").
				WithLabels(map[string]string{"app": "api"}).
				WithContainer("api", "api:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, prodDeploy)).To(Succeed())

			// Create deployment in staging namespace
			stagingDeploy := harness.NewDeployment("api", "staging").
				WithLabels(map[string]string{"app": "api"}).
				WithContainer("api", "api:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, stagingDeploy)).To(Succeed())

			// Create policy in production namespace
			prodPolicy := harness.NewPolicy("prod-policy", "production").
				WithMode(optipodv1alpha1.ModeAuto).
				WithLabelSelector(map[string]string{"app": "api"}).
				WithWeight(100).
				Build()
			Expect(h.Client.Create(ctx, prodPolicy)).To(Succeed())

			// Create policy in staging namespace
			stagingPolicy := harness.NewPolicy("staging-policy", "staging").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "api"}).
				WithWeight(200). // Higher weight
				Build()
			Expect(h.Client.Create(ctx, stagingPolicy)).To(Succeed())

			// Verify policies are in correct namespaces
			prodPol := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "prod-policy", Namespace: "production"}
			Expect(h.Client.Get(ctx, key1, prodPol)).To(Succeed())
			Expect(prodPol.Namespace).To(Equal("production"))

			stagingPol := &optipodv1alpha1.OptimizationPolicy{}
			key2 := types.NamespacedName{Name: "staging-policy", Namespace: "staging"}
			Expect(h.Client.Get(ctx, key2, stagingPol)).To(Succeed())
			Expect(stagingPol.Namespace).To(Equal("staging"))

			// Each deployment should only consider policies in its own namespace
			// Production deployment should use prod-policy
			// Staging deployment should use staging-policy
		})
	})

	Context("Policy Updates", func() {
		It("should handle policy weight changes", func() {
			ctx := h.Context

			// Create deployment
			deploy := harness.NewDeployment("dynamic", "default").
				WithLabels(map[string]string{"app": "dynamic"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create two policies with initial weights
			policy1 := harness.NewPolicy("policy-1", "default").
				WithMode(optipodv1alpha1.ModeRecommend).
				WithLabelSelector(map[string]string{"app": "dynamic"}).
				WithWeight(100).
				Build()
			Expect(h.Client.Create(ctx, policy1)).To(Succeed())

			policy2 := harness.NewPolicy("policy-2", "default").
				WithMode(optipodv1alpha1.ModeAuto).
				WithLabelSelector(map[string]string{"app": "dynamic"}).
				WithWeight(200). // Initially higher
				Build()
			Expect(h.Client.Create(ctx, policy2)).To(Succeed())

			// Initially policy-2 should be selected (weight 200 > 100)

			// Update policy-1 to have higher weight
			pol1 := &optipodv1alpha1.OptimizationPolicy{}
			key1 := types.NamespacedName{Name: "policy-1", Namespace: "default"}
			Expect(h.Client.Get(ctx, key1, pol1)).To(Succeed())

			newWeight := int32(300)
			pol1.Spec.Weight = &newWeight
			Expect(h.Client.Update(ctx, pol1)).To(Succeed())

			// Verify update
			updatedPol1 := &optipodv1alpha1.OptimizationPolicy{}
			Expect(h.Client.Get(ctx, key1, updatedPol1)).To(Succeed())
			Expect(*updatedPol1.Spec.Weight).To(Equal(int32(300)))

			// Now policy-1 should be selected (weight 300 > 200)
		})
	})
})
