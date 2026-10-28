# Prometheus Authentication Quick Start

Quick reference for configuring OptiPod with authenticated Prometheus.

## 🚀 Quick Setup

### 1. Create Secret

Choose your authentication method:

**Basic Auth:**
```bash
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password='your-password' \  # pragma: allowlist secret
  -n optipod-system
```

**Bearer Token:**
```bash
kubectl create secret generic prometheus-token \
  --from-literal=token='your-token' \
  -n optipod-system
```

**TLS Certificates:**
```bash
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=ca.pem \
  --from-file=tls.crt=client.pem \
  --from-file=tls.key=client-key.pem \
  -n optipod-system
```

### 2. Install OptiPod

**Basic Auth:**
```bash
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.existingSecret.name=prometheus-credentials \
  -n optipod-system
```

**Bearer Token:**
```bash
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=prometheus-token \
  -n optipod-system
```

**mTLS:**
```bash
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.tls.enabled=true \
  --set metricsProvider.prometheus.tls.existingSecret.name=prometheus-tls \
  -n optipod-system
```

### 3. Verify

```bash
# Check controller logs
kubectl logs -n optipod-system deployment/optipod-controller -f

# Look for successful Prometheus connection
# Should see: "Prometheus health check passed"
```

## 📝 Values File Method

Create `my-values.yaml`:

```yaml
metricsProvider:
  type: prometheus
  prometheus:
    url: "https://prometheus.example.com"
    auth:
      type: basic  # or "bearer"
      basic:
        existingSecret:
          name: prometheus-credentials
    tls:
      enabled: true
      existingSecret:
        name: prometheus-tls
    timeout: "30s"
```

Install:
```bash
helm install optipod ./charts/optipod -f my-values.yaml -n optipod-system
```

## 🔧 Helper Script

Use the provided script for easy secret creation:

```bash
# Basic auth
./scripts/create-prometheus-secrets.sh \
  --type basic \
  --username optipod \
  --password 'your-password'

# Bearer token
./scripts/create-prometheus-secrets.sh \
  --type bearer \
  --token 'your-token'

# TLS
./scripts/create-prometheus-secrets.sh \
  --type tls \
  --ca-file ca.pem \
  --cert-file client.pem \
  --key-file client-key.pem
```

## 🌩️ Cloud Providers

**AWS Managed Prometheus (AMP):**
```bash
# Get token
TOKEN=$(aws sts get-session-token --query 'Credentials.SessionToken' --output text)

# Create secret
kubectl create secret generic amp-token \
  --from-literal=token="$TOKEN" \
  -n optipod-system

# Install
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-xxx \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=amp-token \
  -n optipod-system
```

**GCP Managed Prometheus:**
```bash
# Get token
TOKEN=$(gcloud auth print-access-token)

# Create secret
kubectl create secret generic gcp-prometheus-token \
  --from-literal=token="$TOKEN" \
  -n optipod-system

# Install
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://monitoring.googleapis.com/v1/projects/PROJECT_ID/location/global/prometheus \
  --set metricsProvider.prometheus.auth.type=bearer \
  --set metricsProvider.prometheus.auth.bearer.existingSecret.name=gcp-prometheus-token \
  -n optipod-system
```

## 🐛 Troubleshooting

**Connection refused:**
```bash
# Test from within cluster
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v https://prometheus.example.com/api/v1/query?query=up
```

**401 Unauthorized:**
- Check credentials in secret
- Verify secret is mounted correctly
- Check controller logs for auth errors

**Certificate errors:**
- Verify CA certificate is correct
- Check certificate expiration
- Ensure certificate matches hostname

**View mounted secrets:**
```bash
# Check environment variables
kubectl exec -n optipod-system deployment/optipod-controller -- env | grep PROMETHEUS

# Check mounted files
kubectl exec -n optipod-system deployment/optipod-controller -- ls -la /etc/prometheus/tls/
```

## 📚 Full Documentation

For detailed information, see:
- [Prometheus Authentication Guide](PROMETHEUS_AUTHENTICATION.md)
- [Configuration Reference](../charts/optipod/CONFIGURATION.md)
- [Example Values](../charts/optipod/examples/prometheus-auth-values.yaml)

## ⚠️ Security Reminders

- ✅ Always use Kubernetes Secrets
- ✅ Enable TLS in production
- ✅ Rotate credentials regularly
- ❌ Never put credentials in values.yaml
- ❌ Never use command-line flags for passwords
- ❌ Never disable TLS verification in production
