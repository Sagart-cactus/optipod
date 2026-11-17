#!/usr/bin/env bash
# Build a matched, versioned console/installer/optimizer bundle. No credentials.
set -euo pipefail
cd "$(dirname "$0")/../.."
version=0.3.1
controller_version=1.6.0-console.1
output="${1:-.dev/console/release}"
mkdir -p "$output"
output=$(cd "$output" && pwd)
python3 - <<'PY'
import pathlib, re
chart = pathlib.Path('charts/optipod-console/Chart.yaml').read_text()
assert re.search(r'^version: 0\.3\.1$', chart, re.M)
assert re.search(r'^appVersion: "?0\.3\.1"?$', chart, re.M)
version = pathlib.Path('internal/consoleversion/version.go').read_text()
assert 'ChartVersion = "0.3.1"' in version
plan = pathlib.Path('internal/consoleinstall/plan.go').read_text()
assert 'ControllerVersion = "1.6.0-console.1"' in plan
PY
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}; arch=${target#*/}
  staging=$(mktemp -d)
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X github.com/optipod/optipod/internal/consoleversion.Version=$version" \
    -o "$staging/optipod-console" ./cmd/optipod-console
  tar -czf "$output/optipod-console_${version}_${os}_${arch}.tar.gz" -C "$staging" optipod-console
  rm -rf "$staging"
done
helm dependency build charts/optipod-console --skip-refresh
helm lint charts/optipod-console --set server.existingSecret=release-test
helm package charts/optipod-console --destination "$output"
helm package charts/optipod --version "$controller_version" --app-version "console-$version" --destination "$output"
python3 - "$output" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = sorted(root.glob('*.tar.gz')) + sorted(root.glob('*.tgz'))
(root/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in files))
PY
