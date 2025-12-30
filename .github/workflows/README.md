# GitHub Actions Workflows

This directory contains automated workflows for the OptipOd project.

## Website Deployment Workflows

### deploy-website.yml
Automatically deploys the landing page to GitHub Pages when changes are made to the `website/` directory.

**Triggers:**
- Push to main branch with changes in `website/` directory
- Pull requests to main branch with changes in `website/` directory
- Manual workflow dispatch

**Features:**
- Validates HTML, CSS, and JavaScript (with warnings for non-critical issues)
- Runs Lighthouse CI for performance auditing
- Builds and optimizes website assets
- Deploys to GitHub Pages (main branch only)
- Provides deployment status notifications

### rollback-website.yml
Provides manual rollback capability for the website deployment.

**Usage:**
- Can be triggered manually from GitHub Actions UI
- Allows specifying a commit SHA to rollback to (defaults to previous commit)
- Validates target commit before rollback
- Rebuilds and deploys the specified version

### website-health-check.yml
Monitors the deployed website health and performance.

**Schedule:**
- Runs every 6 hours automatically
- Can be triggered manually

**Checks:**
- Website availability (HTTP status)
- Critical page elements presence
- Basic performance metrics
- Response time validation

## Usage Instructions

### Deploying Changes
1. Make changes to files in the `website/` directory
2. Commit and push to main branch
3. The deployment workflow will automatically trigger
4. Monitor the workflow progress in the Actions tab
5. Check deployment status notifications

### Rolling Back
1. Go to Actions tab in GitHub
2. Select "Rollback Landing Page" workflow
3. Click "Run workflow"
4. Optionally specify a commit SHA to rollback to
5. Confirm the rollback operation

### Monitoring Health
- Health checks run automatically every 6 hours
- View results in the Actions tab under "Website Health Check"
- Manual health checks can be triggered as needed

## Configuration

### Required Secrets
- `LHCI_GITHUB_APP_TOKEN`: Optional token for Lighthouse CI GitHub integration

### Permissions
The workflows require the following permissions:
- `contents: read` - To checkout repository code
- `pages: write` - To deploy to GitHub Pages
- `id-token: write` - For GitHub Pages deployment authentication

## Troubleshooting

### Deployment Failures
- Check the workflow logs for specific error messages
- Verify all required files exist in the website directory
- Ensure build process completes successfully locally
- Use the rollback workflow if needed

### Validation Warnings
- CSS and JavaScript validation issues are treated as warnings
- The deployment will continue even with linting issues
- Fix validation issues for better code quality

### Performance Issues
- Lighthouse CI may fail if performance thresholds aren't met
- Check the Lighthouse report for specific recommendations
- Optimize assets and code as needed