package console

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestManagementGrantsAndLifecycle(t *testing.T) {
	s, admin := bootstrapTestServer(t)
	ctx := context.Background()
	reg := testRegistration("managed")
	issued := registerAndIssue(t, s, admin, reg)
	w := onboardingRequest(s, "POST", "/api/enroll", issued.Token, map[string]string{"clusterID": reg.ID, "kubeUID": reg.KubeUID})
	var runtime IssuedCredential
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &runtime) != nil {
		t.Fatal(w.Code)
	}
	grant := UserGrant{Subject: "reader", Role: "viewer", Clusters: []string{reg.ID}, Namespaces: reg.Namespaces, IssueToken: true}
	w = onboardingRequest(s, "POST", "/api/admin/users", admin, grant)
	var credential map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &credential) != nil || credential["token"] == "" {
		t.Fatal("grant failed", w.Code)
	}
	if w = onboardingRequest(s, "GET", "/api/admin/users", credential["token"], nil); w.Code != 403 {
		t.Fatal("viewer administered users")
	}
	if w = onboardingRequest(s, "GET", "/api/me", credential["token"], nil); w.Code != 200 {
		t.Fatal("viewer could not authenticate")
	}
	grant.Disabled = true
	grant.IssueToken = false
	if w = onboardingRequest(s, "POST", "/api/admin/users", admin, grant); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = onboardingRequest(s, "GET", "/api/me", credential["token"], nil); w.Code != 401 {
		t.Fatal("disabled viewer retained access")
	}
	if w = onboardingRequest(s, "POST", "/api/admin/users", admin, UserGrant{Subject: "admin", Role: "viewer"}); w.Code != 409 {
		t.Fatal("last administrator could be demoted", w.Code)
	}
	reg.Revision = 1
	reg.Name = "Renamed"
	reg.Installation.Stage = "preflight"
	if err := s.Store.UpdateRegistration(ctx, "admin", reg); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.UpdateRegistration(ctx, "admin", reg); err != ErrConflict {
		t.Fatal("stale update overwrote registration", err)
	}
	reg.Revision = 2
	reg.Namespaces = []string{"outside"}
	if err := s.Store.UpdateRegistration(ctx, "admin", reg); err != ErrConflict {
		t.Fatal("issued credential scope changed", err)
	}
	if err := s.Store.SetConnected(ctx, "admin", reg.ID, false); err != nil {
		t.Fatal(err)
	}
	if access, err := s.agentAccess(ctx, runtime.Token); err != nil || access != nil {
		t.Fatal("disconnected credential authenticated")
	}
	if _, err := s.Store.IssueEnrollment(ctx, "admin", reg.ID); err != ErrDenied {
		t.Fatal("disconnected cluster enrolled")
	}
	if err := s.Store.SetConnected(ctx, "admin", reg.ID, true); err != nil {
		t.Fatal(err)
	}
	if access, err := s.agentAccess(ctx, runtime.Token); err != nil || access != nil {
		t.Fatal("reconnect resurrected revoked token")
	}
	if _, err := s.Store.IssueEnrollment(ctx, "admin", reg.ID); err != nil {
		t.Fatal("reconnect cannot enroll", err)
	}
}

func TestManagementBulkTemplatesGroupsAndHealth(t *testing.T) {
	s, admin := bootstrapTestServer(t)
	ctx := context.Background()
	regs := []Registration{testRegistration("bulk-a"), testRegistration("bulk-b")}
	if err := s.Store.RegisterBulk(ctx, "admin", regs); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RegisterBulk(ctx, "admin", regs); err != nil {
		t.Fatal("idempotent bulk failed", err)
	}
	conflict := regs[0]
	conflict.KubeUID = regs[1].KubeUID
	if err := s.Store.RegisterBulk(ctx, "admin", []Registration{testRegistration("must-rollback"), conflict}); err != ErrConflict {
		t.Fatal("conflicting bulk accepted", err)
	}
	all, err := s.Store.Registrations(ctx)
	if err != nil || len(all) != 2 {
		t.Fatal("bulk was not atomic", err)
	}
	template := EnvironmentTemplate{Name: "qa", Registration: Registration{Environment: "QA", MetricsSource: "metrics-server", InstallMode: "new", InstallMethod: "gitops", Namespaces: []string{"team"}}}
	if err = s.Store.SaveTemplate(ctx, "admin", template); err != nil {
		t.Fatal(err)
	}
	templates, err := s.Store.Templates(ctx)
	if err != nil || len(templates) != 1 {
		t.Fatal("template did not persist", err)
	}
	grant := UserGrant{Subject: "engineers", Role: "viewer", Clusters: []string{"bulk-a"}, Namespaces: []string{"team"}}
	if _, err = s.Store.SaveGrant(ctx, "admin", grant, true); err != nil {
		t.Fatal(err)
	}
	r, err := s.groupReader(ctx, "issuer|user", []string{"engineers"})
	if err != nil || r == nil || len(r.Clusters) != 1 || r.Clusters[0] != "bulk-a" {
		t.Fatal("group grants missing", err)
	}
	grant.Disabled = true
	if _, err = s.Store.SaveGrant(ctx, "admin", grant, true); err != nil {
		t.Fatal(err)
	}
	if r, err = s.groupReader(ctx, "issuer|user", []string{"engineers"}); err != nil || r != nil {
		t.Fatal("disabled group retained access", err)
	}
	if err = s.Store.MaintainAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	notifications, err := s.Store.Notifications(ctx)
	if err != nil || len(notifications) == 0 {
		t.Fatal("health notifications missing", err)
	}
	if err = s.Store.MaintainAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := s.Store.Notifications(ctx)
	if err != nil || len(next) != len(notifications) {
		t.Fatal("health notifications duplicated", err)
	}
	if w := onboardingRequest(s, "GET", "/api/admin/health", admin, nil); w.Code != 200 {
		t.Fatal("health unavailable", w.Code)
	}
}

func TestSSOGroupUnionsPreserveClusterNamespacePairs(t *testing.T) {
	s, _ := bootstrapTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"scope-a", "scope-b"} {
		if err := s.Store.RegisterBulk(ctx, "admin", []Registration{testRegistration(id)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO op_clusters(id,name,kube_uid,stream_id) VALUES($1,$1,'uid','stream')`, id); err != nil {
			t.Fatal(err)
		}
		for _, ns := range []string{"one", "two"} {
			if _, err := s.Store.Pool.Exec(ctx, `INSERT INTO op_workloads(cluster_id,uid,namespace,name,kind,mode,run_id,observed_at,data) VALUES($1,$2,$2,$2,'Deployment','Unmanaged','run',now(),'{}')`, id, ns); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, g := range []UserGrant{{Subject: "a", Role: RoleViewer, Clusters: []string{"scope-a"}, Namespaces: []string{"one"}}, {Subject: "b", Role: RoleViewer, Clusters: []string{"scope-b"}, Namespaces: []string{"two"}}} {
		if _, err := s.Store.SaveGrant(ctx, "admin", g, true); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := s.groupReader(ctx, "issuer|user", []string{"a", "b"})
	if err != nil || reader == nil {
		t.Fatal("combined group access failed", err)
	}
	page, err := s.Store.List(ctx, "workloads", Query{Clusters: reader.Clusters, NamespaceScopes: reader.NamespaceScopes})
	if err != nil || len(page.Items) != 2 {
		t.Fatal("group scope union widened or lost authorized rows", err)
	}
	summary, err := s.Store.Summary(ctx, reader, "")
	if err != nil || summary.Workloads != 2 {
		t.Fatal("summary escaped pairwise scope", err)
	}
	page, err = s.Store.List(ctx, "workloads", Query{Clusters: reader.Clusters, NamespaceScopes: reader.NamespaceScopes, Cluster: "scope-a", Namespace: "two"})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("cross-group namespace access granted", err)
	}
}

func TestDisconnectAndEnrollmentShareLockOrder(t *testing.T) {
	s, _ := bootstrapTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := range 5 {
		r := testRegistration(fmt.Sprintf("race-%d", i))
		if err := s.Store.RegisterBulk(ctx, "admin", []Registration{r}); err != nil {
			t.Fatal(err)
		}
		enrollment, err := s.Store.IssueEnrollment(ctx, "admin", r.ID)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- s.Store.SetConnected(ctx, "admin", r.ID, false) }()
		runtime, err := s.Store.RedeemEnrollment(ctx, enrollment.Token, r.ID, r.KubeUID)
		if err != nil && err != ErrDenied {
			t.Fatal("concurrent enrollment failed unexpectedly", err)
		}
		if err = <-done; err != nil {
			t.Fatal("disconnect deadlocked", err)
		}
		if runtime != nil {
			if agent, e := s.agentAccess(ctx, runtime.Token); e != nil || agent != nil {
				t.Fatal("disconnected runtime remains authorized", e)
			}
		}
	}
}
