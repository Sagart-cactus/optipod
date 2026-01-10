package workflows

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/optipod/optipod/test/clusterless/harness"
	"github.com/optipod/optipod/test/clusterless/simulators"
)

var _ = Describe("Webhook Mutations", func() {
	var h *harness.TestHarness
	var webhookSim *simulators.WebhookSimulator

	BeforeEach(func() {
		h = harness.NewTestHarness()
		webhookSim = simulators.NewWebhookSimulator(h)
	})

	AfterEach(func() {
		h.Cleanup()
	})

	Context("Pod Mutation", func() {
		It("should mutate pod based on recommendation annotations", func() {
			ctx := h.Context

			// Create deployment with OptiPod annotations
			deploy := harness.NewDeployment("nginx", "default").
				WithAnnotations(map[string]string{
					"optipod.io/webhook-enabled":      "true",
					"optipod.io/cpu-request.nginx":    "300m",
					"optipod.io/memory-request.nginx": "512Mi",
					"optipod.io/cpu-limit.nginx":      "600m",
					"optipod.io/memory-limit.nginx":   "1Gi",
				}).
				WithContainer("nginx", "nginx:latest", "100m", "128Mi").
				Build()
			Expect(h.Client.Create(ctx, deploy)).To(Succeed())

			// Create pod with same annotations (simulating controller creating pod)
			pod := harness.NewPod("nginx-pod-123", "default").
				WithAnnotations(deploy.Annotations).
				WithLabels(map[string]string{"app": "nginx"}).
				WithContainer("nginx", "nginx:latest", "100m", "128Mi").
				Build()

			// Simulate admission request
			response := webhookSim.MutatePod(pod, false)

			// Verify admission allowed
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Result.Code).To(Equal(int32(200)))

			// Extract patches from response
			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())

			// Verify patches generated (4 patches: CPU request, mem request, CPU limit, mem limit)
			Expect(patches).To(HaveLen(4))

			// Verify patch content
			patchPaths := make([]string, len(patches))
			patchValues := make(map[string]interface{})
			for i, patch := range patches {
				patchPaths[i] = patch.Path
				patchValues[patch.Path] = patch.Value
			}

			// Check all expected patches are present
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/requests/cpu"))
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/requests/memory"))
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/limits/cpu"))
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/limits/memory"))

			// Verify patch values
			Expect(patchValues["/spec/containers/0/resources/requests/cpu"]).To(Equal("300m"))
			Expect(patchValues["/spec/containers/0/resources/requests/memory"]).To(Equal("512Mi"))
			Expect(patchValues["/spec/containers/0/resources/limits/cpu"]).To(Equal("600m"))
			Expect(patchValues["/spec/containers/0/resources/limits/memory"]).To(Equal("1Gi"))
		})

		It("should not mutate pod when webhook is disabled", func() {
			// Create pod without webhook-enabled annotation
			pod := harness.NewPod("no-webhook-pod", "default").
				WithLabels(map[string]string{"app": "test"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// Simulate admission request
			response := webhookSim.MutatePod(pod, false)

			// Verify admission allowed but no patches
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Patch).To(BeNil())

			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())
			Expect(patches).To(BeEmpty())
		})

		It("should handle partial annotations", func() {
			// Create pod with only CPU request annotation
			pod := harness.NewPod("partial-pod", "default").
				WithAnnotations(map[string]string{
					"optipod.io/webhook-enabled": "true",
					"optipod.io/cpu-request.app": "250m",
					// Missing memory request, CPU limit, memory limit
				}).
				WithLabels(map[string]string{"app": "partial"}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// Simulate admission request
			response := webhookSim.MutatePod(pod, false)

			// Verify admission allowed
			Expect(response.Allowed).To(BeTrue())

			// Extract patches
			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())

			// Should have only 1 patch for CPU request
			Expect(patches).To(HaveLen(1))
			Expect(patches[0].Path).To(Equal("/spec/containers/0/resources/requests/cpu"))
			Expect(patches[0].Value).To(Equal("250m"))
		})

		It("should handle multiple containers", func() {
			// Create pod with multiple containers
			pod := harness.NewPod("multi-container-pod", "default").
				WithAnnotations(map[string]string{
					"optipod.io/webhook-enabled":         "true",
					"optipod.io/cpu-request.frontend":    "200m",
					"optipod.io/memory-request.frontend": "256Mi",
					"optipod.io/cpu-request.sidecar":     "50m",
					"optipod.io/memory-request.sidecar":  "64Mi",
				}).
				WithLabels(map[string]string{"app": "multi"}).
				WithContainer("frontend", "frontend:v1", "100m", "128Mi").
				Build()

			// Add second container manually
			pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
				Name:  "sidecar",
				Image: "sidecar:v1",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
			})

			// Simulate admission request
			response := webhookSim.MutatePod(pod, false)

			// Verify admission allowed
			Expect(response.Allowed).To(BeTrue())

			// Extract patches
			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())

			// Should have 4 patches (2 per container)
			Expect(patches).To(HaveLen(4))

			// Verify patches for both containers
			patchPaths := make([]string, len(patches))
			for i, patch := range patches {
				patchPaths[i] = patch.Path
			}

			// Frontend container (index 0)
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/requests/cpu"))
			Expect(patchPaths).To(ContainElement("/spec/containers/0/resources/requests/memory"))

			// Sidecar container (index 1)
			Expect(patchPaths).To(ContainElement("/spec/containers/1/resources/requests/cpu"))
			Expect(patchPaths).To(ContainElement("/spec/containers/1/resources/requests/memory"))
		})
	})

	Context("Patch Application", func() {
		It("should apply patches correctly to pod", func() {
			// Create original pod
			originalPod := harness.NewPod("patch-test-pod", "default").
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// Create patches
			patches := []simulators.PatchOperation{
				{
					Op:    "replace",
					Path:  "/spec/containers/0/resources/requests/cpu",
					Value: "300m",
				},
				{
					Op:    "replace",
					Path:  "/spec/containers/0/resources/requests/memory",
					Value: "512Mi",
				},
			}

			// Apply patches
			patchedPod, err := webhookSim.ApplyPatchesToPod(originalPod, patches)
			Expect(err).NotTo(HaveOccurred())

			// Verify original pod unchanged
			originalContainer := originalPod.Spec.Containers[0]
			Expect(originalContainer.Resources.Requests.Cpu().String()).To(Equal("100m"))
			Expect(originalContainer.Resources.Requests.Memory().String()).To(Equal("128Mi"))

			// Verify patched pod has new values
			patchedContainer := patchedPod.Spec.Containers[0]
			Expect(patchedContainer.Resources.Requests.Cpu().String()).To(Equal("300m"))
			Expect(patchedContainer.Resources.Requests.Memory().String()).To(Equal("512Mi"))
		})

		It("should extract resources from patches", func() {
			patches := []simulators.PatchOperation{
				{
					Op:    "replace",
					Path:  "/spec/containers/0/resources/requests/cpu",
					Value: "400m",
				},
				{
					Op:    "replace",
					Path:  "/spec/containers/0/resources/requests/memory",
					Value: "768Mi",
				},
				{
					Op:    "replace",
					Path:  "/spec/containers/0/resources/limits/cpu",
					Value: "800m",
				},
				{
					Op:    "replace",
					Path:  "/spec/containers/1/resources/requests/cpu",
					Value: "100m",
				},
			}

			resources, err := webhookSim.ExtractResourcesFromPatches(patches)
			Expect(err).NotTo(HaveOccurred())

			// Should have resources for 2 containers
			Expect(resources).To(HaveLen(2))

			// Container 0 resources
			container0 := resources["container-0"]
			Expect(container0.Requests.Cpu().String()).To(Equal("400m"))
			Expect(container0.Requests.Memory().String()).To(Equal("768Mi"))
			Expect(container0.Limits.Cpu().String()).To(Equal("800m"))

			// Container 1 resources
			container1 := resources["container-1"]
			Expect(container1.Requests.Cpu().String()).To(Equal("100m"))
		})
	})

	Context("Error Handling", func() {
		It("should handle invalid resource values", func() {
			pod := harness.NewPod("invalid-pod", "default").
				WithAnnotations(map[string]string{
					"optipod.io/webhook-enabled": "true",
					"optipod.io/cpu-request.app": "invalid-cpu",
				}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// This should not fail at patch generation, but would fail at application
			response := webhookSim.MutatePod(pod, false)
			Expect(response.Allowed).To(BeTrue())

			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())
			Expect(patches).To(HaveLen(1))

			// Applying the patch should fail
			_, err = webhookSim.ApplyPatchesToPod(pod, patches)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse resource value"))
		})

		It("should handle missing containers", func() {
			patches := []simulators.PatchOperation{
				{
					Op:    "replace",
					Path:  "/spec/containers/5/resources/requests/cpu", // Container 5 doesn't exist
					Value: "100m",
				},
			}

			pod := harness.NewPod("single-container-pod", "default").
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// Should fail when applying patch to non-existent container
			_, err := webhookSim.ApplyPatchesToPod(pod, patches)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("container index 5 out of range"))
		})
	})

	Context("Dry Run", func() {
		It("should handle dry run requests", func() {
			pod := harness.NewPod("dry-run-pod", "default").
				WithAnnotations(map[string]string{
					"optipod.io/webhook-enabled": "true",
					"optipod.io/cpu-request.app": "200m",
				}).
				WithContainer("app", "app:v1", "100m", "128Mi").
				Build()

			// Simulate dry run admission request
			response := webhookSim.MutatePod(pod, true)

			// Should still generate patches for dry run
			Expect(response.Allowed).To(BeTrue())

			patches, err := webhookSim.GetPatchesFromResponse(response)
			Expect(err).NotTo(HaveOccurred())
			Expect(patches).To(HaveLen(1))
			Expect(patches[0].Value).To(Equal("200m"))
		})
	})
})
