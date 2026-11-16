ALTER TABLE op_registrations ADD COLUMN installation jsonb NOT NULL DEFAULT '{}';
ALTER TABLE op_registrations ADD COLUMN revision bigint NOT NULL DEFAULT 1;
ALTER TABLE op_registrations ADD COLUMN require_mtls boolean NOT NULL DEFAULT false;
ALTER TABLE op_credentials ADD COLUMN public_key_hash text NOT NULL DEFAULT '';
ALTER TABLE op_sessions ADD COLUMN oidc_groups text[];
CREATE TABLE op_groups (
 name text PRIMARY KEY, role text NOT NULL CHECK(role IN ('admin','viewer')),
 clusters text[] NOT NULL DEFAULT '{}', namespaces text[] NOT NULL DEFAULT '{}',
 disabled boolean NOT NULL DEFAULT false
);
CREATE TABLE op_templates (
 name text PRIMARY KEY, registration jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE op_agent_health (
 cluster_id text PRIMARY KEY, received_at timestamptz NOT NULL DEFAULT now(),
 data jsonb NOT NULL
);
CREATE TABLE op_notifications (
 id bigserial PRIMARY KEY, event_key text UNIQUE NOT NULL,
 cluster_id text NOT NULL, kind text NOT NULL, message text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), acknowledged_at timestamptz
);
CREATE TABLE op_recovery_checks (
 id bigserial PRIMARY KEY, verified_at timestamptz NOT NULL DEFAULT now(),
 kind text NOT NULL CHECK(kind='logical-restore'), schema_version integer NOT NULL,
 details jsonb NOT NULL
);
