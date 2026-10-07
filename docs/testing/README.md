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
| `make test-e2e-labels E2E_LABELS="diagnostics"` | Run the new suite filtered by Ginkgo label (also `E2E_LABELS="smoke"`, `"smoke && webhook"`, `"webhook"`, `"failure-recovery"`, ...). `E2E_SUITE_TIMEOUT` (default `45m`) bounds the `go test` timeout. |
| `make test-e2e-deliberate-failure` | Opt-in spec that fails one deliberate assertion to exercise pre-teardown redacted diagnostics; a nonzero exit and a redacted bundle are the expected outcome, not a product defect. |

Labels currently in the suite: `diagnostics` (harness self-check), `deliberate-failure`
(opt-in failure-path validation). The behavior labels `smoke`, `webhook` and
`failure-recovery` from `design.md` §5 arrive with the M2 smoke features; `conformance`,
`rbac`, `observability` and friends with M3.

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
legacy checks are never credited as deployed-behaviour proof; and every coverage-matrix row is
`Planned` at M1 — `make test-e2e-smoke` first exists in M2 and `make test-e2e-conformance` in
M3.
