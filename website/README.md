# OptiPod Website

Marketing and documentation website for OptiPod - an open-source Kubernetes resource optimization operator.

## Quick Start

```bash
cd website
npm install
npm run dev
```

Visit `http://localhost:4321`

## Available Commands

```bash
npm run dev          # Start dev server
npm run build        # Build for production
npm run preview      # Preview production build
npm run check        # Run Astro + ESLint + Prettier checks
npm run fix          # Auto-fix ESLint and Prettier issues
```

## Tech Stack

- **Framework**: [Astro](https://astro.build) - Static site generator
- **Styling**: [Tailwind CSS](https://tailwindcss.com) - Utility-first CSS
- **Documentation**: [Starlight](https://starlight.astro.build) - Astro docs theme
- **Template**: Based on [AstroWind](https://github.com/onwidget/astrowind)

## Project Structure

```
website/
├── public/              # Static assets (favicon, robots.txt, etc.)
├── src/
│   ├── assets/         # Images, styles, favicons
│   ├── components/     # Astro components
│   │   ├── blog/       # Blog-specific components
│   │   ├── common/     # Shared components (meta, analytics)
│   │   ├── ui/         # UI primitives (buttons, forms)
│   │   └── widgets/    # Page sections (hero, features, etc.)
│   ├── content/        # Content collections
│   │   └── docs/       # Documentation MDX files
│   ├── layouts/        # Page layouts
│   ├── pages/          # File-based routing
│   └── utils/          # Helper functions
├── astro.config.ts     # Astro configuration
├── tailwind.config.js  # Tailwind configuration
└── package.json
```

## Content Management

### Documentation

Documentation lives in `src/content/docs/docs/` and is organized by category:

- `getting-started/` - Installation, quick start, introduction
- `concepts/` - Core concepts and terminology
- `guides/` - How-to guides
- `reference/` - API reference, CRD specs, annotations
- `advanced/` - Advanced topics

Edit `.mdx` files directly. Starlight handles navigation and layout automatically.

### Homepage & Marketing Pages

Main pages are in `src/pages/`:
- `index.astro` - Homepage
- `privacy.md` - Privacy policy
- `terms.md` - Terms of service

## Configuration

### Site Metadata

Edit `src/config.yaml` for site-wide settings:
- Site name, description, URLs
- Social media links
- Analytics configuration
- Navigation structure

### Astro Config

`astro.config.ts` controls:
- Integrations (Tailwind, MDX, Sitemap, etc.)
- Build settings
- Starlight documentation configuration

## Deployment

The site deploys automatically via GitHub Actions when changes are pushed to `main`.

### GitHub Pages

Workflow: `.github/workflows/website-build.yml`
- Builds on push to `website/**` paths
- Deploys to GitHub Pages
- URL: `https://sagart-cactus.github.io/optipod/`

### Manual Deployment

```bash
npm run build
# Output in dist/ directory
```

## Development Notes

### Adding New Documentation

1. Create `.mdx` file in appropriate `src/content/docs/docs/` subdirectory
2. Add frontmatter:
   ```yaml
   ---
   title: Page Title
   description: Page description
   ---
   ```
3. Navigation updates automatically based on file structure

### Styling Guidelines

- Use Tailwind utility classes
- Follow existing component patterns
- Keep designs minimal and technical
- Prioritize readability and whitespace

### Target Audience

- Staff+ engineers
- Platform / SRE teams
- Infrastructure-focused OSS contributors

Avoid marketing hype. Prefer clarity, precision, and calm confidence.

## Related Documentation

- [Main OptiPod README](../README.md) - Operator documentation
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute
- [Roadmap](../ROADMAP.md) - Project roadmap

## License

Apache 2.0 - See [LICENSE.md](../LICENSE.md)
