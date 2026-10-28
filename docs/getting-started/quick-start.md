# Quick Start Guide

Get started with OptiPod in minutes. This guide walks you through installing OptiPod and creating your first optimization policy in safe Recommend mode.

## What You'll Learn

- Install OptiPod in your cluster
- Create a basic optimization policy
- Review recommendations on your workloads
- Understand the safety model

## Step 1: Install OptiPod

Install OptiPod using Helm (recommended):

```bash
helm install optipod oci://ghcr.io/sagart-cactus/charts/optipod \
  --namespace optipod-system \
  --create-namespace
```

Verify the installation:

```bash
# Check pods are running
kubectl get pods -n optipod-system

# Expected output:
# NAME                                    READY   STATUS    RESTARTS   AGE
# optipod-controller-manager-xxx          1/1     Running   0          1m
# optipod-webhook-xxx                     1/1     Running   0          1m
```

## Step 2: Create Your First Policy

Create a file named `my-first-policy.yaml`:

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: safe-recommendations
  namespace: default
spec:
  # Recommend mode: safe to try, no mutations
  mode: Recommend

  # Target workloads with the label optipod.io/enabled=true
  selector:
    workloadSelector:
      matchLabels:
        optipod.io/enabled: "true"

  # Metrics configuration
  metricsConfig:
    provider: metrics-server
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2

  # Resource bounds
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"

  # Update strategy (not used in Recommend mode)
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

Apply the policy:

```bash
kubectl apply -f my-first-policy.yaml
```

## Step 3: Label a Workload

Label an existing deployment to enable optimization:

```bash
# Label your deployment
kubectl label deployment my-app optipod.io/enabled=true

# Or create a new deployment with the label
kubectl create deployment nginx --image=nginx:latest
kubectl label deployment nginx optipod.io/enabled=true
```

## Step 4: Review Recommendations

Wait a few minutes for OptiPod to collect metrics and generate recommendations.

### Check Policy Status

```bash
kubectl describe optimizationpolicy safe-recommendations -n default
```

You'll see aggregate information like:

```yaml
Status:
  Workloads Discovered:  5
  Workloads Processed:   5
  Workloads By Type:
    Deployment:  5
  Last Reconciliation:  2025-01-28T10:30:00Z
```

### View Individual Workload Recommendations

Recommendations are stored as annotations on each workload:

```bash
# View recommendations for a specific deployment
kubectl get deployment nginx -o yaml | grep -A10 "optipod.io/recommendation"
```

You'll see annotations like:

```yaml
metadata:
  annotations:
    optipod.io/managed: "true"
    optipod.io/policy: "safe-recommendations"
    optipod.io/last-recommendation: "2025-01-28T10:30:00Z"
    optipod.io/recommendation.nginx.cpu-request: "250m"
    optipod.io/recommendation.nginx.memory-request: "512Mi"
```

## Step 5: Generate an Impact Report

Before switching to Auto mode, generate a report to see the potential impact:

```bash
# Download the report script
curl -fsSL https://raw.githubusercontent.com/Sagart-cactus/optipod/main/scripts/optipod-recommendation-report.sh -o optipod-recommendation-report.sh
chmod +x optipod-recommendation-report.sh

# Generate HTML report
./optipod-recommendation-report.sh -o html -f optipod-impact.html

# Open the report in your browser
open optipod-impact.html  # macOS
xdg-open optipod-impact.html  # Linux
```

The report shows:
- Total CPU and memory changes across all workloads
- Per-workload recommendations
- Warnings for potential issues
- Replica-weighted impact calculations

## Understanding the Safety Model

### Recommend Mode (Safe Default)

In Recommend mode:
- ✅ No workloads are modified
- ✅ No pods are restarted
- ✅ Recommendations are written as annotations
- ✅ Safe to try in production

### Auto Mode (Opt-In)

To enable automatic application of recommendations:

```yaml
spec:
  mode: Auto  # Change from Recommend to Auto
```

**Important**: Only switch to Auto mode after:
1. Reviewing recommendations in Recommend mode
2. Generating and reviewing the impact report
3. Testing in a non-production environment
4. Ensuring your resource bounds are appropriate

### Safety Features

OptiPod includes multiple safety mechanisms:

1. **Resource Bounds**: Min/max limits prevent extreme recommendations
2. **Safety Factor**: Adds a buffer (e.g., 1.2 = 20% above observed usage)
3. **Percentile-Based**: Uses P90/P95 instead of average to handle spikes
4. **Rolling Window**: Analyzes metrics over time (e.g., 24h) for stability
5. **Update Controls**: Configure what can be updated and when

## Next Steps

### Learn More About OptiPod

- [Creating Your First Policy](first-policy.md) - Detailed policy configuration guide
- [Architecture Overview](../concepts/architecture.md) - Understand how OptiPod works
- [Operational Modes](../concepts/modes.md) - Learn about Auto, Recommend, and Disabled modes

### Configure Advanced Features

- [Update Strategies](../concepts/update-strategies.md) - SSA vs Webhook strategies
- [Safety Model](../concepts/safety-model.md) - Understand safety guarantees
- [GitOps Integration](../guides/gitops-integration.md) - Use OptiPod with ArgoCD/Flux

### Troubleshooting

- [Troubleshooting Guide](../guides/troubleshooting.md) - Common issues and solutions
- [Webhook Troubleshooting](../guides/webhook-troubleshooting.md) - Webhook-specific issues

## Common Questions

### Why aren't recommendations appearing?

Recommendations require:
- Sufficient metrics data (default: 10 samples over rolling window)
- Workloads must be running and consuming resources
- Workloads must match the policy selector

Check controller logs:
```bash
kubectl logs -n optipod-system -l app.kubernetes.io/component=controller --tail=50
```

### How long does it take to see recommendations?

- **metrics-server**: 5-10 minutes (depends on sampling interval)
- **Prometheus**: Immediate (uses historical data)

### Can I use OptiPod with ArgoCD?

Yes! OptiPod is designed to be GitOps-safe:
- Use **webhook strategy** for ArgoCD compatibility
- Recommendations are stored in workload metadata (not spec)
- No sync conflicts with GitOps tools

See the [GitOps Integration Guide](../guides/gitops-integration.md) for details.

### What happens if I delete a policy?

- OptiPod stops processing workloads under that policy
- Existing recommendations remain as annotations
- No automatic rollback of applied changes

## Summary

You've successfully:
- ✅ Installed OptiPod
- ✅ Created an optimization policy in safe Recommend mode
- ✅ Labeled a workload for optimization
- ✅ Reviewed recommendations
- ✅ Generated an impact report

OptiPod is now monitoring your workloads and providing recommendations. When you're ready, you can switch to Auto mode to apply recommendations automatically.

---

*For production deployments, see the [Installation Guide](installation.md) for advanced configuration options.*
