# Prometheus Setup Guide for OptipPod

This guide explains how to set up and configure Prometheus as a metrics provider for OptipPod. Prometheus is now fully supported and production-ready.

> **⚠️ Important**: Currently, the metrics provider is configured globally at the controller level via the `--metrics-provider` flag. While optimization policies have a `metricsConfig.provider` field, it must match the global controller setting. Per-policy provider selection is planned for a future release.

## Overview

OptipPod's Prometheus integration provides:
- ✅ **Production-ready**: Fully tested and supported
- ✅ **Advanced metrics**: Leverage PromQL for complex queries
- ✅ **Historical data**: Analyze trends over extended time periods
- ✅ **Custom metrics**: Support for additional container metrics
- ✅ **High availability**: Works with clustered Prometheus setups

## Prerequisites

- Kubernetes cluster (1.29+)
- Prometheus deployed and accessible within the cluster
- OptipPod installed (see [Installation Guide](INSTALLATION.md))

## Prometheus Deployment Options

### Option 1: Prometheus Operator (Recommended)

The easiest way to deploy Prometheus is using the Prometheus Operator:

```bash
# Add Prometheus community Helm repository
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install kube-prometheus-stack
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --set prometheus.prometheusSpec.retention=7d \
  --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage=50Gi
```

### Option 2: Manual Prometheus Deployment

If you prefer manual deployment, ensure your Prometheus configuration includes:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-config
  namespace: monitoring
data:
  prometheus.yml: |
    global:
      scrape_interval: 15s
      evaluation_interval: 15s
    
    scrape_configs:
    - job_name: 'kubernetes-cadvisor'
      kubernetes_sd_configs:
      - role: node
      scheme: https
      tls_config:
        ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        insecure_skip_verify: true
      bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
      relabel_configs:
      - action: labelmap
        regex: __meta_kubernetes_node_label_(.+)
      - target_label: __address__
        replacement: kubernetes.default.svc:443
      - source_labels: [__meta_kubernetes_node_name]
        regex: (.+)
        target_label: __metrics_path__
        replacement: /api/v1/nodes/${1}/proxy/metrics/cadvisor
```

## OptipPod Configuration

### 1. Configure Prometheus URL

Update your OptipPod configuration to use Prometheus:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: optipod-config
  namespace: optipod-system
data:
  config.yaml: |
    metricsProvider: prometheus
    prometheusURL: "http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090"
    reconciliationInterval: 5m
```

### 2. Update Controller Arguments

If using command-line arguments:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: optipod-controller-manager
  namespace: optipod-system
spec:
  template:
    spec:
      containers:
      - name: manager
        args:
        - --metrics-provider=prometheus
        - --prometheus-url=http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090
        - --reconciliation-interval=5m
```

### 3. Create Optimization Policy

Create an optimization policy that uses Prometheus:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: prometheus-policy
  namespace: optipod-system
spec:
  mode: Auto
  
  selector:
    namespaceSelector:
      matchLabels:
        optimize: "true"
    workloadSelector:
      matchLabels:
        optimize: "true"
  
  metricsConfig:
    provider: prometheus  # Must match controller's --metrics-provider setting
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  
  resourceBounds:
    cpu:
      min: "50m"
      max: "8000m"
    memory:
      min: "64Mi"
      max: "16Gi"
  
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
    useServerSideApply: true
  
  reconciliationInterval: 5m
```

## Required Prometheus Metrics

OptipPod requires the following metrics to be available in Prometheus:

### CPU Metrics
- `container_cpu_usage_seconds_total`: Container CPU usage counter
- `rate(container_cpu_usage_seconds_total[5m])`: CPU usage rate calculation

### Memory Metrics
- `container_memory_working_set_bytes`: Container memory working set
- `container_memory_usage_bytes`: Container memory usage (alternative)

### Labels Required
- `namespace`: Kubernetes namespace
- `pod`: Pod name
- `container`: Container name

## Verification

### 1. Check Prometheus Connectivity

```bash
# Port forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090

# Test connectivity
curl http://localhost:9090/api/v1/query?query=up

# Check for container metrics
curl "http://localhost:9090/api/v1/query?query=container_cpu_usage_seconds_total"
```

### 2. Verify OptipPod Integration

```bash
# Check OptipPod controller logs
kubectl logs -n optipod-system -l control-plane=controller-manager --tail=50

# Check optimization policy status
kubectl get optimizationpolicy -n optipod-system

# Describe policy for detailed status
kubectl describe optimizationpolicy prometheus-policy -n optipod-system
```

### 3. Monitor Optimization Events

```bash
# Watch for optimization events
kubectl get events -n <target-namespace> --field-selector reason=UpdateSuccess

# Check workload annotations for recommendations
kubectl get deployment <deployment-name> -n <namespace> -o yaml | grep optipod.io
```

## Troubleshooting

### Common Issues

#### 1. Connection Refused
```
Error: failed to query CPU metrics: Get "http://prometheus:9090/api/v1/query_range": dial tcp: lookup prometheus on 10.96.0.10:53: no such host
```

**Solution**: Verify the Prometheus URL is correct and accessible from the OptipPod controller pod.

#### 2. No Metrics Found
```
Error: failed to query CPU metrics: no data returned from Prometheus
```

**Solution**: Ensure Prometheus is scraping cAdvisor metrics and the required labels are present.

#### 3. Insufficient Samples
```
Skipped workload: Missing metrics: no samples in result
```

**Solution**: Wait for Prometheus to collect more data points, or reduce the `rollingWindow` duration.

#### 4. Provider Mismatch
```
Policy validation failed: metricsConfig.provider 'prometheus' does not match controller setting 'metrics-server'
```

**Solution**: Ensure the policy's `metricsConfig.provider` matches the controller's `--metrics-provider` flag. Currently, all policies must use the same provider as configured at the controller level.

### Debug Commands

```bash
# Check Prometheus targets
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090
# Visit http://localhost:9090/targets

# Test specific queries
curl "http://localhost:9090/api/v1/query?query=rate(container_cpu_usage_seconds_total{namespace=\"default\"}[5m])"

# Check OptipPod controller connectivity
kubectl exec -n optipod-system deployment/optipod-controller-manager -- \
  curl -s http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/-/healthy
```

## Performance Considerations

### Prometheus Configuration
- **Retention**: Set appropriate retention period (7-30 days recommended)
- **Storage**: Ensure sufficient storage for metrics data
- **Scrape Interval**: 15-30 seconds is typically sufficient
- **Query Timeout**: Configure reasonable timeouts for large clusters

### OptipPod Configuration
- **Rolling Window**: Balance between accuracy and performance (12-24h recommended)
- **Reconciliation Interval**: Avoid too frequent reconciliation (5-15m recommended)
- **Safety Factor**: Use appropriate safety margins (1.1-1.5 recommended)

## Advanced Configuration

### Custom Queries (Future Enhancement)
OptipPod's architecture supports custom Prometheus queries. This feature is planned for future releases:

```yaml
metricsConfig:
  provider: prometheus
  prometheusConfig:
    url: "http://prometheus:9090"
    queries:
      cpu: 'rate(container_cpu_usage_seconds_total{namespace="{namespace}",pod="{pod}"}[5m])'
      memory: 'container_memory_working_set_bytes{namespace="{namespace}",pod="{pod}"}'
```

### High Availability Setup
For production environments, consider:
- Prometheus clustering or federation
- Multiple Prometheus instances with load balancing
- Backup and disaster recovery procedures

## Migration from Metrics-Server

If migrating from metrics-server to Prometheus:

1. **Deploy Prometheus** alongside existing metrics-server
2. **Test with a subset** of workloads using namespace selectors
3. **Gradually migrate** policies to use Prometheus
4. **Monitor performance** and adjust configuration as needed
5. **Decommission metrics-server** once fully migrated

## Support

For issues specific to Prometheus integration:
1. Check the [troubleshooting section](#troubleshooting) above
2. Review OptipPod controller logs for detailed error messages
3. Verify Prometheus is collecting the required metrics
4. Open an issue on the OptipPod GitHub repository with logs and configuration details

## Frequently Asked Questions

### Q: Why do I need to specify the provider in both the controller and the policy?

**A**: Currently, this is a known limitation. The metrics provider is configured globally at the controller level via the `--metrics-provider` flag. While optimization policies have a `metricsConfig.provider` field, it must match the global controller setting. 

**Per-policy provider selection is planned for a future release** where you'll be able to use different providers for different policies (e.g., Prometheus for production workloads, metrics-server for development).

### Q: Will OptipPod keep increasing limits indefinitely with limitConfig?

**A**: No! OptipPod has a two-level bounds system:

1. **Request Bounds**: The `resourceBounds.cpu.max` and `resourceBounds.memory.max` strictly limit requests
2. **Limit Calculation**: Limits = `bounded_request × multiplier`

**Example**:
```yaml
resourceBounds:
  cpu:
    max: "2000m"  # Requests never exceed 2000m
limitConfig:
  cpuLimitMultiplier: 1.5  # Max limit = 2000m × 1.5 = 3000m
```

### Q: Can I use different Prometheus instances for different policies?

**A**: Not currently. All policies use the same Prometheus instance configured at the controller level. Per-policy provider configuration is planned for a future release.

### Q: How do I migrate from metrics-server to Prometheus?

**A**: Follow these steps:

1. Deploy Prometheus alongside metrics-server
2. Test with a subset of workloads using namespace selectors
3. Update the controller's `--metrics-provider` flag to `prometheus`
4. Update all policy `metricsConfig.provider` fields to `prometheus`
5. Restart the controller
6. Monitor and adjust configuration as needed

### Q: What happens if Prometheus is unavailable?

**A**: OptipPod will:
- Log connection errors
- Skip optimization for affected workloads
- Continue processing other policies and workloads
- Retry on the next reconciliation cycle

The system is designed to be resilient to temporary metrics provider outages.