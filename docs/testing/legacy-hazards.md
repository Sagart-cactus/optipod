# Legacy test hazards and CI-history diagnosis (M1)

This report audits the two legacy cluster-based E2E suites and the historical GitHub Actions
behavior that motivate the refactor. It fulfills validation assertion VAL-DESIGN-004. Every
finding cites a source `path:line`. These suites were **not executed unchanged** (see
`baseline.md`); the hazards below are exactly why.

> **M3 retirement status (feature `kind-observability-compatibility-and-legacy-retirement`).**
> The memory-safety artifacts are deleted (`test/e2e/memory_safety_integration_test.go`,
> `test/e2e/run_memory_safety_tests.sh`, `hack/test-memory-decrease-safety.sh` and their
> now-unreferenced `hack/test-*memory*`/`test-*safety-factor*` fixtures), the decrease-guard
> contexts in `test/e2e/error_handling_test.go` and `test/clusterless/workflows/safety_test.go`
> are retired in place, the superseded runner scripts (`hack/test-*.sh` test wrappers,
> `test/e2e/run_policy_tests.sh`, `run_resource_bounds_tests.sh`, `simple_policy_test.sh`) are
> deleted, and the orphaned `website/src/content/doc/doc/**` tree claiming the obsolete
> memory-protection API is removed (see `inventory.md`, retirement-execution note). The tracked
> `config/manager/kustomization.yaml` image override that an H5-style tracked-file edit leaked
> (`optipod-controller:contract-e2e`) is reset to the scaffold default `controller:latest`; the
> new harness renders into temporary copies and never edits tracked config.
>
> **M4 retirement status (feature `safe-make-and-actions-topology`).** The remaining legacy
> suites are now deleted: the 14-file `test/e2e` root suite (Section 1, hazards H1–H10), the
> `test/e2e/contract/` suite (Section 2, hazards C1–C6) and `test/e2e/helpers/` (the shared
> `--all` cleanup helper). Their Make targets (`setup-e2e`, `test-e2e`, `test-e2e-focus`,
> `e2e-cluster`, `test-e2e-contract`, `cleanup-e2e`, `cleanup-contract-e2e`) are removed, and
> `hack/verify-release-separation.sh` (which asserted the retired CI topology) is deleted with
> them. `.github/workflows/e2e.yml` is rewritten to the smoke-on-PR/push plus
> nightly/manual-conformance topology (the empty root `env:` fault of Section 4.3 is gone with
> the old file), `test.yml` now runs the non-mutating fast gate instead of `make test`, and
> `release.yml`'s test job does the same. Every retired scenario's per-row disposition and
> reason is in `inventory.md`.

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
| H10 | **Broad deletion of *all* Deployments in `default`** | `CleanupHelper.CleanupAllWorkloads()` runs `kubectl delete deployment --all -n default --ignore-not-found=true` (`test/e2e/helpers/test_helpers.go:206`); `CleanupAllPolicies()` similarly deletes `optimizationpolicy --all -n optipod-system` (`:198`) | Because the suite runs against the developer's **current context** (H1), this deletes **every** Deployment in that cluster's `default` namespace — not just the suite's own fixtures — destroying unrelated user workloads. The suite-level `cleanupTestResources()` policy cleanup at `e2e_suite_test.go:313` and `CleanupAllPolicies()` (`:198`) both delete `optimizationpolicy --all -n optipod-system` — scoped to OptiPod's **own** namespace (`optipodNamespace = "optipod-system"`, `e2e_suite_test.go:19`), **not** `default` — so they do not reach unrelated user Deployments, but on the developer's current context they still remove every OptimizationPolicy in `optipod-system` |

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
| C6 | **Install subprocesses target the ambient default kubeconfig, not the private config** | `CreateConsistentCluster` runs `kind create cluster --name <fixed>` with **no `--kubeconfig`** (`contract/execution_environment.go:212-224`), so Kind writes the cluster into the developer's **default kubeconfig and switches current-context** to it. `installCRDsAndController()` then runs `kubectl apply`/`kubectl wait`/`kustomize build`/`kubectl apply -f -` subprocesses with **no `--kubeconfig`, no `--context`, and no `KUBECONFIG` in the environment** (`contract/suite_test.go:209,216,240`), so CRD and controller installation land on whatever current-context the default kubeconfig happens to hold | Split-brain, context-dependent targeting: the typed controller-runtime client is built from a private `kind get kubeconfig` string (`suite_test.go:163-169`), but the **mutating install subprocesses rely on ambient default-kubeconfig current-context**. If that context pointed at another (e.g. production) cluster at install time, the CRD and controller would be applied there. Cluster verification even hard-codes `--context kind-<name>` (`execution_environment.go:235-243`), reading/writing the shared default kubeconfig |

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

In CI, `e2e.yml` installs **Kind v0.20.0** (`e2e.yml:38`) and **kubectl `stable`** (floating)
(`e2e.yml:46`) — versions that differ from the pinned local toolchain, compounding the
reproducibility gap. `e2e.yml` then runs the **contract** suite via `make test-e2e-contract`
(`e2e.yml:70`), which does **not** install metrics-server. The floating **latest**
metrics-server installer (H4) lives in the **unrun main E2E suite**'s `BeforeSuite` —
`installMetricsServer()` (`e2e_suite_test.go:37`) applying
`.../releases/latest/download/components.yaml` (`e2e_suite_test.go:111`) — **not** in `e2e.yml`.

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
| Install subprocesses on ambient default kubeconfig (C6) | Every Kind/kubectl/kustomize subprocess receives the harness's **private `--kubeconfig`/`KUBECONFIG`** (and the typed client its matching explicit `rest.Config`); `kind create` always writes the private kubeconfig, never the default; no reliance on current-context |
| Broad `--all -n default` deletion (H10) | Operate in per-spec **unique namespaces** on the owned cluster via the private config; delete by owned namespace/label scope only; never `kubectl delete deployment --all` or `optimizationpolicy --all` against a shared namespace or the developer's context |
| `@latest`/floating installers (H3, H4, C3) and CI Kind/kubectl drift | Pinned CLIs, pinned node image digest, pinned fixture images; no `latest` in the test path |
| Tracked `config/manager`/CRD edits (H5, H6, C1) | Render manifests to temporary files; no `kustomize edit`/`controller-gen` against the source tree |
| Ignored errors (H8, C4) | Strict error propagation; only expected not-found tolerated on repeat cleanup |
| Fixed sleeps (Section 1 list, C5 sleeps) | Bounded condition/watch waits with deadlines |
| Setup-only/readiness-only assertions (C5) | Assert observed recommendations/applied resources/admission outcomes, not mere readiness |
| Simulator-as-proof (Section 3) | Keep clusterless only for production-path/fixture checks; real admission proven on the Kind suite |
| CI never gates code (Section 4.1) | Smoke on relevant PR/push; conformance nightly + manual; valid workflow syntax (actionlint) |
</content>
