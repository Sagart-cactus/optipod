package console

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type EnvironmentTemplate struct {
	Name         string       `json:"name"`
	Registration Registration `json:"registration"`
}

func (s *Store) SaveTemplate(ctx context.Context, actor string, template EnvironmentTemplate) error {
	if !identifier.MatchString(template.Name) || len(template.Name) > 63 {
		return ErrInvalid
	}
	r := template.Registration
	// Templates carry defaults, not a reusable identity or transport credential.
	if r.ID != "" || r.KubeUID != "" || r.Name != "" || r.Revision != 0 || r.RequireMTLS {
		return ErrInvalid
	}
	r.ID = "template"
	r.Name = "template"
	r.KubeUID = "00000000-0000-4000-8000-000000000000"
	if err := r.Validate(); err != nil {
		return err
	}
	data, _ := json.Marshal(template.Registration)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO op_templates(name,registration) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET registration=$2,updated_at=now()`, template.Name, data); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "template.save", template.Name, template.Registration); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Templates(ctx context.Context) ([]EnvironmentTemplate, error) {
	rows, err := s.Pool.Query(ctx, `SELECT name,registration FROM op_templates ORDER BY name LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []EnvironmentTemplate{}
	for rows.Next() {
		var template EnvironmentTemplate
		if err = rows.Scan(&template.Name, &template.Registration); err != nil {
			return nil, err
		}
		result = append(result, template)
	}
	return result, rows.Err()
}

// RegisterBulk is an atomic, idempotent create operation for GitOps reconciliation.
// It never changes existing grants, identities or registrations implicitly.
func (s *Store) RegisterBulk(ctx context.Context, actor string, registrations []Registration) error {
	if len(registrations) == 0 || len(registrations) > 50 {
		return ErrInvalid
	}
	registrations = slices.Clone(registrations)
	slices.SortFunc(registrations, func(a, b Registration) int { return strings.Compare(a.ID, b.ID) })
	seen := map[string]bool{}
	for _, r := range registrations {
		if err := r.Validate(); err != nil || seen[r.ID] {
			return ErrInvalid
		}
		seen[r.ID] = true
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, r := range registrations {
		if err = bulkRegistration(ctx, tx, actor, r); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func bulkRegistration(ctx context.Context, tx pgx.Tx, actor string, r Registration) error {
	if r.Namespaces == nil {
		r.Namespaces = []string{}
	}
	installation, _ := json.Marshal(r.Installation)
	// Serialize same-ID creation and reject collisions with legacy inventory.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,674303))`, r.ID); err != nil {
		return err
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT name=$2 AND environment=$3 AND kube_uid=$4 AND namespaces=$5 AND metrics_source=$6 AND install_mode=$7 AND install_method=$8 AND installation=$9::jsonb AND require_mtls=$10 FROM op_registrations WHERE id=$1`, r.ID, r.Name, r.Environment, r.KubeUID, r.Namespaces, r.MetricsSource, r.InstallMode, r.InstallMethod, installation, r.RequireMTLS).Scan(&matches)
	if err == nil {
		if !matches {
			return ErrConflict
		}
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var existing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_clusters WHERE id=$1)`, r.ID).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO op_registrations(id,name,environment,kube_uid,namespaces,metrics_source,install_mode,install_method,installation,require_mtls) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.Name, r.Environment, r.KubeUID, r.Namespaces, r.MetricsSource, r.InstallMode, r.InstallMethod, installation, r.RequireMTLS)
	if err != nil {
		return err
	}
	return audit(ctx, tx, actor, "cluster.register", r.ID, r)
}

// Group grants are recomputed for every session request. Disabling a database
// account overrides IdP groups; removing a group mapping revokes its sessions.
func (s *Server) groupReader(ctx context.Context, subject string, groups []string) (*Reader, error) {
	var disabled bool
	if err := s.Store.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM op_users WHERE subject=$1 AND disabled)`, subject).Scan(&disabled); err != nil {
		return nil, err
	}
	if disabled {
		return nil, nil
	}
	if len(groups) > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.Store.Pool.Query(ctx, `SELECT role,clusters,namespaces FROM op_groups WHERE name=ANY($1::text[]) AND NOT disabled`, groups)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r := &Reader{Subject: subject, Role: RoleViewer, Clusters: []string{}, NamespaceScopes: map[string][]string{}}
	found := false
	for rows.Next() {
		var role string
		var clusters, namespaces []string
		if err = rows.Scan(&role, &clusters, &namespaces); err != nil {
			return nil, err
		}
		found = true
		if role == RoleAdmin {
			r.Role = RoleAdmin
		}
		for _, id := range clusters {
			prior, exists := r.NamespaceScopes[id]
			if !exists {
				r.Clusters = append(r.Clusters, id)
				r.NamespaceScopes[id] = append([]string{}, namespaces...)
			} else if len(namespaces) == 0 {
				r.NamespaceScopes[id] = []string{}
			} else if len(prior) > 0 {
				r.NamespaceScopes[id] = append(prior, namespaces...)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if r.Role == RoleAdmin {
		r.NamespaceScopes = nil
	}

	return s.expandAdmin(ctx, r)
}

func safeGroupClaims(claims map[string]any, claim string) []string {
	values, ok := claims[claim].([]any)
	if !ok || len(values) > 100 {
		return nil
	}
	result := []string{}
	for _, v := range values {
		if group, ok := v.(string); ok && strings.TrimSpace(group) != "" && len(group) <= 200 {
			result = append(result, group)
		}
	}
	return result
}
