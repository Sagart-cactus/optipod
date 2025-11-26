# End-to-End Tests for OptiPod

This directory contains end-to-end (E2E) tests for the OptiPod Kubernetes operator.

## Prerequisites

- **Kind** (Kubernetes in Docker): Used to create a local test cluster
- **kubectl**: Kubernetes command-line tool
- **Docker**: Required by Kind to run Kubernetes nodes
- **Go 1.21+**: To run the test suite

## Installation

### Install Kind

```bash
# On macOS
brew install kind

# On Linux
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind
```

### Install kubectl

```bash
# On macOS
brew install kubectl

# On Linux
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

## Running E2E Tests

### Quick Start

Run all E2E tests with automatic cluster setup and teardown:

```bash
make test-e2e
```

This command will:
1. Create a Kind cluster named `optipod-test-e2e`
2. Build the OptiPod operator Docker image
3. Load the image into the Kind cluster
4. Install CertManager (if needed)
5. Deploy the operator
6. Run all E2E test scenarios
7. Clean up the cluster

### Manual Cluster Management

If you want to keep the cluster running between test runs:

```bash
# Create the cluster
make setup-test-e2e

# Run tests (without cleanup)
KIND=kind KIND_CLUSTER=optipod-test-e2e go test -tags=e2e ./test/e2e/ -v -ginkgo.v

# Clean up when done
make cleanup-test-e2e
```

### Running Specific Tests

To run a specific test scenario:

```bash
KIND=kind KIND_CLUSTER=optipod-test-e2e go test -tags=e2e ./test/e2e/ -v -ginkgo.focus="should create and validate OptimizationPolicy"
```

## Test Scenarios

The E2E test suite covers the following scenarios:

### 1. Controller Deployment
- Verifies the OptiPod controller pod is running
- Checks that the metrics endpoint is accessible
- Validates Prometheus metrics exposure

### 2. OptimizationPolicy Creation and Validation
- Creates OptimizationPolicy resources
- Validates policy configuration
- Checks policy status conditions
- Tests invalid policy configurations

### 3. Workload Discovery
- Deploys sample workloads (Deployments)
- Verifies workloads are discovered by policies
- Tests label selector matching
- Validates namespace filtering

### 4. Recommend Mode
- Creates policies in Recommend mode
- Verifies recommendations are generated
- Ensures workloads are NOT modified
- Validates recommendation format (CPU, memory, explanation)

### 5. Auto Mode
- Creates policies in Auto mode
- Verifies recommendations are applied to workloads
- Checks lastApplied timestamps
- Validates resource request updates

### 6. Resource Bounds
- Tests min/max CPU and memory bounds
- Verifies recommendations are clamped to bounds
- Validates bound enforcement across different scenarios

### 7. Disabled Mode
- Creates policies in Disabled mode
- Verifies workloads are not processed
- Ensures no recommendations are generated

### 8. Prometheus Metrics
- Validates OptiPod-specific metrics are exposed
- Checks controller reconciliation metrics
- Verifies metric values are updated

### 9. Error Handling
- Tests invalid policy configurations
- Verifies validation error messages
- Checks Kubernetes event creation

## Test Architecture

The E2E tests use:
- **Ginkgo**: BDD-style testing framework
- **Gomega**: Matcher library for assertions
- **Kind**: Local Kubernetes cluster
- **kubectl**: Kubernetes API interactions

## Debugging Failed Tests

If tests fail, the suite automatically collects:
- Controller manager pod logs
- Kubernetes events in the test namespace
- Pod descriptions
- Metrics endpoint output

These logs are printed to the test output for debugging.

### Manual Debugging

To inspect the cluster after a test failure:

```bash
# Keep the cluster running by skipping cleanup
KIND=kind KIND_CLUSTER=optipod-test-e2e go test -tags=e2e ./test/e2e/ -v -ginkgo.v

# In another terminal, inspect resources
kubectl --context kind-optipod-test-e2e get pods -n optipod-system
kubectl --context kind-optipod-test-e2e logs -n optipod-system <controller-pod-name>
kubectl --context kind-optipod-test-e2e get optimizationpolicies -A
kubectl --context kind-optipod-test-e2e describe optimizationpolicy <policy-name> -n optipod-system
```

## Environment Variables

- `KIND`: Path to the kind binary (default: `kind`)
- `KIND_CLUSTER`: Name of the Kind cluster (default: `optipod-test-e2e`)
- `CERT_MANAGER_INSTALL_SKIP`: Skip CertManager installation if already present (default: `false`)
- `IMG`: Docker image name for the operator (default: `example.com/optipod:v0.0.1`)

## Continuous Integration

The E2E tests are designed to run in CI environments. Example GitHub Actions workflow:

```yaml
name: E2E Tests
on: [push, pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Install Kind
        run: |
          curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
          chmod +x ./kind
          sudo mv ./kind /usr/local/bin/kind
      - name: Run E2E Tests
        run: make test-e2e
```

## Troubleshooting

### Kind cluster creation fails
```bash
# Check Docker is running
docker ps

# Check Kind version
kind version

# Try creating cluster manually
kind create cluster --name optipod-test-e2e
```

### Image loading fails
```bash
# Build image manually
make docker-build IMG=example.com/optipod:v0.0.1

# Load into Kind
kind load docker-image example.com/optipod:v0.0.1 --name optipod-test-e2e
```

### Controller pod not starting
```bash
# Check pod status
kubectl --context kind-optipod-test-e2e get pods -n optipod-system

# Check pod logs
kubectl --context kind-optipod-test-e2e logs -n optipod-system <pod-name>

# Check events
kubectl --context kind-optipod-test-e2e get events -n optipod-system
```

### Tests timeout
- Increase timeout values in test code
- Check cluster resources: `kubectl top nodes`
- Verify metrics-server is running (required for some tests)

## Contributing

When adding new E2E tests:
1. Follow the existing test structure using Ginkgo/Gomega
2. Use descriptive test names with `It("should ...")`
3. Clean up resources in `AfterEach` or at the end of tests
4. Add appropriate timeouts and polling intervals
5. Include helpful error messages in assertions
6. Update this README with new test scenarios
