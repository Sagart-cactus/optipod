# OptiPod E2E Testing Guide

## Quick Reference

### Run All E2E Tests
```bash
make test-e2e
```

### Run Tests with Existing Cluster
```bash
# Setup once
make setup-test-e2e

# Run tests multiple times
KIND=kind KIND_CLUSTER=optipod-test-e2e go test -tags=e2e ./test/e2e/ -v -ginkgo.v

# Cleanup when done
make cleanup-test-e2e
```

### Run Specific Test
```bash
KIND=kind KIND_CLUSTER=optipod-test-e2e go test -tags=e2e ./test/e2e/ -v \
  -ginkgo.focus="should create and validate OptimizationPolicy"
```

## Test Coverage

### ✅ Implemented Test Scenarios

1. **Controller Deployment & Health**
   - Controller pod running and ready
   - Metrics endpoint serving
   - Prometheus metrics exposed

2. **OptimizationPolicy CRUD Operations**
   - Create policies in different modes (Auto, Recommend, Disabled)
   - Validate policy configuration
   - Check Ready conditions
   - Reject invalid configurations

3. **Workload Discovery**
   - Deploy sample Deployments
   - Verify label selector matching
   - Check namespace filtering
   - Validate workload appears in policy status

4. **Recommend Mode (Property 8)**
   - Generate recommendations without modifying workloads
   - Verify recommendations contain CPU and memory values
   - Ensure workload specs remain unchanged
   - Validate recommendation format

5. **Auto Mode (Property 17)**
   - Apply recommendations automatically
   - Verify lastApplied timestamp is set
   - Check workload resources are updated

6. **Resource Bounds (Property 4)**
   - Test min/max CPU bounds
   - Test min/max memory bounds
   - Verify recommendations are clamped to bounds

7. **Disabled Mode (Property 19)**
   - Verify workloads are not processed
   - Ensure no recommendations are generated
   - Validate historical status is preserved

8. **Prometheus Metrics (Property 25)**
   - Verify optipod_workloads_monitored metric
   - Verify optipod_reconciliation_duration_seconds metric
   - Check controller_runtime_reconcile_total metric

9. **Error Handling**
   - Invalid policy configurations (min > max)
   - Validation error messages
   - Kubernetes event creation

10. **Resource Cleanup**
    - Delete test policies
    - Delete test namespaces
    - Clean up test workloads

## Test Requirements Validation

The E2E tests validate the following requirements from the design document:

| Requirement | Test Scenario | Status |
|-------------|---------------|--------|
| 1.1, 1.2, 1.3 | Workload monitoring and updates | ✅ Covered |
| 2.1-2.5 | Resource bounds enforcement | ✅ Covered |
| 4.1-4.4 | Recommend mode behavior | ✅ Covered |
| 6.1, 6.2 | Policy validation | ✅ Covered |
| 6.3-6.5 | Workload discovery | ✅ Covered |
| 7.1-7.7 | Mode-specific behavior | ✅ Covered |
| 9.1-9.6 | Status management | ✅ Covered |
| 10.1-10.5 | Prometheus metrics | ✅ Covered |
| 11.1-11.4 | Event creation | ✅ Covered |

## Test Execution Flow

```
1. BeforeSuite
   ├── Build operator Docker image
   ├── Load image into Kind cluster
   └── Install CertManager (if needed)

2. BeforeAll (per test context)
   ├── Create optipod-system namespace
   ├── Label namespace with security policy
   ├── Install CRDs
   └── Deploy controller

3. Test Execution
   ├── Controller health checks
   ├── Policy creation and validation
   ├── Workload deployment
   ├── Recommendation generation
   ├── Mode-specific behavior tests
   ├── Bounds enforcement tests
   ├── Metrics validation
   └── Error handling tests

4. AfterEach (on failure)
   ├── Collect controller logs
   ├── Collect Kubernetes events
   ├── Collect metrics output
   └── Describe failed pods

5. AfterAll
   ├── Delete test resources
   ├── Undeploy controller
   ├── Uninstall CRDs
   └── Delete namespace

6. AfterSuite
   └── Uninstall CertManager (if installed)
```

## Expected Test Duration

- **Full suite**: ~10-15 minutes
  - Cluster setup: 2-3 minutes
  - Image build and load: 2-3 minutes
  - Controller deployment: 1-2 minutes
  - Test execution: 5-7 minutes
  - Cleanup: 1-2 minutes

- **Individual test**: 30 seconds - 3 minutes
  - Depends on reconciliation intervals and timeouts

## Test Timeouts

Default timeouts configured in tests:
- `SetDefaultEventuallyTimeout`: 2 minutes
- `SetDefaultEventuallyPollingInterval`: 1 second
- Policy Ready condition: 2 minutes
- Deployment ready: 2 minutes
- Workload discovery: 3 minutes
- Recommendation generation: 3-4 minutes
- Metrics availability: 2 minutes

## Debugging Tips

### View Controller Logs
```bash
kubectl --context kind-optipod-test-e2e logs -n optipod-system \
  -l control-plane=controller-manager --tail=100
```

### Check Policy Status
```bash
kubectl --context kind-optipod-test-e2e get optimizationpolicies -A -o yaml
```

### View Workload Status
```bash
kubectl --context kind-optipod-test-e2e get deployments -n test-workloads -o yaml
```

### Check Events
```bash
kubectl --context kind-optipod-test-e2e get events -n optipod-system --sort-by=.lastTimestamp
kubectl --context kind-optipod-test-e2e get events -n test-workloads --sort-by=.lastTimestamp
```

### Access Metrics
```bash
# Port-forward to metrics endpoint
kubectl --context kind-optipod-test-e2e port-forward -n optipod-system \
  svc/optipod-controller-manager-metrics-service 8443:8443

# In another terminal, query metrics
curl -k https://localhost:8443/metrics
```

### Inspect Kind Cluster
```bash
# List clusters
kind get clusters

# Get cluster info
kubectl --context kind-optipod-test-e2e cluster-info

# Check node status
kubectl --context kind-optipod-test-e2e get nodes

# Check all pods
kubectl --context kind-optipod-test-e2e get pods -A
```

## Known Limitations

1. **Metrics Provider**: Tests use `metrics-server` provider, but metrics-server may not be available in Kind clusters by default. The controller will handle missing metrics gracefully.

2. **In-Place Resize**: Kind clusters may not have the InPlacePodVerticalScaling feature gate enabled. Tests focus on recommendation generation rather than actual in-place resize.

3. **Actual Metrics**: Tests don't generate real workload metrics. The controller may skip updates due to missing metrics, which is expected behavior.

4. **Timing**: Some tests rely on reconciliation intervals and may need adjustment based on cluster performance.

## Future Enhancements

Potential additions to the E2E test suite:

- [ ] Test with mock Prometheus metrics provider
- [ ] Test in-place resize on clusters with feature gate enabled
- [ ] Test StatefulSet and DaemonSet workloads
- [ ] Test multi-namespace scenarios
- [ ] Test RBAC restrictions
- [ ] Test global dry-run mode
- [ ] Test policy updates and transitions
- [ ] Test concurrent policy processing
- [ ] Test metrics provider failover
- [ ] Performance tests with many workloads

## CI/CD Integration

### GitHub Actions Example
```yaml
name: E2E Tests
on: [push, pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 30
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
      
      - name: Upload logs on failure
        if: failure()
        uses: actions/upload-artifact@v3
        with:
          name: e2e-logs
          path: |
            /tmp/kind-*
            test_output.log
```

## Support

For issues or questions about E2E tests:
1. Check the test output for detailed error messages
2. Review controller logs for reconciliation errors
3. Verify Kind cluster is healthy
4. Check that all prerequisites are installed
5. Consult the main README.md for project setup

## Contributing

When adding new E2E tests:
1. Follow Ginkgo BDD style: `It("should ...")`
2. Use descriptive test names
3. Add appropriate timeouts with `Eventually`
4. Clean up resources after tests
5. Update this guide with new scenarios
6. Ensure tests are idempotent
7. Add comments for complex test logic
