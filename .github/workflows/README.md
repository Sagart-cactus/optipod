# GitHub Actions Workflows

This directory contains the CI workflows for the OptiPod project. Local
equivalents for every test tier are discoverable via `make help`; see
[docs/testing/README.md](../../docs/testing/README.md) for the test
architecture.

## Test tiers

### test.yml — Tests (fast gate)

Runs on every push and pull request. Executes `make test-fast` (short unit,
envtest and clusterless tests) and `go vet`. The gate never regenerates,
formats or otherwise mutates tracked files.

### e2e.yml — Kind E2E (smoke and conformance)

Runs the hermetic installed-controller Kind suite (`test/e2e/suite`). Each
hosted run owns one uniquely named Kind cluster with a private kubeconfig,
builds/loads/installs the checkout controller once and removes only its own
cluster.

- **Smoke job**: every pull request and push touching controller (`internal/`,
  `cmd/`), API (`api/`), chart (`charts/`), manifest (`config/`), test
  (`test/`), Make (`Makefile`), build (`Dockerfile`, `go.mod`/`go.sum`) or
  workflow inputs. Runs `make test-e2e-smoke`; a test failure fails the job.
- **Conformance job**: nightly schedule (`0 2 * * *`) and manual dispatch.
  Runs `make test-e2e-conformance`, which includes all smoke coverage.

Both jobs install pinned Kind/kubectl/kustomize binaries (version- and
SHA256-verified), cache Go modules/build outputs via `actions/setup-go`, and
unconditionally upload the run's JUnit, phase-timing and redacted failure
diagnostics from `test/e2e/artifacts/` while preserving the test's exit code.

### lint.yml — Lint

Runs pinned `golangci-lint` on every push and pull request.

### ci.yml — CI

Builds the manager binary and the production Docker image (no push) on pushes
to `main`/`feature/**`/`fix/**` and on pull requests.

### pre-commit.yml — Pre-commit Checks

Runs the pre-commit hooks plus formatting, lint, secrets and YAML checks on
pushes and pull requests to `main`/`develop`.

### release.yml — Release

Tag (`v*.*.*`) and manual-dispatch only. Runs the fast gate and clusterless
tests, then builds, scans, signs and publishes images, the Helm chart and the
GitHub release. Never triggered by the test workflows.

### static.yml — Deploy Website to Pages

Deploys the `website/` tree to GitHub Pages on pushes to `main` that touch
`website/`, and on manual dispatch.
