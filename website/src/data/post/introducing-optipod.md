---
publishDate: 2025-01-28T00:00:00Z
title: 'Introducing OptiPod: GitOps-Safe Kubernetes Resource Optimization'
excerpt: 'Learn how OptiPod brings explainable, policy-driven resource optimization to Kubernetes without breaking your GitOps workflows.'
image: https://images.unsplash.com/photo-1667372393119-3d4c48d07fc9?q=80&w=2832&auto=format&fit=crop
category: 'Announcements'
tags:
  - kubernetes
  - gitops
  - resource-optimization
author: 'OptiPod Team'
metadata:
  canonical: https://sagart-cactus.github.io/optipod/blog/introducing-optipod
---

We're excited to introduce **OptiPod**, an open-source Kubernetes operator designed to help you optimize resource requests and limits without breaking your GitOps workflows.

## The Problem

Kubernetes resource management is hard. Set requests too high, and you waste money. Set them too low, and you risk performance issues or OOMKills. Traditional solutions like VPA (Vertical Pod Autoscaler) often conflict with GitOps tools like ArgoCD and Flux, creating a frustrating experience for platform teams.

## The OptiPod Approach

OptiPod takes a different approach:

### 1. Recommend First, Apply When Ready

By default, OptiPod operates in **Recommend mode**. It analyzes your workloads using Prometheus metrics and adds recommendations as annotations to your Deployments, StatefulSets, and DaemonSets. No mutations, no surprises.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    optipod.io/recommendation: |
      cpu: 100m
      memory: 256Mi
    optipod.io/recommendation-reason: "Based on p95 usage over 7 days"
```

When you're ready, opt-in to **Auto mode** per policy to let OptiPod apply recommendations automatically.

### 2. GitOps Compatible

OptiPod doesn't fight with your GitOps controllers. Instead of mutating pod templates (which causes drift), it:

- Stores recommendations in workload metadata
- Uses an optional webhook to inject resources at pod creation time
- Lets ArgoCD/Flux manage the source of truth

### 3. Explainable Recommendations

Every recommendation comes with clear reasoning:

- Which metrics were used
- What percentile was considered
- Safety factors applied
- Why changes were or weren't made

No black boxes, no surprises.

## Key Features

- **Policy-Driven**: Define optimization policies with label selectors
- **Safe by Default**: Gradual memory decreases, configurable safety factors
- **Prometheus Integration**: Uses your existing metrics infrastructure
- **Multi-Workload Support**: Deployments, StatefulSets, DaemonSets
- **Flexible Authentication**: Basic auth, bearer tokens, or mTLS for Prometheus

## Getting Started

Install OptiPod with Helm:

```bash
helm repo add optipod https://sagart-cactus.github.io/optipod
helm install optipod optipod/optipod \
  --set prometheus.url=http://prometheus:9090
```

Create your first policy:

```yaml
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: optimize-staging
spec:
  mode: Recommend  # Start safe
  targetWorkloads:
    labelSelector:
      matchLabels:
        environment: staging
  metricsSource:
    prometheus:
      url: http://prometheus:9090
```

## What's Next?

We're just getting started. Check out our [roadmap](https://github.com/Sagart-cactus/optipod/blob/main/ROADMAP.md) to see what's coming next, including:

- Additional metrics providers
- Advanced recommendation algorithms
- Cost optimization insights
- Multi-cluster support

## Get Involved

OptiPod is open source and we'd love your contributions:

- [GitHub Repository](https://github.com/Sagart-cactus/optipod)
- [Documentation](https://sagart-cactus.github.io/optipod/docs)
- [Report Issues](https://github.com/Sagart-cactus/optipod/issues)

Try OptiPod today and let us know what you think!
