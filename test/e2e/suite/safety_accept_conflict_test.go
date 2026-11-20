package suite

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/safety"
	"github.com/optipod/optipod/test/e2e/framework"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = It("records the cooldown despite a concurrent safety observation", Label("ssa", "version-conflict", "cooldown"), func(ctx context.Context) {
	ns := specNamespace(ctx, "cooldown-conflict")
	DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
	dep := framework.NewDeploymentFixture(ns.Name, "worker", 1, framework.ContainerFixture{Name: "app", CPURequest: "50m", MemoryRequest: "64Mi"})
	Expect(env.Clients.Ctrl.Create(ctx, dep)).To(Succeed())
	Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, dep.Name, framework.WaitOptions{Timeout: 2 * time.Minute})).To(Succeed())
	w := &discovery.Workload{Kind: "Deployment", Name: dep.Name, Namespace: ns.Name, Object: dep}
	state, err := safety.Load(ctx, env.Clients.Ctrl, w, dep.Spec.Template.Spec.Containers)
	Expect(err).NotTo(HaveOccurred())
	Expect(state.Save(ctx, env.Clients.Ctrl, w)).To(Succeed())
	stale, err := safety.Load(ctx, env.Clients.Ctrl, w, dep.Spec.Template.Spec.Containers)
	Expect(err).NotTo(HaveOccurred())
	cfg := rest.CopyConfig(env.Clients.RESTConfig())
	var codes []int
	deployments := env.Clients.Kube.AppsV1().Deployments(ns.Name)
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return conflictRoundTrip(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPatch || !strings.HasSuffix(req.URL.Path, "/deployments/worker") {
				return next.RoundTrip(req)
			}
			if len(codes) == 0 {
				live, err := deployments.Get(ctx, dep.Name, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				var observed safety.State
				if err := json.Unmarshal([]byte(live.Annotations[safety.StateAnnotation]), &observed); err != nil {
					return nil, err
				}
				observed.Containers["app"].Recovery = &safety.RecoveryState{Count: 2}
				value, err := json.Marshal(observed)
				if err != nil {
					return nil, err
				}
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{safety.StateAnnotation: string(value)}}})
				if err != nil {
					return nil, err
				}
				if _, err := deployments.Patch(ctx, dep.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
					return nil, err
				}
			}
			resp, err := next.RoundTrip(req)
			if resp != nil {
				codes = append(codes, resp.StatusCode)
			}
			return resp, err
		})
	})
	writer, err := client.New(cfg, client.Options{Scheme: framework.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	cpu, mem := resource.MustParse("80m"), resource.MustParse("96Mi")
	accepted := []api.ContainerRecommendation{{Container: "app", CPU: &cpu, Memory: &mem}}
	now := time.Now().UTC().Truncate(time.Second)
	Expect(state.Accept(ctx, writer, w, accepted, now)).To(Succeed())
	Expect(codes[0]).To(Equal(http.StatusConflict))
	Expect(codes[len(codes)-1]).To(Equal(http.StatusOK))
	Expect(apierrors.IsConflict(stale.Save(ctx, env.Clients.Ctrl, w))).To(BeTrue())
	saved, err := safety.Load(ctx, env.Clients.Ctrl, w, dep.Spec.Template.Spec.Containers)
	Expect(err).NotTo(HaveOccurred())
	Expect(saved.Containers["app"].Recovery.Count).To(BeEquivalentTo(2))
	Expect(saved.LastChange).To(Equal(now))
	Expect(saved.Accept(ctx, writer, w, accepted, now.Add(time.Minute))).To(Succeed())
	Expect(saved.LastChange).To(Equal(now))
	p := framework.NewPolicyFixture(ns.Name, "scope", api.ModeAuto)
	p.Spec.UpdateStrategy.Stability = &api.StabilityConfig{Cooldown: metav1.Duration{Duration: 30 * time.Minute}}
	larger := resource.MustParse("128Mi")
	proposed := []api.ContainerRecommendation{{Container: "app", Memory: &larger}}
	saved.Guard(proposed, p, now.Add(10*time.Minute))
	Expect(proposed[0].Memory.String()).To(Equal("96Mi"))
})
