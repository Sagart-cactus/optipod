# OptipOd Landing Page

Modern, high-performance landing page for OptipOd Kubernetes resource optimization.

## Development Setup

### Prerequisites

- Node.js 18+ and npm 9+
- Modern web browser for testing

### Installation

```bash
cd website
npm install
```

### Development Commands

```bash
# Start development server with live reload
npm run dev

# Build optimized version
npm run build

# Run all validation and tests
npm test

# Individual validation commands
npm run validate:html    # HTML validation
npm run validate:css     # CSS linting
npm run validate:js      # JavaScript linting

# Performance testing
npm run lighthouse       # Lighthouse CI audit

# Asset optimization
npm run optimize:images  # Image compression and WebP conversion
npm run optimize:css     # CSS minification
npm run optimize:js      # JavaScript minification

# Clean build artifacts
npm run clean
```

## Performance Requirements

The landing page must meet these Lighthouse thresholds:

- **Performance**: ≥90
- **Accessibility**: ≥95
- **Best Practices**: ≥90
- **SEO**: ≥90

Core Web Vitals targets:
- First Contentful Paint: ≤2s
- Largest Contentful Paint: ≤3s
- Cumulative Layout Shift: ≤0.1
- Total Blocking Time: ≤300ms

## Code Quality Standards

- **HTML**: W3C validation with semantic markup
- **CSS**: Stylelint standard configuration
- **JavaScript**: ESLint standard configuration
- **Images**: Optimized with WebP/AVIF support

## File Structure

```
website/
├── index.html              # Main landing page
├── package.json            # Dependencies and scripts
├── lighthouserc.json       # Performance testing config
├── .htmlvalidate.json      # HTML validation rules
├── .stylelintrc.json       # CSS linting rules
├── .eslintrc.json          # JavaScript linting rules
├── .gitignore              # Git ignore patterns
└── README.md               # This file
```

## Deployment

The landing page is automatically deployed to GitHub Pages via GitHub Actions when changes are pushed to the `main` branch in the `website/` directory.

## Contributing

1. Make changes to the landing page
2. Run `npm test` to validate all code quality checks
3. Ensure Lighthouse performance thresholds are met
4. Commit changes - deployment is automatic

## License

Apache 2.0 - see the [LICENSE](../LICENSE) file for details.