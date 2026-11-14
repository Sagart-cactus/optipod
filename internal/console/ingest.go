package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrConflict = errors.New("cluster identity, stream or sequence conflict")

// Ingest acknowledges only committed batches. A retry of the same batch is a
// no-op; a different payload at the same sequence is rejected.
func (s *Store) Ingest(ctx context.Context, id, name string, b *Batch) error {
	if err := b.Validate(); err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO op_clusters(id,name,kube_uid,stream_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, name, b.ClusterUID, b.StreamID)
	if err != nil {
		return err
	}
	var uid, stream string
	var last int64
	err = tx.QueryRow(ctx, `SELECT kube_uid,stream_id,last_seq FROM op_clusters WHERE id=$1 FOR UPDATE`, id).Scan(&uid, &stream, &last)
	if err != nil {
		return err
	}
	if uid != b.ClusterUID || stream != b.StreamID {
		return ErrConflict
	}
	digest := Digest(b)
	if b.Sequence <= last {
		var prior string
		if err = tx.QueryRow(ctx, `SELECT digest FROM op_batches WHERE cluster_id=$1 AND seq=$2`, id, b.Sequence).Scan(&prior); err != nil {
			return err
		}
		if prior != digest {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if b.Sequence != last+1 {
		return ErrConflict
	}
	if err = ingestInventory(ctx, tx, id, b); err != nil {
		return err
	}
	for _, r := range b.Records {
		data, marshalErr := json.Marshal(r)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO op_history(cluster_id,uid,namespace,name,record_name,digest,phase,observed_at,created_at,data)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, id, string(r.Workload.UID), r.Workload.Namespace, r.Workload.Name, r.Name, Digest(r), r.Phase, b.CapturedAt, r.Created, data)
		if err != nil {
			return err
		}
	}
	if b.Complete {
		for _, table := range []string{"op_workloads", "op_policies"} {
			if _, err = tx.Exec(ctx, "UPDATE "+table+" SET deleted=true WHERE cluster_id=$1 AND run_id<>$2", id, b.RunID); err != nil {
				return err
			}
		}
		warnings, _ := json.Marshal(b.Warnings)
		if _, err = tx.Exec(ctx, `UPDATE op_clusters SET name=$2,captured_at=$3,received_at=now(),warnings=$4 WHERE id=$1`, id, name, b.CapturedAt, warnings); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE op_clusters SET last_seq=$2 WHERE id=$1`, id, b.Sequence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO op_batches(cluster_id,seq,digest) VALUES($1,$2,$3)`, id, b.Sequence, digest); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ingestInventory(ctx context.Context, tx pgx.Tx, id string, b *Batch) error {
	for _, w := range b.Workloads {
		data, err := json.Marshal(w)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO op_workloads(cluster_id,uid,namespace,name,kind,mode,run_id,observed_at,data)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(cluster_id,uid) DO UPDATE SET namespace=EXCLUDED.namespace,name=EXCLUDED.name,kind=EXCLUDED.kind,mode=EXCLUDED.mode,run_id=EXCLUDED.run_id,observed_at=EXCLUDED.observed_at,data=EXCLUDED.data,deleted=false`, id, w.UID, w.Namespace, w.Name, w.Kind, string(w.Mode), b.RunID, b.CapturedAt, data)
		if err != nil {
			return err
		}
		sample, _ := json.Marshal(map[string]int64{"requestedMilliCPU": w.RequestedMilliCPU, "requestedMemoryBytes": w.RequestedMemoryBytes})
		if _, err = tx.Exec(ctx, `INSERT INTO op_samples(cluster_id,uid,namespace,name,observed_at,data) VALUES($1,$2,$3,$4,$5,$6)`, id, w.UID, w.Namespace, w.Name, b.CapturedAt, sample); err != nil {
			return err
		}
	}
	for _, p := range b.Policies {
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO op_policies(cluster_id,uid,namespace,name,run_id,observed_at,data)
		 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(cluster_id,uid) DO UPDATE SET run_id=EXCLUDED.run_id,observed_at=EXCLUDED.observed_at,data=EXCLUDED.data,deleted=false`, id, p.UID, p.Namespace, p.Name, b.RunID, b.CapturedAt, data)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO op_policy_revisions(cluster_id,uid,namespace,name,generation,digest,observed_at,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, id, p.UID, p.Namespace, p.Name, p.Generation, Digest(p), b.CapturedAt, data)
		if err != nil {
			return fmt.Errorf("policy revision: %w", err)
		}
	}
	return nil
}
