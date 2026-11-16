package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func onboardingRequest(s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, s.Config.PublicURL+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func bootstrapTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	bootstrap, admin := randomToken(), randomToken()
	s := &Server{Store: testStore(t), Config: Config{PublicURL: "http://127.0.0.1:8766", BootstrapTokenHash: TokenHash(bootstrap)}}
	if err := s.Config.Validate(); err != nil {
		t.Fatal(err)
	}
	w := onboardingRequest(s, "POST", "/auth/bootstrap", "wrong", map[string]string{"subject": "admin", "token": admin})
	if w.Code != 401 {
		t.Fatalf("unprotected bootstrap: %d", w.Code)
	}
	w = onboardingRequest(s, "POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "admin", "token": admin})
	if w.Code != 204 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].HttpOnly {
		t.Fatalf("bootstrap failed or missing protected session: %d", w.Code)
	}
	if w = onboardingRequest(s, "POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "second-admin", "token": randomToken()}); w.Code != 401 {
		t.Fatal("bootstrap credential could be reused")
	}
	return s, admin
}

func testRegistration(id string) Registration {
	return Registration{ID: id, Name: strings.ToUpper(id), Environment: "test", KubeUID: uuid.NewString(), Namespaces: []string{"team"}, MetricsSource: "prometheus", InstallMode: "existing", InstallMethod: "helm"}
}

func registerAndIssue(t *testing.T, s *Server, admin string, reg Registration) IssuedCredential {
	t.Helper()
	if w := onboardingRequest(s, "POST", "/api/admin/clusters", admin, reg); w.Code != 201 {
		t.Fatalf("register: %d", w.Code)
	}
	w := onboardingRequest(s, "POST", "/api/admin/enrollments", admin, map[string]string{"clusterID": reg.ID})
	var issued IssuedCredential
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &issued) != nil || issued.Token == "" {
		t.Fatalf("issue enrollment: %d", w.Code)
	}
	return issued
}

func TestOnboardingBootstrapEnrollmentAndRevocation(t *testing.T) {
	s, admin := bootstrapTestServer(t)
	ctx := context.Background()
	reg := testRegistration("qa")
	enrollment := registerAndIssue(t, s, admin, reg)
	if w := onboardingRequest(s, "POST", "/api/admin/clusters", admin, reg); w.Code != 409 {
		t.Fatal("duplicate cluster identity accepted")
	}
	for _, input := range []map[string]string{{"clusterID": "other", "kubeUID": reg.KubeUID}, {"clusterID": reg.ID, "kubeUID": uuid.NewString()}} {
		if w := onboardingRequest(s, "POST", "/api/enroll", enrollment.Token, input); w.Code != 401 {
			t.Fatal("wrong cluster binding accepted")
		}
	}
	input := map[string]string{"clusterID": reg.ID, "kubeUID": reg.KubeUID}
	w := onboardingRequest(s, "POST", "/api/enroll", enrollment.Token, input)
	var runtime IssuedCredential
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &runtime) != nil {
		t.Fatalf("redeem: %d", w.Code)
	}
	if w = onboardingRequest(s, "POST", "/api/enroll", enrollment.Token, input); w.Code != 401 {
		t.Fatal("single-use enrollment accepted a replay")
	}
	batch := testBatch(1)
	batch.ClusterUID = reg.KubeUID
	if w = onboardingRequest(s, "POST", "/api/ingest", enrollment.Token, batch); w.Code != 401 {
		t.Fatal("enrollment token can ingest")
	}
	batch.Workloads[0].Namespace = "forbidden"
	if w = onboardingRequest(s, "POST", "/api/ingest", runtime.Token, batch); w.Code != 403 {
		t.Fatal("runtime credential escaped namespace binding")
	}
	batch.Workloads[0].Namespace = "team"
	batch.ClusterUID = uuid.NewString()
	if w = onboardingRequest(s, "POST", "/api/ingest", runtime.Token, batch); w.Code != 403 {
		t.Fatal("runtime credential escaped cluster binding")
	}
	batch.ClusterUID = reg.KubeUID
	if w = onboardingRequest(s, "POST", "/api/ingest", runtime.Token, batch); w.Code != 200 {
		t.Fatalf("dynamic agent ingestion: %d", w.Code)
	}
	// Fresh server instances see registrations, identities and administrator access.
	s = &Server{Store: s.Store, Config: s.Config}
	if w = onboardingRequest(s, "GET", "/api/workloads", admin, nil); w.Code != 200 {
		t.Fatal("administrator did not retain access after restart")
	}
	if w = onboardingRequest(s, "GET", "/api/admin/clusters", runtime.Token, nil); w.Code != 401 {
		t.Fatal("agent received administrator access")
	}
	if w = onboardingRequest(s, "POST", "/api/admin/revoke", admin, map[string]string{"id": runtime.ID}); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w = onboardingRequest(s, "POST", "/api/ingest", runtime.Token, batch); w.Code != 401 {
		t.Fatal("revoked runtime credential accepted")
	}
	audit, err := s.Store.Audit(ctx, 0)
	if err != nil || len(audit) != 5 {
		t.Fatalf("expected bootstrap, registration, issuance, redemption and revocation audit events: %d %v", len(audit), err)
	}
	encoded, _ := json.Marshal(audit)
	for _, token := range []string{admin, enrollment.Token, runtime.Token} {
		if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), TokenHash(token)) {
			t.Fatal("credential leaked into administrative audit")
		}
	}
	page, err := s.Store.List(ctx, "history", Query{Clusters: []string{reg.ID}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("revocation lost observed history")
	}
}

func TestOnboardingConcurrentRedemptionExpiryAndPermissions(t *testing.T) {
	s, admin := bootstrapTestServer(t)
	reg := testRegistration("prod")
	enrollment := registerAndIssue(t, s, admin, reg)
	ctx := context.Background()
	results := make(chan error, 8)
	for range 8 {
		go func() { _, err := s.Store.RedeemEnrollment(ctx, enrollment.Token, reg.ID, reg.KubeUID); results <- err }()
	}
	succeeded := 0
	for range 8 {
		if <-results == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("single-use race: %d redemptions", succeeded)
	}
	expired, err := s.Store.IssueEnrollment(ctx, "admin", reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Pool.Exec(ctx, `UPDATE op_credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.RedeemEnrollment(ctx, expired.Token, reg.ID, reg.KubeUID); err == nil {
		t.Fatal("expired enrollment accepted")
	}
	viewer := randomToken()
	if _, err = s.Store.Pool.Exec(ctx, `INSERT INTO op_users(subject,role,token_hash,clusters) VALUES('viewer','viewer',$1,$2)`, TokenHash(viewer), []string{reg.ID}); err != nil {
		t.Fatal(err)
	}
	if w := onboardingRequest(s, "GET", "/api/admin/audit", viewer, nil); w.Code != 403 {
		t.Fatal("viewer could read administrator audit")
	}
	if w := onboardingRequest(s, "POST", "/api/admin/clusters", viewer, testRegistration("other")); w.Code != 403 {
		t.Fatal("viewer could register clusters")
	}
	// A changed database grant is effective for existing sessions on every replica.
	login := onboardingRequest(s, "POST", "/auth/login", viewer, map[string]string{"token": viewer})
	if login.Code != 204 {
		t.Fatal("viewer login failed")
	}
	if _, err = s.Store.Pool.Exec(ctx, `UPDATE op_users SET disabled=true WHERE subject='viewer'`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", s.Config.PublicURL+"/api/me", nil)
	r.AddCookie(login.Result().Cookies()[0])
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("disabled account retained a session")
	}
	if w = onboardingRequest(s, "GET", "/api/me", viewer, nil); w.Code != 401 {
		t.Fatal("disabled account retained token access")
	}
}

func TestOnboardingBootstrapRaceAndSharedRateLimit(t *testing.T) {
	store := testStore(t)
	bootstrap := randomToken()
	s := &Server{Store: store, Config: Config{PublicURL: "http://127.0.0.1:8766", BootstrapTokenHash: TokenHash(bootstrap)}}
	var wg sync.WaitGroup
	results := make(chan int, 4)
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := onboardingRequest(s, "POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "admin-" + string(rune('a'+i)), "token": randomToken()})
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for code := range results {
		if code == 204 {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("bootstrap race allowed %d administrators", wins)
	}
	for i := range 21 {
		// Alternate replicas; a per-process limiter would incorrectly allow this.
		replica := &Server{Store: store, Config: s.Config}
		w := onboardingRequest(replica, "POST", "/api/enroll", "wrong", map[string]string{"clusterID": "qa", "kubeUID": uuid.NewString()})
		if (i < 20 && w.Code != 401) || (i == 20 && w.Code != 429) {
			t.Fatalf("shared rate limit attempt %d: %d", i, w.Code)
		}
	}
}

func TestOnboardingMigrationPreservesVersionOneInventory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Ingest(ctx, "qa", "QA", testBatch(1)); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema in this test's disposable schema only.
	if _, err := s.Pool.Exec(ctx, `DROP TABLE op_credentials,op_registrations,op_users,op_bootstrap,op_admin_audit,op_rate_limits,op_groups,op_templates,op_agent_health,op_notifications,op_recovery_checks; ALTER TABLE op_sessions DROP COLUMN oidc_groups; UPDATE op_schema SET version=1`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, "workloads", Query{Clusters: []string{"qa"}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("version-one inventory lost during migration")
	}
	var version int
	if err = s.Pool.QueryRow(ctx, `SELECT version FROM op_schema`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("schema version was not advanced")
	}
}
