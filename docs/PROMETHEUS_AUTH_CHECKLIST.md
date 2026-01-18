# Prometheus Authentication Setup Checklist

Use this checklist to ensure your Prometheus authentication is properly configured.

## Pre-Installation Checklist

### 1. Prometheus Access
- [ ] Prometheus URL is accessible from Kubernetes cluster
- [ ] Prometheus requires authentication (basic, bearer, or mTLS)
- [ ] You have valid credentials for Prometheus
- [ ] Prometheus version is compatible (2.x or later)

### 2. Credentials Gathered
- [ ] **For Basic Auth:** Username and password
- [ ] **For Bearer Token:** Valid token (not expired)
- [ ] **For mTLS:** CA certificate, client certificate, and client key
- [ ] Credentials have been tested manually (curl/browser)

### 3. Kubernetes Environment
- [ ] Kubernetes cluster is running (1.19+)
- [ ] kubectl is configured and working
- [ ] Helm 3 is installed
- [ ] Target namespace exists or will be created

## Installation Checklist

### 4. Secret Creation
- [ ] Kubernetes secret created with correct name
- [ ] Secret is in the correct namespace (optipod-system)
- [ ] Secret contains all required keys:
  - **Basic Auth:** `username` and `password`
  - **Bearer Token:** `token`
  - **mTLS:** `ca.crt`, `tls.crt`, `tls.key`
- [ ] Secret verified: `kubectl get secret <name> -n optipod-system`

### 5. Helm Values Configuration
- [ ] `metricsProvider.type` set to `prometheus`
- [ ] `metricsProvider.prometheus.url` is correct (https:// for TLS)
- [ ] `metricsProvider.prometheus.auth.type` matches your method
- [ ] Secret reference is correct in values
- [ ] TLS configuration is correct (if using TLS)
- [ ] Timeout is appropriate for your environment

### 6. OptiPod Installation
- [ ] Helm chart installed successfully
- [ ] Controller pod is running
- [ ] No CrashLoopBackOff or ImagePullBackOff
- [ ] Controller logs show successful startup

## Post-Installation Verification

### 7. Authentication Verification
- [ ] Controller logs show "Prometheus health check passed"
- [ ] No authentication errors in logs (401, 403)
- [ ] No TLS errors in logs (certificate errors)
- [ ] Environment variables are set correctly (check with kubectl exec)
- [ ] TLS certificates are mounted (if using mTLS)

### 8. Functional Testing
- [ ] Create a test OptimizationPolicy
- [ ] Policy status shows metrics are being collected
- [ ] Recommendations are generated
- [ ] No errors in policy status
- [ ] Metrics queries are succeeding

### 9. Security Verification
- [ ] Credentials are NOT in values.yaml
- [ ] Credentials are NOT in command-line arguments
- [ ] Credentials are NOT in logs
- [ ] TLS verification is enabled (not skipped)
- [ ] Secrets have appropriate RBAC restrictions

## Detailed Verification Steps

### Check Controller Logs
```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
```

**Look for:**
- ✅ `"Prometheus health check passed"`
- ✅ `"Successfully queried Prometheus"`
- ❌ `"401 Unauthorized"`
- ❌ `"403 Forbidden"`
- ❌ `"certificate verify failed"`

### Verify Secret Mounting
```bash
# Check environment variables
kubectl exec -n optipod-system deployment/optipod-controller -- env | grep PROMETHEUS

# Expected output for basic auth:
# PROMETHEUS_USERNAME=optipod
# PROMETHEUS_PASSWORD=***

# Check mounted files (for TLS)
kubectl exec -n optipod-system deployment/optipod-controller -- ls -la /etc/prometheus/tls/

# Expected output:
# ca.crt
# tls.crt
# tls.key
```

### Test Prometheus Connectivity
```bash
# From within cluster
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v -u username:password https://prometheus:9090/api/v1/query?query=up

# Should return 200 OK with Prometheus response
```

### Verify Policy Functionality
```bash
# Create test policy
cat <<EOF | kubectl apply -f -
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: test-auth
  namespace: default
spec:
  targetWorkloads:
    - apiVersion: apps/v1
      kind: Deployment
      name: test-app
  metricsWindow: 5m
  updateMode: Auto
EOF

# Check policy status
kubectl get optimizationpolicy test-auth -o yaml

# Look for:
# - status.conditions with type: Ready
# - status.recommendations populated
# - No error messages
```

## Troubleshooting Checklist

### If Authentication Fails

- [ ] Verify secret exists: `kubectl get secret <name> -n optipod-system`
- [ ] Check secret contents: `kubectl get secret <name> -n optipod-system -o yaml`
- [ ] Verify credentials work manually (curl test)
- [ ] Check controller logs for specific error
- [ ] Verify secret name matches Helm values
- [ ] Ensure secret is in correct namespace
- [ ] Check for typos in username/password/token

### If TLS Fails

- [ ] Verify CA certificate is correct
- [ ] Check certificate expiration: `openssl x509 -in ca.crt -text -noout`
- [ ] Verify certificate matches hostname
- [ ] Check if Prometheus requires client certificates
- [ ] Verify certificate files are readable
- [ ] Check for certificate chain issues

### If Connection Fails

- [ ] Verify Prometheus URL is correct
- [ ] Test connectivity from within cluster
- [ ] Check network policies
- [ ] Verify DNS resolution
- [ ] Check firewall rules
- [ ] Increase timeout if needed

## Security Audit Checklist

### Credential Security
- [ ] Secrets are not committed to Git
- [ ] Secrets are not in Helm values files
- [ ] Secrets are not in command-line history
- [ ] Secrets are not in logs
- [ ] Secrets have appropriate RBAC
- [ ] Secrets are encrypted at rest (if required)

### TLS Security
- [ ] TLS is enabled for production
- [ ] Certificate verification is NOT skipped
- [ ] Certificates are from trusted CA
- [ ] Certificates are not expired
- [ ] Private keys are protected
- [ ] Certificate rotation is planned

### Access Control
- [ ] Prometheus user has minimal required permissions
- [ ] Service account has minimal RBAC
- [ ] Network policies restrict access
- [ ] Secrets are namespace-scoped
- [ ] Audit logging is enabled

## Maintenance Checklist

### Regular Tasks
- [ ] Monitor controller logs for auth failures
- [ ] Check certificate expiration dates
- [ ] Rotate credentials on schedule
- [ ] Update documentation with changes
- [ ] Test backup/restore procedures
- [ ] Review and update RBAC policies

### Credential Rotation
- [ ] Document rotation schedule
- [ ] Test rotation procedure
- [ ] Update secrets without downtime
- [ ] Verify new credentials work
- [ ] Update documentation
- [ ] Notify team of changes

### Monitoring
- [ ] Set up alerts for auth failures
- [ ] Monitor certificate expiration
- [ ] Track Prometheus query success rate
- [ ] Monitor controller health
- [ ] Set up log aggregation

## Documentation Checklist

### Team Documentation
- [ ] Document Prometheus URL
- [ ] Document authentication method
- [ ] Document secret names and locations
- [ ] Document rotation procedures
- [ ] Document troubleshooting steps
- [ ] Document escalation procedures

### Runbooks
- [ ] Create runbook for auth failures
- [ ] Create runbook for certificate renewal
- [ ] Create runbook for credential rotation
- [ ] Create runbook for disaster recovery
- [ ] Test runbooks with team

## Compliance Checklist

### Security Compliance
- [ ] Credentials meet password policy
- [ ] TLS meets security standards
- [ ] Audit logging is enabled
- [ ] Access is properly restricted
- [ ] Documentation is complete

### Operational Compliance
- [ ] Change management followed
- [ ] Deployment tested in staging
- [ ] Rollback plan documented
- [ ] Team trained on new setup
- [ ] Monitoring configured

## Sign-Off

### Pre-Production
- [ ] All checklist items completed
- [ ] Testing successful in staging
- [ ] Documentation reviewed
- [ ] Team trained
- [ ] Rollback plan tested

### Production
- [ ] Deployed to production
- [ ] Monitoring verified
- [ ] No errors in logs
- [ ] Functionality confirmed
- [ ] Team notified

---

## Quick Reference

### Essential Commands

**View Logs:**
```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
```

**Check Secret:**
```bash
kubectl get secret prometheus-credentials -n optipod-system -o yaml
```

**Test Connectivity:**
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v https://prometheus:9090/api/v1/query?query=up
```

**Restart Controller:**
```bash
kubectl rollout restart deployment/optipod-controller -n optipod-system
```

**Rollback:**
```bash
helm rollback optipod -n optipod-system
```

---

## Support

If you encounter issues:
1. ✅ Complete this checklist
2. 📖 Review [Troubleshooting Guide](PROMETHEUS_AUTHENTICATION.md#troubleshooting)
3. 🔍 Check [Common Issues](PROMETHEUS_MIGRATION_GUIDE.md#common-issues-and-solutions)
4. 💬 Open GitHub issue with checklist results
