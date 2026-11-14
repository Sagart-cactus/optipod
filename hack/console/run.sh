#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/bin:$PATH"
runtime="${CONSOLE_CONTAINER_RUNTIME:-podman}"
export KIND_EXPERIMENTAL_PROVIDER="$runtime"
if [[ "$(uname -s)" == Darwin ]]; then
  export CONTAINER_CONNECTION="${CONTAINER_CONNECTION:-podman-machine-default-root}"
fi
mkdir -p .dev/console
go run ./hack/console -action validate-primary
primary=$(python3 -c 'import json; print(json.load(open(".dev/cluster.json"))["Kubeconfig"])')
peer="$PWD/.dev/console/peer.kubeconfig"
ns=optipod-console-e2e
action="${1:-test}"

if [[ "$action" == up ]]; then
  if [[ ! -f "$peer" ]]; then
    kind create cluster --name optipod-e2e-console-peer --image kindest/node:v1.35.0@sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661 --kubeconfig "$peer"
    chmod 600 "$peer"
  fi
fi
go run ./hack/console -action validate

if [[ "$action" == up || "$action" == reload ]]; then
  python3 hack/console/local_config.py
  for cfg in "$primary" "$peer"; do
    kubectl --kubeconfig "$cfg" create namespace "$ns" --dry-run=client -o yaml | kubectl --kubeconfig "$cfg" apply -f -
    kubectl --kubeconfig "$cfg" apply -f config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml
  done
  kubectl --kubeconfig "$primary" -n "$ns" create secret generic database --from-file=password=.dev/console/password --dry-run=client -o yaml | kubectl --kubeconfig "$primary" apply -f -
  kubectl --kubeconfig "$primary" apply -f test/e2e/console/postgres.yaml
  kubectl --kubeconfig "$primary" -n "$ns" rollout status statefulset/postgres --timeout=180s
  kubectl --kubeconfig "$primary" -n "$ns" create secret generic console-server --from-file=config.json=.dev/console/server-config.json --from-file=database-url=.dev/console/database-url-in-cluster --dry-run=client -o yaml | kubectl --kubeconfig "$primary" apply -f -
  mkdir -p .dev/console/image
  CGO_ENABLED=0 GOOS=linux go build -trimpath -o .dev/console/image/optipod-console ./cmd/optipod-console
  tag="dev-$(shasum -a 256 .dev/console/image/optipod-console | cut -c1-12)"
  printf 'FROM gcr.io/distroless/static:nonroot\nCOPY optipod-console /optipod-console\nUSER 65532:65532\nENTRYPOINT ["/optipod-console"]\n' > .dev/console/image/Dockerfile
  "$runtime" build -t "docker.io/optipod-e2e/console:$tag" .dev/console/image
  "$runtime" save -o .dev/console/image.tar "docker.io/optipod-e2e/console:$tag"
  for name in optipod-e2e-dev optipod-e2e-console-peer; do
    kind load image-archive .dev/console/image.tar --name "$name"
  done
  primary_ip=$("$runtime" inspect optipod-e2e-dev-control-plane --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')
  for cluster in local-qa local-prod; do
    cfg="$primary"; server=true; endpoint=http://console-server:8080
    if [[ "$cluster" == local-prod ]]; then
      cfg="$peer"; server=false; endpoint="http://$primary_ip:31866"
    fi
    kubectl --kubeconfig "$cfg" -n "$ns" create secret generic console-agent --from-file=token=".dev/console/$cluster-token" --dry-run=client -o yaml | kubectl --kubeconfig "$cfg" apply -f -
    helm upgrade --install console charts/optipod-console --kubeconfig "$cfg" -n "$ns" \
      --set image.repository=docker.io/optipod-e2e/console --set "image.tag=$tag" \
      --set "server.enabled=$server" --set server.existingSecret=console-server \
      --set "server.configRevision=$(shasum -a 256 .dev/console/server-config.json | cut -c1-16)" \
      --set server.service.type=NodePort --set server.service.nodePort=31866 \
      --set agent.enabled=true --set "agent.clusterID=$cluster" --set "agent.serverURL=$endpoint" \
      --set agent.existingSecret=console-agent --set agent.allowInsecureHTTP=true --set agent.interval=10s \
      --wait --timeout 180s
    kubectl --kubeconfig "$cfg" -n "$ns" rollout status statefulset/console-agent --timeout=180s
  done
  go run ./hack/console -action seed
  rm -f .dev/console/image.tar
fi

if [[ "$action" == test ]]; then
  export OPTIPOD_TEST_DATABASE_URL_FILE="$PWD/.dev/console/database-url"
  export OPTIPOD_CONSOLE_E2E_KUBECONFIGS="$primary:$peer"
  export OPTIPOD_CONSOLE_E2E_URL="${OPTIPOD_CONSOLE_E2E_URL:-http://127.0.0.1:8876}"
  export OPTIPOD_CONSOLE_E2E_VIEWER_TOKEN_FILE="$PWD/.dev/console/viewer-token"
  go run ./hack/console -action wait-database
  curl -fsS --retry 20 --retry-connrefused --retry-all-errors --retry-delay 1 "$OPTIPOD_CONSOLE_E2E_URL/readyz" >/dev/null
  go test -count=1 -race ./internal/console/... ./internal/consoleagent/...
  go test -count=1 -v ./test/e2e/console/...
  bash hack/console/backup-test.sh
elif [[ "$action" == forward || "$action" == database-forward ]]; then
  service=console-server; ports=8876:8080
  if [[ "$action" == database-forward ]]; then service=postgres; ports=15432:5432; fi
  trap 'exit 0' INT TERM
  while true; do
    kubectl --kubeconfig "$primary" -n "$ns" port-forward --address 127.0.0.1 "service/$service" "$ports" || true
    sleep 1
  done
elif [[ "$action" != up && "$action" != reload ]]; then
  echo "usage: $0 up|reload|test|forward|database-forward" >&2
  exit 1
fi
