---
title: OptiPod Website Documentation
description: Documentation structure and guidelines for the OptiPod website
---

# OptiPod Website Documentation

This directory contains the MDX version of the OptiPod documentation for the website, built with [Astro](https://astro.build/) and [Starlight](https://starlight.astro.build/).

## Structure

The documentation is organized following the [Diátaxis framework](https://diataxis.fr/):

```
docs/
├── getting-started/    # Learning-oriented tutorials
├── concepts/           # Understanding-oriented explanations
├── guides/             # Task-oriented how-to guides
├── reference/          # Information-oriented reference docs
└── advanced/           # Advanced topics
```

## MDX Format

Files use MDX (Markdown with JSX) format, which allows:

- Standard Markdown syntax
- JSX components for interactive elements
- Frontmatter for metadata
- Code syntax highlighting
- Mermaid diagrams

### Frontmatter Example

```mdx
---
title: Installation Guide
description: How to install OptiPod in your Kubernetes cluster
---

# Installation Guide

Content goes here...
```

## Navigation Configuration

The sidebar navigation is configured in `website/astro.config.ts` in the `starlight.sidebar` section.

When adding new documentation pages:

1. Create the MDX file in the appropriate directory
2. Add frontmatter with title and description
3. Update the sidebar configuration in `astro.config.ts`

## Building the Website

```bash
# Install dependencies
cd website
npm install

# Development server
npm run dev

# Build for production
npm run build

# Preview production build
npm run preview
```

## Dual Documentation

This MDX documentation is synchronized with the Markdown documentation in `docs/`:

- **Content**: Same information in both versions
- **Format**: Adapted for web (MDX) vs. plain text (Markdown)
- **Examples**: Identical code examples and commands
- **Links**: Cross-references maintained in both

## Validation

Before committing changes:

```bash
# From project root
./scripts/validate-docs.sh
```

This validates:
- Script references
- Internal links
- MDX frontmatter
- Documentation structure

## Contributing

When updating website documentation:

1. **Update both versions** - Modify both MDX (website/) and Markdown (docs/)
2. **Test locally** - Run `npm run dev` to preview changes
3. **Validate** - Run validation scripts
4. **Check responsive design** - Test on mobile and desktop
5. **Verify links** - Ensure all internal links work

## Styling

Custom styles are in:
- `website/src/assets/styles/docs.css` - Documentation-specific styles
- `website/tailwind.config.js` - Tailwind configuration

## Components

Reusable components are in `website/src/components/`:
- Use existing components when possible
- Create new components for repeated patterns
- Document component usage

---

*For more information, see the main [documentation README](../../../docs/README.md).*
