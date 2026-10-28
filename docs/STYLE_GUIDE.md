# OptiPod Documentation Style Guide

This guide defines the writing style and conventions for OptiPod documentation.

## Diátaxis Framework

OptiPod documentation follows the [Diátaxis framework](https://diataxis.fr/), which organizes documentation into four types:

### 1. Tutorials (Learning-Oriented)

**Purpose:** Help users learn by doing

**Characteristics:**
- Step-by-step instructions
- Focused on learning, not production use
- Provides a complete, working example
- Explains what the user is doing and why

**Location:** `docs/getting-started/`

**Example:** Quick Start guide

### 2. How-To Guides (Task-Oriented)

**Purpose:** Help users accomplish specific tasks

**Characteristics:**
- Focused on solving a specific problem
- Assumes basic knowledge
- Provides practical steps
- Goal-oriented

**Location:** `docs/guides/`

**Example:** "How to create an optimization policy"

### 3. Reference (Information-Oriented)

**Purpose:** Provide technical descriptions

**Characteristics:**
- Comprehensive and accurate
- Describes the system as it is
- Structured for lookup
- No explanations or tutorials

**Location:** `docs/reference/`

**Example:** CRD specification, API reference

### 4. Explanation (Understanding-Oriented)

**Purpose:** Clarify and illuminate concepts

**Characteristics:**
- Discusses topics at a higher level
- Explains design decisions
- Provides context and background
- Helps users understand "why"

**Location:** `docs/concepts/`

**Example:** Architecture overview, safety model

## Writing Style

### Voice and Tone

- **Active voice:** "OptiPod analyzes metrics" not "Metrics are analyzed by OptiPod"
- **Present tense:** "OptiPod creates recommendations" not "OptiPod will create recommendations"
- **Direct address:** "You can configure" not "Users can configure"
- **Professional but friendly:** Clear and helpful, not overly formal

### Formatting

#### Headings

- Use sentence case: "Creating your first policy" not "Creating Your First Policy"
- Use descriptive headings that indicate content
- Maintain hierarchy: # → ## → ### → ####
- Don't skip heading levels

#### Code Blocks

Always specify the language:

```yaml
# Good
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
```

Not:
```
# Bad - no language specified
apiVersion: optipod.io/v1alpha1
```

#### Commands

Show commands with their output when helpful:

```bash
$ kubectl get optimizationpolicies
NAME                MODE        AGE
production-policy   Auto        5d
staging-policy      Recommend   3d
```

Use `$` for user commands, no prefix for output.

#### File Paths

Use backticks for file paths: `config/samples/policy.yaml`

#### UI Elements

Use **bold** for UI elements: Click **Apply** to save changes.

#### Emphasis

- Use **bold** for important terms on first use
- Use *italics* sparingly for emphasis
- Use `code formatting` for technical terms, commands, and values

### Terminology

#### Consistent Terms

Use these terms consistently:

| Use | Don't Use |
|-----|-----------|
| OptimizationPolicy | optimization policy, policy CRD |
| workload | deployment, pod, container |
| recommendation | suggestion, proposal |
| Auto mode | automatic mode, auto-mode |
| Recommend mode | recommendation mode, recommend-mode |
| Kubernetes | K8s (except in informal contexts) |
| namespace | Namespace (unless referring to the API object) |

#### Abbreviations

- Define abbreviations on first use: "Custom Resource Definition (CRD)"
- Use the abbreviation consistently after definition
- Common abbreviations don't need definition: API, CPU, RAM, YAML

### Code Examples

#### Completeness

All code examples should be:
- **Complete:** Can be copied and used as-is
- **Tested:** Verified to work
- **Realistic:** Based on actual use cases
- **Annotated:** Include comments explaining key parts

#### Example Structure

```yaml
# Good example with context
apiVersion: optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: production-policy
  namespace: optipod-system
spec:
  # Target all Deployments in the production namespace
  targetRef:
    kind: Deployment
  namespaceSelector:
    matchNames:
      - production
  # Use Auto mode to apply recommendations automatically
  mode: Auto
  # Collect metrics from Prometheus
  metricsProvider:
    type: Prometheus
    prometheus:
      address: http://prometheus:9090
```

### Links

#### Internal Links

- Use relative paths: `[Installation](../getting-started/installation.md)`
- Link to related documentation
- Verify links work in both docs/ and website/

#### External Links

- Use descriptive link text: [Kubernetes documentation](https://kubernetes.io/docs/)
- Not: [Click here](https://kubernetes.io/docs/)
- Open external links in new tab (website only)

### Lists

#### Ordered Lists

Use for sequential steps:

1. Install OptiPod
2. Create a policy
3. Review recommendations

#### Unordered Lists

Use for non-sequential items:

- CPU requests
- Memory requests
- CPU limits
- Memory limits

### Tables

Use tables for structured data:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| mode | string | Yes | Operational mode (Auto, Recommend, Disabled) |
| targetRef | object | Yes | Target workload selector |

### Admonitions

Use admonitions for important information:

**Note:** Additional information that's helpful but not critical.

**Important:** Information that users should pay attention to.

**Warning:** Information about potential problems or risks.

**Tip:** Helpful suggestions or best practices.

## Documentation Structure

### File Naming

- Use lowercase with hyphens: `creating-policies.md`
- Be descriptive: `switching-to-auto-mode.md` not `auto.md`
- Match between docs/ and website/: `docs/guides/creating-policies.md` ↔ `website/src/content/docs/docs/guides/creating-policies.mdx`

### Document Structure

Every document should have:

1. **Title** (H1) - Clear, descriptive title
2. **Overview** - Brief introduction (1-2 paragraphs)
3. **Prerequisites** (if applicable) - What users need to know/have
4. **Main Content** - Organized with clear headings
5. **Related Documentation** - Links to related docs
6. **Next Steps** (if applicable) - What to do next

### Frontmatter (MDX only)

```yaml
---
title: Document Title
description: Brief description for SEO and previews
---
```

## Best Practices

### Do

✅ Use real examples from the codebase
✅ Test all commands and code examples
✅ Update both Markdown and MDX versions
✅ Link to related documentation
✅ Use consistent terminology
✅ Include troubleshooting sections
✅ Provide context and explanations
✅ Use diagrams for complex concepts

### Don't

❌ Use placeholder or fake examples
❌ Assume too much prior knowledge
❌ Use jargon without explanation
❌ Create orphaned documentation
❌ Copy-paste without testing
❌ Use inconsistent terminology
❌ Skip validation steps
❌ Forget to update navigation

## Validation

Before submitting documentation:

1. **Run validation:** `./scripts/validate-docs.sh`
2. **Check synchronization:** `./scripts/sync-docs.sh`
3. **Test examples:** Verify all code examples work
4. **Review links:** Ensure all links are valid
5. **Check formatting:** Verify proper Markdown/MDX syntax
6. **Proofread:** Check for typos and grammar

## Questions?

If you have questions about documentation style:

1. Check this style guide
2. Look at existing documentation for examples
3. Ask in GitHub issues or discussions

---

*This style guide is based on industry best practices and the Diátaxis framework.*
