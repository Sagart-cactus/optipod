---
publishDate: 2025-01-28T00:00:00Z
title: 'GitOps and Resource Optimization: Why They Should Work Together'
excerpt: 'Explore how OptiPod bridges the gap between GitOps workflows and dynamic resource optimization in Kubernetes.'
image: https://images.unsplash.com/photo-1618401471353-b98afee0b2eb?q=80&w=2788&auto=format&fit=crop
category: 'Best Practices'
tags:
  - gitops
  - argocd
  - flux
  - kubernetes
author: 'OptiPod Team'
metadata:
  canonical: https://sagart-cactus.github.io/optipod/blog/gitops-resource-optimization
---

GitOps has become the gold standard for managing Kubernetes applications. But when it comes to resource optimization, many teams face a dilemma: how do you dynamically adjust resources without breaking GitOps principles?

## The GitOps Dilemma

GitOps tools like ArgoCD and Flux treat Git as the single source of truth. When something changes in the cluster that doesn't match Git, they detect drift and revert it. This is great for security and consistency, but it creates challenges for dynamic resource optimization.

Traditional tools like VPA (Vertical Pod Autoscaler) directly mutate pod specs, causing constant drift detection and sync conflicts. Teams are forced to choose between GitOps and optimization.

## The OptiPod Solution

OptiPod was designed from the ground up to work with GitOps, not against it.

### Strategy 1: Metadata-Only Recommendations

In **Recommend mode**, OptiPod only writes to workload metadata (annotations), never to pod templates:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    optipod.io/recommendation: |
      cpu: 200m
      memory: 512Mi
spec:
  template:
    spec:
      containers:
      - name: app
        resources:
          requests:
            cpu: 500m      # Original value from Git
            memory: 1Gi    # Original value from Git
```

ArgoCD sees no drift because the pod template hasn't changed. You can review recommendations and update Git when ready.

### Strategy 2: Webhook-Based Application

When you're ready for automatic optimization, OptiPod's webhook strategy keeps GitOps happy:

1. **Git remains the source of truth** for pod templates
2. **OptiPod stores recommendations** in workload annotations
3. **Webhook injects resources** at pod creation time
4. **ArgoCD/Flux never see drift** because pod templates don't change

```yaml
# In Git (managed by ArgoCD)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    optipod.io/recommendation: |
      cpu: 200m
      memory: 512Mi
spec:
  template:
    spec:
      containers:
      - name: app
        # No resources specified - webhook will inject
```

The webhook reads the annotation and injects the recommended resources when pods are created. ArgoCD never sees a difference between Git and the cluster.

## Best Practices

### 1. Start with Recommend Mode

Begin by deploying OptiPod in Recommend mode across your cluster:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: cluster-wide-recommendations
spec:
  mode: Recommend
  targetWorkloads:
    labelSelector:
      matchExpressions:
      - key: optipod.io/enabled
        operator: In
        values: ["true"]
```

Review recommendations for a few weeks to build confidence.

### 2. Enable Auto Mode Gradually

Start with non-critical workloads:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: optimize-dev
spec:
  mode: Auto
  targetWorkloads:
    labelSelector:
      matchLabels:
        environment: dev
```

Monitor the impact before expanding to staging and production.

### 3. Configure ArgoCD Properly

Tell ArgoCD to ignore OptiPod annotations:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
data:
  resource.customizations.ignoreDifferences.apps_Deployment: |
    jsonPointers:
    - /metadata/annotations/optipod.io~1recommendation
    - /metadata/annotations/optipod.io~1recommendation-reason
    - /metadata/annotations/optipod.io~1last-updated
```

This prevents ArgoCD from showing false drift.

### 4. Use the Webhook for Auto Mode

Deploy the OptiPod webhook when using Auto mode:

```bash
helm install optipod optipod/optipod \
  --set webhook.enabled=true \
  --set prometheus.url=http://prometheus:9090
```

The webhook requires cert-manager, which OptiPod can auto-detect and bootstrap.

## Real-World Example

Here's how a typical team adopts OptiPod with GitOps:

**Week 1-2: Assessment**
- Deploy OptiPod in Recommend mode
- Review recommendations across all workloads
- Identify optimization opportunities

**Week 3-4: Dev/Test**
- Enable Auto mode for dev environment
- Monitor resource usage and application performance
- Adjust policies based on results

**Week 5-6: Staging**
- Expand Auto mode to staging
- Run load tests to validate recommendations
- Fine-tune safety factors if needed

**Week 7+: Production**
- Gradually enable Auto mode for production workloads
- Start with stateless services
- Monitor cost savings and performance

## Conclusion

GitOps and resource optimization don't have to be at odds. With the right approach, you can have both:

- **Maintain GitOps principles** with Git as the source of truth
- **Optimize resources dynamically** based on actual usage
- **Reduce costs** without sacrificing reliability
- **Keep your platform team happy** with no drift conflicts

Try OptiPod today and see how it fits into your GitOps workflow. Check out our [ArgoCD integration guide](https://sagart-cactus.github.io/optipod/docs/guides/gitops-integration) for detailed setup instructions.

## Resources

- [OptiPod Documentation](https://sagart-cactus.github.io/optipod/docs)
- [ArgoCD Integration Guide](https://sagart-cactus.github.io/optipod/docs/guides/gitops-integration)
- [GitHub Repository](https://github.com/Sagart-cactus/optipod)
