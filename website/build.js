#!/usr/bin/env node

import fs from 'fs'
import path from 'path'
import { execSync } from 'child_process'

console.log('🚀 Starting asset optimization pipeline...')

// Load content configuration for version information
let versionInfo = ''
try {
  const configPath = path.join('content', 'config.json')
  if (fs.existsSync(configPath)) {
    const config = JSON.parse(fs.readFileSync(configPath, 'utf8'))
    if (config.build) {
      const { version, commitHash, buildDate } = config.build

      let versionText = ''
      if (version) versionText += `Version ${version}`
      if (commitHash) {
        if (versionText) versionText += ' • '
        versionText += `Build ${commitHash}`
      }
      if (buildDate) {
        const date = new Date(buildDate)
        const formattedDate = date.toLocaleDateString('en-US', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        })
        if (versionText) versionText += ' • '
        versionText += `Built ${formattedDate}`
      }

      if (versionText) {
        versionInfo = `
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
    </div>`
      }
    }
  }
} catch (error) {
  console.log('⚠️  Could not load version information:', error.message)
}

console.log('🚀 Starting asset optimization pipeline...')

// Ensure dist directory exists
if (!fs.existsSync('dist')) {
  fs.mkdirSync('dist', { recursive: true })
}

// Create subdirectories
['css', 'js', 'images'].forEach(dir => {
  const dirPath = path.join('dist', dir)
  if (!fs.existsSync(dirPath)) {
    fs.mkdirSync(dirPath, { recursive: true })
  }
})

try {
  // Step 1: Optimize images
  console.log('📸 Optimizing images...')

  // Check if there are any images to optimize
  const imageExtensions = ['.jpg', '.jpeg', '.png', '.gif', '.svg']
  const hasRootImages = fs.readdirSync('.').some(file =>
    imageExtensions.some(ext => file.toLowerCase().endsWith(ext))
  )

  // Check for images directory
  const hasImagesDir = fs.existsSync('images') && fs.statSync('images').isDirectory()

  if (hasRootImages) {
    // Generate WebP versions
    try {
      execSync('imagemin "**/*.{jpg,jpeg,png}" --out-dir=dist/images --plugin=imagemin-webp --plugin.webp.quality=85', { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  WebP optimization skipped (no suitable images found)')
    }

    // Generate AVIF versions
    try {
      execSync('imagemin "**/*.{jpg,jpeg,png}" --out-dir=dist/images --plugin=imagemin-avif --plugin.avif.quality=80', { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  AVIF optimization skipped (no suitable images found)')
    }

    // Compress original formats
    try {
      execSync('imagemin "**/*.{jpg,jpeg,png,gif,svg}" --out-dir=dist/images --plugin=imagemin-mozjpeg --plugin.mozjpeg.quality=85 --plugin=imagemin-pngquant --plugin.pngquant.quality=[0.6,0.8] --plugin=imagemin-svgo', { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  Image compression had issues, continuing...')
    }
  } else if (hasImagesDir) {
    // Copy images directory to dist
    try {
      execSync('cp -r images dist/', { stdio: 'inherit' })
      console.log('✅ Images directory copied to dist/')
    } catch (e) {
      console.log('⚠️  Failed to copy images directory')
    }
  } else {
    console.log('📸 No images found to optimize')
  }

  // Step 2: Optimize CSS
  console.log('🎨 Optimizing CSS...')

  // Check if CSS files exist
  if (fs.existsSync('css') && fs.readdirSync('css').some(file => file.endsWith('.css'))) {
    // Purge unused CSS
    execSync('purgecss --config purgecss.config.cjs', { stdio: 'inherit' })

    // Minify CSS
    execSync('cleancss -o dist/css/styles.min.css css/*.css', { stdio: 'inherit' })
    console.log('✅ CSS optimized successfully')
  } else {
    console.log('🎨 No CSS files found to optimize')
  }

  // Step 3: Optimize JavaScript
  console.log('⚡ Optimizing JavaScript...')

  // Check if JS files exist
  if (fs.existsSync('js') && fs.readdirSync('js').some(file => file.endsWith('.js'))) {
    // Bundle JavaScript files
    const jsFiles = fs.readdirSync('js').filter(file => file.endsWith('.js'))
    let bundledContent = ''

    jsFiles.forEach(file => {
      const content = fs.readFileSync(path.join('js', file), 'utf8')
      bundledContent += `\n/* ${file} */\n${content}\n`
    })

    fs.writeFileSync('dist/js/bundle.js', bundledContent)

    // Minify bundled JavaScript
    execSync('terser dist/js/bundle.js -o dist/js/bundle.min.js --compress --mangle --source-map', { stdio: 'inherit' })
    console.log('✅ JavaScript optimized successfully')
  } else {
    console.log('⚡ No JavaScript files found to optimize')
  }

  console.log('📊 Generating optimization report...')

  // Step 5: Extract critical CSS and optimize HTML
  console.log('🎨 Extracting critical CSS and adding resource hints...')
  try {
    execSync('node critical-css.js', { stdio: 'inherit' })
    console.log('✅ Critical CSS and resource hints added successfully')
  } catch (e) {
    console.log('⚠️  Critical CSS extraction had issues, continuing with resource hints only...')
  }

  // Step 6: Add version information to HTML if available
  if (versionInfo && fs.existsSync('index.html')) {
    console.log('🏷️  Adding version information to HTML...')
    let htmlContent = fs.readFileSync('index.html', 'utf8')

    // Insert version info before closing body tag
    htmlContent = htmlContent.replace('</body>', `${versionInfo}\n  </body>`)

    // Copy to dist directory
    fs.writeFileSync('dist/index.html', htmlContent)
    console.log('✅ Version information added to HTML')
  } else if (fs.existsSync('index.html')) {
    // Copy HTML to dist without version info
    fs.copyFileSync('index.html', 'dist/index.html')
    console.log('📄 HTML copied to dist directory')
  }

  const report = {
    timestamp: new Date().toISOString(),
    optimizations: {
      images: (hasRootImages || hasImagesDir) ? 'Images processed and copied to dist/' : 'No images to optimize',
      css: fs.existsSync('dist/css/styles.min.css') ? 'Purged and minified' : 'No CSS to optimize',
      javascript: fs.existsSync('dist/js/bundle.min.js') ? 'Bundled and minified' : 'No JavaScript to optimize'
    }
  }

  fs.writeFileSync('dist/optimization-report.json', JSON.stringify(report, null, 2))

  console.log('🎉 Asset optimization pipeline completed successfully!')
  console.log('📁 Optimized assets are available in the dist/ directory')
} catch (error) {
  console.error('❌ Asset optimization failed:', error.message)
  process.exit(1)
}
