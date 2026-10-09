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
cluster and prints private-kubeconfig inspection/disposal commands.

| Command | Purpose |
| --- | --- |
| `make test-e2e-smoke` | The one-command smoke entry point (M2): runs the smoke-labelled specs of the new suite end to end from a clean checkout — preflight → owned cluster → one build/load/install → specs → diagnostics on failure → teardown. |
| `make test-e2e-labels E2E_LABELS="diagnostics"` | Run the new suite filtered by Ginkgo label (also `E2E_LABELS="smoke"`, `"smoke && webhook"`, `"webhook"`, `"failure-recovery"`, ...). `E2E_SUITE_TIMEOUT` (default `45m`) bounds the `go test` timeout. |
| `make test-e2e-deliberate-failure` | Opt-in spec that fails one deliberate assertion to exercise pre-teardown redacted diagnostics; a nonzero exit and a redacted bundle are the expected outcome, not a product defect. |

Labels currently in the suite: `smoke` (critical installed-controller behaviour; the smoke specs also carry
`install`/`policy` for API admission and readiness, and `modes` with `recommend` for the Recommend scenario and
`auto` with `ssa` for the Auto scenario). The Auto scenario exercises the restored multi-container coverage on a
two-container workload under the explicit `strategy: ssa`: one workload-wide apply lands both containers' exact
values, the workload rolls once, and a steady state — unchanged revision, ReplicaSet set, pod identities and
template — holds across a further completed reconcile with no rollout churn (VAL-SMOKE-002, VAL-SMOKE-005). The
default-webhook-strategy half of VAL-SMOKE-005 (every eligible container mutated on each pod the Deployment
controller admits naturally, existing pods untouched under the default `onNextRestart` rollout) is written and
verified live but is not yet in the suite: it requires the pod→ReplicaSet→Deployment parent-annotation lookup
the production mutator performs, and the kustomize `config/rbac/role.yaml` drifts from the shipped Helm chart
(which grants the controller's service account `replicasets` read for exactly that lookup) so a
`config/`-installed controller silently admits new Deployment pods unmutated; with the chart-mirroring one-line
role addition the spec and the full suite pass. The `webhook` label also covers the SSA
managed-field-ownership scenario and the live TLS admission probe that mutates an eligible hand-labelled pod and
admits ineligible pods unchanged — that probe proves admission reachability only, never natural rollout, which is
a conformance-tier concern), `diagnostics` (harness self-check) and `deliberate-failure` (opt-in failure-path
validation). The remaining behavior label from `design.md` §5 — `failure-recovery` — arrives with the remaining
M2 smoke feature; `conformance`, `rbac`, `observability` and friends with M3.

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
