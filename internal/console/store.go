package console

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.ConnectTimeout = 10 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Store{Pool: p}, nil
}

// Migrate serializes concurrent startup and refuses unknown schema versions.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(674301)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS op_schema(version integer NOT NULL)`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM op_schema`).Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("database schema %d is newer than this server", version)
	}
	if version == 0 {
		if _, err = tx.Exec(ctx, schema); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO op_schema VALUES (1)`); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Prune applies a clock-driven retention policy even to inactive workloads.
// Current inventory and its latest policy revision remain available.
func (s *Store) Prune(ctx context.Context, days int) error {
	if days < 1 {
		return fmt.Errorf("retention must be at least one day")
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	for _, table := range []string{"op_history", "op_samples"} {
		if _, err := s.Pool.Exec(ctx, "DELETE FROM "+table+" WHERE observed_at < $1", cutoff); err != nil {
			return err
		}
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM op_policy_revisions r WHERE observed_at < $1
	 AND id NOT IN (SELECT max(id) FROM op_policy_revisions GROUP BY cluster_id,uid)`, cutoff); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM op_sessions WHERE expires_at < now()`)
	return err
}
