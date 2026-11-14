package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testConfig() Config {
	return Config{PublicURL: "http://127.0.0.1:8766",
		Agents:  []AgentCredential{{ID: "qa", Name: "QA", TokenHash: TokenHash("qa-agent")}, {ID: "prod", Name: "Production", TokenHash: TokenHash("prod-agent")}},
		Readers: []Reader{{Subject: "viewer", TokenHash: TokenHash("viewer-token"), Clusters: []string{"qa"}, Namespaces: []string{"team"}}},
	}
}

func TestSharedAPIAuthenticationScopesAndDurability(t *testing.T) {
	store := testStore(t)
	cfg := testConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Store: store, Config: cfg}).Handler()
	request := func(method, path, token string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, cfg.PublicURL+path, bytes.NewReader(data))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/api/workloads", "", 401}, {"GET", "/api/workloads", "qa-agent", 401},
		{"GET", "/api/workloads?cluster=prod", "viewer-token", 403},
		{"GET", "/api/history?namespace=private", "viewer-token", 403},
		{"POST", "/api/workloads", "viewer-token", 405},
		{"POST", "/api/ingest", "viewer-token", 401},
		{"GET", "/api/workloads?cursor=invalid", "viewer-token", 400},
	} {
		if w := request(tc.method, tc.path, tc.token, nil); w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	for _, token := range []string{"qa-agent", "prod-agent"} {
		if w := request("POST", "/api/ingest", token, testBatch(1)); w.Code != 200 {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}
	}
	w := request("GET", "/api/workloads", "viewer-token", nil)
	var page Page
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 || page.Items[0].ClusterID != "qa" {
		t.Fatalf("scope: %d %s", w.Code, w.Body.String())
	}
	// A successful login never exposes the token in browser storage or HTML.
	w = request("POST", "/auth/login", "", map[string]string{"token": "viewer-token"})
	if w.Code != 204 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login: %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Value == "viewer-token" {
		t.Fatal("unsafe session")
	}
	h = (&Server{Store: store, Config: cfg}).Handler() // API restart; session lives in PostgreSQL.
	r := httptest.NewRequest("GET", cfg.PublicURL+"/api/me", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("session did not survive API restart")
	}
	r = httptest.NewRequest("POST", cfg.PublicURL+"/auth/logout", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin logout accepted")
	}
	r = httptest.NewRequest("POST", cfg.PublicURL+"/auth/logout", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("logout failed")
	}
	r = httptest.NewRequest("GET", cfg.PublicURL+"/api/me", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("session survived logout")
	}
	// Namespace restrictions are SQL predicates, including unfiltered requests.
	b := testBatch(2)
	b.Workloads[0].Namespace = "private"
	if err := store.Ingest(context.Background(), "qa", "QA", b); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/api/workloads", "viewer-token", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 0 {
		t.Fatal("namespace scope leak")
	}
}

func TestConfigRejectsUnsafeOrAmbiguousAccess(t *testing.T) {
	c := testConfig()
	c.PublicURL = "http://console.example.com"
	if c.Validate() == nil {
		t.Fatal("public cleartext allowed")
	}
	c = testConfig()
	c.Readers[0].TokenHash = c.Agents[0].TokenHash
	if c.Validate() == nil {
		t.Fatal("shared agent/user credential allowed")
	}
	c = testConfig()
	c.Readers[0].Clusters = []string{"unknown"}
	if c.Validate() == nil {
		t.Fatal("unknown scope allowed")
	}
}
