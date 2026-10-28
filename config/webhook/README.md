# OptipPod Webhook Configuration

This directory contains Kubernetes manifests for deploying the OptipPod mutating webhook. The webhook provides an alternative to Server Side Apply (SSA) for applying resource recommendations, making OptipPod compatible with ArgoCD and other GitOps tools.

## Components

### Core Components

- **`service.yaml`** - Kubernetes service for the webhook endpoint
- **`deployment.yaml`** - Webhook server deployment with high availability
- **`webhook-config.yaml`** - MutatingAdmissionWebhook configuration
- **`rbac.yaml`** - RBAC permissions for webhook operations

### Security & Configuration

- **`certificates.yaml`** - TLS certificate management (cert-manager and manual options)
- **`network-policy.yaml`** - Network security policies
- **`config.yaml`** - Webhook server configuration
- **`pdb.yaml`** - Pod Disruption Budget for high availability

### Installation

- **`install.sh`** - Automated installation script
- **`kustomization.yaml`** - Kustomize configuration

## Quick Start

### Prerequisites

1. Kubernetes cluster (v1.19+)
2. kubectl configured
3. (Optional) cert-manager for automatic certificate management

### Installation Options

#### Option 1: Automatic Installation (Recommended)

```bash
# Install with webhook enabled (default)
./config/webhook/install.sh

# Install in custom namespace
NAMESPACE=my-optipod ./config/webhook/install.sh

# Install without cert-manager (manual certificates)
CERT_MANAGER=manual ./config/webhook/install.sh
```

#### Option 2: Manual Installation with Kustomize

```bash
# With cert-manager
kubectl apply -k config/webhook-enabled/

# Without cert-manager (manual certificates required)
kubectl apply -k config/webhook/
```

#### Option 3: Traditional SSA Mode (No Webhook)

```bash
# Install without webhook support
WEBHOOK_MODE=disabled ./config/webhook/install.sh
```

## Configuration

### Webhook Strategy in OptimizationPolicy

```yaml
apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: my-policy
spec:
  mode: Auto
  updateStrategy:
    strategy: webhook              # Use webhook instead of SSA
    rolloutStrategy: onNextRestart # Control when changes take effect
  # ... other configuration
```

### Strategy Options

- **`strategy: webhook`** - Use mutating webhook (default, ArgoCD compatible)
- **`strategy: ssa`** - Use Server Side Apply (requires SSA permissions)

### Rollout Strategy Options

- **`rolloutStrategy: onNextRestart`** - Apply changes on next pod restart (default)
- **`rolloutStrategy: immediate`** - Trigger rolling restart immediately

## Certificate Management

### Option 1: cert-manager (Recommended)

Automatically manages certificate lifecycle:

```bash
# Install cert-manager first
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Then install OptipPod webhook
kubectl apply -k config/webhook-enabled/
```

### Option 2: Manual Certificates

Generate and manage certificates manually:

```bash
# Generate certificates
openssl req -x509 -newkey rsa:2048 -keyout tls.key -out tls.crt -days 365 -nodes \
  -subj "/CN=optipod-webhook-service.optipod-system.svc" \
  -addext "subjectAltName=DNS:optipod-webhook-service.optipod-system.svc,DNS:optipod-webhook-service.optipod-system.svc.cluster.local"

# Create secret
kubectl create secret tls webhook-server-certs \
  --cert=tls.crt --key=tls.key \
  --namespace=optipod-system
```

## Security Configuration

### Network Policies

The webhook includes network policies that:
- Allow webhook traffic from the API server
- Allow metrics scraping from monitoring namespace
- Allow health checks from any source
- Restrict egress to necessary services only

### RBAC Permissions

The webhook requires permissions for:
- Reading pods and OptimizationPolicies
- Updating workload annotations
- Managing webhook configurations
- Creating events for observability

### Failure Policy

The webhook is configured with `failurePolicy: Ignore` by default, meaning:
- Pod creation continues if webhook is unavailable
- Reduces risk of cluster disruption
- Change to `Fail` for stricter enforcement in production

## Monitoring and Observability

### Metrics

The webhook exposes metrics on port 8080:
- `optipod_webhook_admission_requests_total` - Total admission requests
- `optipod_webhook_admission_duration_seconds` - Request duration
- `optipod_webhook_mutations_total` - Total pod mutations
- `optipod_webhook_errors_total` - Total errors

### Health Checks

- **Liveness**: `/healthz` on port 8081
- **Readiness**: `/readyz` on port 8081

### Logging

Structured JSON logging with configurable levels:
- `debug` - Detailed operation logs
- `info` - Standard operation logs (default)
- `warn` - Warning conditions
- `error` - Error conditions only

## Troubleshooting

### Common Issues

#### 1. Webhook Not Receiving Requests

Check webhook configuration:
```bash
kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration -o yaml
```

Verify service and endpoints:
```bash
kubectl get service optipod-webhook-service -n optipod-system
kubectl get endpoints optipod-webhook-service -n optipod-system
```

#### 2. Certificate Issues

Check certificate status:
```bash
# With cert-manager
kubectl get certificate serving-cert -n optipod-system

# Manual certificates
kubectl get secret webhook-server-certs -n optipod-system
```

#### 3. Pod Mutations Not Applied

Check webhook logs:
```bash
kubectl logs -l control-plane=webhook-server -n optipod-system
```

Verify pod annotations:
```bash
kubectl get pod <pod-name> -o yaml | grep optipod.io
```

#### 4. High Availability Issues

Check pod disruption budget:
```bash
kubectl get pdb webhook-pdb -n optipod-system
```

Verify multiple replicas are running:
```bash
kubectl get deployment optipod-webhook-deployment -n optipod-system
```

### Debug Commands

```bash
# Check webhook server status
kubectl get deployment optipod-webhook-deployment -n optipod-system

# View webhook logs
kubectl logs -l control-plane=webhook-server -n optipod-system -f

# Test webhook connectivity
kubectl port-forward service/optipod-webhook-service 9443:443 -n optipod-system

# Check webhook configuration
kubectl get mutatingwebhookconfiguration -o yaml

# Verify RBAC permissions
kubectl auth can-i --list --as=system:serviceaccount:optipod-system:webhook-server
```

## Migration from SSA

To migrate existing OptimizationPolicies from SSA to webhook:

1. **Update policies** to use webhook strategy:
   ```yaml
   spec:
     updateStrategy:
       strategy: webhook
       rolloutStrategy: onNextRestart
   ```

2. **Deploy webhook** components:
   ```bash
   kubectl apply -k config/webhook-enabled/
   ```

3. **Verify operation** with test workloads

4. **Gradual rollout** - migrate policies one by one

## Performance Considerations

### Resource Requirements

- **CPU**: 50m request, 200m limit per replica
- **Memory**: 64Mi request, 256Mi limit per replica
- **Replicas**: 2 for high availability

### Scaling

The webhook can be scaled based on:
- Pod creation rate in the cluster
- Number of OptimizationPolicies
- Webhook response time requirements

Monitor `optipod_webhook_admission_duration_seconds` to determine if scaling is needed.

## Support

For issues and questions:
1. Check the troubleshooting section above
2. Review webhook logs for error messages
3. Consult the main OptipPod documentation
4. Open an issue in the OptipPod repository