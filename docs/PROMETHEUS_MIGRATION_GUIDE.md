# Prometheus Authentication Migration Guide

This guide helps you migrate from unsecured Prometheus to authenticated Prometheus in OptiPod.

## Overview

If you're currently using OptiPod with an unsecured Prometheus instance and need to add authentication, this guide will walk you through the migration process with zero downtime.

## Pre-Migration Checklist

- [ ] Identify your Prometheus authentication method (basic, bearer, mTLS)
- [ ] Gather credentials (username/password, token, or certificates)
- [ ] Verify Prometheus is accessible with new credentials
- [ ] Plan maintenance window (optional, but recommended)
- [ ] Backup current Helm values

## Migration Scenarios

### Scenario 1: Adding Basic Authentication

**Current State:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "http://prometheus:9090"
```

**Migration Steps:**

1. **Create the secret:**
```bash
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password='your-password' \  # pragma: allowlist secret
  -n optipod-system
```

2. **Update Helm values:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus:9090"  # Note: HTTPS now
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-credentials
```

3. **Upgrade OptiPod:**
```bash
helm upgrade optipod ./charts/optipod \
  -f updated-values.yaml \
  -n optipod-system
```

4. **Verify:**
```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
# Look for: "Prometheus health check passed"
```

### Scenario 2: Adding Bearer Token

**Current State:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "http://prometheus:9090"
```

**Migration Steps:**

1. **Create the secret:**
```bash
kubectl create secret generic prometheus-token \
  --from-literal=token='your-bearer-token' \
  -n optipod-system
```

2. **Update Helm values:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus:9090"
    auth:
      type: bearer
      bearer:
        existingSecret:
          name: prometheus-token
```

3. **Upgrade and verify** (same as Scenario 1)

### Scenario 3: Adding TLS/mTLS

**Current State:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "http://prometheus:9090"
```

**Migration Steps:**

1. **Create the TLS secret:**
```bash
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=ca.pem \
  --from-file=tls.crt=client.pem \
  --from-file=tls.key=client-key.pem \
  -n optipod-system
```

2. **Update Helm values:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus:9090"  # HTTPS required for TLS
    tls:
      enabled: true
      existingSecret:
        name: prometheus-tls
```

3. **Upgrade and verify** (same as Scenario 1)

### Scenario 4: Migrating to AWS Managed Prometheus

**Current State:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "http://prometheus:9090"
```

**Migration Steps:**

1. **Get AWS credentials:**
```bash
# Option 1: Use AWS STS token
TOKEN=$(aws sts get-session-token --query 'Credentials.SessionToken' --output text)

# Option 2: Use IRSA (recommended for production)
# Configure service account with IAM role annotation
```

2. **Create secret:**
```bash
kubectl create secret generic amp-token \
  --from-literal=token="$TOKEN" \
  -n optipod-system
```

3. **Update Helm values:**
```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-xxx"
    auth:
      type: bearer
      bearer:
        existingSecret:
          name: amp-token
    timeout: "60s"  # AMP may need longer timeout
```

4. **Upgrade and verify**

## Zero-Downtime Migration

To ensure zero downtime during migration:

### Option 1: Blue-Green Deployment

1. **Deploy new OptiPod instance with authentication:**
```bash
helm install optipod-new ./charts/optipod \
  -f new-values.yaml \
  -n optipod-system-new \
  --create-namespace
```

2. **Verify new instance works:**
```bash
kubectl logs -n optipod-system-new deployment/optipod-controller -f
```

3. **Switch traffic and delete old instance:**
```bash
helm uninstall optipod -n optipod-system
kubectl delete namespace optipod-system
```

### Option 2: Rolling Update (Recommended)

1. **Create secrets first:**
```bash
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password='your-password' \  # pragma: allowlist secret
  -n optipod-system
```

2. **Update Helm values and upgrade:**
```bash
helm upgrade optipod ./charts/optipod \
  -f updated-values.yaml \
  -n optipod-system
```

The controller will restart with new configuration automatically.

## Rollback Plan

If something goes wrong, you can quickly rollback:

### Quick Rollback

```bash
# Rollback to previous Helm release
helm rollback optipod -n optipod-system

# Or specify revision number
helm rollback optipod 1 -n optipod-system
```

### Manual Rollback

1. **Restore old values:**
```bash
helm upgrade optipod ./charts/optipod \
  -f old-values.yaml \
  -n optipod-system
```

2. **Delete secrets if needed:**
```bash
kubectl delete secret prometheus-credentials -n optipod-system
```

## Verification Steps

After migration, verify everything works:

### 1. Check Controller Logs

```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
```

Look for:
- ✅ `Prometheus health check passed`
- ✅ No authentication errors
- ✅ Metrics queries succeeding

### 2. Test Prometheus Connectivity

```bash
# From within the cluster
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v -u username:password https://prometheus:9090/api/v1/query?query=up
```

### 3. Verify Secrets are Mounted

```bash
# Check environment variables
kubectl exec -n optipod-system deployment/optipod-controller -- \
  env | grep PROMETHEUS

# Check mounted files (for TLS)
kubectl exec -n optipod-system deployment/optipod-controller -- \
  ls -la /etc/prometheus/tls/
```

### 4. Test Optimization Policy

Create a test policy to ensure metrics collection works:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-policy
  namespace: default
spec:
  targetWorkloads:
    - apiVersion: apps/v1
      kind: Deployment
      name: test-app
  metricsWindow: 5m
  updateMode: Auto
```

Check if recommendations are generated:
```bash
kubectl get optimizationpolicy test-policy -o yaml
```

## Common Issues and Solutions

### Issue: 401 Unauthorized

**Cause:** Invalid credentials

**Solution:**
```bash
# Verify secret contents
kubectl get secret prometheus-credentials -n optipod-system -o yaml

# Recreate secret with correct credentials
kubectl delete secret prometheus-credentials -n optipod-system
kubectl create secret generic prometheus-credentials \
  --from-literal=username=correct-username \
  --from-literal=password='correct-password' \  # pragma: allowlist secret
  -n optipod-system

# Restart controller
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

### Issue: Certificate Verification Failed

**Cause:** Invalid or missing CA certificate

**Solution:**
```bash
# Verify certificate
openssl x509 -in ca.pem -text -noout

# Recreate secret with correct CA
kubectl delete secret prometheus-tls -n optipod-system
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=correct-ca.pem \
  -n optipod-system

# Restart controller
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

### Issue: Connection Timeout

**Cause:** Prometheus not accessible or timeout too short

**Solution:**
```bash
# Increase timeout in values
helm upgrade optipod ./charts/optipod \
  --set metricsProvider.prometheus.timeout=60s \
  -n optipod-system
```

### Issue: Secret Not Found

**Cause:** Secret name mismatch or wrong namespace

**Solution:**
```bash
# List secrets
kubectl get secrets -n optipod-system

# Verify secret name in values matches actual secret
# Update Helm values with correct secret name
```

## Post-Migration Tasks

After successful migration:

1. **Update documentation** with new Prometheus URL and auth method
2. **Update runbooks** with new troubleshooting steps
3. **Set up credential rotation** schedule
4. **Monitor controller logs** for a few days
5. **Update disaster recovery** procedures
6. **Train team** on new authentication setup

## Credential Rotation

Set up regular credential rotation:

### Basic Auth Rotation

```bash
# Generate new password
NEW_PASSWORD=$(openssl rand -base64 32)

# Update Prometheus with new password
# (Prometheus-specific steps)

# Update Kubernetes secret
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password="$NEW_PASSWORD" \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart controller to pick up new credentials
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

### Bearer Token Rotation

```bash
# Get new token from your auth provider
NEW_TOKEN="..."

# Update secret
kubectl create secret generic prometheus-token \
  --from-literal=token="$NEW_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart controller
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

### Certificate Rotation

```bash
# Generate new certificates
# (Certificate generation steps)

# Update secret
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=new-ca.pem \
  --from-file=tls.crt=new-client.pem \
  --from-file=tls.key=new-client-key.pem \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart controller
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

## Support

If you encounter issues during migration:

1. Check [Troubleshooting Guide](PROMETHEUS_AUTHENTICATION.md#troubleshooting)
2. Review [Quick Start Guide](PROMETHEUS_QUICK_START.md)
3. Check controller logs for detailed error messages
4. Open an issue on GitHub with:
   - Current configuration (redact credentials!)
   - Error messages from logs
   - Steps to reproduce

## Related Documentation

- [Prometheus Authentication Guide](PROMETHEUS_AUTHENTICATION.md)
- [Quick Start Guide](PROMETHEUS_QUICK_START.md)
- [Configuration Reference](../charts/optipod/CONFIGURATION.md)
- [Example Values](../charts/optipod/examples/prometheus-auth-values.yaml)
