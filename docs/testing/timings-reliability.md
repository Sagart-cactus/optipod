# Measured timings and smoke reliability (M4)

This is the versioned timing and repeat-run evidence for the refactored test
architecture. It fulfils validation assertion VAL-CI-005: per-phase measured times that
distinguish machine, cache and hosted contexts; a repeated-smoke ledger with its actual
denominator; and the measured position against the mission's timing goals.

> **Honesty boundary.** Every number below is a measurement on the machine described under
> "Environment and cache state", taken at commit `192c70b` on branch
> `feature/m1-baseline-hazard-audit` (2026-10-12) unless another provenance is stated.
> "Warm" means the host's Go build cache, Docker layer cache and the pinned Kind node image
> were already populated; "cold" numbers say exactly which cache was emptied. **Hosted
> smoke has since been measured**: PR #73 ran the real GitHub-hosted smoke job at final
> code head `f5ec14c` — see "Hosted (GitHub Actions) timings" below; hosted conformance
> remains *unmeasured* (nightly/manual job configured, never executed). Ten
> consecutive green smoke runs are evidence of stability on this host, not proof of zero
> flakiness — the one timing-sensitive flake observed during this mission (M3 validation,
> fixed in `1395da9`) is recorded below.

## Environment and cache state

| Attribute | Value |
| --- | --- |
| Date measured | 2026-10-12 |
| Commit | `192c70b` (`test(e2e): mark skipped install phases in focused-reuse timings`) |
| OS / arch | macOS (darwin), x86_64 |
| Go | 1.26.1 |
| Logical CPUs / RAM | 16 / 32 GiB |
| Docker | Engine 29.0.1, ~8 GB allocated |
| Kind CLI / node image | v0.31.0 / `kindest/node:v1.35.0@sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661` (pre-pulled) |
| Go build cache | warm (except the explicitly cold runs below, which used a fresh isolated `GOCACHE`) |
| Docker layer cache | warm (except the explicitly `--no-cache` build below) |
| User state | unchanged across every run: `kind get clusters` = `[kind]`, context `kind-kind`, kubeconfig sha256 `defdc8a0…fbfd`, clean `git status --short` |

## Before/after summary (M1 baseline vs. M4 final)

"Before" numbers are from the M1 versioned baseline ([`baseline.md`](./baseline.md),
2026-10-04, same host, warm caches) and planning-era cold references. "After" numbers are
this document's M4 measurements. The fast gate grew between M1 and M4 because M3 added the
production-path unit/envtest suites (VAL-FAST-001..009) to the same packages.

| Command | Before (M1) | After (M4) |
| --- | --- | --- |
| Fast gate, warm (`make test-fast` / equivalent `go test -short`) | 16.00s / 19.45s (two runs) | **24.21s** (exit 0, 12 packages ok) |
| Fast gate, cold | not measured in M1 | **90.49s** (exit 0; fresh isolated `GOCACHE`, warm module/envtest caches) |
| `go vet` (warm) | 1.03s | **1.22s** |
| Pinned `golangci-lint run --timeout=4m` | 2.74s warm; ~105s cold (planning) | **8.08s** warm, 0 issues |
| Controller image build | 272.7s fully cold incl. base-image pulls (planning); 3.19s warm | **80.12s** cold (`docker build --no-cache`, base images pre-pulled); **4.3s** median warm inside the suite (BuildKit cache) |
| Suite compile (`go test -c ./test/e2e/suite/`) | n/a (suite did not exist) | **62.12s** cold (fresh isolated `GOCACHE`); **12.38s** warm |
| Legacy contract E2E (historical hosted) | ~4–4.5 min, installation + CR acceptance only | replaced by the smoke suite below (~5 min warm, 9 specs incl. admission/SSA/restart/multi-container) |

## Kind smoke phase timings (local, warm)

Ten consecutive serial `make test-e2e-smoke` runs (see the ledger below). Every run
produced the identical 11-phase schema
(`preflight, cluster-create, clients, image-build, image-load, render, install-apply,
await-ready, specs, diagnostics, teardown`), with `diagnostics` explicitly
`"skipped": true` on green runs. Phase seconds from each run's `timings.json`:

| Phase | n | min | median | max |
| --- | --- | --- | --- | --- |
| preflight | 10 | 0.2 | 0.3 | 1.0 |
| cluster-create | 10 | 31.5 | 31.9 | 33.5 |
| clients | 10 | 0.0 | 0.0 | 0.0 |
| image-build | 10 | 4.1 | 4.3 | 5.2 |
| image-load | 10 | 6.5 | 7.5 | 7.7 |
| render | 10 | 0.1 | 0.1 | 0.2 |
| install-apply | 10 | 0.5 | 0.6 | 0.8 |
| await-ready | 10 | 12.0 | 14.0 | 14.0 |
| specs (9 smoke specs) | 10 | 234.9 | 239.1 | 240.8 |
| diagnostics | 0 | — | skipped in all 10 green runs | — |
| teardown (cleanup) | 10 | 0.8 | 0.9 | 1.0 |

Suite `runTimeSeconds` 295.5–302.5; end-to-end `make` wall 300–311s per run. Compilation
is not a per-run phase (Go build cache warm after the first run); it is measured separately
above. The diagnostics phase is exercised on failing runs instead — see the
deliberate-failure evidence from `ci-artifact-and-redaction-verification` (2.8s
diagnostics, 1.0s teardown, run `run-1791749487-d436da96` at `7a738db`).

## Focused reuse (`KEEP_CLUSTER=1` + `make test-e2e-reuse`)

A `KEEP_CLUSTER=1` smoke run (exit 0, 305.02s wall) retained its owned cluster
`optipod-e2e-1791754523-464eda8dbcff` and printed the private-kubeconfig reuse and disposal
commands. The focused rerun reused that exact installation:

```
OPTIPOD_E2E_REUSE_CLUSTER=optipod-e2e-1791754523-464eda8dbcff \
OPTIPOD_E2E_REUSE_KUBECONFIG=<printed private path> \
make test-e2e-reuse E2E_LABELS="failure-recovery"
```

Exit 0, **89.71s wall** (suite `runTimeSeconds` 85.1; the single `failure-recovery` spec
passed in 84.1s). The retained cluster was then disposed with the printed
`kind delete cluster --name … --kubeconfig …` command and `kind get clusters` returned to
`[kind]`.

The reuse run's `timings.json` carries the same 11-phase schema as a full run, with the
phases a reuse deliberately does not perform explicitly marked rather than omitted
(uniform machine-readable evidence across smoke, failure and reuse runs):

```
preflight 0.5s | cluster-reuse 0.0s | clients 0.0s | image-build SKIPPED |
image-load SKIPPED | render SKIPPED | install-apply SKIPPED | await-ready 0.0s |
specs 84.1s | diagnostics SKIPPED | teardown 0.0s (retained cluster kept)
```

This explicit-skipped behaviour was added in `192c70b` (M2 user-testing finding: reuse
runs previously omitted `image-build`/`image-load`/`render`/`install-apply` from
`timings.json`).

## Conformance phase timings (local, warm)

One `make test-e2e-conformance` run at `192c70b` (label filter `smoke || conformance`):
**exit 0, 37 passed / 0 failed / 2 skipped (the two opt-in diagnostics specs) in 1140.6s
suite time (1144.8s wall)**; owned cluster removed afterwards. Phases: preflight 1.2s,
cluster-create 32.7s, clients 0.0s, image-build 4.6s, image-load 7.6s, render 0.2s,
install-apply 0.7s, await-ready 12.0s, specs 1080.7s, diagnostics SKIPPED, teardown 0.9s.
This is consistent with the M3 validation measurements (1203s at `1395da9`, 1169s at
`e519293`).

## Hosted (GitHub Actions) timings

Measured on the authorized PR (#73) by the `authorized-pr-smoke-hosted-verification`
feature, from `gh run view` evidence and the uploaded artifact `timings.json`; runner:
GitHub-hosted `ubuntu-latest`, cold caches (no Go build cache reuse beyond
`actions/setup-go` module cache, no Docker layer cache, node image pulled during cluster
creation).

**Hosted smoke, final code head `f5ec14c`** (run
[38183277022](https://github.com/Sagart-cactus/optipod/actions/runs/38183277022),
pull_request, job "Kind smoke suite" id 114600168825, conclusion `success`):

- Job wall 8m49s (2026-10-11 23:10:30Z → 23:19:19Z); `make test-e2e-smoke` step 8m28s;
  suite-internal 418.6s (`run-1791760336-03581a23`, `suiteSucceeded: true`).
- Phases (artifact `timings.json`): preflight 0.1s | cluster-create 37.4s | clients 0.0s |
  image-build 127.9s | image-load 2.9s | render 0.1s | install-apply 0.4s |
  await-ready 12.0s | specs 236.5s | diagnostics SKIPPED (green run) | teardown 1.2s.
- JUnit artifact: 41 tests, 0 failures, 30 skipped; artifact `kind-smoke-artifacts`
  (id 11719045840) uploaded by the `if: always()` step with the failing-exit-preserving
  wiring verified locally.
- The ≥ 10 GiB temp-dir preflight passed on the runner; the first hosted run
  (38181405028 at `58e3783`) instead failed earlier, in the kustomize install step (asset
  URL 404, fixed in `3ecaa11`, SHA256 re-verified). Both failures are recorded in
  `final-report.md` "Hosted verification status".

**Hosted conformance:** *unmeasured* — the nightly/manual job is configured in
`.github/workflows/e2e.yml` but has never executed; no hosted conformance number is
claimed.

## Timing goals vs. measured reality

| Goal (target, not a gate) | Measured | Verdict |
| --- | --- | --- |
| Warm fast layer < 2 min | 24.21s warm local (`make test-fast`) | met locally |
| Hosted smoke < 6 min | hosted: **8m49s job wall / 418.6s (6m58.6s) suite-internal** (run 38183277022, head `f5ec14c`); local warm smoke 300–311s wall | **missed hosted** (cold image build 127.9s + specs 236.5s dominate); local inside the bound |
| Hosted conformance < 12 min | hosted: **unmeasured** (nightly/manual job configured, never executed); local warm conformance 1144.8s wall (~19.1 min) | unmeasured hosted; local exceeds the hosted bound (macOS Docker Desktop VM vs. Linux runner — the hosted number is unknown until a scheduled/dispatched run, no assertion weakened) |
| Focused reuse < 90s | 89.71s wall / 85.1s suite | met, narrowly |

## Repeat-smoke ledger (VAL-CI-005)

Ten independent serial `make test-e2e-smoke` invocations — the full intended denominator;
no environment or budget constraint required fewer. Each invocation created and removed
its own `optipod-e2e-*` cluster; `kind get clusters` returned to exactly `[kind]` after
every run. JUnit counts are per-run `junit.xml` aggregates (41 testcases = 39 specs +
`[BeforeSuite]`/`[AfterSuite]`; 30 skipped = 28 conformance + 2 opt-in diagnostics specs
outside the `smoke` filter).

| Run | Wall (s) | Exit | Tests/Fail/Skip | Artifact dir | Clusters after |
| --- | --- | --- | --- | --- | --- |
| 1 | 311 | 0 | 41/0/30 | `run-1791751190-c84b3332` | `[kind]` |
| 2 | 304 | 0 | 41/0/30 | `run-1791751498-11fd7f00` | `[kind]` |
| 3 | 303 | 0 | 41/0/30 | `run-1791751801-1ee5db8b` | `[kind]` |
| 4 | 302 | 0 | 41/0/30 | `run-1791752105-029ef297` | `[kind]` |
| 5 | 300 | 0 | 41/0/30 | `run-1791752407-47d20b44` | `[kind]` |
| 6 | 305 | 0 | 41/0/30 | `run-1791752708-65c5826d` | `[kind]` |
| 7 | 301 | 0 | 41/0/30 | `run-1791753014-70588e77` | `[kind]` |
| 8 | 303 | 0 | 41/0/30 | `run-1791753314-86b62bfa` | `[kind]` |
| 9 | 305 | 0 | 41/0/30 | `run-1791753618-7ec9e4ab` | `[kind]` |
| 10 | 303 | 0 | 41/0/30 | `run-1791753922-e0036004` | `[kind]` |

Outcome: **10/10 passed, 0 flakes in this campaign.** That is a stability signal for this
host and cache state only. The mission did observe a genuine flake earlier: during M3
validation a `conformance && multi-container` spec intermittently timed out reading the
metrics-fixture trace ("client rate limiter Wait returned an error", 1 failure across the
round-1 full gate and a focused rerun) before the harness fix in `1395da9`; it did not
recur in these runs. Per-run `timings.json`/`junit.xml` and console logs are preserved
under `test/e2e/artifacts/` (git-ignored) with copies in the mission evidence store.

## Reproduce

```bash
make test-fast                                  # fast gate
make test-e2e-smoke                             # one owned-cluster smoke run
KEEP_CLUSTER=1 make test-e2e-smoke              # retain cluster, print reuse/dispose commands
OPTIPOD_E2E_REUSE_CLUSTER=<name> OPTIPOD_E2E_REUSE_KUBECONFIG=<path> \
  make test-e2e-reuse E2E_LABELS="failure-recovery"
kind delete cluster --name <name> --kubeconfig <path>   # printed disposal
make test-e2e-conformance                       # smoke + all conformance labels, one cluster
```

Cold-cache variants use an isolated cache so the developer's caches are never cleared:
`GOCACHE="$(mktemp -d)" make test-fast` and `docker build --no-cache .`.
