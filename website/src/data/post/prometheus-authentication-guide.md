---
publishDate: 2025-01-28T00:00:00Z
title: 'Securing OptiPod: Prometheus Authentication Best Practices'
excerpt: 'Learn how to securely connect OptiPod to your Prometheus instance using basic auth, bearer tokens, or mTLS.'
image: https://images.unsplash.com/photo-1563986768609-322da13575f3?q=80&w=2940&auto=format&fit=crop
category: 'Security'
tags:
  - security
  - prometheus
  - authentication
  - mtls
author: 'OptiPod Team'
metadata:
  canonical: https://sagart-cactus.github.io/optipod/blog/prometheus-authentication-guide
---

Security is paramount when deploying Kubernetes operators. OptiPod needs to query Prometheus for metrics, and doing so securely is essential. In this guide, we'll cover three authentication methods and when to use each.

## Authentication Methods

OptiPod supports three authentication methods for Prometheus:

1. **Basic Authentication** - Username and password
2. **Bearer Token** - Token-based authentication
3. **mTLS** - Mutual TLS with client certificates

## Method 1: Basic Authentication

Basic auth is the simplest method and works well for most deployments.

### Setup

First, create a Kubernetes secret with your credentials:

```bash
kubectl create secret generic prometheus-auth \
  --from-literal=username=optipod \
  --from-literal=password=your-secure-password \
  -n optipod-system
```

Then configure OptiPod to use it:

```yaml
# values.yaml
prometheus:
  url: https://prometheus.example.com
  auth:
    type: basic
    secretName: prometheus-auth
    usernameKey: username
    passwordKey: password
```

Install with Helm:

```bash
helm install optipod optipod/optipod \
  -f values.yaml \
  -n optipod-system
```

### When to Use

- Internal Prometheus instances
- Development and testing environments
- When simplicity is preferred

## Method 2: Bearer Token

Bearer tokens provide better security than basic auth and integrate well with Kubernetes RBAC.

### Setup

Create a ServiceAccount and token for OptiPod:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: optipod-prometheus
  namespace: monitoring
---
apiVersion: v1
kind: Secret
metadata:
  name: optipod-prometheus-token
  namespace: monitoring
  annotations:
    kubernetes.io/service-account.name: optipod-prometheus
type: kubernetes.io/service-account-token
```

Grant permissions to query Prometheus:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus-reader
rules:
- apiGroups: [""]
  resources: ["services/proxy"]
  resourceNames: ["prometheus"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: optipod-prometheus-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: prometheus-reader
subjects:
- kind: ServiceAccount
  name: optipod-prometheus
  namespace: monitoring
```

Configure OptiPod:

```yaml
# values.yaml
prometheus:
  url: https://prometheus.example.com
  auth:
    type: bearer
    secretName: optipod-prometheus-token
    tokenKey: token
```

### When to Use

- Kubernetes-native Prometheus deployments
- When using Prometheus Operator
- Production environments with RBAC

## Method 3: Mutual TLS (mTLS)

mTLS provides the highest level of security with certificate-based authentication.

### Setup

Generate client certificates (or use your PKI):

```bash
# Generate client key
openssl genrsa -out client.key 2048

# Generate CSR
openssl req -new -key client.key -out client.csr \
  -subj "/CN=optipod/O=optipod-system"

# Sign with your CA (example with self-signed CA)
openssl x509 -req -in client.csr \
  -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt \
  -days 365
```

Create a secret with the certificates:

```bash
kubectl create secret generic prometheus-mtls \
  --from-file=ca.crt=ca.crt \
  --from-file=client.crt=client.crt \
  --from-file=client.key=client.key \
  -n optipod-system
```

Configure OptiPod:

```yaml
# values.yaml
prometheus:
  url: https://prometheus.example.com
  auth:
    type: mtls
    secretName: prometheus-mtls
    caCertKey: ca.crt
    clientCertKey: client.crt
    clientKeyKey: client.key
```

### When to Use

- Highly regulated environments
- Multi-tenant clusters
- When certificate-based auth is required by policy

## Security Best Practices

### 1. Use Kubernetes Secrets

Never hardcode credentials in values files or ConfigMaps:

```yaml
# ❌ Bad
prometheus:
  url: https://prometheus.example.com
  username: admin
  password: password123

# ✅ Good
prometheus:
  url: https://prometheus.example.com
  auth:
    type: basic
    secretName: prometheus-auth
```

### 2. Rotate Credentials Regularly

Set up a rotation schedule:

```bash
# Update secret
kubectl create secret generic prometheus-auth \
  --from-literal=username=optipod \
  --from-literal=password=new-secure-password \
  --dry-run=client -o yaml | \
  kubectl apply -f -

# Restart OptiPod to pick up new credentials
kubectl rollout restart deployment optipod-controller \
  -n optipod-system
```

### 3. Use Network Policies

Restrict network access to Prometheus:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: optipod-to-prometheus
  namespace: optipod-system
spec:
  podSelector:
    matchLabels:
      app: optipod
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: monitoring
    - podSelector:
        matchLabels:
          app: prometheus
    ports:
    - protocol: TCP
      port: 9090
```

### 4. Enable TLS

Always use HTTPS for Prometheus connections:

```yaml
prometheus:
  url: https://prometheus.example.com  # Not http://
  auth:
    type: bearer
    secretName: prometheus-token
  tls:
    insecureSkipVerify: false  # Verify certificates
```

### 5. Limit Permissions

Grant OptiPod only the permissions it needs:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-prometheus-reader
rules:
- nonResourceURLs: ["/api/v1/query", "/api/v1/query_range"]
  verbs: ["get"]
```

## Troubleshooting

### Authentication Failures

Check OptiPod logs:

```bash
kubectl logs -n optipod-system \
  deployment/optipod-controller \
  | grep prometheus
```

Common issues:

- **401 Unauthorized**: Wrong credentials or expired token
- **403 Forbidden**: Insufficient permissions
- **Certificate errors**: CA cert mismatch or expired certificates

### Testing Connectivity

Test Prometheus access from within the cluster:

```bash
# For basic auth
kubectl run -it --rm debug \
  --image=curlimages/curl \
  --restart=Never -- \
  curl -u username:password \
  https://prometheus.example.com/api/v1/query?query=up

# For bearer token
kubectl run -it --rm debug \
  --image=curlimages/curl \
  --restart=Never -- \
  curl -H "Authorization: Bearer $TOKEN" \
  https://prometheus.example.com/api/v1/query?query=up
```

## Conclusion

Securing your Prometheus connection is crucial for production deployments. Choose the authentication method that best fits your security requirements:

- **Basic Auth**: Simple and effective for most use cases
- **Bearer Token**: Kubernetes-native and integrates with RBAC
- **mTLS**: Maximum security for regulated environments

For detailed configuration examples, check our [Prometheus authentication documentation](https://sagart-cactus.github.io/optipod/docs/prometheus-authentication).

## Resources

- [Prometheus Authentication Docs](https://sagart-cactus.github.io/optipod/docs/prometheus-authentication)
- [Security Best Practices](https://sagart-cactus.github.io/optipod/docs/advanced/security)
- [Helm Values Reference](https://sagart-cactus.github.io/optipod/docs/reference/helm-values)
