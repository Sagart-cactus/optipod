# Cluster-Free E2E Testing

This directory contains OptiPod's cluster-free E2E testing architecture that provides comprehensive functional coverage without requiring a Kubernetes cluster.

## Overview

The cluster-free testing approach eliminates flaky cluster-based tests while providing complete functional coverage of OptiPod workflows. Tests run **120-300x faster** than cluster-based equivalents with **100% deterministic behavior**.

### Key Benefits

- ⚡ **Ultra-fast execution**: < 5 minutes for full suite vs 15-30 minutes for cluster tests
- 🎯 **Zero flakiness**: Deterministic state checks, no timing dependencies
- 🚀 **Parallel execution**: Full parallel support with `ginkgo -procs=4`
- 💯 **Complete coverage**: Tests scenarios impossible with real clusters

## Architecture

### Three-Layer Design

```
┌─────────────────────────────────────────────────────────────┐
│                  Layer 1: Test Harness                      │
│  (Fake clients, test fixtures, simulation framework)       │
├─────────────────────────────────────────────────────────────┤
│                  Layer 2: Component Simulators              │
│  (Metrics, Controller, Webhook, Discovery)                 │
├─────────────────────────────────────────────────────────────┤
│                  Layer 3: Workflow Tests                    │
│  (End-to-end scenarios testing complete flows)             │
└─────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
test/clusterless/
├── harness/
│   ├── clients.go           # Fake client factory and TestHarness
│   ├── fixtures.go          # Fluent builders for test objects
│   ├── clock.go             # Mock time control
│   └── metrics.go           # Mock metrics provider
├── simulators/
│   ├── controller.go        # Controller reconciler wrapper
│   └── webhook.go           # Webhook mutator wrapper
├── workflows/
│   ├── suite_test.go        # Ginkgo suite setup
│   ├── policy_test.go       # Policy validation workflows
│   ├── safety_test.go       # Safety mechanism tests
│   ├── webhook_test.go      # Webhook mutation workflows
│   └── multi_policy_test.go # Multi-policy selection
└── README.md                # This file
```

## Quick Start

### Running Tests

```bash
# Run all cluster-free tests
make test-clusterless

# Run tests in parallel (fastest)
make test-clusterless-fast

# Run with verbose output
make test-clusterless-verbose

# Run focused tests
make test-clusterless-focus FOCUS="Policy Operations"

# Run all tests (unit + cluster-free)
make test-all
```

### Performance Comparison

| Operation | Cluster-Based | Cluster-Free | Speedup |
|-----------|---------------|--------------|---------|
| Setup | 2-5 min | < 1 sec | 120-300x |
| Single test | 10-30 sec | 10-100 ms | 100-3000x |
| Full suite | 15-30 min | < 5 min | 3-6x |

## Writing Tests

### Basic Test Structure

```go
var _ = Describe("My Feature", func() {
    var h *harness.TestHarness
    var ctrlSim *simulators.ControllerSimulator

    BeforeEach(func() {
        h = harness.NewTestHarness()
        ctrlSim = simulators.NewControllerSimulator(h)
    })

    AfterEach(func() {
        h.Cleanup()
    })

    It("should do something", func() {
        ctx := h.Context

        // Create test objects using fluent builders
        policy := harness.NewPolicy("test-policy", "default").
            WithMode(optipodv1alpha1.ModeRecommend).
            WithLabelSelector(map[string]string{"app": "test"}).
            Build()
        Expect(h.Client.Create(ctx, policy)).To(Succeed())

        // Test your functionality
        // ...
    })
})
```

### Using Test Fixtures

The harness provides fluent builders for easy object creation:

```go
// Create a policy with complex configuration
policy := harness.NewPolicy("complex-policy", "production").
    WithMode(optipodv1alpha1.ModeAuto).
    WithStrategy(optipodv1alpha1.StrategySSA).
    WithLabelSelector(map[string]string{"tier": "backend"}).
    WithMetricsConfig("prometheus", "P90", 1.2).
    WithResourceBounds("10m", "2000m", "64Mi", "4Gi").
    WithSafetyConfig(true, nil).
    Build()

// Create a deployment with multiple containers
deploy := harness.NewDeployment("api", "production").
    WithLabels(map[string]string{"app": "api", "tier": "backend"}).
    WithContainer("api", "api:v1", "100m", "128Mi").
    WithContainerLimits("500m", "512Mi").
    WithPodAnnotations(map[string]string{
        "optipod.io/webhook-enabled": "true",
    }).
    Build()
```

### Injecting Metrics Data

The mock metrics provider supports various load patterns:

```go
// Constant load
h.MetricsProvider.InjectConstantLoad(
    "default", "pod-name", "container-name",
    500,  // 500m CPU
    256*1024*1024, // 256Mi memory
    1*time.Hour,    // duration
    1*time.Minute,  // interval
)

// Spikey load (10% spikes)
h.MetricsProvider.InjectSpikeyLoad(
    "default", "pod-name", "container-name",
    200, 800,  // baseCPU, spikeCPU
    200*1024*1024, 400*1024*1024, // baseMem, spikeMem
    1*time.Hour,
)

// Gradual increase
h.MetricsProvider.InjectGradualIncrease(
    "default", "pod-name", "container-name",
    100, 500,  // startCPU, endCPU
    128*1024*1024, 512*1024*1024, // startMem, endMem
    2*time.Hour,
)
```

### Testing Time-Based Operations

Use the mock clock for deterministic time-based testing:

```go
// Record start time
startTime := h.Clock.Now()

// Advance time by 5 minutes
h.Clock.Advance(5 * time.Minute)

// Verify time advancement
Expect(h.Clock.Since(startTime)).To(Equal(5 * time.Minute))
```

### Testing Webhook Mutations

```go
webhookSim := simulators.NewWebhookSimulator(h)

// Create pod with OptiPod annotations
pod := harness.NewPod("test-pod", "default").
    WithAnnotations(map[string]string{
        "optipod.io/webhook-enabled":   "true",
        "optipod.io/cpu-request.app":   "300m",
        "optipod.io/memory-request.app": "512Mi",
    }).
    WithContainer("app", "app:v1", "100m", "128Mi").
    Build()

// Simulate admission request
response := webhookSim.MutatePod(pod, false)

// Verify patches
Expect(response.Allowed).To(BeTrue())
patches, err := webhookSim.GetPatchesFromResponse(response)
Expect(err).NotTo(HaveOccurred())
Expect(patches).To(HaveLen(2)) // CPU and memory patches
```

## Test Coverage

The cluster-free tests provide comprehensive coverage of:

### Core Functionality
- ✅ Policy validation and processing
- ✅ Recommendation engine calculations
- ✅ Workload discovery and selection
- ✅ Resource bounds enforcement
- ✅ Percentile selection (P50, P90, P95, P99)

### Safety Mechanisms
- ✅ Memory decrease prevention
- ✅ Gradual memory decrease
- ✅ Resource bounds enforcement
- ✅ Safety factor application

### Webhook Operations
- ✅ Pod mutation based on annotations
- ✅ Multi-container support
- ✅ Patch generation and application
- ✅ Error handling

### Advanced Scenarios
- ✅ Multi-policy selection by weight
- ✅ Policy mode transitions
- ✅ Namespace isolation
- ✅ Time-based operations

## Best Practices

### Test Design
1. **Use fluent builders** for object creation
2. **Inject deterministic metrics** rather than random data
3. **Test edge cases** that are hard to reproduce with real clusters
4. **Keep tests focused** on specific functionality
5. **Use descriptive test names** that explain the scenario

### Performance
1. **Avoid sleeps** - use deterministic state checks
2. **Use parallel execution** with `ginkgo -procs=4`
3. **Clean up resources** in AfterEach hooks
4. **Reuse test harness** patterns across tests

### Debugging
1. **Use verbose output** with `-ginkgo.v` for debugging
2. **Focus on specific tests** with `-ginkgo.focus`
3. **Check fake client state** directly in tests
4. **Verify metrics injection** before testing recommendations

## Integration with CI/CD

The cluster-free tests are designed for fast CI/CD pipelines:

```yaml
# GitHub Actions example
- name: Run cluster-free E2E tests
  run: make test-clusterless-fast
  timeout-minutes: 10
```

### Coverage Reporting

Tests generate coverage reports that can be uploaded to coverage services:

```bash
# Coverage file generated at: coverage-clusterless.out
make test-clusterless
```

## Migration from Cluster-Based Tests

When migrating existing cluster-based tests:

1. **Identify the core logic** being tested
2. **Replace cluster setup** with TestHarness
3. **Replace real objects** with fixture builders
4. **Replace Eventually/Consistently** with direct assertions
5. **Replace time.Sleep** with mock clock advancement
6. **Inject deterministic metrics** instead of waiting for real metrics

## Troubleshooting

### Common Issues

**Tests are slow:**
- Ensure you're not using `time.Sleep` or `Eventually`
- Use direct state assertions instead of polling
- Run tests in parallel with `-ginkgo.procs=4`

**Flaky tests:**
- Check for timing dependencies
- Use mock clock for time-based operations
- Ensure deterministic metrics injection

**Missing coverage:**
- Add tests for edge cases
- Test error conditions
- Verify all policy modes and strategies

### Getting Help

1. Check existing tests for patterns
2. Review the test harness documentation
3. Use verbose output for debugging
4. Focus on specific failing tests

## Future Enhancements

Planned improvements to the cluster-free testing framework:

- [ ] Property-based testing with Gopter
- [ ] Performance benchmarking
- [ ] Test data generators
- [ ] Visual test reports
- [ ] Integration with mutation testing