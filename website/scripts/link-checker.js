#!/usr/bin/env node

/**
 * Link Checker for OptipOd Landing Page
 *
 * This script validates all external links in the website to ensure they are accessible.
 * It extracts links from HTML files and checks their HTTP status.
 *
 * Features:
 * - Extracts links from HTML files
 * - Validates external HTTP/HTTPS links
 * - Skips internal anchors and relative paths
 * - Provides detailed reporting of broken links
 * - Configurable timeout and retry logic
 * - CI-friendly exit codes
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import linkCheck from 'link-check'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// Configuration
const CONFIG = {
  // Timeout for each link check in milliseconds
  timeout: '10s',

  // Number of retries for failed links
  retries: 2,

  // Delay between retries in milliseconds
  retryDelay: 1000,

  // User agent string for requests
  userAgent: 'OptipOd-LinkChecker/1.0 (+https://github.com/Sagart-cactus/optipod)',

  // Files to scan for links
  htmlFiles: [],

  // Patterns to ignore (regex strings)
  ignorePatterns: [
    '^mailto:', // Email links
    '^tel:', // Phone links
    '^#', // Internal anchors
    '^javascript:', // JavaScript links
    'localhost', // Local development links
    '127.0.0.1', // Local development links
    'optipod\\.github\\.io' // GitHub Pages URL (not yet set up)
  ],

  // Expected status codes that are considered valid
  validStatusCodes: [200, 201, 202, 203, 204, 205, 206, 300, 301, 302, 303, 304, 307, 308]
}

function listHtmlFiles (rootDir) {
  const files = []
  const stack = [rootDir]
  while (stack.length) {
    const current = stack.pop()
    const entries = fs.readdirSync(current, { withFileTypes: true })
    for (const entry of entries) {
      const full = path.join(current, entry.name)
      if (entry.isDirectory()) {
        stack.push(full)
      } else if (entry.isFile() && entry.name.endsWith('.html')) {
        files.push(full)
      }
    }
  }
  return files
}

/**
 * Extract all links from HTML content
 * @param {string} htmlContent - The HTML content to parse
 * @returns {Array<string>} Array of unique links found
 */
function extractLinks (htmlContent) {
  const links = new Set()

  // Regular expressions to match different types of links
  const patterns = [
    // href attributes in anchor tags
    /href\s*=\s*["']([^"']+)["']/gi,
    // src attributes in script and img tags
    /src\s*=\s*["']([^"']+)["']/gi,
    // action attributes in forms
    /action\s*=\s*["']([^"']+)["']/gi,
    // CSS url() functions
    /url\s*\(\s*["']?([^"')]+)["']?\s*\)/gi
  ]

  patterns.forEach(pattern => {
    let match
    while ((match = pattern.exec(htmlContent)) !== null) {
      const url = match[1].trim()
      if (url && !shouldIgnoreLink(url)) {
        links.add(url)
      }
    }
  })

  return Array.from(links)
}

/**
 * Check if a link should be ignored based on configuration patterns
 * @param {string} url - The URL to check
 * @returns {boolean} True if the link should be ignored
 */
function shouldIgnoreLink (url) {
  return CONFIG.ignorePatterns.some(pattern => {
    const regex = new RegExp(pattern, 'i')
    return regex.test(url)
  })
}

/**
 * Check if a URL is external (HTTP/HTTPS)
 * @param {string} url - The URL to check
 * @returns {boolean} True if the URL is external
 */
function isExternalLink (url) {
  return /^https?:\/\//i.test(url)
}

/**
 * Check a single link with retry logic
 * @param {string} url - The URL to check
 * @param {number} attempt - Current attempt number (for retry logic)
 * @returns {Promise<Object>} Result object with status information
 */
function checkLinkWithRetry (url, attempt = 1) {
  return new Promise((resolve) => {
    const options = {
      timeout: CONFIG.timeout,
      headers: {
        'User-Agent': CONFIG.userAgent
      }
    }

    linkCheck(url, options, (err, result) => {
      if (err || (result && !CONFIG.validStatusCodes.includes(result.statusCode))) {
        if (attempt < CONFIG.retries) {
          console.log(`  Retry ${attempt}/${CONFIG.retries - 1} for ${url}`)
          setTimeout(() => {
            checkLinkWithRetry(url, attempt + 1).then(resolve)
          }, CONFIG.retryDelay)
          return
        }
      }

      resolve({
        url,
        status: result ? result.status : 'error',
        statusCode: result ? result.statusCode : null,
        error: err ? err.message : null,
        attempts: attempt
      })
    })
  })
}

/**
 * Generate a detailed report of link check results
 * @param {Array<Object>} results - Array of link check results
 * @returns {Object} Summary report
 */
function generateReport (results) {
  const report = {
    total: results.length,
    passed: 0,
    failed: 0,
    errors: [],
    warnings: []
  }

  results.forEach(result => {
    if (result.error || !CONFIG.validStatusCodes.includes(result.statusCode)) {
      report.failed++
      report.errors.push({
        url: result.url,
        status: result.status,
        statusCode: result.statusCode,
        error: result.error,
        attempts: result.attempts
      })
    } else {
      report.passed++
    }
  })

  return report
}

/**
 * Print the link check report to console
 * @param {Object} report - The report object from generateReport
 */
function printReport (report) {
  console.log('\n📊 Link Check Report')
  console.log('='.repeat(50))
  console.log(`Total links checked: ${report.total}`)
  console.log(`✅ Passed: ${report.passed}`)
  console.log(`❌ Failed: ${report.failed}`)

  if (report.errors.length > 0) {
    console.log('\n🔍 Failed Links:')
    console.log('-'.repeat(30))
    report.errors.forEach((error, index) => {
      console.log(`${index + 1}. ${error.url}`)
      console.log(`   Status: ${error.status} (${error.statusCode || 'N/A'})`)
      if (error.error) {
        console.log(`   Error: ${error.error}`)
      }
      console.log(`   Attempts: ${error.attempts}`)
      console.log('')
    })
  }

  if (report.warnings.length > 0) {
    console.log('\n⚠️  Warnings:')
    console.log('-'.repeat(30))
    report.warnings.forEach((warning, index) => {
      console.log(`${index + 1}. ${warning}`)
    })
    console.log('')
  }
}

/**
 * Main function to run the link checker
 */
async function main () {
  console.log('🔗 OptipOd Link Checker')
  console.log('Validating external links in website...\n')

  const websiteDir = path.resolve(__dirname, '..')
  CONFIG.htmlFiles = [
    path.join(websiteDir, 'index.html'),
    ...listHtmlFiles(path.join(websiteDir, 'docs'))
  ]
  const allLinks = new Set()

  // Extract links from all HTML files
  for (const filePath of CONFIG.htmlFiles) {
    const htmlFile = path.relative(websiteDir, filePath)

    if (!fs.existsSync(filePath)) {
      console.log(`⚠️  Warning: ${htmlFile} not found, skipping...`)
      continue
    }

    console.log(`📄 Scanning ${htmlFile}...`)
    const htmlContent = fs.readFileSync(filePath, 'utf8')
    const links = extractLinks(htmlContent)

    console.log(`   Found ${links.length} links`)
    links.forEach(link => allLinks.add(link))
  }

  // Filter to only external links
  const externalLinks = Array.from(allLinks).filter(isExternalLink)

  if (externalLinks.length === 0) {
    console.log('✅ No external links found to validate.')
    return
  }

  console.log(`\n🌐 Checking ${externalLinks.length} external links...`)
  console.log('-'.repeat(50))

  // Check all external links
  const results = []
  for (let i = 0; i < externalLinks.length; i++) {
    const url = externalLinks[i]
    console.log(`[${i + 1}/${externalLinks.length}] Checking: ${url}`)

    const result = await checkLinkWithRetry(url)
    results.push(result)

    // Show immediate result
    if (result.error || !CONFIG.validStatusCodes.includes(result.statusCode)) {
      console.log(`  ❌ Failed: ${result.status} (${result.statusCode || 'N/A'})`)
    } else {
      console.log(`  ✅ OK: ${result.statusCode}`)
    }
  }

  // Generate and print report
  const report = generateReport(results)
  printReport(report)

  // Exit with appropriate code
  if (report.failed > 0) {
    console.log('💥 Link validation failed! Please fix the broken links above.')
    process.exit(1)
  } else {
    console.log('🎉 All links are valid!')
    process.exit(0)
  }
}

// Handle uncaught errors
process.on('uncaughtException', (error) => {
  console.error('💥 Uncaught Exception:', error.message)
  process.exit(1)
})

process.on('unhandledRejection', (reason, promise) => {
  console.error('💥 Unhandled Rejection at:', promise, 'reason:', reason)
  process.exit(1)
})

// Run the link checker
if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch(error => {
    console.error('💥 Link checker failed:', error.message)
    process.exit(1)
  })
}

export { extractLinks, checkLinkWithRetry, generateReport }
