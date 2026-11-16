package console

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestOIDCVerifiedIdentityPKCEAndSingleUseState(t *testing.T) {
	testVerifiedOIDC(t, false)
}

func TestOIDCGroupClaimsAndLiveRevocation(t *testing.T) { testVerifiedOIDC(t, true) }

func testVerifiedOIDC(t *testing.T, group bool) {
	t.Helper()
	store := testStore(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var nonce, challenge string
	issuer := httptest.NewUnstartedServer(nil)
	issuer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if e := r.ParseForm(); e != nil {
				t.Error(e)
			}
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				http.Error(w, "wrong PKCE", 400)
				return
			}
			signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
			if e != nil {
				t.Error(e)
				return
			}
			claims, _ := json.Marshal(map[string]any{"iss": issuer.URL, "sub": "alice", "aud": "console", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "groups": []string{"operators"}})
			signed, e := signer.Sign(claims)
			if e != nil {
				t.Error(e)
				return
			}
			compact, e := signed.CompactSerialize()
			if e != nil {
				t.Error(e)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "token_type": "Bearer", "id_token": compact})
		default:
			http.NotFound(w, r)
		}
	})
	issuer.StartTLS()
	defer issuer.Close()
	cfg := testConfig()
	cfg.OIDC = &OIDCConfig{Issuer: issuer.URL, ClientID: "console"}
	if group {
		cfg.OIDC.GroupsClaim = "groups"
		if err = store.RegisterBulk(context.Background(), "admin", []Registration{testRegistration("qa")}); err != nil {
			t.Fatal(err)
		}
		if _, err = store.SaveGrant(context.Background(), "admin", UserGrant{Subject: "operators", Role: RoleViewer, Clusters: []string{"qa"}}, true); err != nil {
			t.Fatal(err)
		}
	} else {
		cfg.Readers = append(cfg.Readers, Reader{Subject: issuer.URL + "|alice", Clusters: []string{"qa"}})
	}
	s := &Server{Store: store, Config: cfg}
	ctx := oidc.ClientContext(context.Background(), issuer.Client())
	if err = s.InitOIDC(ctx); err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRequest(http.MethodGet, cfg.PublicURL+"/auth/oidc", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, start)
	if w.Code != http.StatusFound {
		t.Fatalf("login start: %d", w.Code)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	nonce = location.Query().Get("nonce")
	challenge = location.Query().Get("code_challenge")
	if nonce == "" || challenge == "" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("missing nonce/PKCE")
	}
	cookie := w.Result().Cookies()[0]
	callback := func(state string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, cfg.PublicURL+"/auth/callback?code=code&state="+url.QueryEscape(state), nil).WithContext(ctx)
		r.AddCookie(cookie)
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, r)
		return out
	}
	w = callback(location.Query().Get("state"))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("OIDC callback: %d %s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session created")
	}
	r := httptest.NewRequest(http.MethodGet, cfg.PublicURL+"/api/me", nil)
	r.AddCookie(session)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("verified identity denied")
	}
	if group {
		if _, err = store.SaveGrant(context.Background(), "admin", UserGrant{Subject: "operators", Role: RoleViewer, Clusters: []string{"qa"}, Disabled: true}, true); err != nil {
			t.Fatal(err)
		}
		denied := httptest.NewRecorder()
		s.Handler().ServeHTTP(denied, r)
		if denied.Code != http.StatusUnauthorized {
			t.Fatal("disabled SSO group retained an existing browser session")
		}
	}
	if replay := callback(location.Query().Get("state")); replay.Code != http.StatusUnauthorized {
		t.Fatal("OIDC callback replay accepted")
	}
	// Tampered state is denied before an authorization code is exchanged.
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, start)
	cookie = w.Result().Cookies()[0]
	if bad := callback("tampered"); bad.Code != http.StatusUnauthorized {
		t.Fatal("tampered state accepted")
	}
}
