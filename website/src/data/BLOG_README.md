# OptiPod Blog Posts

This directory contains blog posts for the OptiPod website.

## Adding a New Blog Post

Create a new `.md` or `.mdx` file in this directory with the following frontmatter:

```markdown
---
publishDate: 2024-03-20T00:00:00Z
title: 'Your Blog Post Title'
excerpt: 'A brief description of your post that appears in listings'
image: https://images.unsplash.com/photo-xxxxx  # Optional hero image
category: 'Category Name'  # e.g., 'Announcements', 'Best Practices', 'Tutorials'
tags:
  - tag1
  - tag2
  - tag3
author: 'Author Name'
draft: false  # Set to true to hide from production
metadata:
  canonical: https://sagart-cactus.github.io/optipod/blog/your-post-slug
---

Your blog post content goes here...
```

## Categories

Use these standard categories:
- **Announcements** - Product releases, major updates
- **Best Practices** - How-to guides, recommendations
- **Tutorials** - Step-by-step guides
- **Security** - Security-related topics
- **Cost Optimization** - FinOps and cost-saving content
- **Case Studies** - Real-world examples and results

## Tags

Common tags to use:
- kubernetes
- gitops
- resource-optimization
- prometheus
- cost-optimization
- security
- argocd
- flux
- finops

## Images

Use high-quality images from Unsplash or other free sources. Recommended size: 2000x1000px.

## File Naming

Use kebab-case for file names:
- ✅ `introducing-optipod.md`
- ✅ `kubernetes-cost-optimization.md`
- ❌ `Introducing OptiPod.md`
- ❌ `kubernetes_cost_optimization.md`

## Content Guidelines

- Keep posts focused and actionable
- Include code examples where relevant
- Link to relevant documentation
- Use proper markdown formatting
- Add alt text for images
- Keep paragraphs short and scannable

## Testing Locally

```bash
cd website
npm run dev
```

Visit `http://localhost:4321/optipod/blog` to see your post.
