#!/usr/bin/env node

import fs from 'fs'
import { generate } from 'critical'

console.log('🎨 Extracting critical CSS and adding resource hints...')

async function optimizeHTML () {
  try {
    // Read the original HTML file
    const htmlPath = 'index.html'
    const htmlContent = fs.readFileSync(htmlPath, 'utf8')

    // Extract critical CSS
    console.log('📊 Analyzing above-the-fold CSS...')

    const criticalResult = await generate({
      inline: false, // We'll inline manually for better control
      base: '.',
      src: 'index.html',
      width: 1300,
      height: 900,
      dimensions: [
        {
          width: 320,
          height: 568
        },
        {
          width: 768,
          height: 1024
        },
        {
          width: 1300,
          height: 900
        }
      ],
      penthouse: {
        blockJSRequests: false,
        timeout: 30000
      }
    })

    // Create optimized HTML with critical CSS inlined and resource hints
    const optimizedHTML = addResourceHintsAndCriticalCSS(htmlContent, criticalResult.css)

    // Write optimized HTML to dist directory
    if (!fs.existsSync('dist')) {
      fs.mkdirSync('dist', { recursive: true })
    }

    fs.writeFileSync('dist/index.html', optimizedHTML)

    // Also save the critical CSS separately for reference
    fs.writeFileSync('dist/critical.css', criticalResult.css)

    console.log('✅ Critical CSS extracted and HTML optimized')
    console.log(`📏 Critical CSS size: ${(criticalResult.css.length / 1024).toFixed(2)}KB`)
  } catch (error) {
    console.error('❌ Critical CSS extraction failed:', error.message)

    // Fallback: just add resource hints without critical CSS
    console.log('🔄 Falling back to resource hints only...')
    const htmlContent = fs.readFileSync('index.html', 'utf8')
    const optimizedHTML = addResourceHintsOnly(htmlContent)

    if (!fs.existsSync('dist')) {
      fs.mkdirSync('dist', { recursive: true })
    }

    fs.writeFileSync('dist/index.html', optimizedHTML)
    console.log('✅ Resource hints added (critical CSS extraction skipped)')
  }
}

function addResourceHintsAndCriticalCSS (htmlContent, criticalCSS) {
  // Add resource hints and critical CSS
  const resourceHints = `
    <!-- DNS prefetch for external resources -->
    <link rel="dns-prefetch" href="//api.github.com">
    <link rel="dns-prefetch" href="//fonts.googleapis.com">
    <link rel="dns-prefetch" href="//fonts.gstatic.com">

    <!-- Preconnect to critical third-party origins -->
    <link rel="preconnect" href="https://api.github.com" crossorigin>
    <link rel="preconnect" href="https://fonts.googleapis.com" crossorigin>
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>

    <!-- Preload critical assets -->
    <link rel="preload" href="dist/js/bundle.min.js" as="script">
    <link rel="preload" href="dist/css/styles.min.css" as="style">

    <!-- Critical CSS inlined -->
    <style>
      ${criticalCSS}
    </style>`

  // Insert resource hints and critical CSS after the existing meta tags
  const metaEndIndex = htmlContent.lastIndexOf('</title>') + '</title>'.length
  const beforeMeta = htmlContent.substring(0, metaEndIndex)
  const afterMeta = htmlContent.substring(metaEndIndex)

  // Replace CSS links with async loading
  let optimizedHTML = beforeMeta + resourceHints + afterMeta

  // Make CSS loading async (non-blocking)
  optimizedHTML = optimizedHTML.replace(
    /<link rel="stylesheet" href="([^"]+\.css)">/g,
    '<link rel="preload" href="$1" as="style" onload="this.onload=null;this.rel=\'stylesheet\'">' +
    '<noscript><link rel="stylesheet" href="$1"></noscript>'
  )

  // Update script references to use optimized versions
  optimizedHTML = optimizedHTML.replace(
    /src="js\/([^"]+)\.js"/g,
    'src="dist/js/bundle.min.js"'
  )

  // Remove individual script tags and replace with single optimized bundle
  optimizedHTML = optimizedHTML.replace(
    /<script src="js\/[^"]+\.js"><\/script>\s*/g,
    ''
  )

  // Add the optimized bundle script before closing body tag
  optimizedHTML = optimizedHTML.replace(
    '</body>',
    '    <script src="dist/js/bundle.min.js" defer></script>\n  </body>'
  )

  return optimizedHTML
}

function addResourceHintsOnly (htmlContent) {
  // Add just resource hints without critical CSS
  const resourceHints = `
    <!-- DNS prefetch for external resources -->
    <link rel="dns-prefetch" href="//api.github.com">
    <link rel="dns-prefetch" href="//fonts.googleapis.com">
    <link rel="dns-prefetch" href="//fonts.gstatic.com">

    <!-- Preconnect to critical third-party origins -->
    <link rel="preconnect" href="https://api.github.com" crossorigin>
    <link rel="preconnect" href="https://fonts.googleapis.com" crossorigin>
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>

    <!-- Preload critical assets -->
    <link rel="preload" href="dist/js/bundle.min.js" as="script">
    <link rel="preload" href="dist/css/styles.min.css" as="style">`

  // Insert resource hints after the existing meta tags
  const metaEndIndex = htmlContent.lastIndexOf('</title>') + '</title>'.length
  const beforeMeta = htmlContent.substring(0, metaEndIndex)
  const afterMeta = htmlContent.substring(metaEndIndex)

  let optimizedHTML = beforeMeta + resourceHints + afterMeta

  // Update CSS references to use optimized versions
  optimizedHTML = optimizedHTML.replace(
    /<link rel="stylesheet" href="css\/([^"]+)\.css">/g,
    '<link rel="stylesheet" href="dist/css/$1.css">'
  )

  // Update script references to use optimized versions
  optimizedHTML = optimizedHTML.replace(
    /src="js\/([^"]+)\.js"/g,
    'src="dist/js/bundle.min.js"'
  )

  // Remove individual script tags and replace with single optimized bundle
  optimizedHTML = optimizedHTML.replace(
    /<script src="js\/[^"]+\.js"><\/script>\s*/g,
    ''
  )

  // Add the optimized bundle script before closing body tag
  optimizedHTML = optimizedHTML.replace(
    '</body>',
    '    <script src="dist/js/bundle.min.js" defer></script>\n  </body>'
  )

  return optimizedHTML
}

// Run the optimization
optimizeHTML().catch(console.error)
