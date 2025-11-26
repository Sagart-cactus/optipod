# OptiPod Installation Guide

This guide provides detailed instructions for installing and configuring OptiPod in your Kubernetes cluster.

## Prerequisites

### Required

- **Kubernetes Cluster**: Version 1.29 or higher
- **kubectl**: Configured to access your cluster
- **Metrics Provider**: One of the following:
  - Prometheus (recommended for production)
  - Kubernetes metrics-server
  - Custom metrics provider

### Optional

- **In-Place Pod Resize**: Kubernetes 1.29+ with `InPlacePodVerticalScaling` feature gate enabled
- **Helm**: For Helm-based installation (coming soon)

## Installation Methods

### Method 1: Using kubectl (Recommended)

#### Step 1: Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/yourusername/optipod/main/config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml
```

Verify CRD installation:
```bash
kubectl get crd optimizationpolicies.optipod.optipod.io
```

#### Step 2: Deploy the Operator

```bash
kubectl apply -f https://raw.githubusercontent.com/yourusername/optipod/main/dist/install.yaml
```

This creates:
- `optipod-system` namespace
- ServiceAccount, Roles, and RoleBindings
- Deployment for the operator
- ConfigMap for configuration
- Service for metrics

#### Step 3: Verify Installation

Check that the operator is running:
```bash
kubectl get pods -n optipod-system
```

Expected output:
```
NAME                                        READY   STATUS    RESTARTS   AGE
optipod-controller-manager-xxxxxxxxx-xxxxx   1/1     Running   0          30s
```

Check operator logs:
```bash
kubectl logs -n optipod-system deployment/optipod-controller-manager -f
```

### Method 2: Using Kustomize

Clone the repository and customize:

```bash
git clone https://github.com/yourusername/optipod.git
cd optipod

# Edit config/default/kustomization.yaml to customize
# Then apply:
kubectl apply -k config/default
```

### Method 3: Building from Source

```bash
# Clone and build
git clone https://github.com/yourusername/optipod.git
cd optipod
make build

# Build Docker image
make docker-build IMG=your-registry/optipod:v1.0.0

# Push to registry
make docker-push IMG=your-registry/optipod:v1.0.0

# Deploy with your image
cd config/manager && kustomize edit set image controller=your-registry/optipod:v1.0.0
kubectl apply -k config/default
```

## Configuration

### Operator Configuration

OptiPod can be configured via:
1. Command-line flags (in deployment args)
2. ConfigMap (`optipod-config`)
3. Environment variables

#### ConfigMap Configuration

Edit the ConfigMap in `config/manager/config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: optipod-config
  namespace: optipod-system
data:
  # Global dry-run mode (compute recommendations but never apply)
  dry-run: "false"
  
  # Default metrics provider
  metrics-provider: "prometheus"
  
  # Prometheus URL (if using Prometheus)
  prometheus-url: "http://prometheus-k8s.monitoring.svc:9090"
  
  # Default reconciliation interval
  reconciliation-interval: "5m"
  
  # Enable leader election for HA
  leader-election: "true"
```

Apply changes:
```bash
kubectl apply -f config/manager/config.yaml
kubectl rollout restart deployment/optipod-controller-manager -n optipod-system
```

#### Command-Line Flags

Available flags (set in deployment args):

| Flag | Default | Description |
|------|---------|-------------|
| `--leader-elect` | `true` | Enable leader election |
| `--metrics-bind-address` | `:8080` | Metrics endpoint address |
| `--health-probe-bind-address` | `:8081` | Health probe address |
| `--metrics-provider` | `prometheus` | Metrics backend (prometheus, metrics-server, custom) |
| `--prometheus-url` | `http://prometheus-k8s.monitoring.svc:9090` | Prometheus URL |
| `--dry-run` | `false` | Global dry-run mode |
| `--reconciliation-interval` | `5m` | Default reconciliation interval |

### RBAC Configuration

OptiPod requires the following permissions:

**Cluster-scoped**:
- Read: Deployments, StatefulSets, DaemonSets
- Update: Deployments, StatefulSets, DaemonSets (for resource patching)
- Read: Pods (for metrics collection)
- Create: Events (for notifications)

**Namespace-scoped**:
- Full access to OptimizationPolicy CRDs

The default installation includes all necessary RBAC resources. To restrict OptiPod to specific namespaces, modify the RoleBindings in `config/rbac/`.

### Metrics Provider Setup

#### Prometheus

1. Ensure Prometheus is deployed and accessible
2. Configure the Prometheus URL in the ConfigMap or via `--prometheus-url` flag
3. Verify connectivity:
```bash
kubectl exec -n optipod-system deployment/optipod-controller-manager -- \
  curl http://prometheus-k8s.monitoring.svc:9090/-/healthy
```

Required Prometheus metrics:
- `container_cpu_usage_seconds_total`
- `container_memory_working_set_bytes`

#### Metrics-Server

1. Install metrics-server:
```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

2. Configure OptiPod to use metrics-server:
```yaml
data:
  metrics-provider: "metrics-server"
```

3. Verify metrics-server:
```bash
kubectl top nodes
kubectl top pods
```

## Verification

### Check Operator Health

```bash
# Health check
kubectl get --raw /apis/v1/namespaces/optipod-system/services/optipod-controller-manager-metrics-service:8081/healthz

# Readiness check
kubectl get --raw /apis/v1/namespaces/optipod-system/services/optipod-controller-manager-metrics-service:8081/readyz
```

### Check Prometheus Metrics

```bash
kubectl port-forward -n optipod-system svc/optipod-controller-manager-metrics-service 8080:8080
curl http://localhost:8080/metrics | grep optipod
```

Expected metrics:
- `optipod_workloads_monitored`
- `optipod_workloads_updated`
- `optipod_reconciliation_duration_seconds`

### Create a Test Policy

```bash
cat <<EOF | kubectl apply -f -
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy
  namespace: default
spec:
  mode: Recommend
  selector:
    workloadSelector:
      matchLabels:
        app: test
  metricsConfig:
    provider: prometheus
    rollingWindow: 1h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
    memory:
      min: "128Mi"
      max: "4Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
EOF
```

Check policy status:
```bash
kubectl get optimizationpolicy test-policy -o yaml
kubectl describe optimizationpolicy test-policy
```

## Upgrading

### Upgrade Operator

```bash
# Update CRDs (if changed)
kubectl apply -f https://raw.githubusercontent.com/yourusername/optipod/main/config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml

# Update operator
kubectl apply -f https://raw.githubusercontent.com/yourusername/optipod/main/dist/install.yaml

# Verify upgrade
kubectl rollout status deployment/optipod-controller-manager -n optipod-system
```

### Rollback

```bash
# Rollback deployment
kubectl rollout undo deployment/optipod-controller-manager -n optipod-system

# Verify rollback
kubectl rollout status deployment/optipod-controller-manager -n optipod-system
```

## Uninstallation

### Remove Operator

```bash
kubectl delete -f https://raw.githubusercontent.com/yourusername/optipod/main/dist/install.yaml
```

### Remove CRDs

**Warning**: This will delete all OptimizationPolicy resources!

```bash
kubectl delete crd optimizationpolicies.optipod.optipod.io
```

### Clean Up Namespace

```bash
kubectl delete namespace optipod-system
```

## High Availability

For production deployments, consider:

1. **Leader Election**: Enabled by default, allows multiple replicas
2. **Resource Limits**: Adjust based on cluster size
3. **Pod Disruption Budget**: Ensure availability during updates

Example HA configuration:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: optipod-controller-manager
  namespace: optipod-system
spec:
  replicas: 3  # Multiple replicas with leader election
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  control-plane: controller-manager
              topologyKey: kubernetes.io/hostname
```

## Next Steps

- [Configure your first policy](CRD_REFERENCE.md)
- [Review example policies](EXAMPLES.md)
- [Set up metrics provider](METRICS.md)
- [Troubleshoot issues](TROUBLESHOOTING.md)
