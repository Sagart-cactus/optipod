#!/usr/bin/env node

import fs from 'fs'
import path from 'path'
import { execSync } from 'child_process'

console.log('🚀 Starting asset optimization pipeline...')

const outputDir = process.env.OUTPUT_DIR
  ? path.resolve(process.env.OUTPUT_DIR)
  : path.resolve('dist')

function copyIfExists (sourcePath, destPath) {
  if (!fs.existsSync(sourcePath)) return
  fs.mkdirSync(path.dirname(destPath), { recursive: true })
  fs.copyFileSync(sourcePath, destPath)
}

function copyDirIfExists (sourceDir, destDir) {
  if (!fs.existsSync(sourceDir)) return
  fs.mkdirSync(destDir, { recursive: true })
  fs.cpSync(sourceDir, destDir, { recursive: true, force: true })
}

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

console.log(`📁 Output directory: ${outputDir}`)

// Ensure output directory exists
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true })
}

// Create subdirectories
['css', 'js', 'images'].forEach(dir => {
  const dirPath = path.join(outputDir, dir)
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
      execSync(`imagemin "**/*.{jpg,jpeg,png}" --out-dir="${path.join(outputDir, 'images')}" --plugin=imagemin-webp --plugin.webp.quality=85`, { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  WebP optimization skipped (no suitable images found)')
    }

    // Generate AVIF versions
    try {
      execSync(`imagemin "**/*.{jpg,jpeg,png}" --out-dir="${path.join(outputDir, 'images')}" --plugin=imagemin-avif --plugin.avif.quality=80`, { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  AVIF optimization skipped (no suitable images found)')
    }

    // Compress original formats
    try {
      execSync(`imagemin "**/*.{jpg,jpeg,png,gif,svg}" --out-dir="${path.join(outputDir, 'images')}" --plugin=imagemin-mozjpeg --plugin.mozjpeg.quality=85 --plugin=imagemin-pngquant --plugin.pngquant.quality=[0.6,0.8] --plugin=imagemin-svgo`, { stdio: 'inherit' })
    } catch (e) {
      console.log('⚠️  Image compression had issues, continuing...')
    }
  } else if (hasImagesDir) {
    // Copy images directory to output
    try {
      execSync(`cp -r images "${outputDir}/"`, { stdio: 'inherit' })
      console.log('✅ Images directory copied to output directory')
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
    execSync('purgecss --config purgecss.config.cjs', {
      stdio: 'inherit',
      env: { ...process.env, OUTPUT_DIR: outputDir }
    })

    // Minify CSS
    execSync(`cleancss -o "${path.join(outputDir, 'css', 'styles.min.css')}" css/*.css`, { stdio: 'inherit' })
    console.log('✅ CSS optimized successfully')
  } else {
    console.log('🎨 No CSS files found to optimize')
  }

  // Docs pages reference a standalone stylesheet for dev + production.
  copyIfExists(path.resolve('css', 'docs.css'), path.join(outputDir, 'css', 'docs.css'))
  copyIfExists(path.resolve('css', 'hljs.css'), path.join(outputDir, 'css', 'hljs.css'))

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

    fs.writeFileSync(path.join(outputDir, 'js', 'bundle.js'), bundledContent)

    // Minify bundled JavaScript
    execSync(`terser "${path.join(outputDir, 'js', 'bundle.js')}" -o "${path.join(outputDir, 'js', 'bundle.min.js')}" --compress --mangle --source-map`, { stdio: 'inherit' })
    console.log('✅ JavaScript optimized successfully')
  } else {
    console.log('⚡ No JavaScript files found to optimize')
  }

  console.log('📊 Generating optimization report...')

  // Step 4: Write optimized HTML
  if (fs.existsSync('index.html')) {
    console.log('📄 Writing optimized HTML...')
    let htmlContent = fs.readFileSync('index.html', 'utf8')

    if (versionInfo) {
      htmlContent = htmlContent.replace('</body>', `${versionInfo}\n  </body>`)
    }

    // Replace all CSS links with a single minified bundle.
    htmlContent = htmlContent.replace(
      /<link rel="stylesheet" href="css\/[^"]+">\s*/g,
      ''
    )
    htmlContent = htmlContent.replace(
      '</style>',
      '</style>\n    <link rel="stylesheet" href="css/styles.min.css">'
    )

    // Replace individual scripts with a single bundled script (keeps inline script).
    htmlContent = htmlContent.replace(
      /<script src="js\/[^"]+\.js"><\/script>\s*/g,
      ''
    )
    htmlContent = htmlContent.replace(
      '<script>',
      '    <script src="js/bundle.min.js" defer></script>\n    <script>'
    )

    fs.writeFileSync(path.join(outputDir, 'index.html'), htmlContent)
    console.log('✅ HTML written to output directory')
  }

  // Step 5: Copy static files (docs, sitemap, robots)
  console.log('📚 Copying static docs and metadata...')
  const outputDocsDir = path.join(outputDir, 'docs')
  if (fs.existsSync(outputDocsDir)) {
    fs.rmSync(outputDocsDir, { recursive: true, force: true })
  }
  copyDirIfExists(path.resolve('docs'), outputDocsDir)
  copyIfExists(path.resolve('sitemap.xml'), path.join(outputDir, 'sitemap.xml'))
  copyIfExists(path.resolve('robots.txt'), path.join(outputDir, 'robots.txt'))

  const report = {
    timestamp: new Date().toISOString(),
    optimizations: {
      images: (hasRootImages || hasImagesDir) ? 'Images processed and copied to output directory' : 'No images to optimize',
      css: fs.existsSync(path.join(outputDir, 'css', 'styles.min.css')) ? 'Purged and minified' : 'No CSS to optimize',
      javascript: fs.existsSync(path.join(outputDir, 'js', 'bundle.min.js')) ? 'Bundled and minified' : 'No JavaScript to optimize'
    }
  }

  fs.writeFileSync(path.join(outputDir, 'optimization-report.json'), JSON.stringify(report, null, 2))

  console.log('🎉 Asset optimization pipeline completed successfully!')
  console.log(`📁 Optimized assets are available in: ${outputDir}`)
} catch (error) {
  console.error('❌ Asset optimization failed:', error.message)
  process.exit(1)
}
