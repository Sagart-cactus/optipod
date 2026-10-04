# OptiPod test baseline (M1)

This is the versioned, measured baseline for the test-architecture refactor. It records
exact commands, exit codes, elapsed times, machine and cache state, spec/test counts, and
the paths that were deliberately **not** executed. It fulfills validation assertion
VAL-DESIGN-001.

> **Honesty boundary.** Every timing below is a **warm-cache, single-host measurement**
> taken on the machine described under "Environment and cache state". They are *not*
> universal cold-start numbers. Where a cold measurement exists from planning it is shown
> alongside the warm number so the two are never confused. A single pass is not evidence of
> zero flakiness.

## Environment and cache state

| Attribute | Value |
| --- | --- |
| Date measured | 2026-10-04 |
| OS / arch | macOS (darwin), x86_64 |
| Go | 1.26.1 |
| Logical CPUs / RAM | 16 / 32 GiB |
| Docker | Engine 29.0.1, ~8 GB allocated to the VM, >400 GiB disk free |
| Go build cache | **Warm** — populated by `init.sh` (envtest asset download) and prior planning runs before these measurements |
| Docker build cache | **Warm** — the production image was already built during planning on this host |
| Repo state | clean checkout of `main` (`bee5567`), then feature branch `feature/m1-baseline-hazard-audit` |

### Pinned tool versions used for the baseline

| Tool | Pinned version |
| --- | --- |
| Kind CLI | v0.31.0 |
| Kind node image | `kindest/node:v1.35.0@sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661` |
| kubectl (client) | v1.34.1 |
| kustomize | v5.8.1 |
| controller-gen | v0.22.0 |
| setup-envtest | release-0.25 (v0.25.2), Kubernetes **1.37.0** envtest assets |
| golangci-lint | v2.13.2 |
| actionlint | v1.7.7 |
| Ginkgo / controller-runtime | v2.32.2 / v0.25.1 (per `go.mod`) |

`KUBEBUILDER_ASSETS` resolved to
`<missionTools>/k8s/1.37.0-darwin-amd64` via the pinned `setup-envtest`.

## Declared versus executed test layers

| Layer | Declared location | Executed in this baseline? |
| --- | --- | --- |
| Unit + production-path (`api/v1alpha1`, `internal/*`) | **142 declared** Go `Test*` functions | **140 executed, 2 skipped** under `-short` (see note) |
| Clusterless integration | `test/clusterless/workflows` (33 Ginkgo specs) | **Yes** (fixture/simulator layer — see note) |
| Envtest (real API server) | `internal/controller` Ginkgo "Integration Tests" suite (10 specs) | **Yes**, focused run |
| Legacy main Kind E2E | `test/e2e` Ginkgo suite | **No — unrun unchanged** (hazardous `BeforeSuite`) |
| Legacy contract Kind E2E | `test/e2e/contract` Ginkgo suite | **No — unrun unchanged** (hazardous `BeforeSuite`) |

The 33 clusterless specs exercise production calculation/handler code through **fake clients
and an independent webhook simulator**, not deployed admission or a live API server; a green
clusterless run is not evidence of installed-controller behavior (see
`legacy-hazards.md`).

### Declared vs. executed Go `Test*` count (142 / 140 / 2)

The fast gate runs with `go test -short`. Of the **142** declared `Test*` functions in the
fast-gate packages, **140 execute** and **2 are skipped by design** under `-short` because
they are envtest-backed SSA integration tests that start their own API server:

| Skipped test | Source | Skip guard |
| --- | --- | --- |
| `TestSSAIntegration` | `internal/application/ssa_integration_test.go:46` | `if testing.Short() { t.Skip("Skipping integration test in short mode") }` (`:47-48`) |
| `TestSSAFieldOwnershipWithMultipleManagers` | `internal/application/ssa_integration_test.go:218` | `if testing.Short() { t.Skip(...) }` (`:219-220`) |

Measured skip evidence (`go test -short -run 'TestSSAIntegration|TestSSAFieldOwnershipWithMultipleManagers' -v ./internal/application`):

```
--- SKIP: TestSSAIntegration (0.00s)
    ssa_integration_test.go:48: Skipping integration test in short mode
--- SKIP: TestSSAFieldOwnershipWithMultipleManagers (0.00s)
    ssa_integration_test.go:220: Skipping integration test in short mode
PASS
ok  	github.com/optipod/optipod/internal/application
```

Both skips are counted as **skipped, not passing**. Their SSA managed-field-ownership
behavior is **not** exercised by any other executed layer in this baseline, and in
particular **not** by the focused controller envtest run below: that reconciler integration
constructs its `WorkloadProcessor` with a `nil` application engine
(`internal/controller/integration_test.go:147`), so no SSA apply or managed-field assertion
runs there. The SSA field-ownership proof therefore remains **unexecuted** at M1 and is
scheduled for production-path coverage in M3 (see `coverage-matrix.md` B04). The remaining
`internal/application` `Test*` functions (including the non-envtest SSA property tests such as
`TestProperty_UpdateRequestsOnlyBehavior`) run and pass under `-short`, but those are
patch-shape property checks, not applied before/after field-ownership outcomes.

## Viable fast layer — commands, exits, timings

All commands run from the repository root with the pinned `KUBEBUILDER_ASSETS` exported.
Times are `/usr/bin/time -p` wall-clock (`real`).

### Fast test gate

```
KUBEBUILDER_ASSETS=<pinned 1.37.0 path> \
  go test -short -count=1 ./api/... ./internal/... ./cmd/... ./test/clusterless/workflows/...
```

| Run | Exit | real | user | sys |
| --- | --- | --- | --- | --- |
| First measured (warm build cache) | 0 | 16.00s | 34.69s | 13.24s |
| Repeat (warm build cache) | 0 | 19.45s | 41.01s | 16.89s |

Per-package result and time (first run):

| Package | Result | Time |
| --- | --- | --- |
| `api/v1alpha1` | ok | 0.974s |
| `internal/application` | ok | 2.922s |
| `internal/cache` | ok | 3.350s |
| `internal/config` | ok | 0.403s |
| `internal/controller` | ok | 10.588s |
| `internal/discovery` | ok | 6.622s |
| `internal/metrics` | ok | 9.476s |
| `internal/observability` | ok | 4.314s |
| `internal/policy` | ok | 7.913s |
| `internal/recommendation` | ok | 5.779s |
| `internal/webhook` | ok | 11.472s |
| `cmd` | **no test files** | — |
| `test/clusterless/workflows` | ok (33 Ginkgo specs) | 6.983s |

### go vet

```
go vet ./api/... ./internal/... ./cmd/... ./test/...
```

| Exit | real | user | sys | Output |
| --- | --- | --- | --- | --- |
| 0 | 1.03s | 2.19s | 5.03s | no findings |

### golangci-lint (pinned)

```
<missionTools>/golangci-lint run --timeout=4m
```

| Exit | real | user | sys | Output |
| --- | --- | --- | --- | --- |
| 0 | 2.74s (**warm** lint cache) | 8.14s | 11.94s | `0 issues.` |

> Cold reference from planning on this host: ~105s. The 2.74s warm number must not be
> reported as a cold-cache lint time.

### Focused envtest (real ephemeral API server)

```
KUBEBUILDER_ASSETS=<pinned 1.37.0 path> \
  go test -count=1 -run '^TestControllers$' -v ./internal/controller
```

| Exit | real | Specs | Result |
| --- | --- | --- | --- |
| 0 | 11.29s | Ran 9 of 10 | 9 Passed, 0 Failed, **1 Pending**, 0 Skipped |

The single pending spec is `Integration Tests CRD Status Updates "Should update policy
status with workload information"` (`internal/controller/integration_test.go:193`). This is
a declared-but-not-executed spec and is counted as pending, not passing.

## Production image build

```
docker build -t optipod:baseline-probe .
```

| Exit | real | Cache state |
| --- | --- | --- |
| 0 | 3.19s | **Fully cached (warm)** — all layers `CACHED` |

> Cold reference from planning on this host: **272.7s**. The 3.19s figure reflects a warm
> Docker layer cache only and must never be presented as a cold build time. Dockerfile bases
> (`golang:1.26`, `gcr.io/distroless/static:nonroot`) resolve remotely.

## Isolated Kind probe

A throwaway, strongly-unique cluster was created with a **private kubeconfig**
(`--kubeconfig "$KCFG"`) and the pinned node image, interacted with, then deleted. The user's
default kubeconfig and context were never passed to any command. The recipe below is
**runnable as-is** from the repository root and is deliberately hardened against the three
ways a naive private-Kind probe can still damage developer state:

1. **Strongly unique owned name.** The cluster name combines the shell PID, the current epoch
   second, and `${RANDOM}` so it does not collide with a pre-existing cluster (a bare `$$` PID
   can be reused across reboots). The node image carries the **complete pinned digest**.
2. **Ownership-gated deletion.** A `CREATED` flag is set **only after** `kind create cluster`
   succeeds. Cleanup deletes the cluster **only when this run created it**, so a failed or
   skipped create never deletes a cluster that merely shares the name.
3. **Signal-correct exits, private kubeconfig on delete, fail-fast.** `set -euo pipefail` makes
   any failing phase abort immediately. Cleanup runs from **three dedicated traps**: `EXIT`
   preserves the ordinary success or failing-phase status, while `INT` and `TERM` inject an
   explicit `130`/`143` so an interrupt exits nonzero even when it lands right after a
   *successful* phase — a single shared `$?` trap would capture `0` there and report a false
   success. A run-once guard makes the scoped teardown execute exactly once across the
   signal→`EXIT` cascade. The delete passes `--kubeconfig "$KCFG"` so Kind cannot fall back to
   `$KUBECONFIG` or the developer's `~/.kube/config`; a cleanup failure is reported and forces a
   nonzero exit rather than being masked as success.

The probe never reads, writes, or deletes the developer's default kubeconfig, context, or any
pre-existing cluster.

```bash
set -euo pipefail   # fail-fast: any failing phase aborts and keeps its nonzero exit

# Pinned Kind v0.31.0 CLI and node image (complete digest, no `latest`).
NODE_IMAGE="kindest/node:v1.35.0@sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661"
# Strongly-unique owned name: PID + epoch second + ${RANDOM} (not a reusable bare PID).
CLUSTER="optipod-baseline-probe-$$-$(date +%s)-${RANDOM}"
KCFG="$(mktemp -t optipod-probe-kubeconfig.XXXXXX)"   # private kubeconfig, never the default
CREATED=0   # ownership flag: set to 1 only after THIS run creates the cluster
CLEANED=0   # run-once guard: the scoped teardown body executes exactly once

cleanup() {
  rc="$1"   # explicit status from the firing trap, NOT a shared $?
  if [ "$CLEANED" = "1" ]; then exit "$rc"; fi   # scoped teardown runs exactly once
  CLEANED=1
  if [ "$CREATED" = "1" ]; then
    # Delete ONLY the cluster this run owns, and pass the private kubeconfig so Kind
    # cannot fall back to $KUBECONFIG or the developer's ~/.kube/config.
    if ! kind delete cluster --name "$CLUSTER" --kubeconfig "$KCFG"; then
      echo "WARNING: cleanup of owned cluster $CLUSTER failed; remove it manually" >&2
      if [ "$rc" = "0" ]; then rc=1; fi   # ambiguous cleanup must not report success
    fi
  fi
  rm -f "$KCFG"
  exit "$rc"
}
# Dedicated traps so an interrupt exits nonzero even after a successful phase:
trap 'cleanup "$?"' EXIT   # ordinary success or failing-phase status
trap 'cleanup 130'  INT    # 128 + SIGINT(2)
trap 'cleanup 143'  TERM   # 128 + SIGTERM(15)

kind create cluster --name "$CLUSTER" --kubeconfig "$KCFG" \
  --image "$NODE_IMAGE" --wait 90s
CREATED=1   # we now own this cluster; cleanup is allowed to delete it

kubectl --kubeconfig "$KCFG" get nodes                 # control-plane Ready, v1.35.0
kubectl --kubeconfig "$KCFG" create namespace probe-ns # created + verified
kubectl --kubeconfig "$KCFG" get namespace probe-ns
# No explicit delete here: the ownership-gated cleanup() trap performs the only teardown,
# so a failure in any phase above still triggers scoped, private-kubeconfig cleanup.
```

The cleanup is **ownership-gated and owned-cluster-scoped**: it runs only when `CREATED=1`
(this run successfully created the cluster) and targets the strongly-unique probe name with the
private kubeconfig — never `kind delete cluster` without a name, never without `--kubeconfig`,
and never the developer's `kind` cluster. On an ordinary exit the `EXIT` trap preserves the
success or failing-phase status; if the probe is interrupted the dedicated `INT`/`TERM` traps
inject an explicit `130`/`143`, so a signal that lands right after a *successful* phase still
exits nonzero instead of a false success (a single shared `$?` trap captured `0` there). The
run-once guard makes the scoped teardown execute exactly once across the signal→`EXIT` cascade,
and if the delete itself fails that ambiguous cleanup is logged and forces a nonzero exit. The
`Delete` row below is the clean-exit timing; a mid-run failure or interrupt would report the
nonzero code after the same single scoped cleanup. The trap contract is proved below by a
shell-only simulation that stubs `kind` and never touches a real cluster.

| Phase | Exit | real |
| --- | --- | --- |
| Create (control-plane Ready after 17s) | 0 | 34.52s |
| API interaction (nodes + namespace create/get) | 0 | n/a (sub-second) |
| Delete | 0 | 1.46s |

**Developer-state preservation (snapshot before/after):**

| State | Before | After |
| --- | --- | --- |
| `kind get clusters` | `kind` | `kind` (unchanged) |
| `kubectl config current-context` | `kind-kind` | `kind-kind` (unchanged) |

The probe proves ephemeral private-kubeconfig cluster lifecycle only. It does **not** prove
installed-controller, webhook, or ready-workload behavior; those are M2+ acceptance checks.

### Shell-only trap simulation (no real cluster)

The signal-exit contract is verified **without** creating or deleting a Kind cluster: the
probe's exact trap wiring is driven with `kind create cluster` / `kind delete cluster` replaced
by shell stubs. Each scenario runs as a **foreground** process that self-signals via `$$` after
a successful phase. Foreground execution is required: a backgrounded job inherits `SIGINT` as
ignored (POSIX async behaviour), so its `INT` trap would never fire and the test would be a
false negative. The stubbed cleanup appends one line per invocation to a log, so the line count
proves the scoped teardown ran exactly once.

```bash
set -euo pipefail
MODE="$1"; CLEAN_LOG="$2"
CREATED=0
CLEANED=0

cleanup() {
  rc="$1"
  if [ "$CLEANED" = "1" ]; then exit "$rc"; fi   # scoped teardown runs exactly once
  CLEANED=1
  if [ "$CREATED" = "1" ]; then
    echo cleanup >> "$CLEAN_LOG"   # stub for: kind delete cluster --name ... --kubeconfig ...
  fi
  exit "$rc"
}
trap 'cleanup "$?"' EXIT   # ordinary success or failing-phase status
trap 'cleanup 130'  INT    # 128 + SIGINT(2)
trap 'cleanup 143'  TERM   # 128 + SIGTERM(15)

CREATED=1                  # stub for a successful: kind create cluster ...
true                       # a SUCCESSFUL phase, so a naive $? would be 0 at the signal
case "$MODE" in
  normal) : ;;
  fail)   false ;;                 # a failing phase under set -e
  int)    kill -INT  "$$" ; sleep 10 ;;
  term)   kill -TERM "$$" ; sleep 10 ;;
esac
```

Measured results (each mode run in the foreground as `bash <script> "$MODE" "$log"`, exit code
captured, cleanup-log lines counted; `bash 3.2.57` on this host):

| Mode | Exit (want) | Scoped cleanup invocations (want) |
| --- | --- | --- |
| `normal` | 0 (0) | 1 (1) |
| `fail` | 1 (1) | 1 (1) |
| `int` (SIGINT after a good phase) | 130 (130) | 1 (1) |
| `term` (SIGTERM after a good phase) | 143 (143) | 1 (1) |

For contrast, the previous single `trap cleanup EXIT INT TERM` reading `rc=$?` returns exit
**0** for both `int` and `term`, because `$?` is `0` after the successful phase — the
false-success this fix removes. The `normal` and `fail` exits are unchanged by the fix, so
ordinary success and failing-phase behaviour are preserved.

## Counts summary

| Metric | Value |
| --- | --- |
| Go `Test*` functions declared in fast-gate packages (`api/v1alpha1`, `internal/*`) | 142 |
| Go `Test*` functions executed under `-short` | 140 |
| Go `Test*` functions skipped under `-short` (envtest SSA integration) | 2 |
| Clusterless Ginkgo specs executed | 33 |
| Envtest controller Integration specs | 10 declared (9 passed, 1 pending) |
| `cmd` package | no test files |

## Deliberately unexecuted / skipped paths

| Path | Why not run |
| --- | --- |
| `go test ./test/e2e/...` (main suite) | `BeforeSuite` switches the developer kube-context, installs `@latest`/unpinned tooling, edits tracked `config/manager`, regenerates tracked CRDs — see `legacy-hazards.md` |
| `go test ./test/e2e/contract/...` | `BeforeSuite` edits tracked `config/manager`, runs `make kustomize`, builds/loads an image and installs into a fixed-name cluster — see `legacy-hazards.md` |
| `make test` | Regenerates manifests/CRDs (`manifests generate`), formats code (`fmt`), and writes `cover.out`; not usable as a non-mutating gate |
| 2 `internal/application` SSA tests | `TestSSAIntegration` and `TestSSAFieldOwnershipWithMultipleManagers` self-skip under `-short` (`ssa_integration_test.go:48,220`); reported as skipped, not passing |
| 1 controller envtest spec | Marked `Pending` in source; reported as pending, not passing |

## Post-baseline repository state

`git status --short` was **empty** after all commands above, confirming the viable fast
layer, vet, lint, focused envtest, image build, and the isolated Kind probe mutated no
tracked files and left no working-tree changes.
