# OptiPod legacy scenario inventory and disposition (M1)

This is the versioned, reviewed disposition of every legacy test scenario. It fulfils
validation assertion **VAL-DESIGN-002**. The `/tmp/optipod-planning-inventory.md` planning
table was a seed only; every row below was **re-extracted from the actual source
declarations** on 2026-10-04 and the counts, `path:line` and titles were verified against
`test/e2e`, `test/e2e/contract` and `test/clusterless/workflows` (see "Verification"). No
test code is written in M1; the "new layer/location" and "milestone" columns describe the
approved **future** placement, not present coverage.

## Scope and counting method

A *scenario* is a single executable leaf assertion declaration: a Ginkgo `It(...)` or a Go
`t.Run(...)` subtest. Suite entry points (`TestE2E`, `TestContractE2E`,
`TestClusterlessWorkflows`, and the two Go `Test*` wrappers around the contract subtests) are
**not** counted as scenarios. Fixture/simulator/skip/pending checks are inventoried but are
**never** credited as proof of deployed behaviour.

| Group | Location | Declaration kind | Count | IDs |
| --- | --- | --- | --- | --- |
| Main E2E | `test/e2e/*.go` | Ginkgo `It` | 86 | S001–S004, S036–S050, S051–S062, S063–S066, S067–S074, S075–S090, S091–S110, S111–S117 |
| Contract E2E | `test/e2e/contract/*.go` | Ginkgo `It` | 31 | S005–S035 |
| Clusterless | `test/clusterless/workflows/*.go` | Ginkgo `It` | 33 | S118–S150 |
| Contract Go subtests | `test/e2e/contract/error_handling_unit_test.go` | Go `t.Run` | 5 | S151–S155 |
| **Total** | | | **155** | **S001–S155** |

> The ID ordering follows the planning seed (grouped by file, not strictly by group), so the
> "Main E2E" IDs above are interleaved with contract/clusterless IDs. The per-group **counts**
> (86 / 31 / 33 / 5) are what matter and are verified below.

## Disposition legend

| Disposition | Meaning |
| --- | --- |
| **retained** | Real production-path assertions are kept and re-homed to the correct layer with minimal change. |
| **replaced** | The old test is discarded; the behaviour is proven by a **new** test that exercises a real production entry point. Replaces fixture-only, simulator-only, setup/readiness-only or sleep-based checks. |
| **removed** | Deleted with no replacement: harness self-test, tautological, upstream-library-only, duplicate, pending/skipped, or out of approved scope. |

Disposition totals: **retained 6**, **replaced 87**, **removed 62** (= 155). No pending,
skipped, simulator-only or fixture-only scenario is marked `retained`.

## Inventory

| ID | Old `path:line` — title | New layer → location | Disposition | Milestone | Reason |
| --- | --- | --- | --- | --- | --- |
| S001 | `test/e2e/ci_integration_unit_test.go:15` — should detect CI environment variables | — (deleted) | removed | — | Harness self-test of CI env detection; no OptiPod production entry point. Legacy CI/reporter harness retired with the old suites. |
| S002 | `test/e2e/ci_integration_unit_test.go:39` — should validate timeout error handling | — (deleted) | removed | — | Harness self-test (local timeout/error strings); no product entry point. |
| S003 | `test/e2e/ci_integration_unit_test.go:58` — should create artifact directory structure | — (deleted) | removed | — | Harness self-test (artifact dir layout); replaced by framework JUnit/timing paths in `design.md`. |
| S004 | `test/e2e/ci_integration_unit_test.go:76` — should validate error message formatting | — (deleted) | removed | — | Harness self-test (string formatting); no product behaviour. |
| S005 | `test/e2e/contract/build_system_validation_test.go:33` — should validate that test-e2e-contract target runs only contract tests | — (deleted) | removed | — | Validates the legacy Makefile `test-e2e-contract` target; those targets are replaced by the new Make tiers (`design.md`). Harness self-test. |
| S006 | `test/e2e/contract/build_system_validation_test.go:50` — should validate that e2e-cluster target creates fresh clusters | — (deleted) | removed | — | Harness self-test of the legacy `e2e-cluster` target, which is removed. |
| S007 | `test/e2e/contract/build_system_validation_test.go:67` — should validate that contract target has no code generation dependencies | — (deleted) | removed | — | Harness self-test of legacy Make wiring; superseded by non-generating gate design. |
| S008 | `test/e2e/contract/build_system_validation_test.go:102` — should validate that existing comprehensive test targets are preserved | — (deleted) | removed | — | Harness self-test of legacy Make targets. |
| S009 | `test/e2e/contract/build_system_validation_test.go:132` — should validate that e2e-cluster target can be executed | — (deleted) | removed | — | Harness self-test of removed target. |
| S010 | `test/e2e/contract/build_system_validation_test.go:146` — should validate that test-e2e-contract target can be executed in dry-run mode | — (deleted) | removed | — | Harness self-test of removed target. |
| S011 | `test/e2e/contract/build_system_validation_test.go:162` — should validate that contract targets have proper help documentation | — (deleted) | removed | — | Harness self-test of Make help text. |
| S012 | `test/e2e/contract/build_system_validation_test.go:177` — should validate that contract targets are in the E2E Tests section | — (deleted) | removed | — | Harness self-test of Make section layout. |
| S013 | `test/e2e/contract/consistency_validation_test.go:27` — should have valid and consistent configuration values | — (deleted) | removed | — | Harness self-test of the legacy consistency-config object; not product behaviour. |
| S014 | `test/e2e/contract/consistency_validation_test.go:56` — should initialize execution environment without manual setup | — (deleted) | removed | — | Harness self-test of legacy execution-environment helper. |
| S015 | `test/e2e/contract/consistency_validation_test.go:70` — should use consistent timeout and polling behavior | — (deleted) | removed | — | Harness self-test of helper timeout constants. |
| S016 | `test/e2e/contract/consistency_validation_test.go:90` — should correctly detect the execution environment | — (deleted) | removed | — | Harness self-test of env detection. |
| S017 | `test/e2e/contract/contract_scope_validation_test.go:32` — should enforce user-visible behavior validation only | — (deleted) | removed | — | Harness self-test enforcing the legacy contract-scope rules; superseded by this inventory/matrix. |
| S018 | `test/e2e/contract/contract_scope_validation_test.go:38` — should validate controller installation contract compliance | — (deleted) | removed | — | Harness self-test; real installation is proven at VAL-HARNESS-003 (M2). |
| S019 | `test/e2e/contract/contract_scope_validation_test.go:49` — should validate CR acceptance contract compliance | — (deleted) | removed | — | Harness self-test; real CR acceptance proven at VAL-SMOKE-001 (M2). |
| S020 | `test/e2e/contract/contract_scope_validation_test.go:64` — should validate status convergence contract compliance | — (deleted) | removed | — | Harness self-test; real reconcile status proven at VAL-SMOKE-001 (M2). |
| S021 | `test/e2e/contract/contract_scope_validation_test.go:88` — should validate no forbidden assertions are present | — (deleted) | removed | — | Harness self-test (lints other tests). |
| S022 | `test/e2e/contract/contract_scope_validation_test.go:94` — should validate overall contract compliance | — (deleted) | removed | — | Harness self-test aggregate. |
| S023 | `test/e2e/contract/contract_scope_validation_test.go:102` — should provide clear guidance on allowed assertions | — (deleted) | removed | — | Harness self-test (guidance strings). |
| S024 | `test/e2e/contract/contract_scope_validation_test.go:112` — should identify forbidden assertion patterns | — (deleted) | removed | — | Harness self-test (regex over test source). |
| S025 | `test/e2e/contract/error_handling_test.go:23` — should create actionable error messages without internal details | — (deleted) | removed | — | Duplicates Go subtest S151; exercises the legacy `contract/error_handling.go` reporter, not controller errors. |
| S026 | `test/e2e/contract/error_handling_test.go:69` — should generate clear pass/fail status reports | — (deleted) | removed | — | Duplicates Go subtest S152; reporter self-test. |
| S027 | `test/e2e/contract/error_handling_test.go:103` — should sanitize error messages to remove internal details | — (deleted) | removed | — | Duplicates Go subtest S153; reporter self-test. Real redaction is proven at VAL-HARNESS-006 (M2). |
| S028 | `test/e2e/contract/error_handling_test.go:119` — should provide actionable suggestions for common error scenarios | — (deleted) | removed | — | Duplicates Go subtest S154; reporter self-test. |
| S029 | `test/e2e/contract/error_handling_test.go:144` — should create comprehensive test suite summaries | — (deleted) | removed | — | Duplicates Go subtest S155; reporter self-test. |
| S030 | `test/e2e/contract/install_test.go:54` — should maintain stable operation without restarts | `test/e2e` smoke (`install`) | replaced | M2 | Only checks pod `Ready` + `RestartCount==0` (readiness-only). Replaced by owned-cluster install/readiness proof (VAL-HARNESS-003) with real image/service-account/webhook endpoints. |
| S031 | `test/e2e/contract/minimal_cr_test.go:55` — should be accepted by the API server without validation errors | `test/e2e` smoke (`install`/`policy`) | replaced | M2 | Real but narrow CR create; replaced by admission + reconcile-status smoke (VAL-SMOKE-001) via private kubeconfig. |
| S032 | `test/e2e/contract/minimal_cr_test.go:78` — should be retrievable from the API server after creation | `test/e2e` smoke (`policy`) | replaced | M2 | Narrow get-after-create; folded into VAL-SMOKE-001 (persisted UID/spec + reconcile timestamp). |
| S033 | `test/e2e/contract/minimal_cr_test.go:115` — should persist in the cluster without validation errors | `test/e2e` smoke (`policy`) | replaced | M2 | Duplicate persistence check of S031/S032; replaced by the single VAL-SMOKE-001 scenario. |
| S034 | `test/e2e/contract/status_ready_test.go:56` — should converge to Ready=True condition within timeout period | `test/e2e` smoke (`policy`) | replaced | M2 | `Ready=True` alone is not processing proof; replaced by reconcile status + validation-condition assertions (VAL-SMOKE-001). |
| S035 | `test/e2e/contract/status_ready_test.go:105` — should show status processing activity within timeout period | `test/e2e` smoke (`policy`) | replaced | M2 | Readiness-only; replaced by observed recommendation/status in VAL-SMOKE-001/002. |
| S036 | `test/e2e/error_handling_test.go:45` — should handle policies with malformed resource specifications | unit `api/v1alpha1` + `test/e2e` smoke (`policy`) | replaced | M2/M3 | Setup/acceptance only. Replaced by CRD-schema rejection + runtime-invalid status without workload mutation (VAL-FAST-001, VAL-SMOKE-001). — Fast-layer proof landed: `api/v1alpha1` envtest `TestEnvtestCRDSchemaRejectsInvalidValues` (real-CRD rejection with actionable `spec.*` cause). |
| S037 | `test/e2e/error_handling_test.go:70` — should handle workloads with invalid resource specifications | unit `api/v1alpha1` + `test/e2e` smoke (`policy`) | replaced | M2/M3 | Acceptance-only; replaced by explicit invalid-input cause and unchanged-workload assertions. — Fast-layer proof landed: `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (explicit field causes) + envtest runtime-invalid distinction. |
| S038 | `test/e2e/error_handling_test.go:99` — should handle policies with missing required fields | unit `api/v1alpha1` | replaced | M3 | Replaced by `ValidateCreate` field-error unit cases (VAL-FAST-001). — Fast-layer proof landed: `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (missing required fields with named causes). |
| S039 | `test/e2e/error_handling_test.go:126` — should handle metrics server unavailability gracefully | unit `internal/metrics`,`internal/cache` + `test/e2e` conformance (`metrics-fault`) | replaced | M3 | Checks availability as proxy; no fault induced. Replaced by deterministic unavailable-provider decision + recovery (VAL-FAST-007, VAL-CONF-006). |
| S040 | `test/e2e/error_handling_test.go:173` — should handle partial metrics availability | unit `internal/metrics` + `test/e2e` conformance (`multi-container`/`metrics-fault`) | replaced | M3 | Replaced by partial-series skip/no-apply decision with explicit signal (VAL-FAST-007, VAL-CONF-005/006). |
| S041 | `test/e2e/error_handling_test.go:204` — should handle concurrent policy updates | envtest `internal/controller` + `test/e2e` conformance (`concurrency`) | replaced | M3 | Asserts kubectl success only; replaced by resourceVersion/generation + bounded-conflict assertions (VAL-FAST-008, VAL-CONF-010). |
| S042 | `test/e2e/error_handling_test.go:245` — should handle concurrent workload modifications | envtest `internal/controller`,`internal/cache` + `test/e2e` conformance (`concurrency`) | replaced | M3 | Replaced by preserved-newer-field + conflict-signal assertions (VAL-FAST-008, VAL-CONF-010). |
| S043 | `test/e2e/error_handling_test.go:288` — should detect potentially unsafe memory decreases | unit `internal/recommendation`,`internal/application` + `test/e2e` conformance (`safety`) | replaced | M3 | Explicit TODO/"For now" substitutes a setup assertion for the claimed behaviour. Replaced by exact decrease-block math and withheld-apply assertions (VAL-FAST-005, VAL-CONF-004). |
| S044 | `test/e2e/error_handling_test.go:318` — should handle memory safety thresholds | unit `internal/recommendation` + `test/e2e` conformance (`safety`) | replaced | M3 | Replaced by threshold/clamp assertions with exact quantities (VAL-FAST-005, VAL-CONF-004). |
| S045 | `test/e2e/error_handling_test.go:342` — should handle API server connectivity issues gracefully | `test/e2e` smoke (`failure-recovery`) | replaced | M2 | Disconnect is not actually induced; only availability is checked. Replaced by controller restart/reconcile recovery (VAL-SMOKE-004). |
| S046 | `test/e2e/error_handling_unit_test.go:12` — should format configuration validation errors correctly | — (deleted) | removed | — | Local error-formatting harness check; no product entry point. |
| S047 | `test/e2e/error_handling_unit_test.go:30` — should validate error message content | — (deleted) | removed | — | Local string-content harness check. |
| S048 | `test/e2e/error_handling_unit_test.go:43` — should validate error message clarity | — (deleted) | removed | — | Local string-clarity harness check. |
| S049 | `test/e2e/error_handling_unit_test.go:67` — should classify errors correctly | — (deleted) | removed | — | Local error-classification harness check; no product entry point. |
| S050 | `test/e2e/error_handling_unit_test.go:87` — should handle unknown error types gracefully | — (deleted) | removed | — | Local harness check; no product behaviour. |
| S051 | `test/e2e/invalid_config_unit_test.go:65` — should reject policies with invalid CPU resource bounds (min > max) | unit `api/v1alpha1` + envtest `internal/controller` | retained | M3 | Real `OptimizationPolicy.ValidateCreate()`; kept and re-homed out of the Kind `BeforeSuite`, with envtest adding CRD-schema vs runtime-invalid distinction (VAL-FAST-001). — Fast-layer proof landed: re-homed out of the Kind suite to `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (CPU min>max with both values in the error) + envtest `TestEnvtestSchemaAdmittedButRuntimeInvalid`/`TestEnvtestCRDSchemaRejectsInvalidValues`; the legacy file `test/e2e/invalid_config_unit_test.go` was REMOVED (M3). |
| S052 | `test/e2e/invalid_config_unit_test.go:74` — should reject policies with invalid memory resource bounds (min > max) | unit `api/v1alpha1` + envtest `internal/controller` | retained | M3 | Real validation assertion kept; re-homed to production-path unit + envtest (VAL-FAST-001). — Fast-layer proof landed: re-homed to `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (memory min>max) + envtest runtime-invalid distinction; legacy file removed (M3). |
| S053 | `test/e2e/invalid_config_unit_test.go:83` — should reject policies with zero resource bounds | unit `api/v1alpha1` | retained | M3 | Real validation assertion kept (VAL-FAST-001). — Fast-layer proof landed: re-homed to `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (zero bounds rejected with "greater than zero"); legacy file removed (M3). |
| S054 | `test/e2e/invalid_config_unit_test.go:121` — should reject policies with invalid safety factor | unit `api/v1alpha1` | retained | M3 | Real validation assertion kept (VAL-FAST-001). — Fast-layer proof landed: re-homed to `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (safety factor < 1.0 with observed value); legacy file removed (M3). |
| S055 | `test/e2e/invalid_config_unit_test.go:160` — should reject policies without selectors | unit `api/v1alpha1` | retained | M3 | Real validation assertion kept (VAL-FAST-001). — Fast-layer proof landed: re-homed to `api/v1alpha1` `TestOptimizationPolicy_ValidateCreate_FieldErrors` (selector required with named options); legacy file removed (M3). |
| S056 | `test/e2e/invalid_config_unit_test.go:196` — should accept policies with valid configuration edge cases | unit `api/v1alpha1` + envtest `internal/controller` | retained | M3 | Real positive-validation assertion kept; envtest confirms CRD acceptance + persisted defaults (VAL-FAST-001). — Fast-layer proof landed: re-homed to `api/v1alpha1` validation tables (large/small bounds, max safety factor accepted) + envtest `TestEnvtestCRDDefaultsPersisted`; legacy file removed (M3). |
| S057 | `test/e2e/memory_safety_integration_test.go:43` — should optimize underutilized workload with default limits | unit `internal/recommendation`,`internal/application` + `test/e2e` conformance (`safety`) | replaced | M3 | Sleeps + static-request checks. Replaced by computed recommendation + post-reconcile applied values (VAL-FAST-005, VAL-CONF-004). |
| S058 | `test/e2e/memory_safety_integration_test.go:92` — should optimize overutilized workload with custom limits | unit `internal/recommendation` + `test/e2e` conformance (`safety`) | replaced | M3 | Replaced by exact over-utilised recommendation/apply with limit consistency (VAL-FAST-005, VAL-CONF-004). |
| S059 | `test/e2e/memory_safety_integration_test.go:183` — should respect updateRequestsOnly flag | unit `internal/application` | replaced | M3 | Replaced by `updateRequestsOnly` request/limit assertion (VAL-FAST-005). |
| S060 | `test/e2e/memory_safety_integration_test.go:265` — should complete full optimization cycle without safety check blocking | `test/e2e` conformance (`safety`) | replaced | M3 | Sleep-based full-cycle check; replaced by allowed-increase applied-value assertion (VAL-CONF-004). |
| S061 | `test/e2e/memory_safety_integration_test.go:354` — should work with existing policy configurations | unit `internal/application` + `test/e2e` conformance (`compatibility`) | replaced | M3 | Replaced by legacy-optional-field default behaviour (VAL-FAST-009, VAL-CONF-014). |
| S062 | `test/e2e/memory_safety_integration_test.go:387` — should maintain existing behavior when no limitConfig is specified | unit `internal/application` | replaced | M3 | Replaced by absent-optional-setting safe-default assertion (VAL-FAST-005/009). |
| S063 | `test/e2e/observability_test.go:22` — should expose Prometheus metrics | — (deleted) | removed | — | Declared `Pending`; never asserts. Not credited as proof. Real metrics scrape proven at VAL-CONF-013 (M3). |
| S064 | `test/e2e/observability_test.go:30` — should contain OptipPod-specific metrics after workload processing | — (deleted) | removed | — | Body is `Skip(...)`; never asserts. Behaviour proven at VAL-CONF-013 (M3). |
| S065 | `test/e2e/observability_test.go:37` — should produce structured logs | `test/e2e` conformance (`observability`) | replaced | M3 | Only generic controller-log substring presence; no structure assertion. Replaced by structured record tying policy/workload to outcome (VAL-CONF-013). |
| S066 | `test/e2e/observability_test.go:44` — should log policy processing events | — (deleted) | removed | — | Body is `Skip(...)`; never asserts. Behaviour proven at VAL-CONF-013 (M3). |
| S067 | `test/e2e/observability_unit_test.go:14` — should validate Prometheus metric naming conventions | — (deleted) | removed | — | Regex/naming harness check; no instrumentation entry point. |
| S068 | `test/e2e/observability_unit_test.go:27` — should validate metric suffixes | — (deleted) | removed | — | Regex harness check. |
| S069 | `test/e2e/observability_unit_test.go:48` — should validate metric labels format | — (deleted) | removed | — | Regex harness check. |
| S070 | `test/e2e/observability_unit_test.go:63` — should validate common log patterns | — (deleted) | removed | — | Regex harness check. |
| S071 | `test/e2e/observability_unit_test.go:83` — should validate contextual information in logs | — (deleted) | removed | — | Regex harness check. |
| S072 | `test/e2e/observability_unit_test.go:99` — should detect various sensitive information patterns | — (deleted) | removed | — | Regex harness check; real redaction proven at VAL-HARNESS-006 (M2). |
| S073 | `test/e2e/observability_unit_test.go:114` — should allow safe log content | — (deleted) | removed | — | Regex harness check. |
| S074 | `test/e2e/observability_unit_test.go:136` — should validate regex patterns work correctly | — (deleted) | removed | — | Tests the test's own regex; tautological. |
| S075 | `test/e2e/policy_modes_test.go:45` — should generate recommendations and apply updates | `test/e2e` smoke (`policy`) + conformance (mode) | replaced | M2/M3 | Checks original 100m request, not an emitted recommendation or Auto-applied value. Replaced by before/after request + exact recommendation/applied values (VAL-SMOKE-002, VAL-CONF-001). — Fast-layer proof landed: `internal/controller/modes_envtest_test.go` Recommend spec (exact annotations, unchanged requests) and Auto apply through the production reconciler. |
| S076 | `test/e2e/policy_modes_test.go:103` — should generate recommendations but not apply updates | `test/e2e` smoke (`policy`) + conformance (mode) | replaced | M2/M3 | Weak Recommend non-mutation; replaced by exact recommendation annotation written with unchanged requests (VAL-SMOKE-002, VAL-CONF-001). — Fast-layer proof landed: `internal/controller/modes_envtest_test.go` Recommend spec (exact `optipod.io/recommendation.*` values via the API, no application). |
| S077 | `test/e2e/policy_modes_test.go:161` — should not process workloads at all | `test/e2e` conformance (mode) | replaced | M3 | Weak Disabled check; replaced by Disabled-skip after a completed reconcile with changed metrics (VAL-CONF-001). — Fast-layer proof landed: `internal/controller/modes_envtest_test.go` Disabled spec (discovered=1/processed=0, no annotations, unchanged requests after a completed reconcile). |
| S078 | `test/e2e/policy_modes_test.go:226` — should handle mode changes correctly | `test/e2e` conformance (mode) | replaced | M3 | Replaced by Disabled→Recommend→Auto→Disabled transition with pod-UID/resource evidence (VAL-CONF-001). — Fast-layer proof landed: `internal/controller/modes_envtest_test.go` transitions spec (Recommend→Auto applies, Auto→Disabled blocks a changed-metric reconcile with processed=0). |
| S079 | `test/e2e/resource_bounds_test.go:45` — should respect resource bounds when resources are within limits | unit `internal/recommendation` + conformance (`safety`) | replaced | M3 | Fixture/config-only check. Replaced by exact within-bounds recommendation math + in-cluster applied value (VAL-FAST-005, VAL-CONF-004). |
| S080 | `test/e2e/resource_bounds_test.go:89` — should clamp resources to minimum when below bounds | unit `internal/recommendation` + conformance (`safety`) | replaced | M3 | Replaced by below-min clamp assertion (VAL-FAST-005, VAL-CONF-004). |
| S081 | `test/e2e/resource_bounds_test.go:139` — should clamp resources to maximum when above bounds | unit `internal/recommendation` + conformance (`safety`) | replaced | M3 | Replaced by above-max clamp assertion (VAL-FAST-005, VAL-CONF-004). |
| S082 | `test/e2e/resource_bounds_test.go:190` — should handle different resource units correctly | unit `internal/recommendation` | replaced | M3 | Replaced by unit-conversion recommendation math table (VAL-FAST-005). |
| S083 | `test/e2e/resource_bounds_test.go:231` — should handle invalid resource bounds gracefully | unit `api/v1alpha1` | replaced | M3 | Invalid-bound rejection belongs at API validation, not a "successful" clamp. Replaced by `ValidateCreate`/CRD rejection (VAL-FAST-001). — Fast-layer proof landed: `api/v1alpha1` field-error tables + envtest (`TestEnvtestSchemaAdmittedButRuntimeInvalid` — invalid bounds are denied by validation and flagged by the reconciler, never a fake clamp). |
| S084 | `test/e2e/resource_bounds_unit_test.go:12` — should parse CPU quantities correctly | — (deleted) | removed | — | Exercises `k8s.io/apimachinery` quantity parsing only; no OptiPod bounds-engine entry point. |
| S085 | `test/e2e/resource_bounds_unit_test.go:31` — should parse memory quantities correctly | — (deleted) | removed | — | Upstream quantity parsing only. |
| S086 | `test/e2e/resource_bounds_unit_test.go:51` — should compare resource quantities correctly | — (deleted) | removed | — | Upstream quantity comparison only. |
| S087 | `test/e2e/resource_bounds_unit_test.go:76` — should handle invalid resource quantities | — (deleted) | removed | — | Upstream quantity error handling only. |
| S088 | `test/e2e/simple_unit_test.go:10` — should pass a simple test | — (deleted) | removed | — | Tautological placeholder; no product behaviour. |
| S089 | `test/e2e/simple_unit_test.go:14` — should handle basic string operations | — (deleted) | removed | — | Tautological string op. |
| S090 | `test/e2e/simple_unit_test.go:20` — should handle basic math operations | — (deleted) | removed | — | Tautological math op. |
| S091 | `test/e2e/webhook_integration_test.go:48` — should complete full webhook workflow with immediate rollout | unit `internal/webhook` + `test/e2e` conformance (`rollout`) | replaced | M2/M3 | Live but eventual/readiness check with multi-minute sleeps. Replaced by exact mutator patch unit test + immediate-rollout restart/new-pod admission (VAL-FAST-006, VAL-CONF-008). |
| S092 | `test/e2e/webhook_integration_test.go:195` — should complete workflow with onNextRestart rollout strategy | `test/e2e` conformance (`webhook`) | replaced | M3 | Replaced by onNextRestart "no immediate restart" + deliberate-restart admission (VAL-CONF-007). |
| S093 | `test/e2e/webhook_integration_test.go:348` — should handle mixed strategies in the same cluster | `test/e2e` conformance (`strategy`) | replaced | M3 | Replaced by SSA↔webhook strategy-boundary scenario (VAL-CONF-009). |
| S094 | `test/e2e/webhook_integration_test.go:568` — should handle rolling restart for different workload types | `test/e2e` conformance (`rollout`) | replaced | M3 | Replaced by per-kind immediate-rollout admission for Deployment/StatefulSet/DaemonSet (VAL-CONF-008). |
| S095 | `test/e2e/webhook_integration_test.go:763` — should maintain compatibility with policies without strategy field | `test/e2e` conformance (`compatibility`/`webhook`) | replaced | M3 | Replaced by omitted-strategy default (onNextRestart) behaviour (VAL-CONF-007, VAL-CONF-014). |
| S096 | `test/e2e/webhook_integration_test.go:877` — should handle migration from SSA to webhook strategy | `test/e2e` conformance (`strategy`) | replaced | M3 | Must observe prior SSA managed-field ownership, not only a strategy-field edit. Replaced by VAL-CONF-009. |
| S097 | `test/e2e/webhook_lifecycle_test.go:40` — should start webhook server successfully with valid configuration | `test/e2e` smoke (`webhook`) | replaced | M2 | Readiness-only. Replaced by webhook service/endpoint/certificate readiness gate before admission (VAL-HARNESS-003, VAL-SMOKE-003). |
| S098 | `test/e2e/webhook_lifecycle_test.go:92` — should handle graceful shutdown | `test/e2e` smoke (`failure-recovery`) | replaced | M2 | Mutates shared deployment; replaced by owned-controller restart/recovery (VAL-SMOKE-004). |
| S099 | `test/e2e/webhook_lifecycle_test.go:155` — should handle webhook server failures gracefully | `test/e2e` conformance (`webhook`) | replaced | M3 | Replaced by scoped webhook outage Fail/Ignore outcomes + recovery (VAL-CONF-012). |
| S100 | `test/e2e/webhook_lifecycle_test.go:242` — should have valid TLS certificates for webhook | `test/e2e` conformance (`webhook`) | replaced | M3 | Static secret/CA inspection; replaced by served-certificate DNS/CA-chain admission success (VAL-CONF-012). |
| S101 | `test/e2e/webhook_lifecycle_test.go:273` — should handle certificate rotation | `test/e2e` conformance (`webhook`) | replaced | M3 | Mutates shared config; replaced by TLS/readiness recovery within VAL-CONF-012 scope. |
| S102 | `test/e2e/webhook_lifecycle_test.go:339` — should respect webhook failure policy under various conditions | `test/e2e` conformance (`webhook`) | replaced | M3 | Replaced by explicit Fail-rejects / Ignore-admits-without-mutation assertions (VAL-CONF-012). |
| S103 | `test/e2e/webhook_lifecycle_test.go:420` — should handle webhook timeout scenarios | `test/e2e` conformance (`webhook`) | replaced | M3 | Timing-sensitive; replaced by failure-policy outcome assertions under scoped outage (VAL-CONF-012). |
| S104 | `test/e2e/webhook_lifecycle_test.go:476` — should have correct webhook configuration | `test/e2e` conformance (`webhook`) | replaced | M3 | Static-config read; replaced by live admission outcome + config/endpoint verification (VAL-CONF-012). |
| S105 | `test/e2e/webhook_lifecycle_test.go:522` — should handle webhook configuration updates | `test/e2e` conformance (`webhook`) | replaced | M3 | Mutates shared config; replaced by scoped config change + restored-mutation assertion (VAL-CONF-012). |
| S106 | `test/e2e/webhook_performance_test.go:41` — should handle high volume pod creation without timeouts | — (deleted) | removed | — | Environment-sensitive throughput load test; performance is not an approved behaviour group. Admission correctness proven at VAL-SMOKE-003/VAL-CONF-012. |
| S107 | `test/e2e/webhook_performance_test.go:185` — should maintain webhook responsiveness under concurrent deployment creation | — (deleted) | removed | — | Environment-sensitive concurrency/latency load test; out of approved scope. |
| S108 | `test/e2e/webhook_performance_test.go:352` — should handle webhook server resource limits gracefully | — (deleted) | removed | — | Environment-sensitive resource-limit load test; out of approved scope. |
| S109 | `test/e2e/webhook_performance_test.go:459` — should maintain acceptable latency under normal load | — (deleted) | removed | — | Environment-sensitive latency threshold; out of approved scope. |
| S110 | `test/e2e/webhook_performance_test.go:571` — should handle burst traffic patterns | — (deleted) | removed | — | Environment-sensitive burst load; out of approved scope. |
| S111 | `test/e2e/workload_type_selector_test.go:141` — Should complete full workflow from policy creation to workload discovery with include filter | envtest `internal/discovery`,`internal/controller` + conformance (`selector`) | replaced | M3 | Inspects discovered list only. Replaced by include-filter discovered/affected identity assertions (VAL-FAST-003, VAL-CONF-002). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (include-filter exact identities through the real API). |
| S112 | `test/e2e/workload_type_selector_test.go:232` — Should complete full workflow with exclude filter | envtest `internal/discovery`,`internal/policy` + conformance (`selector`) | replaced | M3 | Replaced by exclude-precedence excluded-identity assertions (VAL-FAST-003, VAL-CONF-002). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (exclude precedence over include). |
| S113 | `test/e2e/workload_type_selector_test.go:303` — Should handle backward compatibility with existing policies | envtest `internal/controller` + conformance (`compatibility`) | replaced | M3 | Replaced by omitted-filter compatibility default (VAL-FAST-003/009, VAL-CONF-014). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (empty and omitted workload-type filters keep all kinds) + `api/v1alpha1` envtest persisted defaults. |
| S114 | `test/e2e/workload_type_selector_test.go:408` — Should handle multiple policies with different workload type filters | envtest `internal/discovery`,`internal/policy` + conformance (`selector`) | replaced | M3 | Replaced by per-policy workload-type selection with API-read per-kind counts/identities (VAL-FAST-003, VAL-CONF-002). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (all three kinds) + `internal/policy/selector_envtest_test.go` per-policy matching. |
| S115 | `test/e2e/workload_type_selector_test.go:526` — Should handle weight-based selection with workload type filtering | unit `internal/policy` + envtest `internal/controller` | replaced | M3 | Replaced by winner selection with workload-type filter (VAL-FAST-003/004). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` + `internal/controller/policy_selection_envtest_test.go` (winner and effect observed before/after changes). |
| S116 | `test/e2e/workload_type_selector_test.go:605` — Should handle policies with conflicting include/exclude filters | unit `internal/policy` + envtest `internal/controller` | replaced | M3 | Replaced by include/exclude precedence assertion (VAL-FAST-003). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (exclude-takes-precedence over include). |
| S117 | `test/e2e/workload_type_selector_test.go:709` — Should handle policies that result in no discoverable workloads | envtest `internal/discovery` | replaced | M3 | Replaced by empty-result + unchanged-workload assertion (VAL-FAST-003). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (nonmatching selector yields an empty result). |
| S118 | `test/clusterless/workflows/multi_policy_test.go:24` — should select highest weight policy when multiple match | unit `internal/policy` + conformance (`policy`) | replaced | M3 | Fake policy objects; asserts stored fields, and an explicit TODO defers real winner selection. Replaced by `internal/policy/selector.go` winner + observed workload winner (VAL-FAST-004, VAL-CONF-003). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` (highest weight) + `internal/controller/policy_selection_envtest_test.go` (observed winner effect). |
| S119 | `test/clusterless/workflows/multi_policy_test.go:88` — should handle policies without weights (default weight 0) | unit `internal/policy` | replaced | M3 | Fixture field inspection; replaced by default-weight selection assertion (VAL-FAST-004). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` (omitted weight defaults to 100 through the API and wins against weight 50). |
| S120 | `test/clusterless/workflows/multi_policy_test.go:129` — should handle conflicting policy modes | unit `internal/policy` + conformance (`policy`) | replaced | M3 | Replaced by Recommend/Auto conflict winner + resulting workload effect (VAL-FAST-004, VAL-CONF-003). — Fast-layer proof landed: `internal/controller/policy_selection_envtest_test.go` (higher-weight Recommend blocks Auto; effect flips when weights change). |
| S121 | `test/clusterless/workflows/multi_policy_test.go:171` — should handle disabled policies | unit `internal/policy` | replaced | M3 | Replaced by Disabled-exclusion in selection (VAL-FAST-004). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` (disabled policy excluded even with the highest weight). |
| S122 | `test/clusterless/workflows/multi_policy_test.go:214` — should prefer more specific selectors | unit `internal/policy` | replaced | M3 | Title asserts specificity precedence that conflicts with weight-first selection; TODO defers it. Replaced only by the actually-supported rule; **no unsupported specificity precedence is introduced** (VAL-FAST-004). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` (equal weights resolve by name; the more specific selector does NOT win — no unsupported specificity precedence). |
| S123 | `test/clusterless/workflows/multi_policy_test.go:269` — should only consider policies in the same namespace | unit `internal/policy` | replaced | M3 | Replaced by namespace-isolation selection assertion (VAL-FAST-004). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` (selection isolated per workload namespace). |
| S124 | `test/clusterless/workflows/multi_policy_test.go:320` — should handle policy weight changes | unit `internal/policy` + conformance (`policy`) | replaced | M3 | Replaced by deterministic re-selection after a weight change (VAL-FAST-004, VAL-CONF-003). — Fast-layer proof landed: `internal/policy/selector_envtest_test.go` + `internal/controller/policy_selection_envtest_test.go` (deterministic re-selection and effect flip after a weight change). |
| S125 | `test/clusterless/workflows/policy_test.go:26` — should create and validate a basic optimization policy | production-path `test/clusterless/workflows` or envtest `internal/controller` | replaced | M3 | Fake-client fixture state, not an end-to-end recommendation. Promote only with a production entry-point assertion (VAL-FAST-002). — Fast-layer proof landed: `api/v1alpha1` envtest `TestEnvtestCRDDefaultsPersisted`/`TestEnvtestStatusSubresourceAndListSemantics` + validation field-error tables (production path, no fake fixture state). |
| S126 | `test/clusterless/workflows/policy_test.go:47` — should handle policy with resource bounds | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by bounds math (VAL-FAST-005). |
| S127 | `test/clusterless/workflows/policy_test.go:76` — should discover deployments matching policy selector | envtest `internal/discovery`,`internal/controller` | replaced | M3 | Fake-client discovery; replaced by API-backed discovery identities (VAL-FAST-003). — Fast-layer proof landed: `internal/discovery/discovery_envtest_test.go` (API-backed discovery identities). |
| S128 | `test/clusterless/workflows/policy_test.go:106` — should process metrics and calculate recommendations | production-path `test/clusterless/workflows` + unit `internal/recommendation` | replaced | M3 | Injected-metrics fixture; replaced by production provider→recommendation path (VAL-FAST-002/005/007). |
| S129 | `test/clusterless/workflows/policy_test.go:142` — should handle constant load metrics | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by percentile math on a deterministic series (VAL-FAST-005). |
| S130 | `test/clusterless/workflows/policy_test.go:172` — should update policy status during reconciliation | envtest `internal/controller` | replaced | M3 | Explicit TODO/"For now" substitutes a setup assertion. Replaced by API-read reconciler status (VAL-FAST-002). — Fast-layer proof landed: `internal/controller/modes_envtest_test.go` (API-read Ready condition and discovered/processed/per-kind status through the production reconciler). |
| S131 | `test/clusterless/workflows/policy_test.go:195` — should handle time-based operations with mock clock | unit `internal/recommendation`,`internal/metrics` | replaced | M3 | Fixture clock behaviour; replaced by deterministic-clock windowed query cases (VAL-FAST-007). |
| S132 | `test/clusterless/workflows/policy_test.go:209` — should apply safety factor to recommendations | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by exact safety-factor arithmetic (VAL-FAST-005). |
| S133 | `test/clusterless/workflows/safety_test.go:27` — should prevent memory decrease when safety is enabled | unit `internal/recommendation`,`internal/application` | replaced | M3 | Fake client + injected metrics inspect fixture values; TODO present. Replaced by actual decrease-block decision (VAL-FAST-005). |
| S134 | `test/clusterless/workflows/safety_test.go:76` — should allow memory increase even with safety enabled | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by allowed-increase assertion (VAL-FAST-005). |
| S135 | `test/clusterless/workflows/safety_test.go:112` — should support gradual memory decrease | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by bounded gradual-decrease step assertion (VAL-FAST-005). |
| S136 | `test/clusterless/workflows/safety_test.go:158` — should enforce minimum resource bounds | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by min-clamp assertion (VAL-FAST-005). |
| S137 | `test/clusterless/workflows/safety_test.go:200` — should enforce maximum resource bounds | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by max-clamp assertion (VAL-FAST-005). |
| S138 | `test/clusterless/workflows/safety_test.go:243` — should apply safety factor correctly | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by safety-factor arithmetic (VAL-FAST-005). |
| S139 | `test/clusterless/workflows/safety_test.go:285` — should handle different percentiles with safety factor | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by percentile×safety math table (VAL-FAST-005). |
| S140 | `test/clusterless/workflows/safety_test.go:318` — should handle missing metrics gracefully | unit `internal/recommendation`,`internal/metrics` | replaced | M3 | Fixture-only; replaced by missing-series skip decision (VAL-FAST-007). |
| S141 | `test/clusterless/workflows/safety_test.go:338` — should handle zero resource usage | unit `internal/recommendation` | replaced | M3 | Fixture-only; replaced by zero-usage decision assertion (VAL-FAST-005). |
| S142 | `test/clusterless/workflows/webhook_test.go:27` — should mutate pod based on recommendation annotations | unit `internal/webhook` + `test/e2e` smoke (`webhook`) | replaced | M2/M3 | Uses the independent `test/clusterless/simulators/webhook.go` simulator, not `internal/webhook/mutator.go`; simulator output is not admission proof. Replaced by real mutator patch unit + live admission (VAL-FAST-006, VAL-SMOKE-003). |
| S143 | `test/clusterless/workflows/webhook_test.go:85` — should not mutate pod when webhook is disabled | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by real no-patch assertion for ineligible pod (VAL-FAST-006). |
| S144 | `test/clusterless/workflows/webhook_test.go:104` — should handle partial annotations | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by real mutator partial-annotation handling (VAL-FAST-006). |
| S145 | `test/clusterless/workflows/webhook_test.go:132` — should handle multiple containers | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by per-container patch paths/quantities on the real mutator (VAL-FAST-006). |
| S146 | `test/clusterless/workflows/webhook_test.go:188` — should apply patches correctly to pod | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by decoded real admission patch assertion (VAL-FAST-006). |
| S147 | `test/clusterless/workflows/webhook_test.go:223` — should extract resources from patches | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by real patch-quantity extraction assertion (VAL-FAST-006). |
| S148 | `test/clusterless/workflows/webhook_test.go:266` — should handle invalid resource values | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by real safe-handling of invalid annotation values (VAL-FAST-006). |
| S149 | `test/clusterless/workflows/webhook_test.go:289` — should handle missing containers | unit `internal/webhook` | replaced | M3 | Simulator-only; replaced by real no-change for unmatched container (VAL-FAST-006). |
| S150 | `test/clusterless/workflows/webhook_test.go:310` — should handle dry run requests | unit `internal/webhook` | replaced | M3 | Simulated `dryRun` is not real admission side-effect semantics. Replaced by real mutator handling (VAL-FAST-006). |
| S151 | `test/e2e/contract/error_handling_unit_test.go:11` — should create actionable error messages without internal details | — (deleted) | removed | — | Exercises the legacy `contract/error_handling.go` reporter/sanitizer (harness), not controller errors; retired with the legacy contract suite. Real redaction proven at VAL-HARNESS-006 (M2). |
| S152 | `test/e2e/contract/error_handling_unit_test.go:57` — should generate clear pass/fail status reports | — (deleted) | removed | — | Reporter self-test; replaced by JUnit output in the new framework. |
| S153 | `test/e2e/contract/error_handling_unit_test.go:106` — should sanitize error messages | — (deleted) | removed | — | Reporter sanitiser self-test; real diagnostic redaction proven at VAL-HARNESS-006. |
| S154 | `test/e2e/contract/error_handling_unit_test.go:140` — should provide actionable suggestions | — (deleted) | removed | — | Reporter self-test; harness-level strings. |
| S155 | `test/e2e/contract/error_handling_unit_test.go:165` — should create comprehensive test suite summaries | — (deleted) | removed | — | Reporter summary self-test; replaced by JUnit/timing artifacts in the new framework. |

## Non-scenario entry points (not counted)

These are suite/wrapper functions, deliberately excluded from the 155 count:

- `test/e2e/e2e_suite_test.go:23` `TestE2E` — Kind creation, metrics-server install, CRD/controller deploy, kube-context mutation, cleanup (see `legacy-hazards.md` H1–H9).
- `test/e2e/contract/suite_test.go:38` `TestContractE2E` — separate Kind/image/install suite (C1–C5).
- `test/clusterless/workflows/suite_test.go:11` `TestClusterlessWorkflows` — Ginkgo fake-client suite bootstrap.
- `test/e2e/contract/error_handling_unit_test.go:10` `TestContractErrorHandling` and `:164` `TestContractTestSummary` — Go wrappers around the five subtests S151–S155.

## Duplicate / invalid / obsolete decisions (summary)

- **Duplicates:** contract Ginkgo S025–S029 duplicate Go subtests S151–S155 (same reporter assertions) — all removed. S033 duplicates the S031/S032 CR-persistence check — collapsed into one smoke scenario.
- **Simulator-only (not admission proof):** S142–S150 call `test/clusterless/simulators/webhook.go`, not the production mutator — all replaced by real `internal/webhook` + live admission.
- **Fixture-only (not end-to-end proof):** S118–S141 inspect fake-client/injected-metrics fixture state — all replaced by production-path unit/envtest/conformance tests.
- **Pending/Skip (never assert):** S063 (`Pending`), S064 & S066 (`Skip`) — removed; observability proven at VAL-CONF-013.
- **Upstream-library-only:** S084–S087 test `k8s.io/apimachinery` quantity parsing — removed.
- **Harness self-tests / tautological:** S001–S024, S046–S050, S067–S074, S088–S090 — removed.
- **Out of approved scope:** S106–S110 webhook performance/latency/burst load — removed (performance is not an approved behaviour group).
- **Explicit TODO/"For now" substituting setup for behaviour:** S043, S118, S122, S130, S133 — all replaced with real entry-point assertions.

## Verification

Re-extracted from source on 2026-10-04 (repo at feature branch off `main` `bee5567`):

```
# per-group It counts
grep -rn "It(" test/e2e --include="*.go" | grep -v "/contract/" | wc -l   # 86
grep -rn "It(" test/e2e/contract --include="*.go" | wc -l                 # 31
grep -rn "It(" test/clusterless/workflows --include="*.go" | wc -l        # 33
grep -c "t.Run(" test/e2e/contract/error_handling_unit_test.go            # 5
# total scenario rows in this table
rg -c '^\| S[0-9]{3} \|' docs/testing/inventory.md                        # 155
```

All 86 + 31 + 33 line/title pairs and the five Go subtest lines (11, 57, 106, 140, 165)
matched this table exactly; `S063` is `Pending` and `S064`/`S066` are `Skip(...)` in
`test/e2e/observability_test.go`.

**Migration note (M3):** the counts above record the pre-migration state and are kept for
provenance. The first migration removal landed with feature `fast-validation-selection-and-modes`:
the misplaced unit-like file `test/e2e/invalid_config_unit_test.go` (S051–S056, pure
`ValidateCreate` assertions wrapped in the legacy Kind `BeforeSuite`) was deleted after
equivalent-or-stronger assertions landed in `api/v1alpha1`
(`TestOptimizationPolicy_ValidateCreate_FieldErrors` and the envtest
`optimizationpolicy_envtest_test.go`), so the main-E2E `It(` count at that revision is
80, not 86. Every remaining legacy file listed in this table still exists until its
disposition is executed by the owning feature.
