# E2E Test Troubleshooting Guide

This guide provides detailed troubleshooting information for common issues encountered when running OptipPod's end-to-end tests.

## Table of Contents

- [Quick Diagnosis](#quick-diagnosis)
- [Environment Issues](#environment-issues)
- [Test Execution Issues](#test-execution-issues)
- [Resource Management Issues](#resource-management-issues)
- [Performance Issues](#performance-issues)
- [CI/CD Issues](#cicd-issues)
- [Debug Techniques](#debug-techniques)
- [Recovery Procedures](#recovery-procedures)

## Quick Diagnosis

### Test Failure Checklist

When tests fail, check these items in order:

1. **Environment Setup**
   - [ ] Kind cluster is running: `kind get clusters`
   - [ ] kubectl can connect: `kubectl cluster-info`
   - [ ] Required images are loaded: `docker images | grep optipod`
   - [ ] Required components are installed: `kubectl get pods -A`

2. **Test Configuration**
   - [ ] Environment variables are set correctly
   - [ ] Test timeouts are appropriate for your environment
   - [ ] Parallel execution settings are compatible
   - [ ] Resource limits are sufficient

3. **Resource State**
   - [ ] No leftover test resources: `kubectl get ns | grep test`
   - [ ] Sufficient cluster resources: `kubectl top nodes`
   - [ ] No conflicting processes: `ps aux | grep optipod`

### Common Error Patterns

| Error Pattern | Likely Cause | Quick Fix |
|---------------|--------------|-----------|
| `no nodes found for cluster` | Kind cluster not running | `kind create cluster` |
| `connection refused` | kubectl not configured | Check KUBECONFIG |
| `image not found` | Image not loaded | `make docker-build && kind load docker-image` |
| `timeout exceeded` | Insufficient timeout | Increase timeout values |
| `resource already exists` | Cleanup failed | Manual resource cleanup |
| `insufficient resources` | Resource limits | Increase cluster resources |

## Environment Issues

### Kind Cluster Issues

#### Problem: Kind cluster creation fails

**Symptoms**:
```
ERROR: failed to create cluster: failed to generate kubeconfig
```

**Diagnosis**:
```bash
# Check Docker status
docker info

# Check available resources
docker system df

# Check for conflicting clusters
kind get clusters
```

**Solutions**:
```bash
# Clean up existing clusters
kind delete cluster --name kind

# Create cluster with specific configuration
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /tmp
    containerPath: /tmp
EOF

# Verify cluster
kubectl cluster-info --context kind-kind
```

#### Problem: Kind cluster becomes unresponsive

**Symptoms**:
```
Unable to connect to the server: dial tcp 127.0.0.1:xxxxx: connect: connection refused
```

**Diagnosis**:
```bash
# Check cluster status
kind get clusters
docker ps | grep kind

# Check cluster logs
kind export logs /tmp/kind-logs
```

**Solutions**:
```bash
# Restart cluster
kind delete cluster && kind create cluster

# Or restart Docker and recreate
sudo systemctl restart docker
kind create cluster
```

### Kubernetes Configuration Issues

#### Problem: kubectl cannot connect to cluster

**Symptoms**:
```
The connection to the server localhost:8080 was refused
```

**Diagnosis**:
```bash
# Check KUBECONFIG
echo $KUBECONFIG
kubectl config current-context
kubectl config get-contexts
```

**Solutions**:
```bash
# Set correct kubeconfig
export KUBECONFIG="$(kind get kubeconfig-path --name="kind")"

# Or use kind's kubeconfig directly
kind get kubeconfig --name="kind" > ~/.kube/config

# Verify connection
kubectl cluster-info
```

#### Problem: Insufficient RBAC permissions

**Symptoms**:
```
forbidden: User "system:serviceaccount:default:default" cannot create resource
```

**Diagnosis**:
```bash
# Check current user
kubectl auth whoami

# Check permissions
kubectl auth can-i create pods
kubectl auth can-i create deployments
```

**Solutions**:
```bash
# Create cluster admin binding (for testing only)
kubectl create clusterrolebinding test-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=default:default

# Or use proper RBAC configuration
kubectl apply -f config/rbac/
```

### Component Installation Issues

#### Problem: CertManager installation fails

**Symptoms**:
```
Failed to install CertManager: timeout waiting for condition
```

**Diagnosis**:
```bash
# Check CertManager status
kubectl get pods -n cert-manager
kubectl get crd | grep cert-manager

# Check installation logs
kubectl logs -n cert-manager deployment/cert-manager
```

**Solutions**:
```bash
# Skip installation if already present
export CERT_MANAGER_INSTALL_SKIP=true

# Or install manually with specific version
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Wait for readiness
kubectl wait --for=condition=Available --timeout=300s deployment/cert-manager -n cert-manager
```

#### Problem: MetricsServer installation fails

**Symptoms**:
```
Failed to install MetricsServer: unable to fetch metrics
```

**Diagnosis**:
```bash
# Check MetricsServer status
kubectl get pods -n kube-system | grep metrics-server
kubectl top nodes

# Check MetricsServer logs
kubectl logs -n kube-system deployment/metrics-server
```

**Solutions**:
```bash
# Skip installation if already present
export METRICS_SERVER_INSTALL_SKIP=true

# Or install with insecure configuration for Kind
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-server
  namespace: kube-system
spec:
  template:
    spec:
      containers:
      - name: metrics-server
        image: k8s.gcr.io/metrics-server/metrics-server:v0.6.1
        args:
        - --cert-dir=/tmp
        - --secure-port=4443
        - --kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname
        - --kubelet-use-node-status-port
        - --kubelet-insecure-tls
EOF
```

## Test Execution Issues

### Test Timeout Issues

#### Problem: Tests timeout during execution

**Symptoms**:
```
Test exceeded timeout of 10m0s
```

**Diagnosis**:
```bash
# Check system resources
kubectl top nodes
kubectl top pods -A

# Check test execution time
go test -v -tags=e2e ./test/e2e -run "TestName" -timeout=1m
```

**Solutions**:
```bash
# Increase timeout
go test -v -tags=e2e ./test/e2e -timeout=30m

# Use timeout multiplier for parallel execution
E2E_TIMEOUT_MULTIPLIER=2.0 go test -v -tags=e2e ./test/e2e

# Optimize test performance
export E2E_PARALLEL_NODES=2  # Reduce parallel load
```

### Parallel Execution Issues

#### Problem: Tests fail in parallel but pass individually

**Symptoms**:
```
Test passed when run individually but failed in parallel execution
```

**Diagnosis**:
```bash
# Run tests individually
go test -v -tags=e2e ./test/e2e -run "TestName" -ginkgo.procs=1

# Run with different parallel configurations
go test -v -tags=e2e ./test/e2e -ginkgo.procs=2
go test -v -tags=e2e ./test/e2e -ginkgo.procs=4
```

**Solutions**:
```bash
# Disable parallel execution
go test -v -tags=e2e ./test/e2e -ginkgo.procs=1

# Use namespace isolation
export E2E_NAMESPACE_ISOLATION=true

# Increase resource limits
export E2E_TIMEOUT_MULTIPLIER=3.0
```

### Test Data Issues

#### Problem: Test configuration generation fails

**Symptoms**:
```
Failed to generate test configuration: invalid parameters
```

**Diagnosis**:
```bash
# Check generator functions
go test -v ./test/e2e/fixtures -run "TestGenerator"

# Validate generated configurations
go run test/e2e/fixtures/validate.go
```

**Solutions**:
```bash
# Use known good configurations
config := helpers.PolicyConfig{
    Name: "test-policy",
    Mode: v1alpha1.ModeRecommend,
    // ... other fields
}

# Or fix generator parameters
generator := fixtures.NewPolicyConfigGenerator()
config := generator.GenerateBasicPolicyConfig("test", v1alpha1.ModeAuto)
```

## Resource Management Issues

### Resource Cleanup Issues

#### Problem: Test resources not cleaned up

**Symptoms**:
```
namespace "test-12345" already exists
resource "test-deployment" already exists
```

**Diagnosis**:
```bash
# List test namespaces
kubectl get ns | grep test

# List test resources
kubectl get all -A | grep test

# Check finalizers
kubectl get ns test-12345 -o yaml | grep finalizers
```

**Solutions**:
```bash
# Manual cleanup
kubectl delete ns --all --selector=test-namespace=true

# Force cleanup with finalizer removal
kubectl patch ns test-12345 -p '{"metadata":{"finalizers":[]}}' --type=merge
kubectl delete ns test-12345 --force --grace-period=0

# Automated cleanup script
./test/e2e/scripts/cleanup-test-resources.sh
```

#### Problem: Resource conflicts during test execution

**Symptoms**:
```
Operation cannot be fulfilled: the object has been modified
```

**Diagnosis**:
```bash
# Check resource versions
kubectl get deployment test-deployment -o yaml | grep resourceVersion

# Check for concurrent modifications
kubectl get events --sort-by='.lastTimestamp' | grep test-deployment
```

**Solutions**:
```bash
# Use unique resource names
name := fmt.Sprintf("test-deployment-%d", time.Now().Unix())

# Implement retry logic
err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    // Resource operation
    return nil
})

# Use optimistic locking
deployment.ResourceVersion = ""  // Let server set version
```

### Resource Limits Issues

#### Problem: Insufficient cluster resources

**Symptoms**:
```
pods "test-pod" is forbidden: exceeded quota
Insufficient cpu/memory
```

**Diagnosis**:
```bash
# Check resource usage
kubectl top nodes
kubectl top pods -A

# Check resource quotas
kubectl describe quota -A
kubectl describe limitrange -A

# Check node capacity
kubectl describe nodes
```

**Solutions**:
```bash
# Increase Kind cluster resources
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /tmp
    containerPath: /tmp
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
        extraArgs:
          enable-admission-plugins: NodeRestriction
    controllerManager:
        extraArgs:
          bind-address: 0.0.0.0
    scheduler:
        extraArgs:
          bind-address: 0.0.0.0
EOF

# Or reduce test resource requirements
config.Resources = helpers.ResourceRequirements{
    Requests: map[string]string{
        "cpu":    "10m",
        "memory": "32Mi",
    },
    Limits: map[string]string{
        "cpu":    "100m", 
        "memory": "128Mi",
    },
}
```

## Performance Issues

### Slow Test Execution

#### Problem: Tests take too long to complete

**Symptoms**:
```
Test suite completed in 45m30s (expected < 15m)
```

**Diagnosis**:
```bash
# Profile test execution
go test -v -tags=e2e ./test/e2e -cpuprofile=cpu.prof -memprofile=mem.prof

# Analyze bottlenecks
go tool pprof cpu.prof
go tool pprof mem.prof

# Check individual test timing
go test -v -tags=e2e ./test/e2e -ginkgo.v | grep "Ran.*in"
```

**Solutions**:
```bash
# Enable performance optimizations
export E2E_PERFORMANCE_MODE=true

# Use parallel execution
export E2E_PARALLEL_NODES=4
go test -v -tags=e2e ./test/e2e -ginkgo.procs=4

# Optimize resource operations
export E2E_FAST_CLEANUP=true
export E2E_SKIP_VALIDATION=true  # Only for development
```

### Memory Issues

#### Problem: Tests consume excessive memory

**Symptoms**:
```
runtime: out of memory: cannot allocate
Test process killed due to memory limit
```

**Diagnosis**:
```bash
# Monitor memory usage
go test -v -tags=e2e ./test/e2e -memprofile=mem.prof

# Check for memory leaks
go tool pprof -http=:8080 mem.prof

# Monitor system memory
watch -n 1 'free -h && kubectl top nodes'
```

**Solutions**:
```bash
# Reduce parallel execution
export E2E_PARALLEL_NODES=2

# Enable garbage collection
export GOGC=50

# Increase system memory or use smaller test datasets
export E2E_REDUCED_DATASET=true
```

## CI/CD Issues

### GitHub Actions Issues

#### Problem: E2E tests fail in CI but pass locally

**Symptoms**:
```
Tests pass locally but fail in GitHub Actions
```

**Diagnosis**:
```bash
# Compare environments
echo "Local Go version: $(go version)"
echo "Local Kind version: $(kind version)"
echo "Local kubectl version: $(kubectl version --client)"

# Check CI logs for differences
# Look for resource constraints, timing issues, or environment differences
```

**Solutions**:
```bash
# Match CI environment locally
export CI=true
export GITHUB_ACTIONS=true

# Use CI-specific configuration
if [ "$CI" = "true" ]; then
    export E2E_TIMEOUT_MULTIPLIER=3.0
    export E2E_PARALLEL_NODES=2
fi

# Add retry logic for flaky tests
go test -v -tags=e2e ./test/e2e -ginkgo.flake-attempts=3
```

#### Problem: Test artifacts not collected

**Symptoms**:
```
Test failed but no diagnostic information available
```

**Diagnosis**:
```bash
# Check artifact collection configuration
ls -la /tmp/optipod-diagnostics/
ls -la artifacts/

# Verify GitHub Actions artifact upload
# Check workflow file for artifact collection steps
```

**Solutions**:
```yaml
# Ensure artifact collection in GitHub Actions
- name: Collect test artifacts
  if: failure()
  run: |
    mkdir -p artifacts
    cp -r /tmp/optipod-diagnostics artifacts/ || true
    kubectl get all -A > artifacts/cluster-state.yaml || true
    kind export logs artifacts/kind-logs || true

- name: Upload artifacts
  if: failure()
  uses: actions/upload-artifact@v3
  with:
    name: e2e-test-artifacts
    path: artifacts/
    retention-days: 7
```

## Debug Techniques

### Verbose Logging

Enable detailed logging for troubleshooting:

```bash
# Ginkgo verbose output
go test -v -tags=e2e ./test/e2e -ginkgo.v -ginkgo.trace

# Controller-runtime debug logging
export KUBEBUILDER_ASSETS=$(setup-envtest use --use-env -p path)
go test -v -tags=e2e ./test/e2e -ginkgo.v

# Kubernetes API debug logging
export KUBECTL_EXTERNAL_DIFF="diff -u"
kubectl diff -f config/samples/
```

### Interactive Debugging

Debug tests interactively:

```bash
# Run single test with debugging
go test -v -tags=e2e ./test/e2e -run "TestSpecificTest" -ginkgo.focus="specific scenario"

# Pause test execution for inspection
# Add this to test code:
By("Pausing for manual inspection")
fmt.Println("Cluster state available for inspection")
fmt.Println("Press Enter to continue...")
fmt.Scanln()

# Use delve debugger
dlv test -- -test.run TestSpecificTest -tags=e2e
```

### Log Analysis

Analyze logs for issues:

```bash
# Collect all relevant logs
kubectl logs -n optipod-system deployment/optipod-controller-manager > controller.log
kubectl get events --sort-by='.lastTimestamp' > events.log
kind export logs /tmp/kind-logs

# Search for error patterns
grep -i error controller.log
grep -i failed events.log
grep -i timeout /tmp/kind-logs/kind-control-plane/kubelet.log

# Analyze timing issues
grep -E "Started|Completed" controller.log | sort
```

### Resource Inspection

Inspect resource states during failures:

```bash
# Capture cluster state
kubectl get all -A > cluster-state.yaml
kubectl describe nodes > node-state.yaml
kubectl get events --sort-by='.lastTimestamp' > events.yaml

# Inspect specific resources
kubectl describe deployment test-deployment
kubectl describe pod test-pod
kubectl logs test-pod --previous

# Check resource relationships
kubectl get deployment test-deployment -o yaml
kubectl get replicaset -l app=test-deployment
kubectl get pods -l app=test-deployment
```

## Recovery Procedures

### Complete Environment Reset

When all else fails, reset the entire test environment:

```bash
#!/bin/bash
# complete-reset.sh

echo "Performing complete environment reset..."

# Stop all tests
pkill -f "go test.*e2e"

# Clean up Kind clusters
kind delete cluster --name kind
kind delete cluster --name optipod-test

# Clean up Docker resources
docker system prune -f
docker volume prune -f

# Clean up test artifacts
rm -rf /tmp/optipod-diagnostics
rm -rf /tmp/kind-logs
rm -f *.prof

# Recreate Kind cluster
kind create cluster --name kind

# Verify environment
kubectl cluster-info
kubectl get nodes

echo "Environment reset complete"
```

### Partial Recovery

For less severe issues, try partial recovery:

```bash
#!/bin/bash
# partial-recovery.sh

echo "Performing partial recovery..."

# Clean up test namespaces
kubectl delete ns --all --selector=test-namespace=true --timeout=60s

# Force cleanup stuck namespaces
for ns in $(kubectl get ns | grep test | awk '{print $1}'); do
    kubectl patch ns $ns -p '{"metadata":{"finalizers":[]}}' --type=merge
    kubectl delete ns $ns --force --grace-period=0
done

# Restart OptipPod controller
kubectl rollout restart deployment/optipod-controller-manager -n optipod-system

# Wait for readiness
kubectl rollout status deployment/optipod-controller-manager -n optipod-system

echo "Partial recovery complete"
```

### Test-Specific Recovery

For specific test failures:

```bash
#!/bin/bash
# test-specific-recovery.sh

TEST_NAME=$1
NAMESPACE=$2

echo "Recovering from test failure: $TEST_NAME"

# Clean up test-specific resources
if [ ! -z "$NAMESPACE" ]; then
    kubectl delete ns $NAMESPACE --force --grace-period=0
fi

# Clean up test-specific CRDs
kubectl delete optimizationpolicies --all -A

# Clean up test-specific RBAC
kubectl delete clusterrolebinding --selector=test-rbac=true

# Restart relevant components
kubectl rollout restart deployment/optipod-controller-manager -n optipod-system

echo "Test-specific recovery complete for: $TEST_NAME"
```

## Getting Help

If you continue to experience issues after following this guide:

1. **Check the logs**: Collect and analyze all relevant logs
2. **Search existing issues**: Look for similar problems in the project's issue tracker
3. **Create a detailed issue**: Include:
   - Environment information (OS, Go version, Kind version)
   - Complete error messages and stack traces
   - Steps to reproduce the issue
   - Relevant configuration files
   - Test artifacts and logs

4. **Contact the team**: Reach out through the project's communication channels

### Issue Template

When reporting test issues, use this template:

```markdown
## Test Failure Report

### Environment
- OS: [e.g., Ubuntu 20.04]
- Go Version: [e.g., 1.21.0]
- Kind Version: [e.g., v0.20.0]
- kubectl Version: [e.g., v1.28.0]

### Test Information
- Test Name: [e.g., TestPolicyModeValidation]
- Test File: [e.g., policy_modes_test.go]
- Execution Mode: [e.g., parallel with 4 nodes]

### Error Details
```
[Paste complete error message and stack trace]
```

### Steps to Reproduce
1. [Step 1]
2. [Step 2]
3. [Step 3]

### Expected Behavior
[Describe what should happen]

### Actual Behavior
[Describe what actually happened]

### Additional Context
[Any additional information, logs, or screenshots]

### Attempted Solutions
[List what you've already tried]
```

This troubleshooting guide should help you resolve most common issues with the OptipPod e2e test suite. Keep it updated as new issues and solutions are discovered.