package console

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Installation contains preferences and resumable wizard progress, never credentials.
type Installation struct {
	Namespace           string `json:"namespace,omitempty"`
	Release             string `json:"release,omitempty"`
	ControllerNamespace string `json:"controllerNamespace,omitempty"`
	ControllerRelease   string `json:"controllerRelease,omitempty"`
	PrometheusURL       string `json:"prometheusURL,omitempty"`
	StorageClass        string `json:"storageClass,omitempty"`
	Stage               string `json:"stage,omitempty"`
}

func (i Installation) Validate() error {
	if len(i.Release) > 53 || len(i.ControllerRelease) > 53 {
		return ErrInvalid
	}
	for _, value := range []string{i.Namespace, i.Release, i.ControllerNamespace, i.ControllerRelease, i.StorageClass} {
		if value != "" && len(validation.IsDNS1123Label(value)) != 0 {
			return ErrInvalid
		}
	}
	if !slices.Contains([]string{"", "registered", "preflight", "install", "verify"}, i.Stage) {
		return ErrInvalid
	}
	if i.PrometheusURL != "" {
		u, err := url.Parse(i.PrometheusURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != schemeHTTPS) {
			return ErrInvalid
		}
	}
	return nil
}

type UserGrant struct {
	Subject    string   `json:"subject"`
	Role       string   `json:"role"`
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces"`
	Disabled   bool     `json:"disabled"`
	IssueToken bool     `json:"issueToken,omitempty"`
}

func (g UserGrant) Validate() error {
	if strings.TrimSpace(g.Subject) == "" || len(g.Subject) > 200 || (g.Role != RoleAdmin && g.Role != RoleViewer) || len(g.Clusters) > 1000 || len(g.Namespaces) > 100 {
		return ErrInvalid
	}
	if g.Role == RoleAdmin && (len(g.Clusters) > 0 || len(g.Namespaces) > 0) {
		return ErrInvalid
	}
	for _, ns := range g.Namespaces {
		if len(validation.IsDNS1123Label(ns)) > 0 {
			return ErrInvalid
		}
	}
	return nil
}

func (s *Store) SaveGrant(ctx context.Context, actor string, g UserGrant, group bool) (string, error) {
	if err := g.Validate(); err != nil || (group && g.IssueToken) {
		return "", ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize changes to administrators so concurrent demotions cannot remove the last one.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(674302)`); err != nil {
		return "", err
	}
	if g.Clusters == nil {
		g.Clusters = []string{}
	}
	if g.Namespaces == nil {
		g.Namespaces = []string{}
	}
	for _, id := range g.Clusters {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_registrations WHERE id=$1 UNION SELECT 1 FROM op_clusters WHERE id=$1)`, id).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return "", ErrInvalid
		}
	}
	if group {
		_, err = tx.Exec(ctx, `INSERT INTO op_groups(name,role,clusters,namespaces,disabled) VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(name) DO UPDATE SET role=$2,clusters=$3,namespaces=$4,disabled=$5`, g.Subject, g.Role, g.Clusters, g.Namespaces, g.Disabled)
	} else {
		var last bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_users WHERE subject=$1 AND role='admin' AND NOT disabled)
		 AND (SELECT count(*) FROM op_users WHERE role='admin' AND NOT disabled)=1`, g.Subject).Scan(&last)
		if err != nil {
			return "", err
		}
		if last && (g.Disabled || g.Role != RoleAdmin) {
			return "", ErrConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO op_users(subject,role,clusters,namespaces,disabled) VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(subject) DO UPDATE SET role=$2,clusters=$3,namespaces=$4,disabled=$5`, g.Subject, g.Role, g.Clusters, g.Namespaces, g.Disabled)
	}
	if err != nil {
		return "", err
	}
	token := ""
	if g.IssueToken {
		token = randomToken()
		if _, err = tx.Exec(ctx, `UPDATE op_users SET token_hash=$2 WHERE subject=$1`, g.Subject, TokenHash(token)); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM op_sessions WHERE subject=$1`, g.Subject); err != nil {
			return "", err
		}
	}
	action := "user.save"
	if group {
		action = "group.save"
	}
	if err = audit(ctx, tx, actor, action, g.Subject, g); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

func (s *Store) Grants(ctx context.Context, group bool) ([]UserGrant, error) {
	query := `SELECT subject,role,clusters,namespaces,disabled FROM op_users ORDER BY subject LIMIT 1000`
	if group {
		query = `SELECT name,role,clusters,namespaces,disabled FROM op_groups ORDER BY name LIMIT 1000`
	}
	rows, err := s.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []UserGrant{}
	for rows.Next() {
		var g UserGrant
		if err = rows.Scan(&g.Subject, &g.Role, &g.Clusters, &g.Namespaces, &g.Disabled); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *Store) UpdateRegistration(ctx context.Context, actor string, r Registration) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Revision < 1 {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Once credentials exist, UID, scope and transport binding are immutable.
	// Name/environment and installer progress can still be edited without data loss.
	var uid string
	var namespaces []string
	var issued, requireMTLS bool
	err = tx.QueryRow(ctx, `SELECT kube_uid,namespaces,require_mtls,EXISTS(SELECT 1 FROM op_credentials WHERE cluster_id=$1) FROM op_registrations WHERE id=$1 FOR UPDATE`, r.ID).Scan(&uid, &namespaces, &requireMTLS, &issued)
	if err != nil {
		return err
	}
	if uid != r.KubeUID || r.RequireMTLS != requireMTLS || (issued && !slices.Equal(namespaces, r.Namespaces)) {
		return ErrConflict
	}
	if r.Namespaces == nil {
		r.Namespaces = []string{}
	}
	installation, _ := json.Marshal(r.Installation)
	result, err := tx.Exec(ctx, `UPDATE op_registrations SET name=$2,environment=$3,namespaces=$4,metrics_source=$5,install_mode=$6,install_method=$7,installation=$8,revision=revision+1,updated_at=now() WHERE id=$1 AND revision=$9`, r.ID, r.Name, r.Environment, r.Namespaces, r.MetricsSource, r.InstallMode, r.InstallMethod, installation, r.Revision)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	if err = audit(ctx, tx, actor, "cluster.update", r.ID, r); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetConnected(ctx context.Context, actor, id string, connected bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE op_registrations SET disconnected=$2,revision=revision+1,updated_at=now() WHERE id=$1`, id, !connected)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	if !connected {
		if _, err = tx.Exec(ctx, `UPDATE op_credentials SET revoked_at=COALESCE(revoked_at,now()) WHERE cluster_id=$1`, id); err != nil {
			return err
		}
	}
	action := "cluster.disconnect"
	if connected {
		action = "cluster.reconnect"
	}
	if err = audit(ctx, tx, actor, action, id, map[string]bool{"historyRetained": true}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO op_notifications(event_key,cluster_id,kind,message) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, randomToken(), id, action, "Telemetry connection changed; optimizer behavior is unchanged."); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
