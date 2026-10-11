# OptiPod test-architecture design (M1)

This is the approved, versioned design for the hermetic Kind harness, suite lifecycle,
deterministic metrics fixture, focus labels, Make targets, CI topology and the first
executable acceptance checks. It is authored **before** any test implementation and supports
validation assertion **VAL-DESIGN-003** (alongside `coverage-matrix.md`). It restates the
boundaries in `../../architecture.md` as concrete interfaces; it does **not** implement the
harness in M1. Companion documents: `baseline.md` (VAL-DESIGN-001), `inventory.md`
(VAL-DESIGN-002), `legacy-hazards.md` (VAL-DESIGN-004).

## 1. Pins (no `latest` in the test path)

| Input | Pinned value |
| --- | --- |
| Kind CLI | v0.31.0 (driven as a subprocess, **not** imported into the root module) |
| Kind node image | `kindest/node:v1.35.0@sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661` |
| kubectl | v1.34.1 |
| kustomize | v5.8.1 |
| controller-gen | v0.22.0 |
| setup-envtest | release-0.25 (v0.25.2), Kubernetes **1.37.0** envtest assets |
| golangci-lint | v2.13.2 |
| actionlint | v1.7.7 |
| Ginkgo / Gomega | v2.32.2 / per `go.mod` |
| controller-runtime / client-go | v0.25.1 / per `go.mod` |
| Metrics fixture image | a pinned Prometheus-compatible image **with digest**, selected and recorded in M2 |

Every external binary and image in the test path is pinned/checksummed. The harness never runs
`go install ...@latest`, never applies the floating metrics-server manifest, and never pulls an
untagged image (contrast `legacy-hazards.md` H3, H4, C3).

## 2. Framework package and interfaces

The Go framework lives under `test/e2e/framework` (a test-only package). It is organised into
coherent responsibilities rather than one monolithic `BeforeSuite`. The interfaces below are
the **design contract**; signatures are illustrative and finalised in M2.

```go
package framework

// Preflight verifies all pinned prerequisites and returns an actionable error naming the
// missing/incompatible item. It creates no cluster.
type Preflight interface {
    Check(ctx context.Context) error // Docker daemon, Kind CLI+node image, kubectl,
                                      // kustomize/controller-gen, arch, disk, dynamic port bind
}

// Cluster owns exactly one collision-resistant Kind cluster and a private kubeconfig.
type Cluster interface {
    Name() string                 // e.g. optipod-e2e-<rand>; never a fixed name
    KubeconfigPath() string       // temp file; contents never logged/archived
    RESTConfig() *rest.Config      // derived from the private kubeconfig only
    Create(ctx context.Context) error
    Delete(ctx context.Context) error // ownership-checked; refuses non-owned clusters
    Owns(name string) bool
}

// Installer builds+loads the checkout image once and installs CRDs/RBAC/controller/webhook
// once from manifests rendered to temp files (no edits to tracked config/manager).
type Installer interface {
    BuildAndLoadImage(ctx context.Context) (imageRef string, err error)
    Install(ctx context.Context, opts InstallOptions) error // CRDs, RBAC, controller, webhook
    AwaitReady(ctx context.Context) error // CRD Established, Deployment Available, webhook
                                          // service/endpoints/certificate validity
}

// MetricsFixture deploys an in-cluster, pinned Prometheus-compatible service and serves a
// deterministic CPU(cores)/memory(bytes) matrix over /api/v1/query_range.
type MetricsFixture interface {
    Deploy(ctx context.Context) (serviceURL string, err error) // ClusterIP
    SetSeries(samples MatrixSamples) error // present/partial/missing/invalid/unavailable cases
    QueryTrace() []RecordedQuery            // what the controller actually requested
}

// Typed clients are always built from Cluster.RESTConfig(); the default kubeconfig/context is
// never read or switched.
type Clients interface {
    Ctrl() client.Client          // controller-runtime typed client
    Kube() kubernetes.Interface   // client-go
}

// Diagnostics captures a redacted bundle BEFORE teardown on failure.
type Diagnostics interface {
    Collect(ctx context.Context, scope Scope) (bundleDir string, err error) // logs, events,
                                 // object YAML/status, pod/deploy state, webhook config/CA meta
    JUnit(path string) error
    Timings(path string) error    // per-phase and per-spec; skipped phases marked
}
```

### Per-spec isolation helpers

Each spec allocates a **unique namespace and resource names**, builds typed
Deployment/StatefulSet/DaemonSet and policy fixtures, waits on conditions/watches with
deadlines and observed `resourceVersion`/`generation`, and cleans up idempotently. Eligible
webhook pods carry the production object-selector label **`optipod.io/webhook-enabled=true`**
(verified in `config/webhook/webhook-config.yaml`) set on the workload **pod template before
creation**, so naturally generated pods are admitted. No shared fixed resource names, no
unexplained fixed sleeps, no silent functional retries; any expected API denial/invalid
response is asserted.

## 3. Suite lifecycle and ownership

| Phase | Action | Safety invariant |
| --- | --- | --- |
| Preflight | Verify pins + host capability; fail with remedy, create nothing | No cluster on failure (VAL-HARNESS-001) |
| Own cluster | Create one `optipod-e2e-<rand>` cluster + private kubeconfig | Default context/kubeconfig never read or switched (VAL-HARNESS-002) |
| Build/load/install once | Build uniquely tagged image, load once, render temp manifests, install CRDs/RBAC/controller/webhook once | No `kustomize edit`/`controller-gen` on tracked `config/manager`; deployed image ID + SA verified (VAL-HARNESS-003) |
| Metrics fixture | Deploy pinned Prometheus-compatible ClusterIP; point the **deployed** controller at it via supported temp config | Fixture is an input, not proof the controller consumed it (VAL-SMOKE-002) |
| Per-spec | Unique ns/names, typed clients, bounded waits, idempotent cleanup | Specs do not affect each other (VAL-HARNESS-004) |
| Diagnostics + cleanup | On success/failure/timeout/signal: collect redacted bundle (failure only) then ownership-aware teardown with an independent deadline | Only owned cluster deleted; cleanup failures stay visible; `KEEP_CLUSTER` opt-in retention (VAL-HARNESS-005/006) |

**Ownership rule.** New ordinary runs own and remove their cluster. Focused reuse may attach
only to a retained, harness-created cluster; a user cluster (e.g. `kind`) is never treated as
disposable. If ownership is uncertain, the harness stops. Reuse is explicit only: it happens
solely when an operator sets `OPTIPOD_E2E_REUSE_CLUSTER`/`OPTIPOD_E2E_REUSE_KUBECONFIG` (the
values a `KEEP_CLUSTER=1` run prints); the harness verifies the named cluster carries the
harness prefix, is actually present and has a readable private kubeconfig, and never
rebuilds, reinstalls or deletes it — a set-but-unusable request fails the run rather than
falling back to anything implicit (`make test-e2e-reuse`).

**Concurrency.** One Kind suite at a time (Docker ~8 GB on the reference host); kube-apiserver
and port-forward ports are dynamically allocated localhost ports, never fixed host ports
(3000/5000/7000 are off-limits).

## 4. Deterministic metrics fixture path

The default manager config selects the Prometheus provider. The fixture serves reproducible
samples through the supported `/api/v1/query_range` protocol with per-workload/container
CPU (cores) and memory (bytes) matrices and these cases: **present, partial, missing, invalid,
unavailable**. Tests point the deployed binary at the fixture `Service` with temporary
configuration and then assert the controller-consumed recommendation/status and downstream
workload effects (not the fixture's own state). This fixture does not substitute for real
admission or API-server semantics. The real metrics-server sampling path has different knobs;
the two are never conflated.

## 5. Focus labels

Labels select subsets over the **same** framework/installed-controller path (not a second
harness):

| Label | Scope | Tiers |
| --- | --- | --- |
| `smoke` | Critical installed-controller behaviour | smoke |
| `conformance` | Full installed Kubernetes-dependent coverage | conformance |
| `webhook` | Live admission, TLS, failure policy | smoke + conformance |
| `failure-recovery` | Controller restart / recovery | smoke + conformance |
| `rbac` | Shipped service-account authorization | conformance |
| `observability` | Supported metrics scrape + structured logs | conformance |
| `slow` | Long disruptive combinations | conformance |

Mode/selector/safety/rollout/strategy/concurrency/compatibility/metrics-fault focus labels
select the corresponding conformance areas. The documented harness-specific label invocation
selects `smoke`, `webhook` or `failure-recovery` **without** invoking either legacy suite
(VAL-HARNESS-007).

## 6. Make targets

| Target | Milestone | Behaviour |
| --- | --- | --- |
| `make test-e2e-smoke` | M2 | Runs the new framework smoke suite (label `smoke`) end to end: preflight → own cluster → build/load/install once → specs → diagnostics → teardown. Exposed in `make help`. |
| `make test-e2e-labels E2E_LABELS="..."` | M2 | Same suite/installed-controller path with a Ginkgo label filter (`failure-recovery`, `webhook`, ...), never a legacy suite. |
| `make test-e2e-reuse` | M2 | Explicit focused rerun against a retained harness cluster (`OPTIPOD_E2E_REUSE_CLUSTER`/`OPTIPOD_E2E_REUSE_KUBECONFIG` from a `KEEP_CLUSTER=1` run): verifies the retained install is ready, keeps it unbuilt/unreinstalled and never deletes it. |
| `make test-e2e-conformance` | M3 | Runs smoke **plus** the full conformance labels over the same owned installed cluster. |

Both targets launch Go/Ginkgo with label filters and **must not** delegate to the legacy
fixed-name, context-mutating or tracked-file-generating setup (legacy `test-e2e`, `e2e-cluster`,
`test-e2e-contract`, `make deploy`). The standing fast gate stays non-mutating:

```
KUBEBUILDER_ASSETS="$(setup-envtest use 1.37.0 -p path)" \
  go test -short -count=1 ./api/... ./internal/... ./cmd/... ./test/clusterless/workflows/...
go vet ./api/... ./internal/... ./cmd/... ./test/...
golangci-lint run --timeout=4m
```

`make test` is **not** used as a gate while it regenerates manifests/CRDs and formats tracked
files (see `baseline.md`); the non-mutating gate is `make test-fast`. Until M4 retired the
legacy suites, `go test ./test/e2e/...` was never used as a focused harness command because it
included them; after the retirement the glob selects only the hermetic `suite`, `framework` and
`metricsfixture` packages, but the label-filtered Make targets remain the documented entry
points.

## 7. CI topology

Replaces the current CI (see `legacy-hazards.md` §4): `e2e.yml` runs only nightly/dispatch and
never gates code; `test.yml` runs the mutating `make test`; `ci.yml` builds without tests.

| Trigger | Job | Tier |
| --- | --- | --- |
| PR + push touching controller/API/chart/manifests/test/Make/workflow inputs | smoke job | `make test-e2e-smoke` on a fresh hosted runner; **propagates test failure** |
| `schedule` (nightly) | conformance job | `make test-e2e-conformance` over the same harness |
| `workflow_dispatch` | conformance job | manual full conformance |

CI caches only safe Go dependency/build inputs, and **unconditionally** uploads JUnit, phase/
spec timing and redacted failure diagnostics while preserving the test's failing exit. Path
filters must not miss controller, API, chart, manifest or test changes. Pinned actionlint
validates every modified workflow. The empty root `env:` syntax fault that caused the zero-job
run `36252723861` (reproduced locally, not quoted from GitHub) is fixed in M4. No protected
branch push, tag, release, merge or deployment is in scope; only one authorized feature-branch
PR verifies hosted smoke (M4, VAL-CI-003).

## 8. Diagnostics and redaction

Before cleanup on failure the harness collects: redacted controller logs, scoped namespace
events, relevant object YAML/status, pod/deployment conditions, webhook service/config/CA
**metadata** (never the private key), per-phase and per-spec timings and JUnit. It **never**
archives kubeconfig contents, Secrets, bearer tokens, credentials or private keys. The original
failure stays identifiable and the run exits nonzero. A test-only canary-data redaction check
guards the scrubber (VAL-HARNESS-006).

## 9. Executable acceptance checks (gates per milestone)

These are the concrete, runnable checks each milestone must pass. **At M1 none are executable
yet** — they define "done" for later milestones.

**M1 (now — documentation only):**

```
rg -c '^\| S[0-9]{3} \|' docs/testing/inventory.md     # == 155
grep -rn "It(" test/e2e --include=*.go | grep -v /contract/ | wc -l   # == 86
grep -rn "It(" test/e2e/contract --include=*.go | wc -l               # == 31
grep -rn "It(" test/clusterless/workflows --include=*.go | wc -l      # == 33
# fast gate / vet / lint reproduce baseline.md exits (0/0/0)
```
Acceptance: inventory is 155 unique rows with one disposition + new location each; every
approved behaviour group in `coverage-matrix.md` names a planned test, observable, milestone and
legacy cross-reference; the fast gate, vet and lint reproduce `baseline.md`.

**M2 first smoke acceptance (`make test-e2e-smoke`):**
1. From a clean checkout with declared prerequisites but **no manual Kind setup**, the target
   completes: preflight → owned cluster → one build/load/install → specs → diagnostics →
   teardown.
2. `kind get clusters`, `kubectl config current-context`, the default-kubeconfig checksum and
   `git status --short` are **identical before and after** (only the owned `optipod-e2e-<rand>`
   cluster appears during the run and is gone after).
3. Deployed pod uses the built image + shipped service account; CRD `Established`, Deployment
   `Available`, webhook endpoints/certificate ready **before** admission checks.
4. A minimal valid policy is created/read via the private API and gets a reconcile status/Ready;
   malformed schema is rejected with a cause; a CRD-admitted-but-runtime-invalid policy yields a
   clear invalid status without mutating a workload.
5. Recommend writes exact per-container recommendation annotations with unchanged requests/limits
   and no pod restart; explicit Auto applies the distinct values.
6. Live webhook admission mutates an eligible (`optipod.io/webhook-enabled=true`) pod; an
   ineligible pod is unchanged.
7. Controller restart recovers processing (new ready pod on same image/SA; changed metric input
   → changed recommendation/applied value).
8. A deliberately failing spec exits nonzero, produces a **redacted** diagnostic bundle + JUnit
   **before** teardown, and a sensitive-data scan finds nothing; `KEEP_CLUSTER=1` prints the
   owned cluster name, private kubeconfig path and inspect/dispose commands.
9. `make help` lists `test-e2e-smoke`; the documented label command selects `smoke`/`webhook`/
   `failure-recovery` without invoking a legacy suite; JUnit result/failure counts match.

**M3 conformance acceptance (`make test-e2e-conformance`):** includes all smoke coverage plus
every conformance label group in `coverage-matrix.md` (modes/selectors/weights/bounds/memory/
multi-container/metrics-faults/concurrency/recovery/RBAC/webhook TLS & failure policy/
observability/legacy optional fields), each proving its claimed outcome via before/after API
observation after a completed relevant reconcile/admission.

**M4 CI acceptance:** pinned actionlint accepts the modified workflows; an authorized
feature-branch PR schedules and completes a real hosted smoke job whose result/duration/SHA
match; timing and repeated-run reliability evidence is reported honestly (goals, not deletions
of coverage).

## 10. Minimal production-code policy

Only minimal testability seams are allowed, each with a direct test and written justification;
CRD/API compatibility, public configuration and safety semantics are preserved. Production
behaviour is never altered to paper over a failing test — a real defect is escalated, not
hidden.
