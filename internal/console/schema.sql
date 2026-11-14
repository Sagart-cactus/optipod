CREATE TABLE IF NOT EXISTS op_clusters (
 id text PRIMARY KEY, name text NOT NULL, kube_uid text NOT NULL,
 stream_id text NOT NULL, last_seq bigint NOT NULL DEFAULT 0,
 captured_at timestamptz, received_at timestamptz,
 warnings jsonb NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS op_batches (
 cluster_id text NOT NULL REFERENCES op_clusters(id), seq bigint NOT NULL,
 digest text NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(cluster_id, seq)
);
CREATE TABLE IF NOT EXISTS op_workloads (
 id bigserial PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_clusters(id),
 uid text NOT NULL, namespace text NOT NULL, name text NOT NULL, kind text NOT NULL,
 mode text NOT NULL, run_id text NOT NULL, observed_at timestamptz NOT NULL,
 deleted boolean NOT NULL DEFAULT false, data jsonb NOT NULL,
 UNIQUE(cluster_id, uid)
);
CREATE INDEX IF NOT EXISTS op_workload_scope ON op_workloads(cluster_id, namespace, id);
CREATE TABLE IF NOT EXISTS op_policies (
 id bigserial PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_clusters(id),
 uid text NOT NULL, namespace text NOT NULL, name text NOT NULL,
 run_id text NOT NULL, observed_at timestamptz NOT NULL,
 deleted boolean NOT NULL DEFAULT false, data jsonb NOT NULL,
 UNIQUE(cluster_id, uid)
);
CREATE TABLE IF NOT EXISTS op_policy_revisions (
 id bigserial PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_clusters(id),
 uid text NOT NULL, namespace text NOT NULL, name text NOT NULL,
 generation bigint NOT NULL, digest text NOT NULL,
 observed_at timestamptz NOT NULL, data jsonb NOT NULL,
 UNIQUE(cluster_id, uid, digest)
);
CREATE TABLE IF NOT EXISTS op_history (
 id bigserial PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_clusters(id),
 uid text NOT NULL, namespace text NOT NULL, name text NOT NULL,
 record_name text NOT NULL, digest text NOT NULL, phase text NOT NULL,
 observed_at timestamptz NOT NULL, created_at timestamptz NOT NULL,
 data jsonb NOT NULL, UNIQUE(cluster_id, namespace, record_name, digest)
);
CREATE INDEX IF NOT EXISTS op_history_scope ON op_history(cluster_id, namespace, uid, id DESC);
CREATE INDEX IF NOT EXISTS op_history_retention ON op_history(observed_at);
CREATE TABLE IF NOT EXISTS op_samples (
 id bigserial PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_clusters(id),
 uid text NOT NULL, namespace text NOT NULL, name text NOT NULL,
 observed_at timestamptz NOT NULL, data jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS op_samples_scope ON op_samples(cluster_id, uid, id DESC);
CREATE INDEX IF NOT EXISTS op_samples_retention ON op_samples(observed_at);
CREATE TABLE IF NOT EXISTS op_sessions (
 token_hash text PRIMARY KEY, subject text NOT NULL, clusters text[] NOT NULL,
 expires_at timestamptz NOT NULL
);
