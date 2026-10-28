# RBAC Configuration

This guide covers Role-Based Access Control (RBAC) configuration for OptiPod, including required permissions, security best practices, and custom role examples.

## Overview

OptiPod requires specific Kubernetes RBAC permissions to function correctly. The operator needs permissions to:

- Read workload resources (Deployments, StatefulSets, DaemonSets)
- Update workload resource requests and limits
- Read metrics from the Metrics API
- Manage OptimizationPolicy custom resources
- Create Kubernetes events for audit trails

## Default RBAC Configuration

OptiPod's Helm chart creates a ClusterRole with the minimum required permissions. The default configuration uses:

- **ServiceAccount**: `controller-manager` (in the installation namespace)
- **ClusterRole**: `manager-role` (cluster-wide permissions)
- **ClusterRoleBinding**: `manager-rolebinding` (binds ServiceAccount to ClusterRole)

### Core Permissions

The `manager-role` ClusterRole includes these permissions:

```yaml
# Read workload resources
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "update", "patch"]

# Read pods and namespaces
- apiGroups: [""]
  resources: ["pods", "namespaces"]
  verbs: ["get", "list", "watch"]

# Read metrics
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods", "nodes"]
  verbs: ["get", "list"]

# Manage OptimizationPolicy CRDs
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# Update OptimizationPolicy status
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies/status"]
  verbs: ["get", "update", "patch"]

# Create events
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

### Webhook Permissions

If the webhook is enabled, additional permissions are required:

```yaml
# Manage webhook configurations
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["mutatingadmissionwebhookconfigurations"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Metrics Server Authentication

For secure metrics endpoints, OptiPod uses a separate role for authentication:

```yaml
# Metrics authentication role
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews"]
  verbs: ["create"]

- apiGroups: ["authorization.k8s.io"]
  resources: ["subjectaccessreviews"]
  verbs: ["create"]
```

### Leader Election

For high availability deployments, OptiPod needs leader election permissions:

```yaml
# Leader election (namespace-scoped)
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

## Security Best Practices

### Principle of Least Privilege

OptiPod follows the principle of least privilege:

1. **Read-only by default**: Most resources only require read permissions
2. **Selective write access**: Only workload resources and CRDs can be modified
3. **No pod creation**: OptiPod cannot create or delete pods
4. **No secret access**: OptiPod does not access Secrets or ConfigMaps (except for leader election)

### Namespace Isolation

For multi-tenant clusters, consider namespace-scoped permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: optipod-namespace-role
  namespace: production
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: optipod-namespace-binding
  namespace: production
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: optipod-namespace-role
subjects:
- kind: ServiceAccount
  name: controller-manager
  namespace: optipod-system
```

**Note**: Namespace-scoped permissions require OptimizationPolicies to target only the specific namespace.

### Read-Only Mode

For testing or audit purposes, create a read-only role:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-readonly-role
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods", "namespaces"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods", "nodes"]
  verbs: ["get", "list"]
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies/status"]
  verbs: ["get", "update", "patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

This role allows OptiPod to generate recommendations but prevents it from applying changes.

## Custom RBAC Configurations

### Restricting Workload Types

Limit OptiPod to specific workload types:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-deployments-only
rules:
# Only Deployments
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "update", "patch"]
# Still need to read pods
- apiGroups: [""]
  resources: ["pods", "namespaces"]
  verbs: ["get", "list", "watch"]
# Other required permissions...
```

### Multi-Namespace Deployment

For OptiPod instances managing specific namespaces:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: optipod-team-a-role
  namespace: team-a
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: optipod-team-a-binding
  namespace: team-a
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: optipod-team-a-role
subjects:
- kind: ServiceAccount
  name: controller-manager
  namespace: optipod-system
```

**Important**: You still need ClusterRole permissions for:
- OptimizationPolicy CRDs (cluster-scoped)
- Metrics API access
- Leader election (if enabled)

### Audit-Only Role

For compliance and auditing without modifications:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: optipod-audit-role
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods", "namespaces"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods", "nodes"]
  verbs: ["get", "list"]
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["optipod.optipod.io"]
  resources: ["optimizationpolicies/status"]
  verbs: ["get", "update", "patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

Use this with `mode: Recommend` in all policies to ensure no changes are applied.

## Troubleshooting RBAC Issues

### Permission Denied Errors

If you see errors like:

```
Failed to update workload: deployments.apps "nginx" is forbidden: 
User "system:serviceaccount:optipod-system:controller-manager" cannot patch resource "deployments"
```

**Solution**: Verify the ClusterRoleBinding exists and references the correct ServiceAccount:

```bash
kubectl get clusterrolebinding manager-rolebinding -o yaml
```

Check that the ServiceAccount matches:

```yaml
subjects:
- kind: ServiceAccount
  name: controller-manager
  namespace: optipod-system  # Must match OptiPod installation namespace
```

### Metrics Access Denied

If OptiPod cannot read metrics:

```
Failed to collect metrics: pods.metrics.k8s.io is forbidden
```

**Solution**: Ensure the metrics-server is installed and the ClusterRole includes metrics permissions:

```bash
# Check metrics-server
kubectl get deployment metrics-server -n kube-system

# Verify permissions
kubectl auth can-i get pods.metrics.k8s.io \
  --as=system:serviceaccount:optipod-system:controller-manager
```

### Webhook Configuration Errors

If the webhook fails to start:

```
Failed to create webhook configuration: mutatingwebhookconfigurations.admissionregistration.k8s.io is forbidden
```

**Solution**: Verify webhook permissions are included in the ClusterRole:

```bash
kubectl get clusterrole manager-role -o yaml | grep -A 5 admissionregistration
```

## Verifying RBAC Configuration

### Check ServiceAccount

```bash
kubectl get serviceaccount controller-manager -n optipod-system
```

### Check ClusterRole

```bash
kubectl get clusterrole manager-role -o yaml
```

### Check ClusterRoleBinding

```bash
kubectl get clusterrolebinding manager-rolebinding -o yaml
```

### Test Permissions

Use `kubectl auth can-i` to verify permissions:

```bash
# Test deployment update permission
kubectl auth can-i update deployments \
  --as=system:serviceaccount:optipod-system:controller-manager

# Test metrics read permission
kubectl auth can-i get pods.metrics.k8s.io \
  --as=system:serviceaccount:optipod-system:controller-manager

# Test OptimizationPolicy access
kubectl auth can-i create optimizationpolicies.optipod.optipod.io \
  --as=system:serviceaccount:optipod-system:controller-manager
```

All commands should return `yes` for OptiPod to function correctly.

## Helm Configuration

### Custom ServiceAccount

To use a custom ServiceAccount:

```yaml
# values.yaml
serviceAccount:
  create: false
  name: my-custom-sa
```

### Disable RBAC Creation

If your cluster has pre-configured RBAC:

```yaml
# values.yaml
rbac:
  create: false
```

**Warning**: Ensure all required permissions are granted to the ServiceAccount before disabling RBAC creation.

### Namespace-Scoped Installation

For namespace-scoped permissions:

```yaml
# values.yaml
rbac:
  clusterRole: false  # Creates Role instead of ClusterRole
  namespaced: true
```

**Limitation**: Namespace-scoped installations can only manage workloads in the installation namespace.

## Security Considerations

### Pod Security Standards

OptiPod's controller pod runs with restricted security context:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  seccompProfile:
    type: RuntimeDefault
```

### Network Policies

Restrict network access to OptiPod:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: optipod-network-policy
  namespace: optipod-system
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: optipod
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 8443  # Metrics endpoint
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # Kubernetes API
  - to:
    - namespaceSelector:
        matchLabels:
          name: prometheus
    ports:
    - protocol: TCP
      port: 9090  # Prometheus
```

### Audit Logging

Enable Kubernetes audit logging to track OptiPod actions:

```yaml
# audit-policy.yaml
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: RequestResponse
  users:
  - system:serviceaccount:optipod-system:controller-manager
  verbs: ["update", "patch"]
  resources:
  - group: "apps"
    resources: ["deployments", "statefulsets", "daemonsets"]
```

## Related Documentation

- [Installation Guide](/docs/getting-started/installation)
- [Operations Guide](/docs/advanced/operations)
- [Troubleshooting](/docs/guides/troubleshooting)
- [Security Model](/docs/concepts/safety-model)
