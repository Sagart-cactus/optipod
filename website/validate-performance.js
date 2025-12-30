#!/usr/bin/env node

import fs from 'fs'

console.log('🚀 Running comprehensive performance validation...')

// Performance validation results
const results = {
  timestamp: new Date().toISOString(),
  performance: {},
  accessibility: {},
  seo: {},
  optimizations: {},
  recommendations: []
}

try {
  // 1. Validate build artifacts exist
  console.log('📁 Validating build artifacts...')
  const requiredFiles = [
    '../docs/index.html',
    '../docs/js/bundle.min.js',
    '../docs/css/styles.min.css'
  ]

  let artifactsValid = true
  requiredFiles.forEach(file => {
    if (!fs.existsSync(file)) {
      console.log(`❌ Missing: ${file}`)
      artifactsValid = false
    } else {
      const stats = fs.statSync(file)
      console.log(`✅ Found: ${file} (${(stats.size / 1024).toFixed(2)}KB)`)
    }
  })

  if (!artifactsValid) {
    throw new Error('Required build artifacts are missing')
  }

  // 2. Analyze bundle sizes
  console.log('📊 Analyzing bundle sizes...')
  const bundleStats = fs.statSync('../docs/js/bundle.min.js')
  const cssStats = fs.statSync('../docs/css/styles.min.css')
  const htmlStats = fs.statSync('../docs/index.html')

  results.optimizations = {
    jsBundle: {
      size: bundleStats.size,
      sizeKB: (bundleStats.size / 1024).toFixed(2),
      compressed: bundleStats.size < 100000 // Less than 100KB is good
    },
    cssBundle: {
      size: cssStats.size,
      sizeKB: (cssStats.size / 1024).toFixed(2),
      compressed: cssStats.size < 50000 // Less than 50KB is good
    },
    html: {
      size: htmlStats.size,
      sizeKB: (htmlStats.size / 1024).toFixed(2),
      optimized: htmlStats.size < 200000 // Less than 200KB is good
    }
  }

  // 3. Check for critical CSS
  console.log('🎨 Checking critical CSS optimization...')
  const htmlContent = fs.readFileSync('../docs/index.html', 'utf8')
  const hasCriticalCSS = htmlContent.includes('<style>')
  const hasResourceHints = htmlContent.includes('rel="preload"') || htmlContent.includes('rel="preconnect"') || htmlContent.includes('dns-prefetch')

  results.optimizations.criticalCSS = hasCriticalCSS
  results.optimizations.resourceHints = hasResourceHints

  if (hasCriticalCSS) {
    console.log('✅ Critical CSS is inlined')
  } else {
    console.log('⚠️  Critical CSS not found')
    results.recommendations.push('Consider implementing critical CSS inlining')
  }

  if (hasResourceHints) {
    console.log('✅ Resource hints are present')
  } else {
    console.log('⚠️  Resource hints not found')
    results.recommendations.push('Add resource hints for better performance')
  }

  // 4. Validate accessibility features
  console.log('♿ Validating accessibility features...')
  const accessibilityChecks = {
    skipLink: htmlContent.includes('skip-link'),
    ariaLabels: htmlContent.includes('aria-label'),
    semanticHTML: htmlContent.includes('<main') && htmlContent.includes('<nav'),
    altText: htmlContent.includes('alt=') || htmlContent.includes('aria-label'),
    focusManagement: htmlContent.includes('tabindex') && htmlContent.includes('focus')
  }

  results.accessibility = accessibilityChecks

  Object.entries(accessibilityChecks).forEach(([check, passed]) => {
    if (passed) {
      console.log(`✅ ${check}: Present`)
    } else {
      console.log(`❌ ${check}: Missing`)
      results.recommendations.push(`Improve ${check} for better accessibility`)
    }
  })

  // 5. Validate SEO elements
  console.log('🔍 Validating SEO elements...')
  const seoChecks = {
    metaTags: htmlContent.includes('name="description"'),
    openGraph: htmlContent.includes('property="og:'),
    twitterCards: htmlContent.includes('property="twitter:'),
    structuredData: htmlContent.includes('application/ld+json'),
    canonicalURL: htmlContent.includes('rel="canonical"'),
    sitemap: htmlContent.includes('sitemap.xml')
  }

  results.seo = seoChecks

  Object.entries(seoChecks).forEach(([check, passed]) => {
    if (passed) {
      console.log(`✅ ${check}: Present`)
    } else {
      console.log(`❌ ${check}: Missing`)
      results.recommendations.push(`Add ${check} for better SEO`)
    }
  })

  // 6. Check JavaScript integration
  console.log('⚡ Validating JavaScript integration...')
  const jsContent = fs.readFileSync('../docs/js/bundle.min.js', 'utf8')
  const jsChecks = {
    analytics: jsContent.includes('Analytics'),
    githubStats: jsContent.includes('GitHubStats'),
    featuresGrid: jsContent.includes('FeaturesGrid'),
    comparisonTable: jsContent.includes('ComparisonTable'),
    codeExamples: jsContent.includes('CodeExamples')
  }

  Object.entries(jsChecks).forEach(([component, present]) => {
    if (present) {
      console.log(`✅ ${component}: Bundled`)
    } else {
      console.log(`❌ ${component}: Missing from bundle`)
      results.recommendations.push(`Ensure ${component} is properly bundled`)
    }
  })

  // 7. Performance score calculation
  console.log('📈 Calculating performance score...')
  const performanceFactors = {
    bundleSize: results.optimizations.jsBundle.compressed ? 25 : 15,
    cssSize: results.optimizations.cssBundle.compressed ? 15 : 10,
    criticalCSS: results.optimizations.criticalCSS ? 20 : 10,
    resourceHints: results.optimizations.resourceHints ? 15 : 5,
    accessibility: Object.values(accessibilityChecks).filter(Boolean).length * 3,
    seo: Object.values(seoChecks).filter(Boolean).length * 2
  }

  const totalScore = Object.values(performanceFactors).reduce((sum, score) => sum + score, 0)
  results.performance.score = totalScore
  results.performance.maxScore = 100
  results.performance.percentage = Math.round((totalScore / 100) * 100)

  console.log(`📊 Performance Score: ${results.performance.percentage}/100`)

  if (results.performance.percentage >= 90) {
    console.log('🎉 Excellent performance!')
  } else if (results.performance.percentage >= 75) {
    console.log('👍 Good performance with room for improvement')
  } else {
    console.log('⚠️  Performance needs optimization')
  }

  // 8. Generate recommendations
  if (results.recommendations.length === 0) {
    results.recommendations.push('All performance optimizations are in place!')
  }

  // 9. Save results
  fs.writeFileSync('../docs/performance-report.json', JSON.stringify(results, null, 2))
  console.log('📄 Performance report saved to docs/performance-report.json')

  // 10. Final validation summary
  console.log('\n🎯 Final Validation Summary:')
  console.log(`Performance Score: ${results.performance.percentage}/100`)
  console.log(`Accessibility Features: ${Object.values(accessibilityChecks).filter(Boolean).length}/${Object.keys(accessibilityChecks).length}`)
  console.log(`SEO Elements: ${Object.values(seoChecks).filter(Boolean).length}/${Object.keys(seoChecks).length}`)
  console.log(`Bundle Optimization: ${results.optimizations.jsBundle.compressed ? 'Optimized' : 'Needs optimization'}`)

  if (results.recommendations.length > 1) {
    console.log('\n💡 Recommendations:')
    results.recommendations.forEach((rec, index) => {
      console.log(`${index + 1}. ${rec}`)
    })
  }

  console.log('\n✅ Performance validation completed successfully!')
} catch (error) {
  console.error('❌ Performance validation failed:', error.message)
  process.exit(1)
}
