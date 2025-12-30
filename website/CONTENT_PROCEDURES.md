# Content Update Procedures

This document outlines the procedures for updating content on the OptipOd landing page.

## Content Structure

The website content is organized into separate JSON files for maintainability:

- `content/config.json` - Site configuration and metadata
- `content/features.json` - Feature descriptions and categorization
- `content/navigation.json` - Navigation structure and links

## Automated Updates

### Version Information

Version information is automatically updated during the build process:

```bash
# Update version and build metadata
npm run update-content

# Or run the content manager directly
node scripts/content-manager.js update
```

### Build Metadata

The following information is automatically updated:

- **Build Date**: Timestamp of the build
- **Commit Hash**: Short Git commit hash
- **Version**: Latest release version from GitHub
- **Branch**: Current Git branch

## Manual Content Updates

### Updating Features

1. Edit `content/features.json`
2. Add, modify, or remove features as needed
3. Ensure each feature has: `id`, `title`, `description`, `category`, `priority`
4. Run validation: `npm run validate-content`

### Updating Navigation

1. Edit `content/navigation.json`
2. Update main navigation or CTA buttons
3. Ensure external links are marked with `"external": true`
4. Add analytics tracking IDs for important links

### Updating Site Configuration

1. Edit `content/config.json`
2. Update site metadata, repository information, or feature flags
3. Validate changes: `npm run validate-content`

## Content Validation

Before deploying content changes:

```bash
# Validate all content files
npm run validate-content

# Check for broken links
npm run validate:links

# Run full validation suite
npm run validate
```

## Version Display

Version information is automatically displayed in the bottom-right corner of the website when available. This includes:

- Release version (from GitHub releases)
- Build commit hash
- Build date

## Content Maintenance Schedule

- **Weekly**: Review and update dynamic content
- **Monthly**: Validate all external links and references
- **Per Release**: Update version information and changelog references
- **Quarterly**: Comprehensive content review and optimization

## Troubleshooting

### Content Validation Errors

If content validation fails:

1. Check JSON syntax in content files
2. Verify all required fields are present
3. Ensure external links are accessible
4. Review error messages for specific issues

### Version Update Issues

If version information isn't updating:

1. Check GitHub API access (rate limits)
2. Verify repository configuration in `config.json`
3. Ensure Git information is available in build environment
4. Check build logs for specific errors

## Best Practices

1. **Separate Concerns**: Keep content, structure, and styling separate
2. **Validate Changes**: Always run validation before committing
3. **Test Links**: Verify external links are accessible
4. **Version Control**: Track all content changes in Git
5. **Documentation**: Update this document when procedures change

---

*Last updated: $(date)*
