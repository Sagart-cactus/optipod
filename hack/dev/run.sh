#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH="$PWD/bin:$PATH"
export KIND_EXPERIMENTAL_PROVIDER=podman
export OPTIPOD_E2E_HOST_BUILD=1
# Override explicitly for Linux/rootless Podman or another dedicated connection.
if [[ "$(uname -s)" == Darwin ]]; then
  export CONTAINER_CONNECTION="${CONTAINER_CONNECTION:-podman-machine-default-root}"
fi
exec go run ./hack/dev -action "${1:-up}" -labels "${2:-smoke}"
