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

Honesty boundaries that apply across all M1 docs: local timings are warm, single-host
measurements (not universal cold-start numbers); pending/skipped/simulator-only/fixture-only
legacy checks are never credited as deployed-behaviour proof; and every coverage-matrix row is
`Planned` at M1 — `make test-e2e-smoke` first exists in M2 and `make test-e2e-conformance` in
M3.
