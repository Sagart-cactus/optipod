---
publishDate: 2025-01-28T00:00:00Z
title: 'GitOps and Resource Optimization: Why They Often Clash — and How to Make Them Work Together'
excerpt: 'GitOps and optimization can coexist, but only if the workflow respects ownership, review, and rollout safety.'
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

## GitOps and Resource Optimization

**Why They Often Clash — and How to Make Them Work Together**

GitOps promises safety, repeatability, and control.

Resource optimization promises efficiency and cost reduction.

In practice, these two ideas often collide.

## Where the tension starts

GitOps enforces a simple rule: Git is the source of truth.

Any change applied directly to the cluster is eventually reverted. Drift is detected, flagged, and corrected. This is exactly what makes GitOps powerful.

But resource optimization tools often assume the opposite. They expect to mutate live workloads, adjust requests dynamically, and let the cluster converge on a better state.

When these worlds meet, friction is inevitable.

## What actually happens in GitOps environments

A typical flow looks like this:

- An optimization tool detects overprovisioning
- It applies a resource change
- ArgoCD or Flux detects drift
- The change is reverted
- Engineers lose confidence in automation

From the optimizer's perspective, the change was correct. From GitOps' perspective, the change was unauthorized.

Both systems are behaving as designed.

## Where recommendations fall short

Most cost tools stop at recommendations.

Implementing those changes means editing requests and limits across many workloads. In a GitOps setup, that translates to coordinated PRs, reviews, and rollouts—exactly the kind of overhead that slows optimization to a crawl.

## Drift isn't a failure — it's a signal

Drift tells you something important: ownership matters.

In GitOps environments, ownership lives in Git. Any optimization strategy that ignores this will eventually be rejected—either by tooling or by humans.

Effective optimization must:

- generate changes that flow through Git
- respect deployment workflows
- avoid surprise mutations

This shifts the problem from "how do we apply recommendations?" to "how do we integrate optimization into GitOps safely?"

## Making optimization GitOps-friendly

To work with GitOps instead of against it, optimization needs to be:

- **Predictable** — changes should be explainable and reviewable
- **Incremental** — small adjustments reduce risk
- **Policy-driven** — intent should be encoded, not inferred
- **Ownership-aware** — field conflicts must be avoided

This is not something VPA or ad-hoc scripts handle well on their own. It's why [OptiPod](https://sagart-cactus.github.io/optipod/) treats GitOps as a first-class constraint.

## Where OptiPod fits

OptiPod was designed with GitOps as a hard constraint. If you're using ArgoCD or Flux, the [GitOps integration guide](https://sagart-cactus.github.io/optipod/docs/guides/gitops-integration) walks through the setup.

Instead of mutating workloads directly, it:

- generates recommended changes
- allows teams to review them
- applies mutations in a GitOps-safe manner
- respects Server-Side Apply ownership

Optimization becomes part of the deployment lifecycle, not a side effect that GitOps has to undo. You can explore the approach in the [OptiPod docs](https://sagart-cactus.github.io/optipod/docs).

## Closing thought

GitOps and optimization are not opposing goals.

They only clash when tooling assumes it can bypass ownership and safety.

When optimization respects GitOps workflows, efficiency stops being risky—and starts being sustainable. If you want to dive deeper, start with the [OptiPod overview](https://sagart-cactus.github.io/optipod/docs/getting-started/introduction).
