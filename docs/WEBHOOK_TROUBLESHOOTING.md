# OptipPod Webhook Troubleshooting Guide

This guide helps diagnose and resolve common issues with OptipPod's mutating webhook functionality.

## Quick Diagnostics

### Check Webhook Status

```bash
# Check webhook deployment
kubectl get deployment optipod-webhook-deployment -n optipod-system

# Check webhook service
kubectl get service optipod-webhook-service -n optipod-system

# Check webhook configuration
kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration

# Check webhook logs
kubectl logs -l control-plane=webhook-server -n optipod-system --tail=50
```

### Verify Certificate Status

```bash
# With cert-manager
kubectl get certificate serving-cert -n optipod-system
kubectl describe certificate serving-cert -n optipod-system

# Manual certificates
kubectl get secret webhook-server-certs -n optipod-system
kubectl describe secret webhook-server-certs -n optipod-system
```

## Common Issues and Solutions

### 1. Webhook Not Receiving Requests

**Symptoms:**
- Pods are created but not modified by webhook
- No webhook logs for admission requests
- Webhook metrics show zero requests

**Diagnosis:**
```bash
# Check webhook configuration
kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration -o yaml

# Verify service endpoints
kubectl get endpoints optipod-webhook-service -n optipod-system

# Check if pods match webhook selectors
kubectl get pod <pod-name> -o yaml | grep -A 10 "annotations:"
```

**Solutions:**

1. **Check Object Selector**: Webhook only processes pods with `optipod.io/webhook-enabled: "true"` annotation:
   ```yaml
   metadata:
     annotations:
       optipod.io/webhook-enabled: "true"
   ```

2. **Verify Namespace Selector**: Ensure namespace is not excluded:
   ```bash
   kubectl label namespace <namespace> control-plane-
   ```

3. **Check Service Endpoints**:
   ```bash
   kubectl get endpoints optipod-webhook-service -n optipod-system
   # Should show webhook pod IPs
   ```

### 2. Certificate Issues

**Symptoms:**
- Webhook returns TLS handshake errors
- API server cannot connect to webhook
- Certificate validation failures in logs

**Diagnosis:**
```bash
# Check certificate validity
kubectl get secret webhook-server-certs -n optipod-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout

# Test webhook connectivity
kubectl port-forward service/optipod-webhook-service 9443:443 -n optipod-system
curl -k https://localhost:9443/mutate-v1-pod
```

**Solutions:**

1. **Regenerate Certificates** (manual mode):
   ```bash
   # Delete existing secret
   kubectl delete secret webhook-server-certs -n optipod-system
   
   # Run installation script to regenerate
   CERT_MANAGER=manual ./config/webhook/install.sh
   ```

2. **Fix cert-manager Issues**:
   ```bash
   # Check cert-manager status
   kubectl get pods -n cert-manager
   
   # Check certificate request
   kubectl get certificaterequest -n optipod-system
   kubectl describe certificaterequest -n optipod-system
   ```

3. **Update DNS Names** in certificate:
   ```yaml
   spec:
     dnsNames:
     - optipod-webhook-service.optipod-system.svc
     - optipod-webhook-service.optipod-system.svc.cluster.local
   ```

### 3. Pod Mutations Not Applied

**Symptoms:**
- Webhook receives requests but doesn't modify pods
- Pods created with original resource specifications
- No mutation patches in webhook logs

**Diagnosis:**
```bash
# Check webhook logs for mutation logic
kubectl logs -l control-plane=webhook-server -n optipod-system | grep -i mutation

# Verify pod annotations
kubectl get pod <pod-name> -o yaml | grep optipod.io

# Check OptimizationPolicy matching
kubectl get optimizationpolicy -A
```

**Solutions:**

1. **Verify Annotations Format**:
   ```yaml
   metadata:
     annotations:
       optipod.io/webhook-enabled: "true"
       optipod.io/cpu-request.container-name: "200m"
       optipod.io/memory-request.container-name: "256Mi"
   ```

2. **Check Policy Selector Matching**:
   ```bash
   kubectl describe optimizationpolicy <policy-name>
   # Verify selector matches pod labels
   ```

3. **Enable Debug Logging**:
   ```yaml
   # In webhook config
   data:
     webhook.yaml: |
       observability:
         logging:
           level: "debug"
   ```

### 4. High Latency or Timeouts

**Symptoms:**
- Pod creation takes longer than expected
- Webhook timeout errors in API server logs
- Admission controller timeouts

**Diagnosis:**
```bash
# Check webhook response times
kubectl logs -l control-plane=webhook-server -n optipod-system | grep duration

# Monitor webhook metrics
kubectl port-forward service/optipod-webhook-service 8080:8080 -n optipod-system
curl http://localhost:8080/metrics | grep webhook_duration
```

**Solutions:**

1. **Increase Timeout**:
   ```yaml
   # In webhook configuration
   webhooks:
   - name: pod-resource-mutator.optipod.io
     timeoutSeconds: 30  # Increase from default 10
   ```

2. **Scale Webhook Deployment**:
   ```bash
   kubectl scale deployment optipod-webhook-deployment --replicas=3 -n optipod-system
   ```

3. **Optimize Resource Limits**:
   ```yaml
   resources:
     limits:
       cpu: 500m      # Increase CPU
       memory: 512Mi  # Increase memory
   ```

### 5. Webhook Server Crashes

**Symptoms:**
- Webhook pods in CrashLoopBackOff state
- Webhook deployment shows 0/N ready replicas
- Error logs in webhook container

**Diagnosis:**
```bash
# Check pod status
kubectl get pods -l control-plane=webhook-server -n optipod-system

# Check crash logs
kubectl logs -l control-plane=webhook-server -n optipod-system --previous

# Check resource usage
kubectl top pods -l control-plane=webhook-server -n optipod-system
```

**Solutions:**

1. **Check Resource Limits**:
   ```yaml
   resources:
     limits:
       memory: 512Mi  # Increase if OOMKilled
   ```

2. **Fix Configuration Issues**:
   ```bash
   # Validate webhook config
   kubectl get configmap webhook-config -n optipod-system -o yaml
   ```

3. **Check Certificate Permissions**:
   ```bash
   # Ensure certificate secret is readable
   kubectl get secret webhook-server-certs -n optipod-system
   ```

### 6. ArgoCD Conflicts

**Symptoms:**
- ArgoCD shows resources as out-of-sync
- Resource ownership conflicts
- Continuous reconciliation loops

**Diagnosis:**
```bash
# Check ArgoCD application status
kubectl get application <app-name> -n argocd -o yaml

# Check resource ownership
kubectl get deployment <deployment-name> -o yaml | grep managedFields
```

**Solutions:**

1. **Use Webhook Strategy** (recommended):
   ```yaml
   spec:
     updateStrategy:
       strategy: webhook  # Avoids SSA conflicts
   ```

2. **Configure ArgoCD Ignore Differences**:
   ```yaml
   spec:
     ignoreDifferences:
     - group: apps
       kind: Deployment
       jsonPointers:
       - /spec/template/spec/containers/0/resources
   ```

3. **Use Annotation-Based Approach**:
   ```yaml
   # Let webhook handle resource modifications
   metadata:
     annotations:
       optipod.io/webhook-enabled: "true"
   ```

## Advanced Debugging

### Enable Verbose Logging

```yaml
# Update webhook config
apiVersion: v1
kind: ConfigMap
metadata:
  name: webhook-config
  namespace: optipod-system
data:
  webhook.yaml: |
    observability:
      logging:
        level: "debug"
        format: "json"
```

### Monitor Webhook Metrics

```bash
# Port forward to metrics endpoint
kubectl port-forward service/optipod-webhook-service 8080:8080 -n optipod-system

# Check webhook metrics
curl http://localhost:8080/metrics | grep optipod_webhook
```

Key metrics to monitor:
- `optipod_webhook_admission_requests_total`
- `optipod_webhook_admission_duration_seconds`
- `optipod_webhook_mutations_total`
- `optipod_webhook_errors_total`

### Test Webhook Manually

```bash
# Create test admission request
cat <<EOF > test-admission.json
{
  "apiVersion": "admission.k8s.io/v1",
  "kind": "AdmissionReview",
  "request": {
    "uid": "test-uid",
    "kind": {"group": "", "version": "v1", "kind": "Pod"},
    "resource": {"group": "", "version": "v1", "resource": "pods"},
    "object": {
      "apiVersion": "v1",
      "kind": "Pod",
      "metadata": {
        "name": "test-pod",
        "namespace": "default",
        "annotations": {
          "optipod.io/webhook-enabled": "true",
          "optipod.io/cpu-request.app": "200m"
        }
      },
      "spec": {
        "containers": [{
          "name": "app",
          "image": "nginx",
          "resources": {"requests": {"cpu": "100m"}}
        }]
      }
    }
  }
}
EOF

# Send test request
kubectl port-forward service/optipod-webhook-service 9443:443 -n optipod-system
curl -k -X POST https://localhost:9443/mutate-v1-pod \
  -H "Content-Type: application/json" \
  -d @test-admission.json
```

### Check Network Policies

```bash
# Verify network policy allows webhook traffic
kubectl get networkpolicy webhook-network-policy -n optipod-system -o yaml

# Test connectivity from API server
kubectl run test-pod --image=curlimages/curl --rm -it -- \
  curl -k https://optipod-webhook-service.optipod-system.svc:443/mutate-v1-pod
```

## Performance Tuning

### Webhook Server Optimization

```yaml
# Increase resources for high-traffic clusters
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi

# Scale replicas for high availability
replicas: 3

# Tune admission timeout
timeoutSeconds: 15
```

### Certificate Rotation

```yaml
# Configure automatic rotation with cert-manager
spec:
  duration: 8760h    # 1 year
  renewBefore: 720h  # 30 days before expiry
```

## Recovery Procedures

### Emergency Webhook Disable

If webhook is causing cluster issues:

```bash
# Disable webhook temporarily
kubectl patch mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration \
  --type='json' -p='[{"op": "replace", "path": "/webhooks/0/failurePolicy", "value": "Ignore"}]'

# Or delete webhook configuration entirely
kubectl delete mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration
```

### Restore from Backup

```bash
# Restore webhook configuration
kubectl apply -k config/webhook/

# Verify restoration
kubectl get mutatingwebhookconfiguration
kubectl get pods -l control-plane=webhook-server -n optipod-system
```

## Getting Help

If issues persist:

1. **Collect Diagnostics**:
   ```bash
   # Gather all relevant information
   kubectl get all -n optipod-system
   kubectl get mutatingwebhookconfiguration
   kubectl logs -l control-plane=webhook-server -n optipod-system --tail=100
   kubectl describe certificate serving-cert -n optipod-system
   ```

2. **Check Known Issues**: Review GitHub issues for similar problems

3. **Enable Debug Mode**: Set logging level to debug for detailed troubleshooting

4. **Contact Support**: Include diagnostic output when reporting issues