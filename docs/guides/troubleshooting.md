# Troubleshooting Guide

This guide helps diagnose and resolve common issues with OptiPod.

## Quick Diagnostics

### Check OptiPod Status

```bash
# Check controller deployment
kubectl get deployment -n optipod-system optipod-controller

# Check controller logs
kubectl logs -n optipod-system deployment/optipod-controller --tail=100

# Check webhook deployment (if enabled)
kubectl get deployment -n optipod-system optipod-webhook

# Check webhook logs
kubectl logs -n optipod-system deployment/optipod-webhook --tail=100
```

### Check Policy Status

```bash
# List all policies
kubectl get optimizationpolicy -A

# Get policy details
kubectl get optimizationpolicy <policy-name> -n optipod-system -o yaml

# Check policy conditions
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.status.conditions}' | jq
```

### Check Events

```bash
# All OptiPod events
kubectl get events -A --field-selector source=optipod

# Policy-specific events
kubectl get events -n optipod-system --field-selector involvedObject.name=<policy-name>

# Workload-specific events
kubectl get events -n <namespace> --field-selector involvedObject.name=<workload-name>
```

## Common Issues

### Issue 1: Policy Validation Failed

**Symptoms:**
- Policy status shows `ValidationFailed`
- Event: `Policy validation failed`
- Policy not processing workloads

**Possible Causes:**

1. **Missing required fields**
2. **Invalid resource bounds**
3. **Invalid selector configuration**
4. **Invalid metrics configuration**

**Diagnosis:**

```bash
# Check policy validation error
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'

# View policy spec
kubectl get optimizationpolicy <policy-name> -n optipod-system -o yaml
```

**Solutions:**

**Missing Selector:**
```yaml
# ❌ Invalid - no selector
spec:
  mode: Recommend
  metricsConfig:
    provider: prometheus

# ✅ Valid - has selector
spec:
  mode: Recommend
  selector:
    namespaceSelector:
      matchLabels:
        environment: production
  metricsConfig:
    provider: prometheus
```

**Invalid Resource Bounds:**
```yaml
# ❌ Invalid - min > max
spec:
  resourceBounds:
    cpu:
      min: "2000m"
      max: "1000m"  # Error: max < min

# ✅ Valid - min < max
spec:
  resourceBounds:
    cpu:
      min: "100m"
      max: "2000m"
```

**Invalid Metrics Config:**
```yaml
# ❌ Invalid - missing provider
spec:
  metricsConfig:
    rollingWindow: 24h

# ✅ Valid - has provider
spec:
  metricsConfig:
    provider: prometheus
    rollingWindow: 24h
```

### Issue 2: No Workloads Discovered

**Symptoms:**
- Policy status shows `workloadsDiscovered: 0`
- No recommendations generated
- No events for workload processing

**Possible Causes:**

1. **Selector doesn't match any workloads**
2. **Workloads in excluded namespaces**
3. **Workload types not included**
4. **RBAC permissions missing**

**Diagnosis:**

```bash
# Check policy selector
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.spec.selector}' | jq

# List workloads in target namespace
kubectl get deployments,statefulsets,daemonsets -n <namespace> --show-labels

# Check RBAC permissions
kubectl auth can-i list deployments \
  --as=system:serviceaccount:optipod-system:optipod-controller

kubectl auth can-i list statefulsets \
  --as=system:serviceaccount:optipod-system:optipod-controller

kubectl auth can-i list daemonsets \
  --as=system:serviceaccount:optipod-system:optipod-controller
```

**Solutions:**

**Fix Selector Mismatch:**
```bash
# Check workload labels
kubectl get deployment <name> -n <namespace> --show-labels

# Update policy selector to match
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"selector":{"workloadSelector":{"matchLabels":{"app":"<label-value>"}}}}}'
```

**Include Workload Types:**
```yaml
# Ensure workload types are included
spec:
  selector:
    workloadTypes:
      include:
        - Deployment
        - StatefulSet
        - DaemonSet
```

**Fix RBAC Permissions:**
```bash
# Check if RBAC role exists
kubectl get clusterrole optipod-controller-role

# Verify role binding
kubectl get clusterrolebinding optipod-controller-rolebinding

# If missing, reinstall OptiPod or apply RBAC manifests
kubectl apply -f config/rbac/
```

### Issue 3: Recommendations Not Applied (Auto Mode)

**Symptoms:**
- Policy in Auto mode
- Recommendations exist in annotations
- No actual resource changes
- Event: `UpdateFailed` or no events

**Possible Causes:**

1. **Global dry-run enabled**
2. **Update strategy misconfigured**
3. **RBAC permissions missing**
4. **Workload doesn't support updates**
5. **SSA conflicts**

**Diagnosis:**

```bash
# Check if dry-run is enabled
kubectl get deployment optipod-controller -n optipod-system \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="DRY_RUN")].value}'

# Check update strategy
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.spec.updateStrategy}' | jq

# Check RBAC for updates
kubectl auth can-i update deployments \
  --as=system:serviceaccount:optipod-system:optipod-controller

kubectl auth can-i patch deployments \
  --as=system:serviceaccount:optipod-system:optipod-controller

# Check for SSA conflicts
kubectl get deployment <name> -n <namespace> -o yaml | grep managedFields -A 20
```

**Solutions:**

**Disable Dry-Run:**
```bash
# Remove DRY_RUN environment variable
kubectl set env deployment/optipod-controller -n optipod-system DRY_RUN-

# Or set to false
kubectl set env deployment/optipod-controller -n optipod-system DRY_RUN=false
```

**Fix Update Strategy:**
```yaml
# Ensure update strategy is configured
spec:
  updateStrategy:
    strategy: ssa  # or webhook
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true
```

**Fix RBAC for Updates:**
```yaml
# Ensure ClusterRole has update/patch permissions
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-controller-role
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch", "update", "patch"]
```

**Resolve SSA Conflicts:**
```bash
# Check field managers
kubectl get deployment <name> -n <namespace> \
  -o jsonpath='{.metadata.managedFields[*].manager}' | tr ' ' '\n'

# If another manager owns fields, enable force in policy
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"updateStrategy":{"forceOwnership":true}}}'
```

### Issue 4: Metrics Collection Failed

**Symptoms:**
- Event: `MetricsCollectionFailed`
- No recommendations generated
- Controller logs show metrics errors

**Possible Causes:**

1. **Prometheus not accessible**
2. **Authentication failed**
3. **Workload has no metrics**
4. **Query timeout**

**Diagnosis:**

```bash
# Check Prometheus connectivity
kubectl get svc -n monitoring prometheus-operated

# Test Prometheus query
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -s "http://prometheus-operated.monitoring:9090/api/v1/query?query=up"

# Check OptiPod metrics config
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.spec.metricsConfig}' | jq

# Check controller logs for metrics errors
kubectl logs -n optipod-system deployment/optipod-controller | grep -i "metrics"
```

**Solutions:**

**Fix Prometheus URL:**
```yaml
# Update metrics config with correct URL
spec:
  metricsConfig:
    provider: prometheus
    prometheusURL: "http://prometheus-operated.monitoring:9090"
```

**Configure Authentication:**
```yaml
# For basic auth
spec:
  metricsConfig:
    provider: prometheus
    prometheusURL: "http://prometheus-operated.monitoring:9090"
    prometheusAuth:
      type: basic
      secretRef:
        name: prometheus-auth
        namespace: optipod-system

# Create secret
kubectl create secret generic prometheus-auth \
  -n optipod-system \
  --from-literal=username=admin \
  --from-literal=password=<password>
```

**Wait for Metrics:**
```bash
# Workloads need runtime data before metrics are available
# Check if workload has been running long enough
kubectl get pods -n <namespace> -l app=<workload>

# Verify metrics exist in Prometheus
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -s "http://prometheus-operated.monitoring:9090/api/v1/query?query=container_cpu_usage_seconds_total{pod=~\"<pod-name>.*\"}"
```

### Issue 5: Webhook Not Mutating Pods

**Symptoms:**
- Webhook deployed
- Pods created without OptiPod resources
- No webhook events

**Possible Causes:**

1. **Webhook not registered**
2. **Certificate issues**
3. **Webhook service not accessible**
4. **Namespace not labeled**
5. **No matching policies**

**Diagnosis:**

```bash
# Check webhook configuration
kubectl get mutatingwebhookconfiguration optipod-webhook

# Check webhook service
kubectl get svc -n optipod-system optipod-webhook

# Check webhook endpoints
kubectl get endpoints -n optipod-system optipod-webhook

# Check webhook logs
kubectl logs -n optipod-system deployment/optipod-webhook

# Check certificate
kubectl get secret -n optipod-system optipod-webhook-cert

# Test webhook health
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -k https://optipod-webhook.optipod-system.svc:443/health
```

**Solutions:**

**Fix Webhook Registration:**
```bash
# Verify webhook configuration exists
kubectl get mutatingwebhookconfiguration optipod-webhook -o yaml

# If missing, reinstall webhook
helm upgrade optipod optipod/optipod \
  --set webhook.enabled=true \
  --reuse-values
```

**Fix Certificate Issues:**
```bash
# Check cert-manager is installed
kubectl get pods -n cert-manager

# Check certificate status
kubectl get certificate -n optipod-system optipod-webhook-cert

# If certificate not ready, check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager

# Force certificate renewal
kubectl delete certificate -n optipod-system optipod-webhook-cert
# Certificate will be recreated automatically
```

**Label Namespace for Webhook:**
```bash
# Webhook only processes labeled namespaces
kubectl label namespace <namespace> optipod.io/webhook=enabled

# Or enable for all namespaces (not recommended)
kubectl patch mutatingwebhookconfiguration optipod-webhook \
  --type json \
  -p '[{"op":"remove","path":"/webhooks/0/namespaceSelector"}]'
```

**Verify Policy Matches:**
```bash
# Check if policy selector matches workload
kubectl get optimizationpolicy -A

# Ensure workload has recommendations
kubectl get deployment <name> -n <namespace> \
  -o jsonpath='{.metadata.annotations}' | jq | grep optipod.io
```

### Issue 6: Performance Degradation After Optimization

**Symptoms:**
- Increased latency
- CPU throttling
- OOMKills
- Pod restarts

**Possible Causes:**

1. **Safety factor too low**
2. **Percentile too low**
3. **Metrics window too short**
4. **Workload behavior changed**
5. **Memory decreased too aggressively**

**Diagnosis:**

```bash
# Check for OOMKills
kubectl get events -n <namespace> --field-selector reason=OOMKilled

# Check CPU throttling
kubectl top pods -n <namespace>

# Check pod restarts
kubectl get pods -n <namespace> \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.containerStatuses[0].restartCount}{"\n"}{end}'

# Check current resources vs recommendations
kubectl get deployment <name> -n <namespace> -o yaml | grep -A 10 resources

# Check policy settings
kubectl get optimizationpolicy <policy-name> -n optipod-system \
  -o jsonpath='{.spec.metricsConfig}' | jq
```

**Solutions:**

**Emergency: Revert to Recommend Mode:**
```bash
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"mode":"Recommend"}}'
```

**Increase Safety Factor:**
```bash
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"metricsConfig":{"safetyFactor":1.5}}}'
```

**Use Higher Percentile:**
```bash
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"metricsConfig":{"percentile":"P95"}}}'
```

**Increase Rolling Window:**
```bash
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"metricsConfig":{"rollingWindow":"48h"}}}'
```

**Enable Gradual Memory Decrease (Not Yet Implemented):**

> **⚠️ Note**: This configuration is accepted but not yet implemented.

```bash
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"updateStrategy":{"gradualDecreaseConfig":{"enabled":true,"memoryDecreasePercentage":10}}}}'
```

**Manually Restore Resources:**
```bash
# Restore previous resource values
kubectl patch deployment <name> -n <namespace> \
  --type strategic \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"<container>","resources":{"requests":{"cpu":"1000m","memory":"2Gi"}}}]}}}}'
```

### Issue 7: Controller Crash Loop

**Symptoms:**
- Controller pod restarting
- CrashLoopBackOff status
- Controller logs show errors

**Possible Causes:**

1. **Invalid configuration**
2. **RBAC permissions missing**
3. **Resource limits too low**
4. **Metrics provider unreachable**

**Diagnosis:**

```bash
# Check pod status
kubectl get pods -n optipod-system -l app=optipod-controller

# Check pod events
kubectl describe pod -n optipod-system -l app=optipod-controller

# Check logs from previous crash
kubectl logs -n optipod-system -l app=optipod-controller --previous

# Check resource usage
kubectl top pod -n optipod-system -l app=optipod-controller
```

**Solutions:**

**Check Configuration:**
```bash
# View controller configuration
kubectl get deployment optipod-controller -n optipod-system -o yaml

# Check for invalid environment variables
kubectl get deployment optipod-controller -n optipod-system \
  -o jsonpath='{.spec.template.spec.containers[0].env}' | jq
```

**Increase Resource Limits:**
```bash
kubectl patch deployment optipod-controller -n optipod-system \
  --type strategic \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"controller","resources":{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}}]}}}}'
```

**Fix RBAC:**
```bash
# Reinstall RBAC
kubectl apply -f https://raw.githubusercontent.com/optipod/optipod/main/config/rbac/
```

### Issue 8: High Controller CPU/Memory Usage

**Symptoms:**
- Controller using excessive resources
- Slow reconciliation
- Cluster performance impact

**Possible Causes:**

1. **Too many workloads**
2. **Reconciliation interval too short**
3. **Metrics queries too frequent**
4. **Memory leak**

**Diagnosis:**

```bash
# Check resource usage
kubectl top pod -n optipod-system -l app=optipod-controller

# Check number of policies and workloads
kubectl get optimizationpolicy -A
kubectl get deployments,statefulsets,daemonsets -A | wc -l

# Check reconciliation frequency
kubectl logs -n optipod-system deployment/optipod-controller | grep "reconciliation"
```

**Solutions:**

**Increase Reconciliation Interval:**
```bash
# Update all policies to reconcile less frequently
kubectl get optimizationpolicy -A -o name | xargs -I {} \
  kubectl patch {} --type merge \
  --patch '{"spec":{"reconciliationInterval":"10m"}}'
```

**Increase Controller Resources:**
```bash
kubectl patch deployment optipod-controller -n optipod-system \
  --type strategic \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"controller","resources":{"limits":{"cpu":"1000m","memory":"1Gi"},"requests":{"cpu":"200m","memory":"256Mi"}}}]}}}}'
```

**Reduce Workload Scope:**
```bash
# Use more specific selectors to reduce workload count
kubectl patch optimizationpolicy <policy-name> -n optipod-system \
  --type merge \
  --patch '{"spec":{"selector":{"namespaceSelector":{"matchLabels":{"optimize":"true"}}}}}'
```

## Diagnostic Commands Reference

### Controller Diagnostics

```bash
# Controller status
kubectl get deployment -n optipod-system optipod-controller
kubectl get pods -n optipod-system -l app=optipod-controller

# Controller logs (last 100 lines)
kubectl logs -n optipod-system deployment/optipod-controller --tail=100

# Controller logs (follow)
kubectl logs -n optipod-system deployment/optipod-controller -f

# Controller logs (previous crash)
kubectl logs -n optipod-system -l app=optipod-controller --previous

# Controller resource usage
kubectl top pod -n optipod-system -l app=optipod-controller

# Controller configuration
kubectl get deployment optipod-controller -n optipod-system -o yaml
```

### Policy Diagnostics

```bash
# List all policies
kubectl get optimizationpolicy -A

# Policy details
kubectl get optimizationpolicy <name> -n optipod-system -o yaml

# Policy status
kubectl get optimizationpolicy <name> -n optipod-system \
  -o jsonpath='{.status}' | jq

# Policy conditions
kubectl get optimizationpolicy <name> -n optipod-system \
  -o jsonpath='{.status.conditions}' | jq

# Policy events
kubectl get events -n optipod-system --field-selector involvedObject.name=<name>
```

### Workload Diagnostics

```bash
# Workload annotations
kubectl get deployment <name> -n <namespace> \
  -o jsonpath='{.metadata.annotations}' | jq | grep optipod.io

# Workload resources
kubectl get deployment <name> -n <namespace> \
  -o jsonpath='{.spec.template.spec.containers[*].resources}' | jq

# Workload events
kubectl get events -n <namespace> --field-selector involvedObject.name=<name>

# Pod status
kubectl get pods -n <namespace> -l app=<name>

# Pod resource usage
kubectl top pods -n <namespace> -l app=<name>
```

### Webhook Diagnostics

```bash
# Webhook status
kubectl get deployment -n optipod-system optipod-webhook
kubectl get pods -n optipod-system -l app=optipod-webhook

# Webhook logs
kubectl logs -n optipod-system deployment/optipod-webhook --tail=100

# Webhook configuration
kubectl get mutatingwebhookconfiguration optipod-webhook -o yaml

# Webhook service
kubectl get svc -n optipod-system optipod-webhook

# Webhook certificate
kubectl get certificate -n optipod-system optipod-webhook-cert
kubectl get secret -n optipod-system optipod-webhook-cert

# Test webhook health
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -k https://optipod-webhook.optipod-system.svc:443/health
```

### Metrics Diagnostics

```bash
# Test Prometheus connectivity
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -s "http://prometheus-operated.monitoring:9090/api/v1/query?query=up"

# Query workload metrics
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -s "http://prometheus-operated.monitoring:9090/api/v1/query?query=container_cpu_usage_seconds_total{pod=~\"<pod-name>.*\"}"

# Check metrics-server (if using)
kubectl top nodes
kubectl top pods -A
```

## Getting Help

If you're still experiencing issues:

1. **Check GitHub Issues**: [github.com/optipod/optipod/issues](https://github.com/optipod/optipod/issues)
2. **Review Documentation**: [optipod.io/docs](https://optipod.io/docs)
3. **Join Community**: Slack/Discord links in README
4. **File a Bug Report**: Include:
   - OptiPod version
   - Kubernetes version
   - Policy configuration
   - Controller logs
   - Relevant events
   - Steps to reproduce

## Next Steps

- [Creating Policies](creating-policies.md) - Policy configuration guide
- [Reviewing Recommendations](reviewing-recs.md) - Understanding recommendations
- [Switching to Auto Mode](switching-to-auto.md) - Enabling automatic optimization
- [Safety Model](../concepts/safety-model.md) - Understanding safety guarantees
- [Operations](../advanced/operations.md) - Operational procedures
