#!/usr/bin/env node

/**
 * Content Manager for OptipOd Landing Page
 *
 * This script manages dynamic content updates, including version information,
 * build metadata, and content synchronization with the repository.
 *
 * Features:
 * - Updates version information from GitHub releases
 * - Manages build metadata (timestamps, commit hashes)
 * - Validates content structure and consistency
 * - Provides content update utilities
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import { execSync } from 'child_process'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// Configuration
const CONFIG = {
  contentDir: path.resolve(__dirname, '..', 'content'),
  outputDir: path.resolve(__dirname, '..'),
  githubApi: 'https://api.github.com',
  repository: {
    owner: 'Sagart-cactus',
    name: 'optipod'
  }
}

/**
 * Load JSON content file with error handling
 * @param {string} filename - Name of the JSON file to load
 * @returns {Object} Parsed JSON content
 */
function loadContent (filename) {
  const filePath = path.join(CONFIG.contentDir, filename)

  if (!fs.existsSync(filePath)) {
    throw new Error(`Content file not found: ${filename}`)
  }

  try {
    const content = fs.readFileSync(filePath, 'utf8')
    return JSON.parse(content)
  } catch (error) {
    throw new Error(`Failed to parse ${filename}: ${error.message}`)
  }
}

/**
 * Save JSON content file with formatting
 * @param {string} filename - Name of the JSON file to save
 * @param {Object} content - Content to save
 */
function saveContent (filename, content) {
  const filePath = path.join(CONFIG.contentDir, filename)
  const jsonContent = JSON.stringify(content, null, 2)

  fs.writeFileSync(filePath, jsonContent, 'utf8')
  console.log(`✅ Updated ${filename}`)
}

/**
 * Get the latest release information from GitHub
 * @returns {Promise<Object>} Release information
 */
async function getLatestRelease () {
  const url = `${CONFIG.githubApi}/repos/${CONFIG.repository.owner}/${CONFIG.repository.name}/releases/latest`

  try {
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error(`GitHub API error: ${response.status}`)
    }

    const release = await response.json()
    return {
      version: release.tag_name,
      name: release.name,
      publishedAt: release.published_at,
      downloadUrl: release.html_url,
      assets: release.assets.map(asset => ({
        name: asset.name,
        downloadUrl: asset.browser_download_url,
        size: asset.size
      }))
    }
  } catch (error) {
    console.warn(`⚠️  Could not fetch latest release: ${error.message}`)
    return null
  }
}

/**
 * Get current Git information
 * @returns {Object} Git information
 */
function getGitInfo () {
  try {
    const commitHash = execSync('git rev-parse HEAD', { encoding: 'utf8' }).trim()
    const shortHash = commitHash.substring(0, 7)
    const branch = execSync('git rev-parse --abbrev-ref HEAD', { encoding: 'utf8' }).trim()
    const commitDate = execSync('git log -1 --format=%ci', { encoding: 'utf8' }).trim()

    return {
      commitHash,
      shortHash,
      branch,
      commitDate
    }
  } catch (error) {
    console.warn(`⚠️  Could not get Git information: ${error.message}`)
    return {
      commitHash: null,
      shortHash: null,
      branch: null,
      commitDate: null
    }
  }
}

/**
 * Update build metadata in config
 * @param {Object} config - Current configuration
 * @returns {Object} Updated configuration
 */
async function updateBuildMetadata (config) {
  const gitInfo = getGitInfo()
  const latestRelease = await getLatestRelease()
  const buildDate = new Date().toISOString()

  // Update build information
  config.build = {
    ...config.build,
    buildDate,
    commitHash: gitInfo.shortHash,
    branch: gitInfo.branch
  }

  // Update version from latest release if available
  if (latestRelease) {
    config.build.version = latestRelease.version
    config.build.releaseDate = latestRelease.publishedAt
    config.build.downloadUrl = latestRelease.downloadUrl
  }

  // Update maintenance information
  config.maintenance = {
    ...config.maintenance,
    lastUpdated: buildDate,
    nextReview: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString() // 30 days from now
  }

  return config
}

/**
 * Validate content structure and consistency
 * @param {Object} config - Site configuration
 * @param {Object} features - Features configuration
 * @param {Object} navigation - Navigation configuration
 * @returns {Array<string>} Array of validation errors
 */
function validateContent (config, features, navigation) {
  const errors = []

  // Validate required config fields
  const requiredConfigFields = ['site.title', 'site.description', 'repository.owner', 'repository.name']
  requiredConfigFields.forEach(field => {
    const keys = field.split('.')
    let value = config
    for (const key of keys) {
      value = value?.[key]
    }
    if (!value) {
      errors.push(`Missing required config field: ${field}`)
    }
  })

  // Validate features structure
  if (!features.features || !Array.isArray(features.features)) {
    errors.push('Features must be an array')
  } else {
    features.features.forEach((feature, index) => {
      if (!feature.id || !feature.title || !feature.description) {
        errors.push(`Feature ${index} missing required fields (id, title, description)`)
      }
    })
  }

  // Validate navigation structure
  if (!navigation.navigation?.main || !Array.isArray(navigation.navigation.main)) {
    errors.push('Navigation main menu must be an array')
  }

  if (!navigation.navigation?.cta || !Array.isArray(navigation.navigation.cta)) {
    errors.push('Navigation CTA buttons must be an array')
  }

  return errors
}

/**
 * Generate version information display
 * @param {Object} config - Site configuration
 * @returns {string} HTML snippet for version display
 */
function generateVersionDisplay (config) {
  const { build } = config

  if (!build.version && !build.commitHash) {
    return ''
  }

  let versionText = ''
  if (build.version) {
    versionText += `Version ${build.version}`
  }

  if (build.commitHash) {
    if (versionText) versionText += ' • '
    versionText += `Build ${build.commitHash}`
  }

  if (build.buildDate) {
    const buildDate = new Date(build.buildDate)
    const formattedDate = buildDate.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    })
    if (versionText) versionText += ' • '
    versionText += `Built ${formattedDate}`
  }

  return `
    <div class="version-info" style="
      position: fixed;
      bottom: 10px;
      right: 10px;
      background: rgba(0, 0, 0, 0.7);
      color: white;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 0.7rem;
      font-family: monospace;
      z-index: 1000;
      opacity: 0.6;
      transition: opacity 0.2s;
    " onmouseover="this.style.opacity='1'" onmouseout="this.style.opacity='0.6'">
      ${versionText}
    </div>
  `
}

/**
 * Update content update procedures documentation
 */
function updateContentProcedures () {
  const proceduresPath = path.join(CONFIG.outputDir, 'CONTENT_PROCEDURES.md')

  const procedures = `# Content Update Procedures

This document outlines the procedures for updating content on the OptipOd landing page.

## Content Structure

The website content is organized into separate JSON files for maintainability:

- \`content/config.json\` - Site configuration and metadata
- \`content/features.json\` - Feature descriptions and categorization
- \`content/navigation.json\` - Navigation structure and links

## Automated Updates

### Version Information

Version information is automatically updated during the build process:

\`\`\`bash
# Update version and build metadata
npm run update-content

# Or run the content manager directly
node scripts/content-manager.js update
\`\`\`

### Build Metadata

The following information is automatically updated:

- **Build Date**: Timestamp of the build
- **Commit Hash**: Short Git commit hash
- **Version**: Latest release version from GitHub
- **Branch**: Current Git branch

## Manual Content Updates

### Updating Features

1. Edit \`content/features.json\`
2. Add, modify, or remove features as needed
3. Ensure each feature has: \`id\`, \`title\`, \`description\`, \`category\`, \`priority\`
4. Run validation: \`npm run validate-content\`

### Updating Navigation

1. Edit \`content/navigation.json\`
2. Update main navigation or CTA buttons
3. Ensure external links are marked with \`"external": true\`
4. Add analytics tracking IDs for important links

### Updating Site Configuration

1. Edit \`content/config.json\`
2. Update site metadata, repository information, or feature flags
3. Validate changes: \`npm run validate-content\`

## Content Validation

Before deploying content changes:

\`\`\`bash
# Validate all content files
npm run validate-content

# Check for broken links
npm run validate:links

# Run full validation suite
npm run validate
\`\`\`

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
2. Verify repository configuration in \`config.json\`
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
`

  fs.writeFileSync(proceduresPath, procedures, 'utf8')
  console.log('✅ Updated content procedures documentation')
}

/**
 * Main command handler
 * @param {string} command - Command to execute
 */
async function main (command = 'update') {
  console.log('🔧 OptipOd Content Manager')
  console.log(`Running command: ${command}\n`)

  try {
    // Load current content
    const config = loadContent('config.json')
    const features = loadContent('features.json')
    const navigation = loadContent('navigation.json')

    switch (command) {
    case 'update': {
      console.log('📝 Updating build metadata...')
      const updatedConfig = await updateBuildMetadata(config)
      saveContent('config.json', updatedConfig)

      console.log('📋 Updating content procedures...')
      updateContentProcedures()

      console.log('✅ Content update completed successfully!')
      break
    }

    case 'validate': {
      console.log('🔍 Validating content structure...')
      const errors = validateContent(config, features, navigation)

      if (errors.length > 0) {
        console.log('❌ Content validation failed:')
        errors.forEach(error => console.log(`  - ${error}`))
        process.exit(1)
      } else {
        console.log('✅ Content validation passed!')
      }
      break
    }

    case 'version-display': {
      console.log('🏷️  Generating version display...')
      const versionHtml = generateVersionDisplay(config)
      console.log('Version display HTML:')
      console.log(versionHtml)
      break
    }

    default:
      console.log('❌ Unknown command:', command)
      console.log('Available commands: update, validate, version-display')
      process.exit(1)
    }
  } catch (error) {
    console.error('💥 Content manager failed:', error.message)
    process.exit(1)
  }
}

// Handle command line arguments
const command = process.argv[2] || 'update'

// Run the content manager
if (import.meta.url === `file://${process.argv[1]}`) {
  main(command).catch(error => {
    console.error('💥 Unexpected error:', error.message)
    process.exit(1)
  })
}

export { loadContent, saveContent, updateBuildMetadata, validateContent, generateVersionDisplay }
