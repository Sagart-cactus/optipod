package suite

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/application"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/recommendation"
	"github.com/optipod/optipod/test/e2e/framework"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type conflictRoundTrip func(*http.Request) (*http.Response, error)

func (f conflictRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var _ = DescribeTable("SSA retries after a competing workload write", Label("ssa", "version-conflict"),
	func(ctx context.Context, concurrentWrite string) {
		ns := specNamespace(ctx, "ssa-version-conflict")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		workload := framework.NewDeploymentFixture(ns.Name, "worker", 1, framework.ContainerFixture{Name: "app", CPURequest: "50m", MemoryRequest: "64Mi"})
		workload.Spec.Template.Spec.Containers[0].Resources.Requests = nil
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed())
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, "worker", framework.WaitOptions{Timeout: 2 * time.Minute})).To(Succeed())
		policy := framework.NewPolicyFixture(ns.Name, "scope", api.ModeAuto, framework.WithStrategy("ssa", ""))
		policy.Spec.UpdateStrategy.AllowFieldOwnershipTakeover = false
		policy.Spec.UpdateStrategy.UpdateRequestsOnly = true
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed())
		cfg := rest.CopyConfig(env.Clients.RESTConfig())
		apiClient := env.Clients.Kube.AppsV1().Deployments(ns.Name)
		var responses []int
		cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
			return conflictRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/deployments/worker") {
					return next.RoundTrip(r)
				}
				if len(responses) == 0 || concurrentWrite == "continuous" {
					key := "testing.optipod.io/concurrent-write"
					if concurrentWrite == "pause" {
						key = history.Paused
					}
					patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%d"}}}`, key, len(responses)))
					if concurrentWrite == "pause" {
						patch = []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"true"}}}`, key))
					}
					if concurrentWrite == "spec" {
						patch = []byte(`{"spec":{"minReadySeconds":7}}`)
					}
					if _, err := apiClient.Patch(ctx, "worker", types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
						return nil, err
					}
				}
				resp, err := next.RoundTrip(r)
				if resp != nil {
					responses = append(responses, resp.StatusCode)
				}
				return resp, err
			})
		})
		dyn, err := dynamic.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workload)
		Expect(err).NotTo(HaveOccurred())
		engine := application.NewEngine(env.Clients.Ctrl, dyn, env.Clients.Kube.Discovery(), false)
		result, err := engine.Apply(ctx, &application.Workload{Name: "worker", Namespace: ns.Name, Kind: "Deployment", Object: &unstructured.Unstructured{Object: obj}}, []application.ContainerRecommendation{{Container: "app", Recommendation: &recommendation.Recommendation{CPU: resource.MustParse("80m"), Memory: resource.MustParse("96Mi")}}}, policy)
		live, readErr := apiClient.Get(ctx, "worker", metav1.GetOptions{})
		Expect(readErr).NotTo(HaveOccurred())
		requests := live.Spec.Template.Spec.Containers[0].Resources.Requests
		if concurrentWrite == "pause" || concurrentWrite == "spec" {
			reason := "workload is paused"
			if concurrentWrite == "spec" {
				reason = "workload spec changed"
			}
			Expect(err).To(MatchError(ContainSubstring(reason)))
			Expect(responses).To(Equal([]int{http.StatusConflict}))
			Expect(requests).To(BeEmpty())
			return
		}
		if concurrentWrite == "continuous" {
			Expect(err).To(MatchError(ContainSubstring("SSA conflict on workload version")))
			Expect(responses).To(Equal([]int{409, 409, 409, 409}))
			Expect(requests).To(BeEmpty())
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Method).To(Equal("ServerSideApply"))
		Expect(responses[0]).To(Equal(http.StatusConflict))
		Expect(responses[len(responses)-1]).To(Equal(http.StatusOK))
		Expect(requests.Cpu().String()).To(Equal("80m"))
		Expect(requests.Memory().String()).To(Equal("96Mi"))
		Expect(live.Annotations["testing.optipod.io/concurrent-write"]).To(Equal("0"))
	}, Entry("refreshes the version after a metadata write", "metadata"), Entry("honors a pause introduced before the retry", "pause"), Entry("defers a changed workload spec", "spec"), Entry("bounds retries during continuous contention", "continuous"))
