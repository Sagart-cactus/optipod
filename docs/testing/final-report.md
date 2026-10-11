# Final mission report — hermetic, fast, comprehensive test architecture

This report closes the four-milestone test-architecture refactor (M1 baseline/design →
M2 hermetic Kind smoke → M3 production-path migration and conformance → M4 CI, measured
timings and reliability). It fulfils validation assertion **VAL-CI-006**: changed and
removed files, exact verification commands and results, before/after timings, the
repeated-run denominator, hosted-run provenance, and the unresolved limits and follow-ups.

> **Provenance and honesty boundary.** All local numbers are measurements on one host
> (macOS x86_64, 16 CPUs / 32 GiB, Docker Engine 29.0.1 with ~8 GB, Go 1.26.1, Kind v0.31.0,
> pinned node image `kindest/node:v1.35.0@sha256:4613778f…6961`), taken 2026-10-12 at commit
> `192c70b`/`c77a6d0` on branch `feature/m1-baseline-hazard-audit` — see
> [`timings-reliability.md`](./timings-reliability.md) for the full environment table,
> per-phase min/median/max and the per-run ledger. Hosted evidence now exists: PR #73
> (`feature/m1-baseline-hazard-audit` → `main`) ran the real GitHub-hosted Kind smoke job
> at the final code head `f5ec14c` (run 38183277022, job success); the full record —
> including the two hosted failures found and fixed during PR verification — is in
> [Hosted verification status](#hosted-verification-status). The nightly/manual
> conformance job is *configured* in `.github/workflows/e2e.yml` but has never executed;
> nothing here claims an unobserved hosted conformance or scheduled success.

## What changed: old → new

The full scenario-level mapping (155 inventoried legacy scenarios S001–S155, each with old
`path:line`, new layer/location, retained/replaced/removed status and reason) is the
versioned [`inventory.md`](./inventory.md); the behaviour-group mapping is
[`coverage-matrix.md`](./coverage-matrix.md) (rows B01–B20, all `Landed` against executing
proof). In summary:

| Old (retired) | New (current) |
| --- | --- |
| Legacy main E2E root suite (`test/e2e/*.go`, 14 files) — fixed cluster names, tracked-`config/manager` edits, ambient-context installs | Unit/envtest production-path suites in `api/v1alpha1`, `internal/*`, plus the hermetic Kind suite `test/e2e/suite` on the `test/e2e/framework` harness (owned uniquely named cluster, private kubeconfig) |
| Contract E2E suite (`test/e2e/contract/`, 13 files) — install/CR-acceptance/Ready-only checks, never ran on PRs | `make test-e2e-smoke` (PR/push CI job) and `make test-e2e-conformance` (nightly/manual), same harness, one owned cluster per invocation |
| `test/e2e/helpers/` shared helpers | `test/e2e/framework` (preflight, owned cluster, build/load/install once, typed clients, condition waits, redacted diagnostics, JUnit/timings) |
| `make test` as the default gate (regenerates/formats tracked files) | `make test-fast` — non-mutating fast gate: short unit + envtest (pinned 1.37.0 assets) + clusterless |
| Mutating legacy Make targets (`setup-e2e`, `test-e2e`, `test-e2e-focus`, `e2e-cluster`, `test-e2e-contract`, `cleanup-e2e`, `cleanup-contract-e2e`) | `make test-e2e-smoke` / `test-e2e-conformance` / `test-e2e-labels E2E_LABELS=…` / `test-e2e-reuse` (explicit retained-cluster reuse) / `test-e2e-deliberate-failure`, all discoverable via `make help` |
| Simulator-only clusterless webhook checks | Real production `internal/webhook` mutator unit tests plus live admission specs; `test/clusterless/workflows` kept only where it exercises production entry points |
| Legacy runner scripts (`hack/test-*.sh`, `test/e2e/*.sh`) and memory-guard fixtures | Deleted; their scenarios are `replaced`/`removed` rows in `inventory.md` |
| Broken `e2e.yml` (empty root `env:` — locally reproduced syntax fault; historical run 36252723861 scheduled zero jobs) | Rewritten `e2e.yml` (smoke on PR/push with path filters, conformance on schedule/dispatch, pinned checksum-verified tools, Go caching, unconditional JUnit/timings/redacted-diagnostics upload that preserves the test exit); `test.yml` and `release.yml` run `make test-fast` + `go vet`; all workflows pass pinned actionlint v1.7.7 |

## Changed / removed files (branch vs. merge-base `bee5567`)

214 files changed: **97 added, 72 deleted, 45 modified**.

- **Deleted (72):** the 14-file legacy root suite `test/e2e/*.go`; `test/e2e/contract/` (13
  files); `test/e2e/helpers/test_helpers.go`; legacy runner scripts (`test/e2e/run_*.sh`,
  `test/e2e/simple_policy_test.sh`, `hack/test-*.sh`, `hack/verify-release-separation.sh`)
  and the `hack/test-policy-*.yaml`/`hack/test-workload-*.yaml` memory-guard fixtures; the
  orphaned, never-rendered `website/src/content/doc/doc/**` tree (16 files) that documented
  an obsolete safety API.
- **Added (97):** `test/e2e/framework/` (40), `test/e2e/suite/` (28 smoke/conformance spec
  files), `test/e2e/metricsfixture/` (3, deterministic in-cluster Prometheus-compatible
  fixture); fast-layer production-path suites in `internal/controller` (6),
  `internal/application` (4), `internal/webhook` (2), `api/v1alpha1` (2) and one each in
  `internal/{recommendation,policy,metrics,discovery,cache}`; this `docs/testing/` doc set
  (baseline, inventory, coverage matrix, design, legacy hazards, timings/reliability, this
  report).
- **Modified (45):** the three user-authorized production fixes —
  `internal/application/engine.go` and `internal/webhook/annotations.go` (one workload-wide
  apply/store per reconcile so multi-container workloads converge, VAL-FIX-001/002) and
  `config/rbac/role.yaml` (regenerated with pinned controller-gen v0.22.0 to mirror the
  shipped Helm chart's `replicasets` read grant, VAL-FIX-003) — plus
  `internal/controller/workload_processor.go` (full per-container recommendation set),
  workflow/Make/steering files, and doc-truth corrections across `docs/`, `website/` and the
  generated CRD descriptions (memory decreases are applied in full; no guard exists).

## Verified commands and exact results

| Command | Exit | Result (provenance) |
| --- | --- | --- |
| `make test-fast` (warm) | 0 | 24.21s, 12 packages ok (192c70b) |
| `make test-fast` (cold, isolated `GOCACHE`) | 0 | 90.49s |
| `go vet ./api/... ./internal/... ./cmd/... ./test/...` | 0 | 1.22s |
| pinned `golangci-lint run --timeout=4m` (v2.13.2) | 0 | 8.08s, 0 issues |
| pinned `actionlint .github/workflows/*.yml` (v1.7.7) | 0 | all workflows accepted |
| `make test-e2e-smoke` | 0 | **10/10 consecutive serial runs** at `192c70b`, 300–311s wall each; per-run JUnit 41 tests / 0 failures / 30 skipped; `kind get clusters` back to `[kind]` after every run |
| `make test-e2e-conformance` | 0 | 37 passed / 0 failed / 2 skipped (the two opt-in diagnostics specs), 1140.6s suite / 1144.8s wall (`192c70b`, run `run-1791755065-8c56ed5a`); consistent with M3 validation runs (1203s at `1395da9`, 1169s at `e519293`) |
| `make test-e2e-deliberate-failure` | 2 (expected nonzero) | redacted diagnostics bundle (41 files, 14 redactions), JUnit and timings written, owned cluster removed (run `run-1791749487-d436da96`, `7a738db`); independent scan found no canary token/PEM bodies, kubeconfig contents or credentials in any artifact |
| `KEEP_CLUSTER=1 make test-e2e-smoke` → `make test-e2e-reuse E2E_LABELS="failure-recovery"` | 0 | retained run 305.02s; focused reuse **89.71s wall** (goal < 90s, met narrowly); printed disposal returned `kind get clusters` to `[kind]` |

User state was byte-identical before/after every Kind invocation (user `kind` cluster,
context `kind-kind`, kubeconfig sha256 `defdc8a0…fbfd`, clean `git status --short`).

## Before/after timings and the reliability denominator

Full tables (per-phase min/median/max, environment, cache state) are in
[`timings-reliability.md`](./timings-reliability.md); the baseline column is
[`baseline.md`](./baseline.md) (M1, same host, warm).

| Command | Before (M1) | After (M4) |
| --- | --- | --- |
| Fast gate, warm | 16.00s / 19.45s | 24.21s (grew: M3 added production-path suites to the same packages) |
| Fast gate, cold | not measured | 90.49s (fresh isolated `GOCACHE`) |
| `go vet` | 1.03s | 1.22s |
| Pinned lint | 2.74s warm / ~105s cold | 8.08s warm |
| Controller image build | 272.7s fully cold (incl. base pulls) / 3.19s warm | 80.12s cold `--no-cache` (bases pre-pulled) / ~4.3s warm in-suite |
| Suite compile | n/a (suite did not exist) | 62.12s cold / 12.38s warm |
| Legacy contract E2E (historical hosted) | ~4–4.5 min, installation + CR acceptance + Ready only | replaced by smoke (~5 min warm) covering admission, both strategies, SSA fields, live webhook mutation, restart recovery and multi-container |
| Kind smoke | n/a | 300–311s wall per run; specs phase median 239.1s; cluster-create median 31.9s; teardown ≤ 1.0s |
| Kind conformance | n/a | 1144.8s wall (37/0/2) |
| Focused reuse | n/a | 89.71s wall |

**Reliability denominator: 10/10** consecutive independent serial `make test-e2e-smoke`
runs passed — the full intended denominator, no environment or budget constraint required
fewer. Ten green runs are a stability signal for this host/cache state, not proof of zero
flakiness: the mission did observe one genuine flake (an M3 `conformance && multi-container`
fixture-trace read timeout, "client rate limiter Wait returned an error"), fixed test-side
in `1395da9` (dedicated fixture client + single bounded trace read) and not seen since.

**Timing goals (goals, not gates):** warm fast < 2 min — met locally (24.21s); focused
reuse < 90s — met narrowly (89.71s); hosted smoke < 6 min — **missed** (8m49s job wall,
6m58.6s suite-internal, at PR #73 head `f5ec14c`; the cold controller image build
(127.9s) and the specs phase (236.5s) dominate — see
[Hosted verification status](#hosted-verification-status)); hosted conformance < 12 min —
**unmeasured** (the nightly/manual job is configured but has never executed). Local warm
conformance (~19.1 min) exceeds the 12-minute *hosted* goal; whether the Linux runner
meets it is unknown until a scheduled/dispatched run, and no assertion was weakened to
compensate.

## Hosted verification status

**Executed (VAL-CI-003).** The reviewed feature branch was pushed and
[PR #73](https://github.com/Sagart-cactus/optipod/pull/73)
(`feature/m1-baseline-hazard-audit` → `main`) was opened on 2026-10-12 (UTC 2026-10-11/12).
The hosted story includes two real failures found by the PR and fixed on the branch —
reported here rather than hidden, per the no-silent-retry rule:

1. **Head `58e3783` — smoke failed in tool install.** Run
   [38181405028](https://github.com/Sagart-cactus/optipod/actions/runs/38181405028)
   (pull_request): the "Kind smoke suite" job failed in "Install pinned Kind, kubectl and
   kustomize" with `curl: (22) ... 404`. Root cause: the workflow stripped the `v` prefix
   from the kustomize release asset name (`kustomize_5.8.1_linux_amd64.tar.gz`), but the
   published asset keeps it (`kustomize_v5.8.1_linux_amd64.tar.gz`). Fixed in `3ecaa11`;
   the pinned SHA256 `029a7f0f…a819d` was re-verified against the real asset before the fix.
   The suite never started, so this run says nothing about test behavior.
2. **Head `3ecaa11` — smoke green; Lint and Pre-commit red.** Run
   [38181590019](https://github.com/Sagart-cactus/optipod/actions/runs/38181590019):
   smoke job success (job wall 8m07s). Separately, the Lint workflow (run 38181589953) and
   Pre-commit Checks (run 38181589946) failed on branch-introduced issues invisible to the
   macOS-local gate: a linux-only `unconvert` finding in
   `test/e2e/framework/disk_linux.go` (`Statfs_t.Bavail` is already `uint64` on linux), and
   `detect-secrets` false positives on the deliberate redaction-test canaries
   (`redact_test.go`, `suite/canary_test.go`), the userinfo-regex comment (`redact.go`),
   the webhook cert Secret *name* constant (`pins.go`) and the three SHA256 pin hex strings
   (`e2e.yml`). Fixed in `f5ec14c` (conversion dropped; inline `pragma: allowlist secret`
   markers using the tool's own comment syntax), each verified locally with
   `GOOS=linux GOARCH=amd64 golangci-lint run ./test/...` (0 issues) and
   `detect-secrets` 1.5.0 against the repo baseline over all tracked files (exit 0).
3. **Final code head `f5ec14c` — all PR checks green.**

**Final-head smoke evidence** (run
[38183277022](https://github.com/Sagart-cactus/optipod/actions/runs/38183277022),
event `pull_request`, headSha `f5ec14cd23dde085e982fd5234d36d60e700e5a2`, conclusion
`success`; verified with `gh run view 38183277022 -R Sagart-cactus/optipod --json
event,headSha,jobs,conclusion`):

| Item | Value |
| --- | --- |
| Job | "Kind smoke suite" (id 114600168825), conclusion `success` |
| Job wall time | 8m49s (2026-10-11 23:10:30Z → 23:19:19Z) |
| `make test-e2e-smoke` step | 8m28s (23:10:47Z → 23:19:15Z) |
| Suite-internal time (artifact `timings.json`) | 418.6s (6m58.6s), `suiteSucceeded: true` |
| Result (artifact `junit.xml`) | 41 tests, 0 failures, 0 errors, 30 skipped (conformance/diagnostics labels excluded at smoke tier) |
| Artifact | `kind-smoke-artifacts` (id 11719045840, 9,384 bytes, unexpired; junit.xml + timings.json, no diagnostics dir on a green run) |
| Runner preflight | passed in 0.1s — the ≥ 10 GiB temp-dir disk check did **not** fail on `ubuntu-latest` |

Hosted phase timings from the uploaded `timings.json` (run-1791760336-03581a23):
preflight 0.1s, cluster-create 37.4s (includes the node-image pull), image-build 127.9s
(cold, no cache), image-load 2.9s, render 0.1s, install-apply 0.4s, await-ready 12.0s,
specs 236.5s, diagnostics SKIPPED (green run), teardown 1.2s.

**Goal assessment (goals, not gates):** hosted smoke < 6 min — **missed**: 8m49s job
wall and even the 6m58.6s suite-internal time exceed it; the cold controller image build
(127.9s) plus the specs phase (236.5s) dominate. No coverage was cut and no timeout or
assertion was inflated to disguise this. Hosted conformance < 12 min — **unmeasured**:
the nightly/manual job is configured in `.github/workflows/e2e.yml` but has never
executed, and it is not claimed as an observed run. Also recorded: the first hosted run
failed *before* preflight on the kustomize 404, and the ≥ 10 GiB disk preflight passed
on the real runner — the predicted disk-preflight blocker did not materialize.

The commit that adds this report text touches only `docs/testing/**`, which is outside
the `e2e.yml` path filter, so it deliberately does not schedule a new smoke run;
`f5ec14c` is the final code head and its run above is the hosted smoke evidence for this
PR. All other PR checks (CI, Tests, Lint, Pre-commit Checks) were also green at
`f5ec14c`.

## Unresolved limits and follow-ups (recorded, not fixed)

These are **unverified follow-ups**, not fixes delivered by this mission. Each was
deliberately left out of scope because changing it would alter shipped production behavior
or install paths beyond the three authorized fixes.

1. **Stale `config/webhook/` standalone install path (three quirks).**
   `config/webhook/webhook-config.yaml:14` uses admission path `/mutate-v1-pod` while the
   production handler registers `/mutate` (the Helm chart uses `/mutate`);
   `config/webhook/deployment.yaml` passes `--webhook-mode`/`--webhook-port`/
   `--webhook-cert-dir` flags that `cmd/main.go` does not implement; and its
   `namespaceSelector` uses an inert `key: name` NotIn exclusion instead of
   `kubernetes.io/metadata.name`, so the kube-system/kube-public/kube-node-lease exclusion
   never matches. The harness deliberately renders its own temporary
   `MutatingWebhookConfiguration` (production `/mutate` path, shipped selectors and
   failurePolicy, harness CA) rather than depending on or repairing these manifests.
2. **`updateRequestsOnly` cannot be set to explicit `false` by a typed Go client.** The
   field is a non-pointer `bool` with `omitempty` plus a schema default of `true`
   (`api/v1alpha1/optimizationpolicy_types.go`), so a typed client-side `false` is omitted
   on serialize and the server defaults it back to `true`; only a raw YAML/unstructured
   apply can express explicit `false`. Making it a pointer (like `useServerSideApply`) is a
   CRD-compatible product fix left for a follow-up.
3. **The 30s recommendation-annotation throttle can leave stored recommendations stale.**
   In `internal/controller/workload_processor.go`, when `optipod.io/last-recommendation` is
   younger than 30 seconds the whole annotation write is skipped (reported as success),
   while the apply path still runs — so stored recommendation annotations can lag the
   applied values until a watch event lands outside the window.
4. **Metrics-collection duration series is mislabeled under Prometheus.**
   `internal/controller/workload_processor.go` hardcodes the
   `optipod_metrics_collection_duration_seconds` provider label as `"metrics-server"` even
   when the Prometheus metrics provider is configured. The observability conformance spec
   asserts series presence without constraining the label, so this mislabel is confirmed
   live but unfixed.
5. **`getFirstPodName` samples an arbitrary pod per container.** With two or more replicas,
   the informer-cache list iteration order is unspecified, so the sampled pod is arbitrary
   (the harness now matches fixture traces per (namespace, container) over any registered
   pod). Deterministic sorted sampling would be a product improvement the mission did not
   make.
6. **Pre-existing webhook lifecycle data race.** `go test -race ./internal/webhook` fails in
   `TestProperty_WebhookLifecycleManagement` (races between `(*Server).Start`,
   `internal/webhook/server.go:141`, and `(*LifecycleManager).waitForServerReady`,
   `internal/webhook/lifecycle.go:303`). Reproduced at the pre-mission base in a detached
   worktree, so it predates this mission; the approved race gate
   (`-short -race ./internal/controller ./internal/cache ./internal/application`) is
   unaffected and passes. If a live webhook run ever hangs or flakes in the
   server-readiness path, treat this as a candidate cause.
7. **Harness-hardening deferrals (from M2 user testing).** The unit-tested
   `InstallSignalGuard` is not wired into the live suite entrypoint, so signal-driven
   owned-cluster cleanup relies on Ginkgo's signal handling plus `AfterSuite` (verified live
   via direct SIGTERM: cleanup completed in ~16s with a nonzero exit); and the bounded
   cleanup-timeout path (independent ~2-minute `CleanupContext` deadline) is unit-verified
   but was never live-exercised.
8. **`make test` still mutates tracked files** (regenerates/formats). The non-mutating
   `make test-fast` is the standing gate; correcting `make test` itself was out of scope.

## Where the evidence lives

- Measured timings, repeat ledger, goal verdicts: [`timings-reliability.md`](./timings-reliability.md)
- Behaviour coverage (B01–B20, all Landed with executing entry points): [`coverage-matrix.md`](./coverage-matrix.md)
- Scenario traceability (S001–S155 dispositions): [`inventory.md`](./inventory.md)
- Baseline and retired-hazard detail: [`baseline.md`](./baseline.md), [`legacy-hazards.md`](./legacy-hazards.md)
- Architecture and acceptance checks: [`design.md`](./design.md)
- User-facing usage: root `README.md` "Testing" section, `.github/workflows/README.md`
