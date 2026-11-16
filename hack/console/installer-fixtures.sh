#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/bin:$PATH"
runtime="${CONSOLE_CONTAINER_RUNTIME:-podman}"
export KIND_EXPERIMENTAL_PROVIDER="$runtime"
if [[ "$(uname -s)" == Darwin ]]; then export CONTAINER_CONNECTION="${CONTAINER_CONNECTION:-podman-machine-default-root}"; fi
# Refuse ambient/remote kubeconfigs before modifying any fixtures.
go run ./hack/console -action validate
primary=$(python3 -c 'import json; print(json.load(open(".dev/cluster.json"))["Kubeconfig"])')
peer="$PWD/.dev/console/peer.kubeconfig"
image=docker.io/optipod-e2e/installer-controller:dev
mkdir -p .dev/console/controller-image
CGO_ENABLED=0 GOOS=linux go build -trimpath -o .dev/console/controller-image/manager ./cmd/main.go
printf 'FROM gcr.io/distroless/static:nonroot\nCOPY manager /manager\nUSER 65532:65532\nENTRYPOINT ["/manager"]\n' > .dev/console/controller-image/Dockerfile
"$runtime" build -t "$image" .dev/console/controller-image
"$runtime" save -o .dev/console/installer-controller.tar "$image"
for name in optipod-e2e-dev optipod-e2e-console-peer; do kind load image-archive .dev/console/installer-controller.tar --name "$name"; done
rm .dev/console/installer-controller.tar
for cfg in "$primary" "$peer"; do
  kubectl --kubeconfig "$cfg" apply -f test/e2e/backends/metrics-server.yaml
  # Only these harness-owned Kind nodes use self-signed kubelet certificates.
  kubectl --kubeconfig "$cfg" -n kube-system patch deployment metrics-server --type=strategic -p \
    '{"spec":{"template":{"spec":{"containers":[{"name":"metrics-server","args":["--cert-dir=/tmp","--secure-port=10250","--kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname","--kubelet-use-node-status-port","--metric-resolution=15s","--kubelet-insecure-tls"]}]}}}}'
  kubectl --kubeconfig "$cfg" -n kube-system rollout status deployment/metrics-server --timeout=180s
done
kubectl --kubeconfig "$primary" apply -f test/e2e/backends/prometheus.yaml
kubectl --kubeconfig "$primary" -n optipod-e2e-backends rollout status deployment/prometheus --timeout=180s
existing=$(kubectl --kubeconfig "$primary" get deployments -A -o json | python3 -c 'import json,sys; print(sum(any(c["name"] in ["manager","controller-manager"] and ("optipod" in c["image"] or "optipod" in d["metadata"]["name"]) for c in d["spec"]["template"]["spec"]["containers"]) for d in json.load(sys.stdin)["items"]))')
if [[ "$existing" == 0 ]]; then
  helm upgrade --install installer-existing charts/optipod --kubeconfig "$primary" -n optipod-console-e2e \
    --set webhook.enabled=false --set certManager.install=false --set metricsProvider.type=metrics-server \
    --set image.repository=docker.io/optipod-e2e/installer-controller --set image.tag=dev --wait --timeout=180s
fi
