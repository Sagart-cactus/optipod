# OptiPod test-architecture documentation

Versioned documentation for the behaviour-preserving test refactor. The milestone-1 (M1)
baseline, inventory and design are authored before any test implementation.

| Document | Fulfils | Purpose |
| --- | --- | --- |
| [`baseline.md`](./baseline.md) | VAL-DESIGN-001 | Measured, reproducible baseline: commands, exits, timings, machine/cache state, counts and deliberately unexecuted paths. |
| [`inventory.md`](./inventory.md) | VAL-DESIGN-002 | One disposition row for each legacy scenario S001–S155 (old `path:line`/title, new layer/location, retained/replaced/removed, reason). |
| [`coverage-matrix.md`](./coverage-matrix.md) | VAL-DESIGN-003 | Behaviour-group → named (planned) test → observable → milestone → legacy cross-reference. |
| [`design.md`](./design.md) | VAL-DESIGN-003 | Framework interfaces, suite lifecycle/ownership, metrics fixture, pins, labels, Make targets, CI topology and executable acceptance checks. |
| [`legacy-hazards.md`](./legacy-hazards.md) | VAL-DESIGN-004 | Legacy `BeforeSuite` hazards, unpinned installers, tracked-file edits, context mutation, and the reproduced zero-job workflow syntax fault. |

**Reading order for a new worker:** `design.md` for the target architecture, `inventory.md`
and `coverage-matrix.md` for what each future test must prove and which legacy scenario it
supersedes, then `baseline.md` and `legacy-hazards.md` for measured context and the hazards
being removed.

## New hermetic Kind suite (M2, now executable)

The new installed-controller suite lives in `test/e2e/suite` and is the only package the
targets below run — the legacy suites (`test/e2e` root package, `test/e2e/contract`) are
never invoked by them, and `go test ./test/e2e/...` is never used. Each invocation owns one
uniquely named Kind cluster with a private kubeconfig, builds/loads/installs the checkout
controller once, and removes only its own cluster. `KEEP_CLUSTER=1` retains the owned
cluster and prints private-kubeconfig inspection, explicit focused-reuse and disposal
commands.

| Command | Purpose |
| --- | --- |
| `make test-e2e-smoke` | The one-command smoke entry point (M2): runs the smoke-labelled specs of the new suite end to end from a clean checkout — preflight → owned cluster → one build/load/install → specs → diagnostics on failure → teardown. |
| `make test-e2e-labels E2E_LABELS="diagnostics"` | Run the new suite filtered by Ginkgo label (also `E2E_LABELS="smoke"`, `"smoke && webhook"`, `"webhook"`, `"failure-recovery"`, ...). `E2E_SUITE_TIMEOUT` (default `45m`) bounds the `go test` timeout. |
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
a ready pod or a stale annotation alone fails the spec. `conformance`, `rbac`, `observability` and friends arrive
with M3.

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
as of M2 while `make test-e2e-conformance` first exists in M3, and every coverage-matrix row
claims only what its named, executing test proves.
