package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/report"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	path := os.Getenv("OPTIPOD_TEST_DATABASE_URL_FILE")
	if path == "" {
		t.Skip("set OPTIPOD_TEST_DATABASE_URL_FILE for real PostgreSQL integration tests")
	}
	dsn, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := Open(ctx, string(dsn))
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("test_console_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(string(dsn))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		admin.Pool.Close()
	})
	s := &Store{Pool: pool}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func testBatch(seq int64) *Batch {
	return &Batch{Version: 1, ClusterUID: "kube-uid", StreamID: "stream", Sequence: seq, RunID: "run", CapturedAt: time.Now().UTC(), Complete: true,
		Workloads: []report.Workload{{UID: "same-uid", Namespace: "team", Name: "web", Kind: "Deployment", Mode: api.ModeRecommend, RequestedMilliCPU: 500}},
		Policies:  []Policy{{UID: "policy-uid", Namespace: "team", Name: "policy", Generation: 1, Spec: api.OptimizationPolicySpec{Mode: api.ModeRecommend}}},
		Records:   []*history.Record{{Version: 1, Name: "record", Workload: history.Reference{UID: "same-uid", Namespace: "team", Name: "web"}, Phase: "Prepared", Created: time.Now().UTC()}},
	}
}

func TestPostgresIsolationReplayAndDeletion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b := testBatch(1)
	for _, id := range []string{"qa", "prod"} {
		if err := s.Ingest(ctx, id, id, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Ingest(ctx, "qa", "QA", b); err != nil {
		t.Fatal(err)
	}
	changed := *b
	changed.Workloads = nil
	if err := s.Ingest(ctx, "qa", "QA", &changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	changed = *b
	changed.Sequence = 3
	if err := s.Ingest(ctx, "qa", "QA", &changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("gap: %v", err)
	}
	for _, kind := range []string{"workloads", "history", "policies", "revisions", "samples"} {
		p, err := s.List(ctx, kind, Query{Clusters: []string{"qa"}})
		if err != nil || len(p.Items) != 1 || p.Items[0].ClusterID != "qa" {
			t.Fatalf("%s isolation/replay: %+v %v", kind, p, err)
		}
		p, err = s.List(ctx, kind, Query{Clusters: []string{"qa"}, Cluster: "prod"})
		if err != nil || len(p.Items) != 0 {
			t.Fatalf("cross-cluster disclosure: %+v %v", p, err)
		}
	}
	// Partial collection must not tombstone the last good inventory.
	empty := testBatch(2)
	empty.RunID = "empty"
	empty.Complete = false
	empty.Workloads = nil
	empty.Policies = nil
	empty.Records = nil
	if err := s.Ingest(ctx, "qa", "QA", empty); err != nil {
		t.Fatal(err)
	}
	p, _ := s.List(ctx, "workloads", Query{Clusters: []string{"qa"}})
	if len(p.Items) != 1 {
		t.Fatal("partial collection deleted inventory")
	}
	empty.Sequence = 3
	empty.Complete = true
	if err := s.Ingest(ctx, "qa", "QA", empty); err != nil {
		t.Fatal(err)
	}
	p, _ = s.List(ctx, "workloads", Query{Clusters: []string{"qa"}})
	if len(p.Items) != 0 {
		t.Fatal("deleted workload still active")
	}
	p, _ = s.List(ctx, "workloads", Query{Clusters: []string{"qa"}, IncludeDeleted: true})
	if len(p.Items) != 1 || !p.Items[0].Deleted {
		t.Fatal("missing tombstone")
	}
	p, _ = s.List(ctx, "history", Query{Clusters: []string{"qa"}})
	if len(p.Items) != 1 {
		t.Fatal("deletion lost history")
	}
	// A second connection/process sees committed data without Kubernetes.
	s2 := &Store{Pool: s.Pool}
	clusters, err := s2.Clusters(ctx, []string{"qa", "prod"})
	if err != nil || len(clusters) != 2 {
		t.Fatalf("clusters: %+v %v", clusters, err)
	}
}

func TestPostgresPaginationRetentionAndIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	b := testBatch(1)
	if err := s.Ingest(ctx, "qa", "QA", b); err != nil {
		t.Fatal(err)
	}
	b.Sequence = 2
	b.ClusterUID = "different-cluster"
	if err := s.Ingest(ctx, "qa", "QA", b); !errors.Is(err, ErrConflict) {
		t.Fatal("cluster identity accepted")
	}
	b.ClusterUID = "kube-uid"
	b.StreamID = "other-writer"
	if err := s.Ingest(ctx, "qa", "QA", b); !errors.Is(err, ErrConflict) {
		t.Fatal("second writer accepted")
	}
	b.StreamID = "stream"
	b.Records[0].Phase = "Applied"
	if err := s.Ingest(ctx, "qa", "QA", b); err != nil {
		t.Fatal(err)
	}
	p, err := s.List(ctx, "history", Query{Clusters: []string{"qa"}, Limit: 1})
	if err != nil || len(p.Items) != 1 || p.NextCursor == "" {
		t.Fatalf("first page: %+v %v", p, err)
	}
	next, err := s.List(ctx, "history", Query{Clusters: []string{"qa"}, Limit: 1, Cursor: p.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == p.Items[0].ID || next.NextCursor != "" {
		t.Fatalf("next page: %+v %v", next, err)
	}
	if _, err = s.List(ctx, "history", Query{Cursor: "1 OR true"}); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE op_history SET observed_at=now()-interval '100 days'`); err != nil {
		t.Fatal(err)
	}
	if err = s.Prune(ctx, 90); err != nil {
		t.Fatal(err)
	}
	p, err = s.List(ctx, "history", Query{Clusters: []string{"qa"}})
	if err != nil || len(p.Items) != 0 {
		t.Fatal("inactive history not expired")
	}
	p, err = s.List(ctx, "workloads", Query{Clusters: []string{"qa"}})
	if err != nil || len(p.Items) != 1 {
		t.Fatal("retention deleted current inventory")
	}
}

func TestPostgresConcurrentMigrationAndFutureVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	results := make(chan error, 4)
	for range 4 {
		go func() { results <- s.Migrate(ctx) }()
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM op_schema`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration ran more than once: %d %v", count, err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE op_schema SET version=2`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("older server accepted a future schema")
	}
}
