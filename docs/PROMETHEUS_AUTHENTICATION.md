# Prometheus Authentication Guide

This guide explains how to configure OptiPod to connect to secured Prometheus instances using various authentication methods.

## Overview

OptiPod supports multiple authentication methods for connecting to Prometheus:

- **None**: No authentication (default, for unsecured Prometheus)
- **Basic Auth**: Username and password authentication
- **Bearer Token**: Token-based authentication (OAuth2/OIDC)
- **mTLS**: Mutual TLS with client certificates

## Configuration Methods

### 1. Basic Authentication

#### Using Kubernetes Secrets (Recommended)

```bash
# Create secret with credentials
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password='your-secure-password' \  # pragma: allowlist secret
  -n optipod-system

# Install with Helm
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.existingSecret.name=prometheus-credentials
```

#### Using values.yaml

```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus.example.com"
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-credentials
          usernameKey: username
          passwordKey: password
```

### 2. Bearer Token Authentication

#### Using Kubernetes Secrets (Recommended)

```bash
# Create secret with token
kubectl create secret generic prometheus-token \
  --from-literal=token='eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...' \
  -n optipod-system

# Install with Helm
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=prometheus-token \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.key=token
```

#### Using values.yaml

```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus.example.com"
    auth:
      type: bearer
      bearer:
        existingSecret:
          name: prometheus-token
          key: token
```

### 3. Mutual TLS (mTLS)

#### Using Kubernetes Secrets

```bash
# Create TLS secret with CA and client certificates
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=ca.pem \
  --from-file=tls.crt=client.pem \
  --from-file=tls.key=client-key.pem \
  -n optipod-system

# Install with Helm
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.tls.enabled=true \
  --set metricsProvider.prometheus.tls.existingSecret.name=prometheus-tls
```

#### Using values.yaml

```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus.example.com"
    tls:
      enabled: true
      existingSecret:
        name: prometheus-tls
        caKey: ca.crt
        certKey: tls.crt
        keyKey: tls.key
```

### 4. Combined: Basic Auth + TLS

```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus.example.com"
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-credentials
    tls:
      enabled: true
      existingSecret:
        name: prometheus-tls
```

## Command-Line Flags

You can also configure Prometheus authentication using command-line flags:

```bash
/manager \
  --metrics-provider=prometheus \
  --prometheus-url=https://prometheus.example.com \
  --prometheus-auth-type=basic \
  --prometheus-tls-ca-file=/etc/prometheus/tls/ca.crt \
  --prometheus-timeout=30s
```

### Available Flags

- `--prometheus-url`: Prometheus server URL
- `--prometheus-auth-type`: Authentication type (none, basic, bearer)
- `--prometheus-username`: Basic auth username (or use PROMETHEUS_USERNAME env)
- `--prometheus-password`: Basic auth password (or use PROMETHEUS_PASSWORD env)
- `--prometheus-bearer-token`: Bearer token (or use PROMETHEUS_BEARER_TOKEN env)
- `--prometheus-tls-ca-file`: TLS CA certificate file
- `--prometheus-tls-cert-file`: TLS client certificate file
- `--prometheus-tls-key-file`: TLS client key file
- `--prometheus-tls-insecure-skip-verify`: Skip TLS verification (not recommended)
- `--prometheus-timeout`: HTTP client timeout (default: 30s)

## Environment Variables

Credentials can be provided via environment variables (recommended over command-line flags):

- `PROMETHEUS_USERNAME`: Basic auth username
- `PROMETHEUS_PASSWORD`: Basic auth password
- `PROMETHEUS_BEARER_TOKEN`: Bearer token

## Security Best Practices

### ✅ DO

1. **Always use Kubernetes Secrets** for credentials
2. **Reference existing secrets** rather than creating inline values
3. **Enable TLS** for production Prometheus connections
4. **Use least-privilege** Prometheus users/tokens
5. **Rotate credentials** regularly
6. **Use RBAC** to restrict secret access

### ❌ DON'T

1. **Never put credentials in values.yaml** (they'll be in Git)
2. **Never use command-line flags** for passwords (visible in `ps`)
3. **Never log credentials** (even in debug mode)
4. **Never disable TLS verification** in production
5. **Never use default/weak passwords**

## Cloud Provider Examples

### AWS Managed Prometheus (AMP)

AWS Managed Prometheus uses SigV4 authentication. You'll need to use a bearer token from AWS STS:

```bash
# Get bearer token from AWS
TOKEN=$(aws sts get-session-token --query 'Credentials.SessionToken' --output text)

# Create secret
kubectl create secret generic amp-token \
  --from-literal=token="$TOKEN" \
  -n optipod-system

# Configure OptiPod
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-xxx \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=amp-token
```

### GCP Managed Prometheus

GCP Managed Prometheus uses OAuth2 tokens:

```bash
# Get OAuth2 token
TOKEN=$(gcloud auth print-access-token)

# Create secret
kubectl create secret generic gcp-prometheus-token \
  --from-literal=token="$TOKEN" \
  -n optipod-system

# Configure OptiPod
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://monitoring.googleapis.com/v1/projects/PROJECT_ID/location/global/prometheus \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=gcp-prometheus-token
```

## Troubleshooting

### Connection Failures

Check the controller logs for authentication errors:

```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
```

Common errors:

- `401 Unauthorized`: Invalid credentials
- `403 Forbidden`: Valid credentials but insufficient permissions
- `x509: certificate signed by unknown authority`: Missing or invalid CA certificate
- `connection refused`: Wrong URL or Prometheus not accessible

### Testing Connectivity

Test Prometheus connectivity from within the cluster:

```bash
# Create a test pod
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- sh

# Test without auth
curl -v http://prometheus:9090/api/v1/query?query=up

# Test with basic auth
curl -v -u username:password https://prometheus:9090/api/v1/query?query=up

# Test with bearer token
curl -v -H "Authorization: Bearer TOKEN" https://prometheus:9090/api/v1/query?query=up
```

### Verifying Secrets

Ensure secrets are correctly mounted:

```bash
# Check secret exists
kubectl get secret prometheus-credentials -n optipod-system

# Check environment variables in pod
kubectl exec -n optipod-system deployment/optipod-controller -- env | grep PROMETHEUS

# Check mounted files
kubectl exec -n optipod-system deployment/optipod-controller -- ls -la /etc/prometheus/tls/
```

## Migration from Unsecured Prometheus

If you're migrating from an unsecured Prometheus setup:

1. Create the necessary secrets
2. Update your Helm values or deployment
3. Restart the controller:

```bash
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

## Related Documentation

- [Installation Guide](INSTALLATION.md)
- [Configuration Reference](../charts/optipod/CONFIGURATION.md)
- [Prometheus Documentation](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)
