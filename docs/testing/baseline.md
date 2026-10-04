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
| Unit + production-path (`api/v1alpha1`, `internal/*`) | 142 Go `Test*` functions | **Yes** |
| Clusterless integration | `test/clusterless/workflows` (33 Ginkgo specs) | **Yes** (fixture/simulator layer — see note) |
| Envtest (real API server) | `internal/controller` Ginkgo "Integration Tests" suite (10 specs) | **Yes**, focused run |
| Legacy main Kind E2E | `test/e2e` Ginkgo suite | **No — unrun unchanged** (hazardous `BeforeSuite`) |
| Legacy contract Kind E2E | `test/e2e/contract` Ginkgo suite | **No — unrun unchanged** (hazardous `BeforeSuite`) |

The 33 clusterless specs exercise production calculation/handler code through **fake clients
and an independent webhook simulator**, not deployed admission or a live API server; a green
clusterless run is not evidence of installed-controller behavior (see
`legacy-hazards.md`).

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

A throwaway, collision-resistant cluster was created with a **private kubeconfig**
(`--kubeconfig <tmp>`) and the pinned node image, interacted with, then deleted. The user's
default kubeconfig and context were never passed to any command.

```
kind create cluster --name optipod-baseline-probe-<pid> --kubeconfig <tmp> \
  --image kindest/node:v1.35.0@sha256:4613778f...269661 --wait 90s
kubectl --kubeconfig <tmp> get nodes            # control-plane Ready, v1.35.0
kubectl --kubeconfig <tmp> create namespace probe-ns   # created + verified
kind delete cluster --name optipod-baseline-probe-<pid>
```

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

## Counts summary

| Metric | Value |
| --- | --- |
| Go `Test*` functions in fast-gate packages (`api/v1alpha1`, `internal/*`) | 142 |
| Clusterless Ginkgo specs executed | 33 |
| Envtest controller Integration specs | 10 declared (9 passed, 1 pending) |
| `cmd` package | no test files |

## Deliberately unexecuted / skipped paths

| Path | Why not run |
| --- | --- |
| `go test ./test/e2e/...` (main suite) | `BeforeSuite` switches the developer kube-context, installs `@latest`/unpinned tooling, edits tracked `config/manager`, regenerates tracked CRDs — see `legacy-hazards.md` |
| `go test ./test/e2e/contract/...` | `BeforeSuite` edits tracked `config/manager`, runs `make kustomize`, builds/loads an image and installs into a fixed-name cluster — see `legacy-hazards.md` |
| `make test` | Regenerates manifests/CRDs (`manifests generate`), formats code (`fmt`), and writes `cover.out`; not usable as a non-mutating gate |
| 1 controller envtest spec | Marked `Pending` in source; reported as pending, not passing |

## Post-baseline repository state

`git status --short` was **empty** after all commands above, confirming the viable fast
layer, vet, lint, focused envtest, image build, and the isolated Kind probe mutated no
tracked files and left no working-tree changes.
</content>
