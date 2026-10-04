# OptiPod behaviour-to-test coverage matrix (M1)

This matrix maps every **approved behaviour group** to the lowest sufficient proof layer, a
**named** (planned) test identifier, a concrete API/output observable, the milestone that
makes it executable, and the legacy scenarios it supersedes. It fulfils validation assertion
**VAL-DESIGN-003**.

> **M1 honesty boundary.** No row below is executable yet. Every row's **M1 status is
> `Planned`**; the test identifier is the *intended* package/label and spec name. A row flips
> to executed only when its milestone lands and the named command passes. `make test-e2e-smoke`
> first exists in M2; `make test-e2e-conformance` and the production-path migrations in M3.
> Pending/skipped/simulator/fixture-only legacy scenarios are tracked in `inventory.md` and are
> never shown here as current proof.

## Layer and label vocabulary

- **unit** — `go test -short` over a production package (`api/v1alpha1`, `internal/*`).
- **envtest** — `go test` with pinned `KUBEBUILDER_ASSETS` over `internal/controller` (real API
  server/etcd/CRDs, production reconciler/handler; no pods/RBAC/admission).
- **clusterless** — production-path check in `test/clusterless/workflows` (fixture-only checks
  are explicitly excluded from this matrix).
- **smoke** — `make test-e2e-smoke`, new `test/e2e` framework suite, labels `smoke`, `webhook`,
  `failure-recovery`.
- **conformance** — `make test-e2e-conformance`, same framework, labels `conformance`, `webhook`,
  `rbac`, `observability`, `failure-recovery`, plus the mode/selector/safety/rollout/strategy/
  concurrency/compatibility/metrics-fault focus labels.

Named test identifiers in the "Planned test" column are the intended Go test function or
Ginkgo spec; they are created in M2/M3, not M1.

## Matrix

| # | Behaviour group | Layer | Planned test (package / label → identifier) | Positive observable | Negative observable | Milestone | Legacy scenarios | M1 status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B01 | Installation & readiness | smoke | `test/e2e` (`smoke`/`install`) → *harness installs CRD/RBAC/controller/webhook once* | Deployed image ID + service account match; CRD `Established`, Deployment `Available`, webhook endpoints/cert ready | A rendered manifest or merely-Ready pod does **not** pass; missing webhook endpoint fails the gate | M2 | S030, S097 | Planned |
| B02 | API admission & CRD schema | unit + envtest + smoke | unit `api/v1alpha1` → `TestValidateCreate*`; envtest `internal/controller` → *CRD create/get*; smoke `test/e2e` (`policy`) | Persisted CR UID/spec + post-create reconcile `resourceVersion`/status via private API; CRD-schema rejection distinct from runtime-invalid status | Malformed value → API reject with cause; CRD-admitted-but-runtime-invalid → clear invalid status, workload unchanged | M2 (smoke) / M3 (unit, envtest) | S031, S032, S033, S036, S037, S125 | Planned |
| B03 | Modes: Disabled / Recommend / Auto | envtest + smoke + conformance | envtest `internal/controller`,`internal/application` → `TestReconcileModes`; conformance `test/e2e` (mode) | Recommend writes exact per-container recommendation annotation with unchanged requests; Auto applies distinct values under strategy; Disabled skips | Disabled adds no recommendation/application after a completed reconcile with changed metrics | M2 (Recommend/Auto smoke) / M3 (transitions) | S034, S035, S075, S076, S077, S078, S130 | Planned |
| B04 | Strategies: SSA vs webhook | unit + conformance | unit `internal/webhook`,`internal/application` → `TestSSAvsWebhookPath`; conformance `test/e2e` (`strategy`) | SSA modifies intended managed fields, unrelated fields intact; switching to webhook yields admitted eligible pods | A strategy-field edit alone is **not** a migration; unmatched pods unchanged | M3 | S093, S096, S059 | Planned |
| B05 | Rollouts: immediate / onNextRestart | conformance | conformance `test/e2e` (`webhook`/`rollout`) → `onNextRestart not immediate`, `immediate mutates new pods` | onNextRestart: metadata written, running pods/template unchanged until a deliberate restart; immediate: restart marker + new admitted pod resources per kind | A stored annotation without an actual restart/new admitted value fails | M3 | S091, S092, S094, S095 | Planned |
| B06 | Three workload kinds (Deploy/STS/DS) | envtest + conformance | envtest `internal/discovery`,`internal/controller` → `TestDiscoverKinds`; conformance `test/e2e` (`conformance`/selector) | Exact recommendation/resources observed on each eligible kind via API | Ineligible kind / excluded kind unchanged after a bounded reconcile | M3 | S094, S111, S114 | Planned |
| B07 | Selectors: include/exclude, namespace, label | unit + envtest + conformance | unit `internal/policy` → `TestSelectorFilters`; envtest `internal/discovery`,`internal/controller`; conformance `test/e2e` (`selector`) | Included identities discovered/affected; omitted-filter compatibility | Non-matching namespace/label and denied namespace remain unchanged (negative selection asserted) | M3 | S111, S112, S113, S114, S116, S117, S127 | Planned |
| B08 | Weights / winner / conflicts | unit + envtest + conformance | unit `internal/policy` → `TestSelectWinner`; envtest `internal/controller`; conformance `test/e2e` (`policy`) | Highest supported weight wins; omitted-weight default; deterministic tie; observed workload winner + applied effect | Disabled/non-matching excluded; **no unsupported specificity precedence**; changed/deleted winner flips consistently | M3 | S115, S118, S119, S120, S121, S122, S123, S124 | Planned |
| B09 | Resource bounds (CPU/memory clamp) | unit + conformance | unit `internal/recommendation` → `TestBoundsClamp`; conformance `test/e2e` (`safety`) | Within/below/above-bound inputs produce expected recommended and applied quantities; limits consistent with policy | Invalid bound → API/validation denial, not a fake successful clamp | M3 | S079, S080, S081, S082, S083, S126, S129, S132, S136, S137 | Planned |
| B10 | Memory decrease safety | unit + conformance | unit `internal/recommendation`,`internal/application` → `TestMemorySafety`; conformance `test/e2e` (`safety`) | Allowed increase applied; explicitly-enabled gradual decrease respects configured steps | Unsafe decrease withheld under default; a blocked apply may still leave recommendation metadata | M3 | S043, S044, S057, S058, S060, S133, S134, S135, S138, S139, S141 | Planned |
| B11 | Multi-container (+partial data) | unit + conformance | unit `internal/controller`,`internal/webhook` → `TestMultiContainer`; conformance `test/e2e` (`multi-container`) | Independently named container recommendations/applications; unrelated sidecar untouched | Missing one required container's metrics → safe skip/no new application with explicit signal | M3 | S040, S145, S149 | Planned |
| B12 | Metrics faults (missing/invalid/unavailable) | unit + conformance | unit `internal/metrics`,`internal/cache` → `TestProviderFaults`; conformance `test/e2e` (`metrics-fault`) | Valid range-query window yields recommendation; cache invalidation + recovery after valid data returns | Missing/short/malformed/unavailable series → skip/error decision, requests unchanged; not read from mock state only | M3 | S039, S040, S128, S131, S140 | Planned |
| B13 | Invalid configuration | unit + envtest | unit `api/v1alpha1` → `TestInvalidConfig`; envtest `internal/controller` | Useful field errors for invalid bounds/selectors/factors/modes/strategy; persisted invalid status | Schema-rejected inputs distinguished from schema-admitted-but-runtime-invalid; no workload mutation on error | M3 | S038, S051, S052, S053, S054, S055, S056, S083 | Planned |
| B14 | Concurrency | envtest (`-race`) + conformance | envtest `internal/controller`,`internal/cache`,`internal/application` → `TestConcurrentReconcile`; conformance `test/e2e` (`concurrency`) | Newer unrelated workload fields and current policy selection preserved; status ordering stable | Overlapping writes either settle on current behaviour or surface a bounded explicit conflict error | M3 | S041, S042 | Planned |
| B15 | Recovery (controller restart) | smoke + conformance | smoke `test/e2e` (`failure-recovery`) → `TestControllerRestartRecovers` | After restart, new ready pod on same image/SA; changed metric input triggers fresh reconcile with changed recommendation/applied value | A ready pod or stale annotation alone does **not** prove recovery | M2 | S045, S098 | Planned |
| B16 | RBAC | conformance | conformance `test/e2e` (`rbac`) → `TestShippedRBAC` | Installed controller SA reconciles/status-updates with shipped roles, no compensating grants | A restricted unrelated identity is denied a representative protected action | M3 | *(new; legacy suite granted extra RBAC — see `legacy-hazards.md` §RBAC)* | Planned |
| B17 | Webhook TLS & failure policy | conformance | conformance `test/e2e` (`webhook`) → `TestWebhookTLSFailurePolicy` | Admission succeeds with valid Service endpoint, served-cert DNS/CA chain; restoring config returns mutation | Scoped outage: `Ignore` admits without mutation, `Fail` rejects matching admission; static YAML/direct-handler tests do not pass | M3 | S099, S100, S101, S102, S103, S104, S105 | Planned |
| B18 | Observability (metrics & logs) | conformance | conformance `test/e2e` (`observability`) → `TestMetricsAndStructuredLogs` | Supported metrics path contains relevant OptiPod series with a real changed counter before/after a named reconcile; structured log record ties policy/workload to outcome | A bare HTTP 200 or generic startup string fails; sensitive values excluded | M3 | S065 | Planned |
| B19 | Legacy optional fields | unit + envtest + conformance | unit `api/v1alpha1`,`internal/application`,`internal/webhook` → `TestLegacyDefaults`; envtest `internal/controller`; conformance `test/e2e` (`compatibility`) | Policies omitting newer weight/type/strategy/rollout/limit settings follow persisted production defaults read from the API | Legacy-owned workload fields/annotations undisturbed; defaults not inferred from Go zero values | M3 | S061, S062, S095, S113 | Planned |
| B20 | Real webhook mutator decisions | unit + smoke | unit `internal/webhook` → `TestMutatorPatch`; smoke `test/e2e` (`webhook`) | Exact patch paths/quantities from the **production** mutator; eligible pod admitted, ineligible pod unchanged (selector `optipod.io/webhook-enabled=true`) | The independent clusterless simulator is **not** counted; calling the mutator directly is unit-only, live admission is proven in smoke | M2 (smoke) / M3 (unit) | S142, S143, S144, S146, S147, S148, S150 | Planned |

## Group coverage check

Every approved behaviour group named in the validation contract maps to at least one row:

| Approved group | Row(s) |
| --- | --- |
| installation | B01 |
| admission (API/CRD) | B02, B13 |
| modes | B03 |
| strategies | B04, B20 |
| rollouts | B05 |
| three workload kinds | B06 |
| selectors | B07 |
| weights/conflicts | B08 |
| bounds | B09 |
| memory safety | B10 |
| multi-container | B11 |
| metrics faults | B12 |
| invalid config | B13 |
| concurrency | B14 |
| recovery | B15 |
| RBAC | B16 |
| webhook TLS & failure policy | B17 |
| observability | B18 |
| legacy optional fields | B19 |

## Legacy-scenario traceability

Every **retained** or **replaced** scenario in `inventory.md` is cross-referenced by at least
one row above (its "Legacy scenarios" column). **Removed** scenarios (62 rows: harness
self-tests, tautological, upstream-only, pending/skipped, simulator-only duplicates, and
out-of-scope performance load) intentionally have no matrix row; their removal rationale lives
in `inventory.md`. The one simulator-only group that carried genuine behaviour (webhook
mutation, S142–S150) is re-proven through B20 against the real `internal/webhook` mutator and
live admission, never the simulator.

B16 (RBAC) is marked *new* because the legacy suite granted extra RBAC during setup (see
`legacy-hazards.md`), which could mask least-privilege defects; the approved coverage tests the
**shipped** roles only.
