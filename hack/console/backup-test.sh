#!/usr/bin/env bash
# Restore a real logical backup into a new disposable database; never replace
# the live local database. The cluster guard runs before any Kubernetes call.
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/bin:$PATH"
export KIND_EXPERIMENTAL_PROVIDER="${CONSOLE_CONTAINER_RUNTIME:-podman}"
if [[ "$(uname -s)" == Darwin ]]; then
  export CONTAINER_CONNECTION="${CONTAINER_CONNECTION:-podman-machine-default-root}"
fi
go run ./hack/console -action validate-primary
primary=$(python3 -c 'import json; print(json.load(open(".dev/cluster.json"))["Kubeconfig"])')
kubectl --kubeconfig "$primary" -n optipod-console-e2e exec -i postgres-0 -- sh -eu <<'SH'
backup="/tmp/console-restore-$$.dump"
restore_db="console_restore_$$"
trap 'dropdb -U postgres --if-exists "$restore_db"; rm -f "$backup"' EXIT
pg_dump -U postgres -d optipod --schema=public --format=custom --file="$backup"
createdb -U postgres "$restore_db"
pg_restore -U postgres --exit-on-error --clean --if-exists --no-owner --dbname="$restore_db" "$backup"
psql -U postgres -d "$restore_db" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
 IF (SELECT count(*) FROM op_workloads WHERE cluster_id IN ('local-qa','local-prod') AND namespace='optipod-console-demo' AND NOT deleted) <> 6 THEN
  RAISE EXCEPTION 'restored inventory mismatch';
 END IF;
 IF (SELECT count(*) FROM op_clusters WHERE id IN ('local-qa','local-prod') AND last_seq > 0) <> 2 THEN
  RAISE EXCEPTION 'restored stream checkpoints missing';
 END IF;
 IF (SELECT count(*) FROM op_history) = 0 OR (SELECT count(*) FROM op_sessions) = 0 THEN
  RAISE EXCEPTION 'restored history or sessions missing';
 END IF;
 IF (SELECT max(version) FROM op_schema) <> 2 THEN
  RAISE EXCEPTION 'restored migration version mismatch';
 END IF;
 IF to_regclass('op_bootstrap') IS NULL OR to_regclass('op_users') IS NULL
    OR to_regclass('op_registrations') IS NULL OR to_regclass('op_credentials') IS NULL
    OR to_regclass('op_admin_audit') IS NULL OR to_regclass('op_rate_limits') IS NULL THEN
  RAISE EXCEPTION 'restored onboarding schema missing';
 END IF;
END $$;
SQL
echo 'PASS: PostgreSQL backup restored inventory, history, sessions, stream checkpoints and onboarding schema into an isolated database'
SH
