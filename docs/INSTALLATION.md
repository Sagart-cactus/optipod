# OptiPod Installation Guide

This guide provides detailed instructions for installing and configuring OptiPod in your Kubernetes cluster.

## Prerequisites

### Required

- **Kubernetes Cluster**: Version 1.29 or higher
- **kubectl**: Configured to access your cluster
- **Metrics Provider**: One of the following:
  - Kubernetes metrics-server (recommended for most use cases)
  - Prometheus (recommended for advanced monitoring setups)
  - Custom metrics provider (planned)

### Optional

- **In-Place Pod Resize**: Kubernetes 1.29+ with `InPlacePodVerticalScaling` feature gate enabled
- **Helm 3.8+**: For Helm-based installation (recommended)
- **cert-manager**: For automatic webhook certificate management (can be auto-installed via Helm)

## Installation Methods

### Method 1: Using Helm (Recommended)

Helm provides the easiest installation with automatic cert-manager detection and certificate management.

#### Quick Start with Auto cert-manager Detection

The chart automatically detects if cert-manager is installed. If not found, it will install cert-manager for you:

```bash
# Install from the OCI registry (recommended)
VERSION=<latest> # see https://github.com/Sagart-cactus/optipod/releases/latest
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version "${VERSION}" \
  --namespace optipod-system \
  --create-namespace
```

#### Install from Local Chart (Development)

```bash
# Navigate to the project root
cd /path/to/optipod

# Build Helm dependencies
helm dependency build charts/optipod

# Install the chart
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace
```

#### Production Installation with Custom Values

Create a custom values file for production:

```yaml
# production-values.yaml
webhook:
  enabled: true
  failurePolicy: Fail  # Stricter enforcement
  deployment:
    replicaCount: 3
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 512Mi

certManager:
  install: false  # Use existing production cert-manager

metrics:
  enabled: true
  serviceMonitor:
    enabled: true  # For Prometheus Operator

logging:
  level: info
  format: json
```

Install with custom values:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version "${VERSION}" \
  --namespace optipod-system \
  --create-namespace \
  --values production-values.yaml
```

#### Install SSA Mode Only (No Webhook)

For SSA-only deployments without webhook support:

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --version "${VERSION}" \
  --namespace optipod-system \
  --create-namespace \
  --set webhook.enabled=false
```

#### Verify Helm Installation

```bash
# Check installation status
helm status optipod -n optipod-system

# Check webhook deployment
kubectl get deployment optipod-webhook -n optipod-system

# Check certificate status
kubectl get certificate -n optipod-system

# Check cert-manager installation
kubectl get pods -n cert-manager
```

### Method 2: Using kubectl (Alternative)

OptiPod supports two deployment strategies:
- **Webhook Strategy** (GitOps-safe): Uses mutating webhooks for ArgoCD compatibility
- **SSA Strategy**: Uses Server Side Apply for direct Kubernetes API updates

#### Step 1: Choose Installation Mode

##### Option A: Webhook Mode (Recommended for ArgoCD/GitOps)

```bash
# Install with webhook support (GitOps-safe)
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install-webhook.yaml
```

Or install a specific version:

```bash
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/download/v1.0.0/install-webhook.yaml
```

##### Option B: SSA Mode (Traditional)

```bash
# Install with SSA support only
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install.yaml
```

##### Option C: Automated Installation Script

```bash
# Download and run installation script
curl -sSL https://raw.githubusercontent.com/Sagart-cactus/optipod/main/config/webhook/install.sh | bash

# Or with custom options
NAMESPACE=my-optipod WEBHOOK_MODE=enabled ./install.sh
```

#### Alternative: Install from Source

If you need to install from the main branch:

```bash
# Install CRDs
kubectl apply -f https://raw.githubusercontent.com/Sagart-cactus/optipod/main/config/crd/bases/optipod.optipod.io_optimizationpolicies.yaml

# Deploy with webhook support (recommended)
kubectl apply -k https://raw.githubusercontent.com/Sagart-cactus/optipod/main/config/webhook-enabled

# OR deploy with SSA only
kubectl apply -k https://raw.githubusercontent.com/Sagart-cactus/optipod/main/config/default
```

This creates:

- `optipod-system` namespace
- ServiceAccount, Roles, and RoleBindings
- Deployment for the operator
- ConfigMap for configuration
- Service for metrics
- **Webhook Mode Only**: Mutating webhook configuration and certificates

> **Strategy Selection**: Use webhook mode for GitOps compatibility. Use SSA mode only if you specifically need Server Side Apply functionality and have the required permissions.

> **Security Note**: All release images are signed with cosign and include SBOMs. You can verify signatures using:
>
> ```bash
> cosign verify ghcr.io/sagart-cactus/optipod:v1.0.0
> ```

#### Step 2: Verify Installation

Check that the operator is running:

```bash
kubectl get pods -n optipod-system
```

Expected output for **Webhook Mode**:

```text
NAME                                        READY   STATUS    RESTARTS   AGE
optipod-controller-manager-xxxxxxxxx-xxxxx   1/1     Running   0          30s
optipod-webhook-deployment-xxxxxxxxx-xxxxx   1/1     Running   0          30s
```

Expected output for **SSA Mode**:

```text
NAME                                        READY   STATUS    RESTARTS   AGE
optipod-controller-manager-xxxxxxxxx-xxxxx   1/1     Running   0          30s
```

Check operator logs:

```bash
kubectl logs -n optipod-system deployment/optipod-controller-manager -f
```

For webhook mode, also check webhook logs:

```bash
kubectl logs -n optipod-system deployment/optipod-webhook-deployment -f
```

#### Step 3: Verify Webhook Configuration (Webhook Mode Only)

Check webhook registration:

```bash
kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration
```

Verify webhook service:

```bash
kubectl get service optipod-webhook-service -n optipod-system
```

Check certificate status (with cert-manager):

```bash
kubectl get certificate serving-cert -n optipod-system
```

### Method 3: Using Kustomize

Clone the repository and customize:

```bash
git clone https://github.com/yourusername/optipod.git
cd optipod

# For webhook mode (recommended)
kubectl apply -k config/webhook-enabled

# For SSA mode only
kubectl apply -k config/default

# For custom configuration, edit kustomization files first
```

### Method 4: Building from Source

```bash
# Clone and build
git clone https://github.com/yourusername/optipod.git
cd optipod
make build

# Build Docker image
make docker-build IMG=your-registry/optipod:v1.0.0

# Push to registry
make docker-push IMG=your-registry/optipod:v1.0.0

# Deploy with webhook support
cd config/manager && kustomize edit set image controller=your-registry/optipod:v1.0.0
kubectl apply -k config/webhook-enabled

# OR deploy with SSA only
kubectl apply -k config/default
```

## Strategy Configuration

OptiPod supports two strategies for applying resource recommendations:

### Webhook Strategy (Default)

**Best for**: ArgoCD, GitOps workflows, environments without SSA permissions

The webhook strategy uses Kubernetes mutating admission webhooks to modify pod resource requests and limits during creation. This approach:

- ✅ Works with ArgoCD and GitOps tools out of the box
- ✅ Doesn't require Server Side Apply permissions
- ✅ Applies changes automatically during pod creation
- ✅ Supports gradual rollout strategies
- ❌ Requires webhook infrastructure and certificates
- ❌ Adds slight latency to pod creation

#### Webhook Strategy Configuration

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: webhook-policy
spec:
  mode: Auto
  updateStrategy:
    strategy: webhook                    # Use webhook strategy
    rolloutStrategy: onNextRestart      # Control when changes take effect
    # SSA-specific fields are ignored
  # ... other configuration
```

**Rollout Strategy Options**:
- `onNextRestart` (default): Apply changes when pods naturally restart
- `immediate`: Trigger rolling restart to apply changes immediately

### SSA Strategy (Traditional)

**Best for**: Direct Kubernetes API access, environments with full SSA permissions

The SSA strategy uses Kubernetes Server Side Apply to directly update workload resource specifications:

- ✅ Direct API updates with immediate effect
- ✅ No additional infrastructure required
- ✅ Lower latency for updates
- ❌ Requires Server Side Apply permissions
- ❌ May conflict with ArgoCD and GitOps tools
- ❌ Can cause resource ownership conflicts

#### SSA Strategy Configuration

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: ssa-policy
spec:
  mode: Auto
  updateStrategy:
    strategy: ssa                       # Use SSA strategy
    useServerSideApply: true           # Enable SSA features
    allowInPlaceResize: true           # Use in-place resize when available
    # Webhook-specific fields are ignored
  # ... other configuration
```

### Migration Between Strategies

#### From SSA to Webhook

1. **Install webhook components**:
   ```bash
   kubectl apply -k config/webhook-enabled/
   ```

2. **Update existing policies**:
   ```yaml
   spec:
     updateStrategy:
       strategy: webhook
       rolloutStrategy: onNextRestart
       # Remove SSA-specific fields
   ```

3. **Verify webhook operation** with test workloads

4. **Gradual migration** - update policies one by one

#### From Webhook to SSA

1. **Ensure SSA permissions** are available

2. **Update policies**:
   ```yaml
   spec:
     updateStrategy:
       strategy: ssa
       useServerSideApply: true
       # Remove webhook-specific fields
   ```

3. **Remove webhook components** (optional):
   ```bash
   kubectl delete -k config/webhook/
   ```

### Mixed Environments

OptiPod supports running both strategies simultaneously:

```yaml
# Policy 1: Webhook for ArgoCD-managed workloads
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: argocd-workloads
spec:
  updateStrategy:
    strategy: webhook
  selector:
    namespaceSelector:
      matchLabels:
        managed-by: argocd
---
# Policy 2: SSA for directly managed workloads
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: direct-workloads
spec:
  updateStrategy:
    strategy: ssa
  selector:
    namespaceSelector:
      matchLabels:
        managed-by: kubectl
```

## Configuration

### Operator Configuration

OptiPod supports **three configuration methods** with the following **precedence order**:

1. **Command-line flags** (highest priority) - Override all other settings
2. **Built-in defaults** - Hardcoded fallback values
3. **ConfigMap** - Currently defined but NOT actively read by the application
4. **Environment variables** - Currently NOT implemented

> **⚠️ Important Configuration Limitation**: The ConfigMap (`optipod-config`) is mounted in the deployment but is **NOT currently being read** by the application code. Only command-line flags and built-in defaults are used. ConfigMap integration is planned for a future release.

#### Command-Line Configuration (Current Method)

The primary way to configure OptiPod is through command-line flags in the deployment args. Edit the deployment in `config/manager/manager.yaml`:

```yaml
containers:
- command:
  - /manager
  args:
    - --leader-elect=true
    - --metrics-provider=metrics-server
    - --prometheus-url=http://prometheus-k8s.monitoring.svc:9090
    - --dry-run=false
    - --reconciliation-interval=1m
    - --health-probe-bind-address=:8081
    - --metrics-bind-address=:8080
```

Available command-line flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--leader-elect` | `false` | Enable leader election for HA |
| `--metrics-bind-address` | `:8080` | Metrics endpoint address |
| `--health-probe-bind-address` | `:8081` | Health probe address |
| `--metrics-provider` | `metrics-server` | **Global** metrics backend (metrics-server, prometheus, custom) |
| `--prometheus-url` | `http://prometheus:9090` | Prometheus URL (when using Prometheus) |
| `--dry-run` | `false` | Global dry-run mode |
| `--reconciliation-interval` | `5m` | Default reconciliation interval |
| `--metrics-server-sampling-interval` | `5m` | Background sampling cadence for metrics-server |
| `--metrics-server-max-samples-per-target` | `2880` | Cache size limits per target |
| `--metrics-server-min-samples-required` | `10` | Minimum samples before recommendations |
| `--metrics-server-target-ttl` | `15m` | Cache eviction TTL |
| `--enable-webhook` | `false` | Enable webhook server functionality |
| `--webhook-port` | `9443` | Webhook server port |
| `--webhook-cert-dir` | `/tmp/k8s-webhook-server/serving-certs` | Webhook certificate directory |

Apply configuration changes:

```bash
kubectl apply -k config/default
kubectl rollout restart deployment/optipod-controller-manager -n optipod-system
```

#### ConfigMap Configuration (Future)

The ConfigMap in `config/manager/config.yaml` is prepared for future use:

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
  metrics-provider: "metrics-server"
  
  # Prometheus URL (when using Prometheus provider)
  prometheus-url: "http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090"
  
  # Default reconciliation interval
  reconciliation-interval: "5m"
  
  # Enable leader election for HA
  leader-election: "true"
```

> **Note**: This ConfigMap is currently **not read** by the application. Use command-line flags instead.

> **⚠️ Important**: The `--metrics-provider` flag sets the metrics backend **globally** for all optimization policies. While individual policies have a `metricsConfig.provider` field, it must currently match the global setting. Per-policy provider selection is planned for a future release.

### Configuration Limitations & Future Plans

#### Current Limitations

- **No ConfigMap integration**: The ConfigMap is mounted but not read by the application
- **No environment variable support**: Environment variables are not processed
- **No dynamic configuration**: Changes require pod restart
- **Global metrics provider only**: Per-policy provider selection not yet available

#### Planned Enhancements (Q1 2025)

- **ConfigMap integration**: Dynamic loading of configuration from ConfigMap
- **Environment variable support**: Standard environment variable configuration
- **Hot-reload capability**: Configuration updates without pod restart
- **Configuration validation**: Better error reporting for invalid configurations
- **Per-policy metrics providers**: Override global provider per optimization policy

### RBAC Configuration

OptiPod requires the following permissions:

**Cluster-scoped**:

- Read: Deployments, StatefulSets, DaemonSets
- Update: Deployments, StatefulSets, DaemonSets (for resource patching)
- Read: Pods (for metrics collection)
- Create: Events (for notifications)

**Namespace-scoped**:

- Full access to OptimizationPolicy CRDs

The default installation includes all necessary RBAC resources. To restrict OptiPod to specific namespaces, modify the
RoleBindings in `config/rbac/`.

### Metrics Provider Setup

#### Prometheus

1. Ensure Prometheus is deployed and accessible in your cluster
2. Configure the Prometheus URL in the ConfigMap or via `--prometheus-url` flag
3. Verify connectivity:

```bash
kubectl exec -n optipod-system deployment/optipod-controller-manager -- \
  curl http://prometheus-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/-/healthy
```

Required Prometheus metrics:

- `container_cpu_usage_seconds_total`
- `container_memory_working_set_bytes`

#### Metrics-Server

1. Install metrics-server:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

1. Configure OptiPod to use metrics-server:

```yaml
data:
  metrics-provider: "metrics-server"
```

1. Verify metrics-server:

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
    provider: metrics-server  # Recommended
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
    strategy: webhook         # Use webhook strategy (default)
    rolloutStrategy: onNextRestart
EOF
```

Check policy status:

```bash
kubectl get optimizationpolicy test-policy -o yaml
kubectl describe optimizationpolicy test-policy
```

### Test Webhook Functionality (Webhook Mode Only)

Create a test deployment to verify webhook operation:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
  labels:
    app: test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
      annotations:
        optipod.io/webhook-enabled: "true"
        optipod.io/cpu-request.app: "200m"
        optipod.io/memory-request.app: "256Mi"
    spec:
      containers:
      - name: app
        image: nginx:alpine
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
EOF
```

Verify webhook mutation:

```bash
# Check if resources were modified by webhook
kubectl get pod -l app=test -o jsonpath='{.items[0].spec.containers[0].resources}'
```

Expected output should show modified resources based on annotations.

## Upgrading

### Upgrade Operator

```bash
# Upgrade to latest release
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install.yaml

# Or upgrade to specific version
kubectl apply -f https://github.com/Sagart-cactus/optipod/releases/download/v1.1.0/install.yaml

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
# Remove using the same version you installed
kubectl delete -f https://github.com/Sagart-cactus/optipod/releases/download/v1.0.0/install.yaml

# Or remove latest
kubectl delete -f https://github.com/Sagart-cactus/optipod/releases/latest/download/install.yaml
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
