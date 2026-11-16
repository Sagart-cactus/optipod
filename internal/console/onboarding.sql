CREATE TABLE op_bootstrap (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 completed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE op_users (
 subject text PRIMARY KEY, role text NOT NULL CHECK(role IN ('admin','viewer')),
 token_hash text UNIQUE, clusters text[] NOT NULL DEFAULT '{}',
 namespaces text[] NOT NULL DEFAULT '{}', disabled boolean NOT NULL DEFAULT false
);
CREATE TABLE op_registrations (
 id text PRIMARY KEY, name text NOT NULL, environment text NOT NULL DEFAULT '',
 kube_uid text NOT NULL, namespaces text[] NOT NULL DEFAULT '{}',
 metrics_source text NOT NULL CHECK(metrics_source IN ('prometheus','metrics-server')),
 install_mode text NOT NULL CHECK(install_mode IN ('new','existing')),
 install_method text NOT NULL CHECK(install_method IN ('helm','gitops')),
 disconnected boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE op_credentials (
 id text PRIMARY KEY, cluster_id text NOT NULL REFERENCES op_registrations(id),
 token_hash text UNIQUE NOT NULL, kind text NOT NULL CHECK(kind IN ('enrollment','runtime')),
 expires_at timestamptz NOT NULL, used_at timestamptz, revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX op_credential_cluster ON op_credentials(cluster_id,created_at);
CREATE TABLE op_admin_audit (
 id bigserial PRIMARY KEY, actor text NOT NULL, action text NOT NULL,
 target text NOT NULL, details jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE op_rate_limits (
 key text PRIMARY KEY, window_start timestamptz NOT NULL, attempts integer NOT NULL
);
