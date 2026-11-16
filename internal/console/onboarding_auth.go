package console

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"

	"github.com/optipod/optipod/internal/consoletls"

	"github.com/jackc/pgx/v5"
)

func (s *Server) readerForToken(ctx context.Context, token string) (*Reader, error) {
	for _, r := range s.Config.Readers {
		if tokenEqual(token, r.TokenHash) {
			return s.expandAdmin(ctx, &r)
		}
	}
	return s.storedReader(ctx, "token_hash", TokenHash(token))
}

func (s *Server) readerForSubject(ctx context.Context, subject string) (*Reader, error) {
	for _, r := range s.Config.Readers {
		if r.Subject == subject {
			return s.expandAdmin(ctx, &r)
		}
	}
	return s.storedReader(ctx, "subject", subject)
}

func (s *Server) storedReader(ctx context.Context, column, value string) (*Reader, error) {
	r := &Reader{}
	// column is an internal constant, never request input.
	err := s.Store.Pool.QueryRow(ctx, `SELECT subject,role,clusters,namespaces FROM op_users WHERE `+column+`=$1 AND NOT disabled`, value).Scan(&r.Subject, &r.Role, &r.Clusters, &r.Namespaces)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.expandAdmin(ctx, r)
}

func (s *Server) expandAdmin(ctx context.Context, r *Reader) (*Reader, error) {
	if r.Role != RoleAdmin {
		return r, nil
	}
	rows, err := s.Store.Pool.Query(ctx, `SELECT id FROM op_registrations UNION SELECT id FROM op_clusters ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r.Clusters = []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		r.Clusters = append(r.Clusters, id)
	}
	return r, rows.Err()
}

func (s *Server) authRateAllowed(w http.ResponseWriter, r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	// Deliberately ignore untrusted forwarded headers. A proxy's clients share
	// this limit unless the deployment supplies distinct authenticated paths.
	key := TokenHash(r.URL.Path + ":" + ip)
	var count int
	err = s.Store.Pool.QueryRow(r.Context(), `INSERT INTO op_rate_limits(key,window_start,attempts) VALUES($1,date_trunc('minute',now()),1)
	 ON CONFLICT(key) DO UPDATE SET window_start=date_trunc('minute',now()),
	 attempts=CASE WHEN op_rate_limits.window_start=date_trunc('minute',now()) THEN LEAST(op_rate_limits.attempts+1,1000) ELSE 1 END RETURNING attempts`, key).Scan(&count)
	if err != nil {
		s.unavailable(w, err)
		return false
	}
	if count > 20 {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many authentication attempts; retry after one minute", http.StatusTooManyRequests)
		return false
	}
	return true
}

type agentAccess struct {
	AgentCredential
	KubeUID       string
	Namespaces    []string
	CredentialID  string
	PublicKeyHash string
	RequireMTLS   bool
}

func (s *Server) authenticatedAgent(r *http.Request) (*agentAccess, error) {
	a, err := s.agentAccess(r.Context(), bearer(r))
	if err != nil || a == nil || !a.RequireMTLS {
		return a, err
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return nil, nil
	}
	cert := r.TLS.PeerCertificates[0]
	if cert.Subject.CommonName != a.CredentialID || len(cert.Subject.OrganizationalUnit) != 1 || cert.Subject.OrganizationalUnit[0] != a.ID || consoletls.PublicKeyHash(cert.PublicKey) != a.PublicKeyHash {
		return nil, nil
	}
	return a, nil
}

func (s *Server) agentAccess(ctx context.Context, token string) (*agentAccess, error) {
	if token == "" || len(token) > 4096 {
		return nil, nil
	}
	for _, a := range s.Config.Agents {
		if tokenEqual(token, a.TokenHash) {
			return &agentAccess{AgentCredential: a}, nil
		}
	}
	a := &agentAccess{}
	err := s.Store.Pool.QueryRow(ctx, `SELECT r.id,r.name,r.kube_uid,r.namespaces,c.id,c.public_key_hash,r.require_mtls FROM op_credentials c JOIN op_registrations r ON r.id=c.cluster_id
	 WHERE c.token_hash=$1 AND c.kind='runtime' AND c.expires_at>now() AND c.revoked_at IS NULL AND NOT r.disconnected`, TokenHash(token)).Scan(&a.ID, &a.Name, &a.KubeUID, &a.Namespaces, &a.CredentialID, &a.PublicKeyHash, &a.RequireMTLS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (a *agentAccess) permits(b *Batch) bool {
	if a.KubeUID != "" && a.KubeUID != b.ClusterUID {
		return false
	}
	if len(a.Namespaces) == 0 {
		return true
	}
	for _, w := range b.Workloads {
		if !slices.Contains(a.Namespaces, w.Namespace) {
			return false
		}
	}
	for _, p := range b.Policies {
		if !slices.Contains(a.Namespaces, p.Namespace) {
			return false
		}
	}
	for _, h := range b.Records {
		if !slices.Contains(a.Namespaces, h.Workload.Namespace) {
			return false
		}
	}
	return true
}
