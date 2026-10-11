# Image URL to use all building/pushing image targets
IMG ?= controller:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Quality

.PHONY: ci-checks-local
ci-checks-local: ## Run all CI checks locally (mirrors GitHub Actions exactly)
	@echo "🚀 Running local CI checks that mirror GitHub Actions..."
	@./scripts/ci-checks-local.sh

.PHONY: setup-pre-commit
setup-pre-commit: ## Set up pre-commit hooks
	@echo "Setting up pre-commit hooks..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit install; \
	elif [ -f "$$HOME/Library/Python/3.9/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.9/bin/pre-commit install; \
	elif [ -f "$$HOME/Library/Python/3.11/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.11/bin/pre-commit install; \
	elif [ -f "$$HOME/.local/bin/pre-commit" ]; then \
		$$HOME/.local/bin/pre-commit install; \
	else \
		echo "pre-commit not found. Installing..."; \
		pip3 install --user pre-commit; \
		if [ -f "$$HOME/Library/Python/3.9/bin/pre-commit" ]; then \
			$$HOME/Library/Python/3.9/bin/pre-commit install; \
		elif [ -f "$$HOME/Library/Python/3.11/bin/pre-commit" ]; then \
			$$HOME/Library/Python/3.11/bin/pre-commit install; \
		elif [ -f "$$HOME/.local/bin/pre-commit" ]; then \
			$$HOME/.local/bin/pre-commit install; \
		fi; \
	fi

.PHONY: pre-commit-run
pre-commit-run: ## Run pre-commit hooks on all files
	@echo "Running pre-commit hooks on all files..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit run --all-files; \
	elif [ -f "$$HOME/.local/bin/pre-commit" ]; then \
		$$HOME/.local/bin/pre-commit run --all-files; \
	elif [ -f "$$HOME/Library/Python/3.9/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.9/bin/pre-commit run --all-files; \
	elif [ -f "$$HOME/Library/Python/3.11/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.11/bin/pre-commit run --all-files; \
	else \
		echo "pre-commit not found. Please run 'make setup-pre-commit' first."; \
		exit 1; \
	fi

.PHONY: pre-commit-update
pre-commit-update: ## Update pre-commit hook versions
	@echo "Updating pre-commit hooks..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit autoupdate; \
	elif [ -f "$$HOME/.local/bin/pre-commit" ]; then \
		$$HOME/.local/bin/pre-commit autoupdate; \
	elif [ -f "$$HOME/Library/Python/3.9/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.9/bin/pre-commit autoupdate; \
	elif [ -f "$$HOME/Library/Python/3.11/bin/pre-commit" ]; then \
		$$HOME/Library/Python/3.11/bin/pre-commit autoupdate; \
	else \
		echo "pre-commit not found. Please run 'make setup-pre-commit' first."; \
		exit 1; \
	fi

.PHONY: format
format: ## Format code using gofmt and goimports
	@echo "Formatting Go code..."
	@gofmt -w .
	@goimports -w .

.PHONY: lint-all
lint-all: lint ## Run all linting checks (alias for lint)

.PHONY: lint-local
lint-local: ## Run all local linting checks (format, yaml, markdown, go)
	@echo "🔍 Running comprehensive local linting checks..."
	@echo "📝 Checking Go formatting..."
	@if [ "$$(gofmt -l . | wc -l)" -gt 0 ]; then \
		echo "❌ Go formatting issues found:"; \
		gofmt -l .; \
		echo "Run 'make format' to fix"; \
		exit 1; \
	fi
	@echo "📦 Checking Go imports..."
	@if [ "$$(goimports -l . | wc -l)" -gt 0 ]; then \
		echo "❌ Go import issues found:"; \
		goimports -l .; \
		echo "Run 'make format' to fix"; \
		exit 1; \
	fi
	@echo "🔧 Running golangci-lint..."
	@$(MAKE) lint
	@echo "📄 Checking YAML files..."
	@if command -v yamllint >/dev/null 2>&1; then \
		yamllint -c .yamllint.yml .; \
	else \
		echo "⚠️  yamllint not found. Install with: pip3 install yamllint"; \
	fi
	@echo "📖 Checking Markdown files..."
	@if command -v markdownlint >/dev/null 2>&1; then \
		markdownlint -c .markdownlint.yml *.md docs/ || true; \
	else \
		echo "⚠️  markdownlint not found. Install with: npm install -g markdownlint-cli"; \
	fi
	@echo "✅ All local checks completed!"

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."
	@# controller-gen v0.22 emits the meta/v1 import without its `v1` alias, which the
	@# goimports check in CI rejects. Normalise here so `make generate` stays clean.
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w api/v1alpha1/zz_generated.deepcopy.go; \
	else \
		echo "goimports not found; run 'make format' before committing generated code"; \
	fi

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name optipod-builder
	$(CONTAINER_TOOL) buildx use optipod-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm optipod-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$out" ]; then echo "$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$out" ]; then echo "$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Webhook Installation

.PHONY: kind-install
kind-install: ## Install OptipPod with webhook support for kind clusters (manual certificates)
	@echo "Installing OptipPod for kind cluster with manual certificates..."
	CERT_MANAGER=manual ./config/webhook/install.sh

.PHONY: webhook-certmanager
webhook-certmanager: ## Install OptipPod with webhook support using cert-manager
	@echo "Installing OptipPod with cert-manager certificates..."
	CERT_MANAGER=cert-manager ./config/webhook/install.sh

.PHONY: webhook-manual
webhook-manual: ## Install OptipPod with webhook support using manual certificates
	@echo "Installing OptipPod with manual certificates..."
	CERT_MANAGER=manual ./config/webhook/install.sh

##@ Fast Gate

.PHONY: test-fast
test-fast: setup-envtest ## Run the non-mutating fast gate: short unit, envtest and clusterless tests (never regenerates or formats tracked files)
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -short -count=1 ./api/... ./internal/... ./cmd/... ./test/clusterless/workflows/...

##@ Cluster-Free E2E Tests

.PHONY: test-clusterless
test-clusterless: ## Run cluster-free E2E tests
	@echo "🧪 Running cluster-free E2E tests..."
	go test -v ./test/clusterless/workflows/... \
		-coverprofile=coverage-clusterless.out \
		-timeout=10m

.PHONY: test-clusterless-fast
test-clusterless-fast: ## Run cluster-free tests in parallel (fast mode)
	@echo "⚡ Running cluster-free E2E tests (fast mode)..."
	go test -v ./test/clusterless/workflows/... \
		-coverprofile=coverage-clusterless.out \
		-timeout=5m

.PHONY: test-clusterless-verbose
test-clusterless-verbose: ## Run with verbose output
	go test -v ./test/clusterless/workflows/... \
		-ginkgo.v

.PHONY: test-clusterless-focus
test-clusterless-focus: ## Run focused cluster-free tests
	@echo "🎯 Running focused cluster-free E2E tests..."
	go test -v ./test/clusterless/workflows/... \
		-ginkgo.focus="$(FOCUS)" \
		-ginkgo.v \
		-timeout=10m

.PHONY: test-all
test-all: test test-clusterless ## Run all tests (unit + cluster-free E2E)
	@echo "✅ All tests completed"

##@ Kind E2E (hermetic suite)

# The Kind suite lives ONLY in test/e2e/suite: these targets never touch the
# default kubeconfig, tracked config or existing clusters. `make
# test-e2e-smoke` is the one-command entry point from a clean checkout: it
# preflights, creates its own uniquely named Kind cluster, builds and loads the
# checkout controller once, installs it together with the webhook and the
# deterministic in-cluster metrics fixture, runs the smoke-labelled specs and
# removes only its own cluster. Per-spec JUnit (junit.xml), measured phase/spec timings
# (timings.json) and, on failure, a redacted pre-teardown diagnostics bundle
# are written under test/e2e/artifacts/run-<id>/ (git-ignored). KEEP_CLUSTER=1
# retains the owned cluster and prints private-kubeconfig inspection, explicit
# focused-reuse (make test-e2e-reuse) and disposal commands; a reused cluster
# keeps the retained install and is never deleted by a run.
E2E_SUITE ?= ./test/e2e/suite/
E2E_SUITE_TIMEOUT ?= 45m
E2E_LABELS ?= smoke

.PHONY: test-e2e-smoke
test-e2e-smoke: ## Run the hermetic installed-controller smoke suite end to end (one command; owns and removes its Kind cluster)
	@echo "Running the hermetic installed-controller smoke suite (label filter: smoke)..."
	go test $(E2E_SUITE) -count=1 -timeout=$(E2E_SUITE_TIMEOUT) -v -ginkgo.v -ginkgo.label-filter="smoke"

.PHONY: test-e2e-labels
test-e2e-labels: ## Run the Kind suite with a Ginkgo label filter (E2E_LABELS="smoke && webhook")
	@echo "Running the Kind suite (package: $(E2E_SUITE)) with label filter: $(E2E_LABELS)"
	go test $(E2E_SUITE) -count=1 -timeout=$(E2E_SUITE_TIMEOUT) -v -ginkgo.v -ginkgo.label-filter="$(E2E_LABELS)"

# Full conformance deliberately INCLUDES the smoke coverage: the label filter
# selects every smoke AND every conformance spec over one owned installed
# cluster, so the conformance command cannot pass by bypassing smoke
# (VAL-CROSS-003). Nothing skipped here is silently dropped - diagnostics,
# canary and deliberate-failure harness self-checks keep their own opt-in
# label invocations.
E2E_CONFORMANCE_TIMEOUT ?= 60m

.PHONY: test-e2e-conformance
test-e2e-conformance: ## Run the hermetic installed-controller conformance suite end to end: smoke plus all conformance labels (one command; owns and removes its Kind cluster)
	@echo "Running the hermetic installed-controller conformance suite (label filter: smoke || conformance)..."
	go test $(E2E_SUITE) -count=1 -timeout=$(E2E_CONFORMANCE_TIMEOUT) -v -ginkgo.v -ginkgo.label-filter="smoke || conformance"

.PHONY: test-e2e-reuse
test-e2e-reuse: ## Focused rerun against a retained harness cluster (OPTIPOD_E2E_REUSE_CLUSTER/OPTIPOD_E2E_REUSE_KUBECONFIG from a KEEP_CLUSTER=1 run; the retained install is kept, not rebuilt)
	@if [ -z "$$OPTIPOD_E2E_REUSE_CLUSTER" ] || [ -z "$$OPTIPOD_E2E_REUSE_KUBECONFIG" ]; then \
		echo "test-e2e-reuse requires OPTIPOD_E2E_REUSE_CLUSTER and OPTIPOD_E2E_REUSE_KUBECONFIG (printed by a KEEP_CLUSTER=1 run)"; \
		exit 2; \
	fi
	@echo "Reusing retained harness cluster $$OPTIPOD_E2E_REUSE_CLUSTER for a focused rerun (label filter: $(E2E_LABELS)); the retained install is kept, not rebuilt"
	go test $(E2E_SUITE) -count=1 -timeout=$(E2E_SUITE_TIMEOUT) -v -ginkgo.v -ginkgo.label-filter="$(E2E_LABELS)"

.PHONY: test-e2e-deliberate-failure
test-e2e-deliberate-failure: ## Deliberately fail one suite spec to verify pre-teardown redacted diagnostics (expected nonzero exit)
	@echo "Deliberately failing one new-suite spec to exercise redacted failure diagnostics (nonzero exit is expected)"
	OPTIPOD_E2E_DELIBERATE_FAILURE=1 go test $(E2E_SUITE) -count=1 -timeout=$(E2E_SUITE_TIMEOUT) -v -ginkgo.v -ginkgo.label-filter="deliberate-failure"

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.22.0

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= release-0.25

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= 1.37

GOLANGCI_LINT_VERSION ?= v2.13.2
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)" ] || { \
set -e; \
TMP_DIR=$$(mktemp -d); \
echo "Downloading $(2)@$(3)" ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
go install $(2)@$(3) ;\
cp "$$(go env GOPATH)/bin/$$(basename $(1))" $(1) ;\
rm -rf $$TMP_DIR ;\
}
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
