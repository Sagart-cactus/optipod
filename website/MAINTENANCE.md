# Website Maintenance Guide

This document provides comprehensive guidance for maintaining the OptipOd landing page, including link validation, content updates, and troubleshooting procedures.

## Table of Contents

- [Link Validation](#link-validation)
- [Content Updates](#content-updates)
- [Build Process](#build-process)
- [Troubleshooting](#troubleshooting)
- [Performance Monitoring](#performance-monitoring)
- [Deployment](#deployment)

## Link Validation

### Automated Link Checking

The website includes automated link validation to ensure all external links remain functional.

#### Running Link Validation

```bash
# Run link validation as part of the build process
npm run validate:links

# Or run the full validation suite
npm run validate
```

#### Link Checker Configuration

The link checker is configured in `scripts/link-checker.js` with the following settings:

- **Timeout**: 10 seconds per link
- **Retries**: 2 attempts for failed links
- **User Agent**: Custom OptipOd identifier
- **Valid Status Codes**: 2xx and 3xx HTTP responses

#### Ignored Link Patterns

The following link types are automatically ignored:

- Email links (`mailto:`)
- Phone links (`tel:`)
- Internal anchors (`#section`)
- JavaScript links (`javascript:`)
- Local development URLs (`localhost`, `127.0.0.1`)

#### Handling Broken Links

When broken links are detected:

1. **Review the Report**: The link checker provides detailed information about each failed link
2. **Verify the Issue**: Manually check if the link is actually broken or temporarily unavailable
3. **Update or Remove**: Either update the link to the correct URL or remove it if no longer relevant
4. **Test Again**: Run the link checker to verify the fix

### Manual Link Verification

For critical links, perform manual verification:

1. **GitHub Repository Links**: Ensure they point to the correct repository and branches
2. **Documentation Links**: Verify they lead to current documentation versions
3. **External Tool Links**: Check that referenced tools and services are still available

## Content Updates

### Updating Static Content

Most content can be updated by editing `index.html` directly:

1. **Hero Section**: Update value proposition, statistics, or call-to-action buttons
2. **Features**: Modify feature descriptions or add new capabilities
3. **Comparison Table**: Update competitive comparisons as needed
4. **Quick Start**: Keep installation and configuration examples current

### Dynamic Content Integration

Some content is automatically updated:

- **GitHub Statistics**: Fetched dynamically via GitHub API
- **Version Information**: Can be configured to pull from releases
- **Build Timestamps**: Automatically updated during deployment

### Content Validation

After making content changes:

```bash
# Validate HTML structure
npm run validate:html

# Check for accessibility issues
npm run lighthouse

# Validate all content
npm run validate
```

## Build Process

### Development Workflow

```bash
# Install dependencies
npm install

# Start development server
npm run dev

# Build for production
npm run build

# Run all validations
npm run test
```

### Build Steps

The build process includes:

1. **Asset Optimization**: Images, CSS, and JavaScript minification
2. **Critical CSS**: Above-the-fold CSS inlining
3. **HTML Validation**: W3C HTML validation
4. **Link Checking**: External link validation
5. **Performance Audit**: Lighthouse CI checks

### Build Artifacts

Generated files are placed in the `dist/` directory:

- `dist/css/`: Optimized CSS files
- `dist/js/`: Minified JavaScript bundles
- `dist/images/`: Compressed images in multiple formats

## Troubleshooting

### Common Issues

#### Link Validation Failures

**Symptom**: Link checker reports broken links
**Solutions**:
- Check if the external service is temporarily down
- Verify the URL hasn't changed
- Update or remove outdated links
- Add temporary ignores for known issues

#### Build Failures

**Symptom**: Build process fails during validation
**Solutions**:
- Check HTML syntax with `npm run validate:html`
- Verify CSS with `npm run validate:css`
- Test JavaScript with `npm run validate:js`
- Review build logs for specific error messages

#### Performance Issues

**Symptom**: Lighthouse scores below thresholds
**Solutions**:
- Optimize images with `npm run optimize:images`
- Review CSS bundle size
- Check for unused JavaScript
- Verify critical CSS inlining

### Debugging Tools

#### Link Checker Debug Mode

Enable verbose logging in the link checker:

```javascript
// In scripts/link-checker.js, add debug logging
const DEBUG = process.env.DEBUG === 'true';
if (DEBUG) {
  console.log('Debug info:', result);
}
```

#### Build Process Debugging

```bash
# Enable verbose npm logging
npm run build --verbose

# Check specific validation steps
npm run validate:html -- --verbose
npm run validate:css -- --verbose
```

## Performance Monitoring

### Lighthouse CI Integration

The website uses Lighthouse CI for automated performance monitoring:

```bash
# Run Lighthouse audit
npm run lighthouse

# View detailed report
open .lighthouseci/lhr-*.html
```

### Performance Thresholds

Current performance targets:

- **Performance Score**: ≥ 90
- **Accessibility Score**: ≥ 95
- **Best Practices Score**: ≥ 90
- **SEO Score**: ≥ 95

### Monitoring External Dependencies

Track the health of external services:

- **GitHub API**: Monitor rate limits and availability
- **CDN Resources**: Verify font and library loading
- **Analytics Services**: Check tracking functionality

## Deployment

### Automated Deployment

The website deploys automatically via GitHub Actions when changes are pushed to the `main` branch in the `website/` directory.

### Deployment Validation

After deployment, verify:

1. **Site Accessibility**: Ensure the site loads correctly
2. **Link Functionality**: Spot-check critical links
3. **Performance**: Run a quick Lighthouse audit
4. **Analytics**: Verify tracking is working

### Rollback Procedures

If issues are detected after deployment:

1. **Immediate Rollback**: Use GitHub Pages rollback feature
2. **Fix and Redeploy**: Address issues and push corrected version
3. **Monitor**: Watch for any additional issues

### Manual Deployment

For emergency deployments or testing:

```bash
# Build the site
npm run build

# Deploy to GitHub Pages (if configured)
npm run deploy
```

## Maintenance Schedule

### Regular Tasks

#### Weekly
- [ ] Run link validation check
- [ ] Review GitHub statistics accuracy
- [ ] Check Lighthouse performance scores

#### Monthly
- [ ] Update dependencies (`npm audit` and `npm update`)
- [ ] Review and update content for accuracy
- [ ] Verify all external links manually
- [ ] Check analytics data for insights

#### Quarterly
- [ ] Comprehensive content review
- [ ] Performance optimization review
- [ ] Security audit of dependencies
- [ ] Backup and disaster recovery testing

## Contact and Support

For maintenance issues or questions:

- **Repository Issues**: [GitHub Issues](https://github.com/Sagart-cactus/optipod/issues)
- **Documentation**: See project README and documentation
- **Community**: Check project discussions and community channels

## Version History

- **v1.0.0**: Initial maintenance documentation
- **v1.1.0**: Added automated link checking
- **v1.2.0**: Enhanced content management procedures

---

*This maintenance guide is part of the OptipOd project. Keep it updated as the website evolves.*
