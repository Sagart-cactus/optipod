package suite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/report"
	"github.com/optipod/optipod/test/e2e/framework"
)

var _ = Describe("Parity: fleet and history explorer", Label("parity", "reporting"), func() {
	It("serves actual allocations and durable before/after records through a read-only API", func(ctx context.Context) {
		ns := specNamespace(ctx, "reporting")
		DeferCleanup(func(ctx context.Context) { Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed()) })
		dep := createResizeWorkload(ctx, ns.Name, "reported", smokeSamples()[:1])
		registerContainerSeries(ctx, ns.Name, dep, smokeSamples()[:1])
		p := framework.NewPolicyFixture(ns.Name, "reported", api.ModeRecommend, framework.WithStrategy("webhook", "immediate"), framework.WithReconciliationInterval(time.Second))
		p.Spec.UpdateStrategy.AllowInPlaceResize = true
		p.Spec.UpdateStrategy.AllowRecreate = false
		Expect(env.Clients.Ctrl.Create(ctx, p)).To(Succeed())
		waitForWorkloadReason(ctx, p, "Generated resource recommendations")
		collector := &report.Collector{Client: env.Clients.Ctrl, Namespace: ns.Name}
		server := httptest.NewUnstartedServer(nil)
		server.Config.Handler = collector.Handler(server.Listener.Addr().String())
		server.Start()
		defer server.Close()
		readFleet := func() *report.Fleet {
			response, err := server.Client().Get(server.URL + "/api/fleet")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = response.Body.Close() }()
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			var fleet report.Fleet
			Expect(json.NewDecoder(response.Body).Decode(&fleet)).To(Succeed())
			return &fleet
		}
		fleet := readFleet()
		Expect(fleet.Workloads).To(HaveLen(1))
		Expect(fleet.Workloads[0].UID).To(Equal(string(dep.UID)))
		Expect(fleet.Workloads[0].RequestedMilliCPU).To(Equal(int64(100)))
		Expect(fleet.Workloads[0].Containers[0].Recommended["cpu-request"]).To(Equal("300m"))
		updatePolicy(ctx, ns.Name, p.Name, func(p *api.OptimizationPolicy) { p.Spec.Mode = api.ModeAuto })
		waitForWorkloadReason(ctx, p, "InPlace")
		fleet = readFleet()
		Expect(fleet.Workloads[0].RequestedMilliCPU).To(Equal(int64(300)))
		Expect(fleet.Workloads[0].Pods[0].Restarts).To(BeZero())
		response, err := server.Client().Get(server.URL + "/api/history?namespace=" + ns.Name + "&uid=" + string(dep.UID))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = response.Body.Close() }()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		var journal report.Journal
		Expect(json.NewDecoder(response.Body).Decode(&journal)).To(Succeed())
		Expect(journal.Records).NotTo(BeEmpty())
		var applied, decision *history.Record
		for _, record := range journal.Records {
			Expect(record.Workload.UID).To(Equal(dep.UID))
			if record.Method == "InPlace" {
				applied = record
			}
			if record.Decision != nil && record.Decision.Outcome == "Applied" {
				decision = record
			}
		}
		Expect(applied).NotTo(BeNil())
		Expect(applied.Phase).To(Equal("Applied"))
		before := applied.BeforeTemplate["app"]
		Expect(before.Requests.Cpu().MilliValue()).To(Equal(int64(100)))
		Expect(decision).NotTo(BeNil())
		Expect(decision.PolicySpec.Mode).To(Equal(api.ModeAuto))
		Expect(decision.Decision.Reason).NotTo(BeEmpty())
		Expect(decision.Decision.AppliedRecord).To(Equal(applied.Name))

		blocked, err := server.Client().Post(server.URL+"/api/fleet", "application/json", nil)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = blocked.Body.Close() }()
		Expect(blocked.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})
})
