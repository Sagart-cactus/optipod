# Webhook Configuration

This guide covers the configuration and operation of OptiPod's mutating admission webhook, including TLS setup, cert-manager integration, high availability, and performance tuning.

## Overview

OptiPod's webhook provides runtime resource injection for pods. When enabled, the webhook:

1. Intercepts pod creation requests
2. Reads resource recommendations from workload annotations
3. Injects resource requests and limits into pod specifications
4. Allows the pod to be created with optimized resources

This approach is **GitOps-compatible** because:
- Recommendations are stored in workload metadata (not pod templates)
- ArgoCD/Flux manage the workload spec
- The webhook applies recommendations at pod creation time
- No conflicts with GitOps controllers

## When to Use the Webhook

Use the webhook strategy when:

- **Using GitOps**: ArgoCD or Flux manage your workloads
- **Immutable infrastructure**: Pod templates should not be modified
- **Gradual rollout**: Apply recommendations only to new pods
- **Compliance requirements**: Workload specs must match Git

Use Server-Side Apply (SSA) instead when:

- **Direct kubectl management**: No GitOps controller
- **Immediate updates**: Want to update existing pods
- **Simpler setup**: Don't want to manage webhook certificates

## Architecture

### Components

```
┌─────────────────┐
│  Kubernetes API │
│     Server      │
└────────┬────────┘
         │
         │ 1. Pod Create Request
         ▼
┌─────────────────┐
│  OptiPod        │
│  Webhook Server │
└────────┬────────┘
         │
         │ 2. Read Annotations
         ▼
┌─────────────────┐
│  Deployment     │
│  Metadata       │
│  (Annotations)  │
└─────────────────┘
         │
         │ 3. Inject Resources
         ▼
┌─────────────────┐
│  Pod with       │
│  Optimized      │
│  Resources      │
└─────────────────┘
```

### Webhook Flow

1. **Policy Reconciliation**: Controller generates recommendations and stores them in workload annotations
2. **Pod Creation**: User or controller creates a pod (e.g., Deployment rollout)
3. **Webhook Intercept**: Kubernetes API server calls OptiPod webhook
4. **Annotation Lookup**: Webhook reads recommendations from parent workload annotations
5. **Resource Injection**: Webhook generates JSON patches to modify pod resources
6. **Pod Admission**: Modified pod is created with optimized resources

## TLS Certificate Management

The webhook requires TLS certificates for secure communication with the Kubernetes API server. OptiPod supports two certificate management approaches:

### Option 1: cert-manager (Recommended)

cert-manager automatically generates and rotates certificates.

**Prerequisites**:
- cert-manager installed in the cluster

**Installation**:

```bash
# Install OptiPod with cert-manager
helm install optipod optipod/optipod \
  --set webhook.enabled=true \
  --set webhook.certManager.enabled=true \
  --namespace optipod-system \
  --create-namespace
```

**How it works**:

1. Helm creates a self-signed Issuer:
   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Issuer
   metadata:
     name: optipod-selfsigned-issuer
   spec:
     selfSigned: {}
   ```

2. Helm creates a Certificate resource:
   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata:
     name: serving-cert
   spec:
     secretName: webhook-server-certs
     dnsNames:
     - optipod-webhook-service.optipod-system.svc
     - optipod-webhook-service.optipod-system.svc.cluster.local
     issuerRef:
       name: optipod-selfsigned-issuer
     duration: 8760h  # 1 year
     renewBefore: 720h  # Renew 30 days before expiry
   ```

3. cert-manager generates the certificate and stores it in a Secret
4. cert-manager injects the CA bundle into the MutatingWebhookConfiguration
5. cert-manager automatically renews certificates before expiry

**Verification**:

```bash
# Check certificate status
kubectl get certificate -n optipod-system

# Check secret
kubectl get secret webhook-server-certs -n optipod-system

# Check webhook configuration has CA bundle
kubectl get mutatingwebhookconfiguration optipod-webhook -o yaml | grep caBundle
```

### Option 2: Manual Certificates

For clusters without cert-manager, use manually generated certificates.

**Generate certificates**:

```bash
# Create certificate directory
mkdir -p /tmp/optipod-certs
cd /tmp/optipod-certs

# Generate private key and certificate
openssl req -x509 -newkey rsa:2048 \
  -keyout tls.key -out tls.crt \
  -days 365 -nodes \
  -subj "/CN=optipod-webhook-service.optipod-system.svc" \
  -addext "subjectAltName=DNS:optipod-webhook-service.optipod-system.svc,DNS:optipod-webhook-service.optipod-system.svc.cluster.local"

# Create Kubernetes secret
kubectl create secret tls webhook-server-certs \
  --cert=tls.crt \
  --key=tls.key \
  -n optipod-system
```

**Install OptiPod**:

```bash
helm install optipod optipod/optipod \
  --set webhook.enabled=true \
  --set webhook.certManager.enabled=false \
  --namespace optipod-system \
  --create-namespace
```

**Update MutatingWebhookConfiguration**:

```bash
# Get CA bundle (base64 encoded certificate)
CA_BUNDLE=$(cat tls.crt | base64 -w 0)

# Patch webhook configuration
kubectl patch mutatingwebhookconfiguration optipod-webhook \
  --type='json' \
  -p="[{'op': 'replace', 'path': '/webhooks/0/clientConfig/caBundle', 'value':'${CA_BUNDLE}'}]"
```

**Certificate Rotation**:

Manual certificates must be rotated before expiry:

```bash
# Check certificate expiry
openssl x509 -in tls.crt -noout -enddate

# Rotate certificate (repeat generation steps)
# Update secret
kubectl create secret tls webhook-server-certs \
  --cert=tls.crt \
  --key=tls.key \
  -n optipod-system \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart webhook pods
kubectl rollout restart deployment optipod-webhook -n optipod-system
```

## Webhook Configuration

### Basic Configuration

Enable the webhook in Helm values:

```yaml
# values.yaml
webhook:
  enabled: true
  
  # Certificate management
  certManager:
    enabled: true  # Use cert-manager (recommended)
  
  # Replica count for high availability
  replicas: 2
  
  # Resource limits
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi
```

### Advanced Configuration

```yaml
# values.yaml
webhook:
  enabled: true
  
  # High availability
  replicas: 3
  
  # Pod disruption budget
  podDisruptionBudget:
    enabled: true
    minAvailable: 1
  
  # Affinity rules
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app.kubernetes.io/name: optipod-webhook
        topologyKey: kubernetes.io/hostname
  
  # Webhook server configuration
  port: 9443
  timeoutSeconds: 10
  failurePolicy: Ignore  # or "Fail" for strict enforcement
  
  # Namespace selector (exclude system namespaces)
  namespaceSelector:
    matchExpressions:
    - key: name
      operator: NotIn
      values: ["kube-system", "kube-public", "kube-node-lease"]
  
  # Object selector (only pods with webhook annotation)
  objectSelector:
    matchExpressions:
    - key: optipod.io/webhook-enabled
      operator: In
      values: ["true"]
  
  # TLS configuration
  tls:
    minVersion: "1.2"
    cipherSuites:
    - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
```

### Failure Policy

The `failurePolicy` determines webhook behavior when the webhook server is unavailable:

- **Ignore** (default): Allow pod creation even if webhook fails
  - Pros: No service disruption
  - Cons: Pods may be created without optimizations
  - Use for: Non-critical workloads, testing

- **Fail**: Block pod creation if webhook fails
  - Pros: Ensures all pods are optimized
  - Cons: Service disruption if webhook is down
  - Use for: Critical workloads, strict compliance

**Recommendation**: Start with `Ignore`, switch to `Fail` after validating webhook stability.

### Namespace and Object Selectors

Control which pods the webhook processes:

```yaml
# Exclude system namespaces
namespaceSelector:
  matchExpressions:
  - key: name
    operator: NotIn
    values: ["kube-system", "kube-public"]

# Only process pods with webhook annotation
objectSelector:
  matchExpressions:
  - key: optipod.io/webhook-enabled
    operator: In
    values: ["true"]
```

This ensures:
- System pods are not affected
- Only opted-in workloads are processed
- Reduced webhook load

## High Availability

### Multiple Replicas

Run multiple webhook replicas for availability:

```yaml
# values.yaml
webhook:
  replicas: 3  # Minimum 2 for HA
  
  podDisruptionBudget:
    enabled: true
    minAvailable: 1  # At least 1 replica always available
```

### Pod Anti-Affinity

Distribute replicas across nodes:

```yaml
# values.yaml
webhook:
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app.kubernetes.io/name: optipod-webhook
        topologyKey: kubernetes.io/hostname
```

### Health Checks

The webhook exposes health endpoints:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8081
  initialDelaySeconds: 15
  periodSeconds: 20

readinessProbe:
  httpGet:
    path: /readyz
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10
```

## Performance Tuning

### Resource Limits

Adjust based on cluster size:

```yaml
# Small clusters (< 100 pods/min)
webhook:
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi

# Large clusters (> 1000 pods/min)
webhook:
  resources:
    limits:
      cpu: 1000m
      memory: 512Mi
    requests:
      cpu: 500m
      memory: 256Mi
```

### Timeout Configuration

Balance between reliability and performance:

```yaml
# Fast timeout (low latency, may fail under load)
webhook:
  timeoutSeconds: 5

# Standard timeout (balanced)
webhook:
  timeoutSeconds: 10

# Long timeout (high reliability, higher latency)
webhook:
  timeoutSeconds: 30
```

### Connection Pooling

The webhook reuses Kubernetes API connections for efficiency. No configuration needed.

## Monitoring

### Metrics

The webhook exposes Prometheus metrics on port 8080:

```
# Admission requests
optipod_webhook_admission_requests_total{namespace, pod_name, dry_run}

# Admission success
optipod_webhook_admission_success_total{namespace, policy, patches_applied}

# Admission failures
optipod_webhook_admission_failures_total{namespace, failure_reason}

# Mutation duration
optipod_webhook_mutation_duration_seconds{namespace, policy}

# Policy matching duration
optipod_webhook_policy_matching_duration_seconds{namespace, policies_found}

# Server health
optipod_webhook_server_health_status{endpoint}

# Certificate expiry
optipod_webhook_certificate_expiry_timestamp_seconds{cert_type}
```

### Grafana Dashboard

Example queries:

```promql
# Admission request rate
rate(optipod_webhook_admission_requests_total[5m])

# Success rate
rate(optipod_webhook_admission_success_total[5m]) / 
rate(optipod_webhook_admission_requests_total[5m])

# P95 mutation latency
histogram_quantile(0.95, 
  rate(optipod_webhook_mutation_duration_seconds_bucket[5m]))

# Certificate expiry (days remaining)
(optipod_webhook_certificate_expiry_timestamp_seconds - time()) / 86400
```

### Alerts

```yaml
# Alert when webhook is down
- alert: OptiPodWebhookDown
  expr: optipod_webhook_server_health_status{endpoint="server"} == 0
  for: 5m
  annotations:
    summary: "OptiPod webhook server is down"

# Alert when certificate expires soon
- alert: OptiPodWebhookCertExpiring
  expr: (optipod_webhook_certificate_expiry_timestamp_seconds - time()) / 86400 < 30
  annotations:
    summary: "OptiPod webhook certificate expires in less than 30 days"

# Alert on high failure rate
- alert: OptiPodWebhookHighFailureRate
  expr: rate(optipod_webhook_admission_failures_total[5m]) > 0.1
  for: 10m
  annotations:
    summary: "OptiPod webhook has high failure rate"
```

## Troubleshooting

### Webhook Not Called

**Symptoms**: Pods created without resource modifications

**Diagnosis**:

```bash
# Check webhook configuration exists
kubectl get mutatingwebhookconfiguration optipod-webhook

# Check webhook service
kubectl get svc optipod-webhook-service -n optipod-system

# Check webhook pods
kubectl get pods -n optipod-system -l app.kubernetes.io/name=optipod-webhook
```

**Solutions**:

1. Verify webhook is enabled in Helm values
2. Check namespace and object selectors match your pods
3. Ensure workload has `optipod.io/webhook-enabled: "true"` annotation

### Certificate Errors

**Symptoms**: Webhook admission failures with TLS errors

**Diagnosis**:

```bash
# Check certificate secret
kubectl get secret webhook-server-certs -n optipod-system

# Check certificate validity
kubectl get certificate -n optipod-system

# Check webhook logs
kubectl logs -n optipod-system -l app.kubernetes.io/name=optipod-webhook
```

**Solutions**:

1. Verify cert-manager is installed and running
2. Check certificate is not expired
3. Verify CA bundle in MutatingWebhookConfiguration matches certificate
4. Restart webhook pods after certificate rotation

### High Latency

**Symptoms**: Slow pod creation times

**Diagnosis**:

```bash
# Check webhook metrics
kubectl port-forward -n optipod-system svc/optipod-webhook-service 8080:8080
curl http://localhost:8080/metrics | grep webhook_mutation_duration

# Check webhook resource usage
kubectl top pods -n optipod-system -l app.kubernetes.io/name=optipod-webhook
```

**Solutions**:

1. Increase webhook resource limits
2. Add more webhook replicas
3. Increase timeout in webhook configuration
4. Optimize policy selectors to reduce matching overhead

### Annotation Parsing Errors

**Symptoms**: Webhook fails to apply recommendations

**Diagnosis**:

```bash
# Check webhook logs
kubectl logs -n optipod-system -l app.kubernetes.io/name=optipod-webhook | grep "annotation"

# Check workload annotations
kubectl get deployment <name> -n <namespace> -o jsonpath='{.metadata.annotations}'
```

**Solutions**:

1. Verify annotation format matches expected pattern
2. Check resource quantity values are valid (e.g., "100m", "256Mi")
3. Ensure annotations are on workload metadata, not pod template

## Security Considerations

### Network Policies

Restrict webhook network access:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: optipod-webhook-netpol
  namespace: optipod-system
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: optipod-webhook
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 9443  # Webhook port
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # Kubernetes API
```

### Pod Security Standards

The webhook runs with restricted security context:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  seccompProfile:
    type: RuntimeDefault
```

### RBAC

The webhook requires minimal permissions:

```yaml
# Read workloads to find parent annotations
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
  verbs: ["get", "list"]

# Read policies
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies"]
  verbs: ["get", "list"]

# Read namespaces for selector matching
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
```

## Related Documentation

- [Update Strategies](/docs/concepts/update-strategies)
- [GitOps Integration](/docs/guides/gitops-integration)
- [Operations Guide](/docs/advanced/operations)
- [Troubleshooting](/docs/guides/troubleshooting)
