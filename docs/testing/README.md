# OptiPod test-architecture documentation

Versioned documentation for the behaviour-preserving test refactor. The milestone-1 (M1)
baseline, inventory and design were authored before any test implementation; the M4 timings
ledger and final report close the mission with measured evidence.

| Document | Fulfils | Purpose |
| --- | --- | --- |
| [`baseline.md`](./baseline.md) | VAL-DESIGN-001 | Measured, reproducible baseline: commands, exits, timings, machine/cache state, counts and deliberately unexecuted paths. |
| [`inventory.md`](./inventory.md) | VAL-DESIGN-002 | One disposition row for each legacy scenario S001–S155 (old `path:line`/title, new layer/location, retained/replaced/removed, reason). |
| [`coverage-matrix.md`](./coverage-matrix.md) | VAL-DESIGN-003 | Behaviour-group → named test → observable → milestone → legacy cross-reference; every row B01–B20 is `Landed` against executing proof. |
| [`design.md`](./design.md) | VAL-DESIGN-003 | Framework interfaces, suite lifecycle/ownership, metrics fixture, pins, labels, Make targets, CI topology and executable acceptance checks. |
| [`legacy-hazards.md`](./legacy-hazards.md) | VAL-DESIGN-004 | Legacy `BeforeSuite` hazards, unpinned installers, tracked-file edits, context mutation, and the reproduced zero-job workflow syntax fault. |
| [`timings-reliability.md`](./timings-reliability.md) | VAL-CI-005 | Measured M4 phase timings (local cold/warm, hosted explicitly unmeasured), the ten-run smoke ledger, focused-reuse timings with uniform skipped-phase markers, and goal-vs-measured verdicts. |
| [`final-report.md`](./final-report.md) | VAL-CI-006 | Concise final mission report: old→new paths, changed/removed files, exact verified commands and results, before/after timings, the 10/10 reliability denominator, hosted-verification status (pending the authorized PR feature) and the unresolved limits/follow-ups. |

**Reading order for a new worker:** `design.md` for the target architecture, `inventory.md`
and `coverage-matrix.md` for what each future test must prove and which legacy scenario it
supersedes, then `baseline.md` and `legacy-hazards.md` for measured context and the hazards
being removed.

## Hermetic Kind suite (M2/M3, executable; legacy suites retired in M4)

The installed-controller suite lives in `test/e2e/suite` and is the only package the
targets below run. The legacy suites (`test/e2e` root package, `test/e2e/contract`,
`test/e2e/helpers`) and their Make targets were retired in M4 (see `inventory.md`,
retirement-execution notes), so `test/e2e` now contains only this suite, the `framework`
and `metricsfixture` packages and the git-ignored `artifacts/` output tree. Each invocation owns one
uniquely named Kind cluster with a private kubeconfig, builds/loads/installs the checkout
controller once, and removes only its own cluster. `KEEP_CLUSTER=1` retains the owned
cluster and prints private-kubeconfig inspection, explicit focused-reuse and disposal
commands.

| Command | Purpose |
| --- | --- |
| `make test-e2e-smoke` | The one-command smoke entry point (M2): runs the smoke-labelled specs of the new suite end to end from a clean checkout — preflight → owned cluster → one build/load/install → specs → diagnostics on failure → teardown. |
| `make test-e2e-labels E2E_LABELS="diagnostics"` | Run the new suite filtered by Ginkgo label (also `E2E_LABELS="smoke"`, `"smoke && webhook"`, `"conformance && observability"`, `"conformance && compatibility"`, ...). `E2E_SUITE_TIMEOUT` (default `45m`) bounds the `go test` timeout. |
| `make test-e2e-conformance` | The one-command full-conformance entry point (M3): label filter `smoke \|\| conformance`, so the smoke coverage and every conformance label (`modes`, `selectors`, `policy`, `safety`, `multi-container`, `metrics-fault`, `webhook`, `rollout`, `strategy`, `concurrency`, `rbac`, `observability`, `compatibility`) run over one owned installed cluster in a single invocation — conformance cannot pass by bypassing smoke (VAL-CROSS-003). `E2E_CONFORMANCE_TIMEOUT` (default `60m`) bounds the `go test` timeout. |
| `make test-e2e-reuse` | Explicit focused rerun against a retained harness cluster: set `OPTIPOD_E2E_REUSE_CLUSTER` and `OPTIPOD_E2E_REUSE_KUBECONFIG` (printed by a `KEEP_CLUSTER=1` run) plus `E2E_LABELS`. The retained install is verified ready and kept — not rebuilt, reinstalled or deleted; only the printed disposal command removes the cluster. |
| `make test-e2e-deliberate-failure` | Opt-in spec that fails one deliberate assertion to exercise pre-teardown redacted diagnostics; a nonzero exit and a redacted bundle are the expected outcome, not a product defect. |

Labels currently in the suite: `smoke` (critical installed-controller behaviour; the smoke specs also carry
`install`/`policy` for API admission and readiness, and `modes` with `recommend` for the Recommend scenario and
`auto` with `ssa` or `webhook` for the two Auto strategies). The Auto SSA scenario exercises the restored
multi-container coverage on a two-container workload under the explicit `strategy: ssa`: one workload-wide apply
lands both containers' exact values, the workload rolls once, and a steady state — unchanged revision, ReplicaSet
set, pod identities and template — holds across a further completed reconcile with no rollout churn
(VAL-SMOKE-002, VAL-SMOKE-005). The default-webhook half of VAL-SMOKE-005 is now in the suite too: an Auto policy
with the update strategy left unset stores every container's recommendations, and the installed mutating webhook
mutates **every** eligible container on each pod the Deployment controller admits naturally while the pre-existing
pod stays untouched under the default `onNextRestart` rollout with no ReplicaSet/revision churn (VAL-SMOKE-005).
That scenario also proves the installed controller ClusterRole grants the `replicasets` read the mutator's
pod→ReplicaSet→Deployment parent-annotation lookup needs and that the controller log records no forbidden
ReplicaSets request: `config/rbac/role.yaml` now mirrors the grant the shipped Helm chart already ships
(VAL-FIX-003), so the kustomize install path mutates new Deployment pods instead of silently admitting them
unmutated. The `webhook` label also covers the SSA
managed-field-ownership scenario and the live TLS admission probe that mutates an eligible hand-labelled pod and
admits ineligible pods unchanged — that probe proves admission reachability only, never natural rollout, which is
a conformance-tier concern), `diagnostics` (harness self-check) and `deliberate-failure` (opt-in failure-path
validation). The `failure-recovery` label covers controller restart recovery (VAL-SMOKE-004): after an observed
recommendation the installed controller pod is deleted, and the replacement pod — ready, on the same image and
shipped service account — must consume changed deterministic fixture input through a fresh reconcile and rewrite
the workload's recommendation annotations (with an advanced resourceVersion and a fresh reconciliation status);
a ready pod or a stale annotation alone fails the spec. With M3 complete, the conformance labels are all live:
`modes` (VAL-CONF-001), `selectors` (VAL-CONF-002), `policy` (VAL-CONF-003), `safety` (VAL-CONF-004, including the
amended memory-decrease-applies-in-full behavior), `multi-container` (VAL-CONF-005), `metrics-fault`
(VAL-CONF-006), `webhook` rollout timing (VAL-CONF-007), `rollout` per-kind immediate admission (VAL-CONF-008),
`strategy` SSA↔webhook boundary (VAL-CONF-009), `concurrency` (VAL-CONF-010), `rbac` shipped-roles least privilege
(VAL-CONF-011), `webhook` TLS/failure policy (VAL-CONF-012), `observability` (VAL-CONF-013) and `compatibility`
(VAL-CONF-014).

The M3 observability scenario scrapes the installed controller's metrics in the shipped ServiceMonitor's shape:
an **in-cluster** HTTPS scrape of the shipped metrics Service from the metrics fixture, whose service account the
harness binds to the shipped (deliberately unbound) metrics-reader ClusterRole and whose projected bearer token the
metrics endpoint's own authentication/authorization filter validates. A scrape through the API server's service
proxy cannot present that token — the API server removes the caller's `Authorization` header after authenticating
it — so the filtered endpoint is scraped from inside the cluster, exactly where a real Prometheus agent runs. The
spec proves named processing (policy-labeled series absent before the policy, present after its completed
reconcile, named counter strictly increasing across a further reconcile) and parseable structured log records
tying the policy/workload to the outcome (VAL-CONF-013).

**Retained-cluster inspection, focused reuse and disposal.** A `KEEP_CLUSTER=1` run keeps its owned cluster and
prints the retained cluster's name, its private kubeconfig path and copy-pasteable commands to (1) inspect the
cluster through that private kubeconfig (`kubectl --kubeconfig <path> get pods -A`), (2) explicitly reuse it for
a focused rerun (`OPTIPOD_E2E_REUSE_CLUSTER=<name> OPTIPOD_E2E_REUSE_KUBECONFIG=<path> make test-e2e-reuse
E2E_LABELS="failure-recovery"` — the reuse is accepted only for a harness-prefixed cluster that is actually
present with its readable private kubeconfig; anything else is refused, and the retained install is verified
ready but never rebuilt or deleted by the rerun), and (3) dispose of it
(`kind delete cluster --name <name> --kubeconfig <path>`). Normal runs always create a new owned cluster and
remove it; reuse only ever happens through the explicit variables above.

Every invocation — success or failure — writes into a git-ignored
`test/e2e/artifacts/run-<id>/` directory (override with `OPTIPOD_E2E_ARTIFACTS_DIR`):

- `junit.xml` — parseable per-spec JUnit (`tests`/`failures`/`skipped` attributes; `[It]`
  testcases are the specs; `[BeforeSuite]`/`[AfterSuite]` testcases record setup state).
- `timings.json` — measured setup, spec, diagnostics and teardown phases; phases that did
  not run in that invocation are explicitly marked `"skipped": true`.
- `diagnostics/` — on failure only: the original failure text, controller logs, scoped
  events, policy/workload/pod YAML/status, deployment/pod state, webhook configuration and
  CA **metadata**, metrics-fixture trace, and Secret **metadata only**. Kubeconfig
  contents, Secret values, private keys, bearer tokens and credentials are structurally
  excluded and additionally scrubbed to `[REDACTED]`.

Honesty boundaries that apply across all M1 docs: local timings are warm, single-host
measurements (not universal cold-start numbers); pending/skipped/simulator-only/fixture-only
legacy checks are never credited as deployed-behaviour proof; `make test-e2e-smoke` exists
as of M2 and `make test-e2e-conformance` as of M3, and every coverage-matrix row
claims only what its named, executing test proves.

**M3 legacy retirement (feature `kind-observability-compatibility-and-legacy-retirement`).**
The tracked `config/manager/kustomization.yaml` no longer carries the leftover
`optipod-controller:contract-e2e` image override — it is reset to the scaffold default
`controller:latest`; the harness render retags the image by name on temporary copies either
way, and the render unit test plus a live `make test-e2e-smoke` run prove the reset green
(the tracked `config/webhook` manifests are untouched; their corrections remain a deferred
production follow-up). The legacy memory-safety decrease-guard artifacts are retired because
production deliberately has no such guard (commit `9dfc868`): `hack/test-memory-decrease-safety.sh`,
`test/e2e/memory_safety_integration_test.go`, `test/e2e/run_memory_safety_tests.sh`, the
"Memory Decrease Safety Scenarios" context in `test/e2e/error_handling_test.go` (S043/S044),
the "Memory Safety" context in `test/clusterless/workflows/safety_test.go` (S133–S135) and the
now-unreferenced `hack/test-*memory*`/`test-*safety-factor*` fixtures; the amended
VAL-FAST-005/VAL-CONF-004 behavior is pinned by `internal/application`
`TestApplyMemoryDecreaseCharacterization` and the conformance `safety` spec. The superseded
legacy runner scripts (`hack/test-policy-modes.sh`, `test-resource-bounds-enforcement.sh`,
`test-update-strategies.sh`, `test-error-handling.sh`, `test-dry-run-mode.sh`,
`test-limit-configuration.sh`, `test-rbac-permissions.sh`, `test-workload-types.sh`,
`test-comprehensive-final.sh`, `test/e2e/run_policy_tests.sh`, `run_resource_bounds_tests.sh`,
`simple_policy_test.sh`) are removed; every scenario they drove is `replaced`/`removed` in
`inventory.md`.

**M4 CI-topology retirement (feature `safe-make-and-actions-topology`).** The remaining legacy
root suite (14 `test/e2e/*.go` files), the `test/e2e/contract/` suite and `test/e2e/helpers/`
are deleted with per-row reasons recorded in `inventory.md`; the legacy Make targets
(`setup-e2e`, `test-e2e`, `test-e2e-focus`, `e2e-cluster`, `test-e2e-contract`, `cleanup-e2e`,
`cleanup-contract-e2e`) are removed. A non-mutating fast gate is exposed as `make test-fast`.
`.github/workflows/e2e.yml` is rewritten to the approved topology — a smoke job on PRs/pushes
that touch controller, API, chart, manifest, test, Make or workflow inputs (test failure fails
the job) and a conformance job on the nightly schedule and manual dispatch — with pinned,
checksum-verified Kind/kubectl/kustomize installs, Go build caching and unconditional
JUnit/timing/redacted-diagnostics upload that preserves the test exit code. `test.yml` now runs
`make test-fast` plus `go vet` instead of the mutating `make test`, and `release.yml`'s test
job does the same. Pinned actionlint v1.7.7 accepts every workflow in `.github/workflows/`.
The orphaned tracked `website/src/content/doc/doc/**` tree (never loaded by Astro, which reads
`website/src/content/docs`) is deleted: it documented an obsolete `safetyConfig`/
`preventOOMKills` API with a memory-protection guard that production does not have.
