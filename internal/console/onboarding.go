package console

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrDenied = errors.New("credential is invalid, expired, used or revoked")
var ErrInvalid = errors.New("invalid onboarding configuration")

type Registration struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Environment   string     `json:"environment"`
	KubeUID       string     `json:"kubeUID"`
	Namespaces    []string   `json:"namespaces"`
	MetricsSource string     `json:"metricsSource"`
	InstallMode   string     `json:"installMode"`
	InstallMethod string     `json:"installMethod"`
	Disconnected  bool       `json:"disconnected"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReceivedAt    *time.Time `json:"receivedAt,omitempty"`
}

func (r Registration) Validate() error {
	if r.Disconnected || !r.CreatedAt.IsZero() || r.ReceivedAt != nil {
		return ErrInvalid
	}
	if !identifier.MatchString(r.ID) || len(r.ID) > 63 || strings.TrimSpace(r.Name) == "" || len(r.Name) > 120 || len(r.Environment) > 120 || len(r.Namespaces) > 100 {
		return ErrInvalid
	}
	if _, err := uuid.Parse(r.KubeUID); err != nil {
		return ErrInvalid
	}
	if (r.MetricsSource != "prometheus" && r.MetricsSource != "metrics-server") || (r.InstallMode != "new" && r.InstallMode != "existing") || (r.InstallMethod != "helm" && r.InstallMethod != "gitops") {
		return ErrInvalid
	}
	for _, ns := range r.Namespaces {
		if len(validation.IsDNS1123Label(ns)) > 0 {
			return ErrInvalid
		}
	}
	return nil
}

type IssuedCredential struct {
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterID"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CredentialInfo struct {
	ID        string     `json:"id"`
	ClusterID string     `json:"clusterID"`
	Kind      string     `json:"kind"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

type AuditEntry struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"createdAt"`
}

func audit(ctx context.Context, tx pgx.Tx, actor, action, target string, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO op_admin_audit(actor,action,target,details) VALUES($1,$2,$3,$4)`, actor, action, target, data)
	return err
}

func (s *Store) Bootstrap(ctx context.Context, subject, hash string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `INSERT INTO op_bootstrap(singleton) VALUES(true) ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO op_users(subject,role,token_hash) VALUES($1,'admin',$2)`, subject, hash); err != nil {
		return err
	}
	if err = audit(ctx, tx, subject, "bootstrap", subject, map[string]string{"role": RoleAdmin}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Register(ctx context.Context, actor string, r Registration) error {
	if err := r.Validate(); err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if r.Namespaces == nil {
		r.Namespaces = []string{}
	}
	var existing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_clusters WHERE id=$1)`, r.ID).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO op_registrations(id,name,environment,kube_uid,namespaces,metrics_source,install_mode,install_method)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.ID, r.Name, r.Environment, r.KubeUID, r.Namespaces, r.MetricsSource, r.InstallMode, r.InstallMethod)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "cluster.register", r.ID, r); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Registrations(ctx context.Context) ([]Registration, error) {
	rows, err := s.Pool.Query(ctx, `SELECT r.id,r.name,r.environment,r.kube_uid,r.namespaces,r.metrics_source,r.install_mode,r.install_method,r.disconnected,r.created_at,c.received_at
	 FROM op_registrations r LEFT JOIN op_clusters c ON c.id=r.id ORDER BY r.created_at,r.id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Registration{}
	for rows.Next() {
		var r Registration
		if err = rows.Scan(&r.ID, &r.Name, &r.Environment, &r.KubeUID, &r.Namespaces, &r.MetricsSource, &r.InstallMode, &r.InstallMethod, &r.Disconnected, &r.CreatedAt, &r.ReceivedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func issueCredential(ctx context.Context, tx pgx.Tx, clusterID, kind string, lifetime time.Duration) (*IssuedCredential, error) {
	c := &IssuedCredential{ID: uuid.NewString(), ClusterID: clusterID, Token: randomToken(), ExpiresAt: time.Now().UTC().Add(lifetime)}
	_, err := tx.Exec(ctx, `INSERT INTO op_credentials(id,cluster_id,kind,token_hash,expires_at) VALUES($1,$2,$3,$4,$5)`, c.ID, c.ClusterID, kind, TokenHash(c.Token), c.ExpiresAt)
	return c, err
}

func (s *Store) IssueEnrollment(ctx context.Context, actor, clusterID string) (*IssuedCredential, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var disconnected bool
	if err = tx.QueryRow(ctx, `SELECT disconnected FROM op_registrations WHERE id=$1 FOR UPDATE`, clusterID).Scan(&disconnected); err != nil {
		return nil, err
	}
	if disconnected {
		return nil, ErrDenied
	}
	c, err := issueCredential(ctx, tx, clusterID, "enrollment", 10*time.Minute)
	if err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, actor, "enrollment.issue", clusterID, map[string]string{credentialIDKey: c.ID}); err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}

func (s *Store) RedeemEnrollment(ctx context.Context, token, clusterID, kubeUID string) (*IssuedCredential, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	err = tx.QueryRow(ctx, `SELECT c.id FROM op_credentials c JOIN op_registrations r ON r.id=c.cluster_id
	 WHERE c.token_hash=$1 AND c.kind='enrollment' AND c.cluster_id=$2 AND r.kube_uid=$3 AND NOT r.disconnected
	 AND c.expires_at>now() AND c.used_at IS NULL AND c.revoked_at IS NULL FOR UPDATE OF c,r`, TokenHash(token), clusterID, kubeUID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDenied
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE op_credentials SET used_at=now() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	c, err := issueCredential(ctx, tx, clusterID, "runtime", 90*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, "enrollment:"+id, "enrollment.redeem", clusterID, map[string]string{credentialIDKey: c.ID}); err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}

func (s *Store) CredentialInfos(ctx context.Context) ([]CredentialInfo, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,cluster_id,kind,expires_at,used_at,revoked_at FROM op_credentials ORDER BY created_at DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CredentialInfo{}
	for rows.Next() {
		var c CredentialInfo
		if err = rows.Scan(&c.ID, &c.ClusterID, &c.Kind, &c.ExpiresAt, &c.UsedAt, &c.RevokedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, actor, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var clusterID string
	if err = tx.QueryRow(ctx, `UPDATE op_credentials SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 RETURNING cluster_id`, id).Scan(&clusterID); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "credential.revoke", clusterID, map[string]string{credentialIDKey: id}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Audit(ctx context.Context, before int64) ([]AuditEntry, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,actor,action,target,details,created_at FROM op_admin_audit WHERE ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT 100`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AuditEntry{}
	for rows.Next() {
		var a AuditEntry
		if err = rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Target, &a.Details, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
