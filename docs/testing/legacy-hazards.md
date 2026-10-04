# Legacy test hazards and CI-history diagnosis (M1)

This report audits the two legacy cluster-based E2E suites and the historical GitHub Actions
behavior that motivate the refactor. It fulfills validation assertion VAL-DESIGN-004. Every
finding cites a source `path:line`. These suites were **not executed unchanged** (see
`baseline.md`); the hazards below are exactly why.

## 1. Legacy main E2E suite — `test/e2e` (`e2e_suite_test.go`)

The suite's `BeforeSuite` (`e2e_suite_test.go:28`) performs cluster/image/install work with
several hazardous side effects on the developer's machine.

| # | Hazard | Evidence | Impact |
| --- | --- | --- | --- |
| H1 | **Mutates the developer's current kube-context** | `createKindCluster()` runs `kubectl config use-context kind-optipod-e2e-test` (`e2e_suite_test.go:102`) | Silently repoints the developer's default kubectl at the test cluster; violates the mission's context-isolation boundary |
| H2 | **Fixed cluster name** | `clusterName = "optipod-e2e-test"` (`e2e_suite_test.go:18`); reused across runs (`e2e_suite_test.go:76`) | No collision resistance; two runs or a stale cluster interfere; ownership is ambiguous |
| H3 | **Unpinned `@latest` tool installs during the test** | `go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest` (`e2e_suite_test.go:222`) and `kustomize/v5@latest` (`e2e_suite_test.go:228`) | Non-reproducible tool versions; test behavior drifts with upstream releases |
| H4 | **Unpinned metrics-server installer** | `kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml` (`e2e_suite_test.go:111`) | Floating `latest` manifest; non-reproducible and network-dependent |
| H5 | **Edits tracked `config/manager`** | `cd config/manager && kustomize edit set image controller=optipod-controller:e2e` (`e2e_suite_test.go:249`) | Rewrites a tracked `kustomization.yaml`; dirties the working tree and can leak into commits |
| H6 | **Regenerates tracked CRDs** | `controller-gen ... output:crd:artifacts:config=config/crd/bases` (`e2e_suite_test.go:240`) | Overwrites tracked generated CRD files as a test side effect |
| H7 | **Mutates a shared namespace** | `labelDefaultNamespace()` labels `default` with `environment=development` (`e2e_suite_test.go:159`) | Side effect on a shared namespace unrelated to the test's own fixtures |
| H8 | **Ignored errors** | `_, _ = utils.Run(cmd) // Ignore cleanup errors` at `e2e_suite_test.go:315,319,326`; also `test/e2e/helpers/test_helpers.go:178,227` and `webhook_lifecycle_test.go:77` (`_ = portForwardCmd.Run()`) | Cleanup/port-forward failures are swallowed, hiding leaked resources and flaky teardown |
| H9 | **Teardown skipped on opt-out** | `AfterSuite` deletes the cluster only when `KEEP_CLUSTER != "true"` (`e2e_suite_test.go:71`) | Fine by itself, but combined with the fixed name (H2) a retained cluster collides with later runs |

### Fixed/unexplained sleeps (whole suite)

`time.Sleep` is used as a substitute for condition-based waits across the suite, including
multi-minute sleeps:

- `policy_modes_test.go:67,124,183` — `60s`; `:238,:248` — `30s`
- `memory_safety_integration_test.go:66,157,245,372,441` — `2m`; `:328` — `3m`
- `webhook_integration_test.go:138,289,515,848,959,1018` — `2m`; `:715` — `3m`
- `webhook_performance_test.go:666` — `30s`; `:31` — `10s`
- `workload_type_selector_test.go:118` — `5s`; several `5s`/`10s` BeforeEach sleeps

These inflate wall time and still race; the new harness replaces them with bounded
condition/watch waits.

## 2. Legacy contract E2E suite — `test/e2e/contract`

`BeforeSuite` (`contract/suite_test.go:65`) creates its own cluster, builds/loads an image
and installs the controller.

| # | Hazard | Evidence | Impact |
| --- | --- | --- | --- |
| C1 | **Edits tracked `config/manager`** | `installCRDsAndController()` runs `kustomize edit set image controller=<tag>` with `cmd.Dir = projectDir + "/config/manager"` (`contract/suite_test.go:226-227`) | Same tracked-file mutation as H5 |
| C2 | **Fixed cluster name and image tag** | `ClusterName: "optipod-contract-e2e"`, `ImageTag: "optipod-controller:contract-e2e"` (`contract/consistency_config.go:72-73`) | No collision resistance; `CreateConsistentCluster` first `kind delete`s any cluster of that name (`contract/execution_environment.go:203`) — destructive to a pre-existing same-named cluster |
| C3 | **Unpinned binaries from PATH** | `KindBinary: "kind"`, `KubectlBinary: "kubectl"` (`contract/consistency_config.go:77-78`); kustomize fetched via `make kustomize` (`contract/execution_environment.go:151`) | Whatever versions are on PATH are used; not pinned/verified |
| C4 | **Ignored error** | `_ = deleteCmd.Run() // Ignore errors` (`contract/execution_environment.go:205`) | Pre-create delete failure is swallowed |
| C5 | **Setup-only / readiness-only assertions** | See table below | A green run proves installation reachable, not optimization behavior |

### Setup-only and readiness-only assertions

These "contract" specs assert only that the controller came up, not that OptiPod does
anything to a workload:

- `contract/install_test.go:54` — *"should maintain stable operation without restarts"*:
  checks only pod `Ready` and container `RestartCount == 0`
  (`install_test.go:83-139`). The comment at `install_test.go:136-137` explicitly removed
  event checking.
- `contract/status_ready_test.go:56` — *"should converge to Ready=True condition within
  timeout period"*; the requirement comment states the suite *"SHALL only verify a
  Ready=True condition appears"* (`status_ready_test.go:55`). A `Ready=True` condition is
  not evidence that metrics were processed or a workload was mutated.

Contract specs also use fixed sleeps, e.g. `contract/contract_scope_validation_test.go:55,70`
(`2s`).

## 3. Clusterless suite is a fixture/simulator layer, not deployed proof

`test/clusterless/workflows` (33 specs, run green in the baseline) uses **fake fixtures and an
independent webhook simulator**, so it must not be counted as installed-controller or live
admission coverage:

- `test/clusterless/simulators/webhook.go:19-20` — `WebhookSimulator` *"simulates webhook
  mutations without HTTP server"*.
- `test/clusterless/workflows/webhook_test.go:17-18` constructs
  `simulators.NewWebhookSimulator(h)` and calls `webhookSim.MutatePod(...)` instead of
  submitting a pod through real API-server admission.

The suite's `BeforeSuite` is harmless (only sets Ginkgo timeouts,
`workflows/suite_test.go:16-25`); the hazard is interpretive — treating simulator output as
admission proof.

## 4. CI trigger, history and the zero-job workflow failure

### 4.1 Trigger and gate limitations

| Workflow | File | Triggers | Limitation |
| --- | --- | --- | --- |
| Contract E2E Tests | `.github/workflows/e2e.yml` | `workflow_dispatch` + `schedule` (nightly `0 2 * * *`) only | **Never runs on push or PR**, so cluster E2E never gates code changes; and *"failures do NOT block releases"* (`e2e.yml` report step) |
| Tests | `.github/workflows/test.yml` | `push`, `pull_request` | Runs `go mod tidy && make test`; `make test` regenerates manifests/CRDs, runs `fmt`, and writes `cover.out` — a mutating gate that can fail on generation/format drift and provides no E2E coverage |
| CI | `.github/workflows/ci.yml` | push to `main`/`feature/**`/`fix/**` + PR | Builds binary and Docker image (no push); no test execution |

In CI, `e2e.yml` also installs **Kind v0.20.0** and **kubectl `stable`** (floating) and the
**latest** metrics-server (`e2e.yml` install steps) — versions that differ from the pinned
local toolchain, compounding the reproducibility gap.

### 4.2 Run 36252723861 — zero-job failure

Read-only `gh run view 36252723861 -R Sagart-cactus/optipod` metadata:

| Field | Value |
| --- | --- |
| Workflow | Contract E2E Tests (`e2e.yml`) |
| Event | `push` |
| Head branch | `claude/triseek-optipod-deps-release-e1tcyz` |
| Head SHA | `c6bcf545dd5e1a4e9efdcf8a211e1040529ed236` |
| Created / Updated | `2026-09-26T15:39:57Z` / `2026-09-26T15:39:57Z` (0s elapsed) |
| Status / Conclusion | `completed` / **`failure`** |
| Jobs | **`[]` (zero jobs scheduled)** |

The run failed instantly with **no jobs**, the signature of a workflow that failed to parse
rather than a test failure.

### 4.3 Locally reproduced root cause — empty root `env:` mapping

`e2e.yml` declares a root `env:` key with no mapping value (`.github/workflows/e2e.yml:19`):

```yaml
permissions:
  contents: read
  actions: read

env:

jobs:
```

Pinned **actionlint v1.7.7** flags this:

```
.github/workflows/e2e.yml:19:5: expecting a single ${{...}} expression or mapping value
for "env" section, but found plain text node [syntax-check]
   |
19 | env:
   |     ^
```

Exit code `1`. The workflow file at the failing revision `c6bcf545` is **byte-identical** to
the current `HEAD` version (verified by fetching the blob at that SHA and `diff` — identical),
and actionlint reports the same `19:5` error against it.

> **Attribution boundary.** GitHub's run metadata exposes **no verbatim parser error**
> (`jobs: []`, zero duration). The empty-`env:` diagnosis above is **locally reproduced with
> pinned actionlint**, consistent with a zero-job parse failure. It is *not* quoted as
> GitHub's own error message, which is not available.

### 4.4 Historical narrow-contract successes (for contrast)

The 19 runs preceding the failure were all `schedule` (nightly) events on SHA `ebee9739`,
all `success`, with durations of **251–272s (~4.2–4.5 minutes)**:

| Run (sample) | Event | Conclusion | Duration |
| --- | --- | --- | --- |
| 25244471959 (2026-05-02) | schedule | success | 260s |
| 24817379391 (2026-04-23) | schedule | success | 272s |
| 24381397239 (2026-04-14) | schedule | success | 254s |

These successes exercised only the **narrow** install/CR-acceptance/`Ready=True` contract
scope (Section 2) and ran **only on the nightly schedule, never on a PR or push**. They are
not evidence of optimization/admission behavior and did not gate any code change.

## 5. Replacement decisions (summary)

| Hazard class | Replacement in the new architecture |
| --- | --- |
| Context mutation (H1), fixed names (H2, C2) | Collision-resistant owned cluster + private kubeconfig; never touch default context |
| `@latest`/floating installers (H3, H4, C3) and CI Kind/kubectl drift | Pinned CLIs, pinned node image digest, pinned fixture images; no `latest` in the test path |
| Tracked `config/manager`/CRD edits (H5, H6, C1) | Render manifests to temporary files; no `kustomize edit`/`controller-gen` against the source tree |
| Ignored errors (H8, C4) | Strict error propagation; only expected not-found tolerated on repeat cleanup |
| Fixed sleeps (Section 1 list, C5 sleeps) | Bounded condition/watch waits with deadlines |
| Setup-only/readiness-only assertions (C5) | Assert observed recommendations/applied resources/admission outcomes, not mere readiness |
| Simulator-as-proof (Section 3) | Keep clusterless only for production-path/fixture checks; real admission proven on the Kind suite |
| CI never gates code (Section 4.1) | Smoke on relevant PR/push; conformance nightly + manual; valid workflow syntax (actionlint) |
</content>
