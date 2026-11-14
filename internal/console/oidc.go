package console

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type LoginProvider struct {
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
}

func (s *Server) InitOIDC(ctx context.Context) error {
	c := s.Config.OIDC
	if c == nil {
		return nil
	}
	if !strings.HasPrefix(c.Issuer, "https://") {
		return fmt.Errorf("OIDC issuer requires HTTPS")
	}
	p, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return fmt.Errorf("OIDC discovery failed: %w", err)
	}
	secret := ""
	if c.ClientSecretFile != "" {
		b, e := os.ReadFile(c.ClientSecretFile)
		if e != nil {
			return e
		}
		secret = strings.TrimSpace(string(b))
	}
	s.OIDC = &LoginProvider{OAuth: oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, Endpoint: p.Endpoint(), RedirectURL: strings.TrimSuffix(s.Config.PublicURL, "/") + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile"}}, Verifier: p.Verifier(&oidc.Config{ClientID: c.ClientID})}
	return nil
}

type loginState struct {
	State, Nonce, Verifier string
	Expires                int64
}

func (s *Server) oidcRoute(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/auth/oidc" {
		state := loginState{State: randomToken(), Nonce: randomToken(), Verifier: oauth2.GenerateVerifier(), Expires: time.Now().Add(5 * time.Minute).Unix()}
		// State is stored server-side in the shared database so callbacks work
		// across replicas. The browser receives only a random one-time handle.
		data, _ := json.Marshal(state)
		handle := randomToken()
		_, err := s.Store.Pool.Exec(r.Context(), `INSERT INTO op_sessions(token_hash,subject,clusters,expires_at) VALUES($1,$2,$3,$4)`, TokenHash(handle), "oidc:"+base64.RawURLEncoding.EncodeToString(data), []string{}, time.Now().Add(5*time.Minute))
		if err != nil {
			s.unavailable(w, err)
			return
		}
		s.cookie(w, "optipod_login", handle, 300)
		http.Redirect(w, r, s.OIDC.OAuth.AuthCodeURL(state.State, oauth2.S256ChallengeOption(state.Verifier), oidc.Nonce(state.Nonce)), http.StatusFound)
		return
	}
	cookie, err := r.Cookie("optipod_login")
	if err != nil {
		http.Error(w, "login expired", http.StatusUnauthorized)
		return
	}
	s.cookie(w, "optipod_login", "", -1)
	var raw string
	err = s.Store.Pool.QueryRow(r.Context(), `DELETE FROM op_sessions WHERE token_hash=$1 AND expires_at>now() RETURNING subject`, TokenHash(cookie.Value)).Scan(&raw)
	if err != nil || !strings.HasPrefix(raw, "oidc:") {
		http.Error(w, "login expired", http.StatusUnauthorized)
		return
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, "oidc:"))
	var state loginState
	if err != nil || json.Unmarshal(data, &state) != nil || state.Expires < time.Now().Unix() || subtle.ConstantTimeCompare([]byte(state.State), []byte(r.URL.Query().Get("state"))) != 1 {
		http.Error(w, "invalid login state", http.StatusUnauthorized)
		return
	}
	token, err := s.OIDC.OAuth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(state.Verifier))
	if err != nil {
		http.Error(w, "identity exchange failed", http.StatusUnauthorized)
		return
	}
	rawID, _ := token.Extra("id_token").(string)
	id, err := s.OIDC.Verifier.Verify(r.Context(), rawID)
	if err != nil || id.Nonce != state.Nonce {
		http.Error(w, "invalid identity", http.StatusUnauthorized)
		return
	}
	subject := id.Issuer + "|" + id.Subject
	for _, reader := range s.Config.Readers {
		if reader.Subject == subject {
			if err = s.issueSession(r.Context(), w, subject); err != nil {
				s.unavailable(w, err)
				return
			}
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}
	http.Error(w, "no console access assigned", http.StatusForbidden)
}

func errorType(err error) string { return fmt.Sprintf("%T", err) }
