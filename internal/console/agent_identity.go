package console

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/optipod/optipod/internal/consoletls"
)

type AgentTLSConfig struct {
	Listen                string `json:"listen"`
	PublicURL             string `json:"publicURL"`
	ServerCertificateFile string `json:"serverCertificateFile"`
	ServerKeyFile         string `json:"serverKeyFile"`
	ServerCAFile          string `json:"serverCAFile,omitempty"`
	ClientCAFile          string `json:"clientCAFile"`
	ClientCAKeyFile       string `json:"clientCAKeyFile"`
}

func (s *Server) InitAgentTLS() (*tls.Config, error) {
	if s.Config.AgentTLS == nil {
		return nil, nil
	}
	cfg := s.Config.AgentTLS
	a, err := consoletls.LoadAuthority(cfg.ClientCAFile, cfg.ClientCAKeyFile)
	if err != nil {
		return nil, err
	}
	s.Authority = a
	pair, err := tls.LoadX509KeyPair(cfg.ServerCertificateFile, cfg.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot load agent TLS server certificate")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(a.PEM) {
		return nil, fmt.Errorf("invalid client CA")
	}
	s.AgentTrustPEM = ""
	if cfg.ServerCAFile != "" {
		data, e := os.ReadFile(cfg.ServerCAFile)
		if e != nil {
			return nil, fmt.Errorf("cannot load agent server trust CA")
		}
		s.AgentTrustPEM = string(data)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven}, nil
}

func (s *Server) renewIdentity(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) || !s.authRateAllowed(w, r) {
		return
	}
	a, err := s.authenticatedAgent(r)
	if err != nil {
		s.unavailable(w, err)
		return
	}
	if a == nil || !a.RequireMTLS || s.Authority == nil {
		s.onboardingError(w, ErrDenied)
		return
	}
	var input struct {
		CSR string `json:"csr"`
	}
	if !decodeInput(w, r, &input) {
		return
	}
	certificate, hash, expires, err := s.Authority.Sign(input.CSR, a.CredentialID, a.ID)
	if err != nil || hash != a.PublicKeyHash {
		s.onboardingError(w, ErrDenied)
		return
	}
	tx, err := s.Store.Pool.Begin(r.Context())
	if err != nil {
		s.unavailable(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	result, err := tx.Exec(r.Context(), `UPDATE op_credentials SET expires_at=$2 WHERE id=$1 AND revoked_at IS NULL AND expires_at>now()`, a.CredentialID, time.Now().Add(90*24*time.Hour))
	if err != nil {
		s.unavailable(w, err)
		return
	}
	if result.RowsAffected() != 1 {
		s.onboardingError(w, ErrDenied)
		return
	}
	if err = audit(r.Context(), tx, "agent:"+a.ID, "certificate.renew", a.ID, map[string]string{credentialIDKey: a.CredentialID}); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.unavailable(w, err)
		return
	}
	writeJSON(w, map[string]any{"certificate": certificate, "expiresAt": expires})
}
