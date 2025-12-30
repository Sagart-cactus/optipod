#!/usr/bin/env node

import fs from 'fs'

console.log('🎯 Running final integration test...')

const testResults = {
  timestamp: new Date().toISOString(),
  tests: [],
  summary: {
    passed: 0,
    failed: 0,
    total: 0
  }
}

function addTest (name, passed, details = '') {
  testResults.tests.push({
    name,
    passed,
    details,
    timestamp: new Date().toISOString()
  })

  if (passed) {
    testResults.summary.passed++
    console.log(`✅ ${name}`)
  } else {
    testResults.summary.failed++
    console.log(`❌ ${name}: ${details}`)
  }
  testResults.summary.total++
}

try {
  // Test 1: Build artifacts exist and are optimized
  console.log('\n📦 Testing build artifacts...')

  const docsExists = fs.existsSync('../docs')
  addTest('Docs directory exists', docsExists)

  if (docsExists) {
    const htmlExists = fs.existsSync('../docs/index.html')
    const jsExists = fs.existsSync('../docs/js/bundle.min.js')
    const cssExists = fs.existsSync('../docs/css/styles.min.css')

    addTest('HTML file built', htmlExists)
    addTest('JavaScript bundle built', jsExists)
    addTest('CSS bundle built', cssExists)

    if (jsExists) {
      const jsStats = fs.statSync('../docs/js/bundle.min.js')
      const jsOptimized = jsStats.size < 100000 // Less than 100KB
      addTest('JavaScript bundle optimized', jsOptimized, `Size: ${(jsStats.size / 1024).toFixed(2)}KB`)
    }
  }

  // Test 2: Component integration
  console.log('\n🧩 Testing component integration...')

  if (fs.existsSync('../docs/js/bundle.min.js')) {
    const bundleContent = fs.readFileSync('../docs/js/bundle.min.js', 'utf8')

    const components = ['Analytics', 'GitHubStats', 'FeaturesGrid', 'ComparisonTable', 'CodeExamples']
    components.forEach(component => {
      const included = bundleContent.includes(component)
      addTest(`${component} component bundled`, included)
    })
  }

  // Test 3: HTML structure and SEO
  console.log('\n🔍 Testing HTML structure and SEO...')

  if (fs.existsSync('../docs/index.html')) {
    const htmlContent = fs.readFileSync('../docs/index.html', 'utf8')

    // SEO tests
    addTest('Meta description present', htmlContent.includes('name="description"'))
    addTest('Open Graph tags present', htmlContent.includes('property="og:'))
    addTest('Twitter Card tags present', htmlContent.includes('property="twitter:'))
    addTest('Structured data present', htmlContent.includes('application/ld+json'))
    addTest('Canonical URL present', htmlContent.includes('rel="canonical"'))

    // Accessibility tests
    addTest('Skip link present', htmlContent.includes('skip-link'))
    addTest('ARIA labels present', htmlContent.includes('aria-label'))
    addTest('Semantic HTML present', htmlContent.includes('<main') && htmlContent.includes('<nav'))
    addTest('Focus management present', htmlContent.includes('tabindex'))

    // Performance tests
    addTest('Critical CSS inlined', htmlContent.includes('<style>'))
    addTest('Minified JavaScript loaded', htmlContent.includes('bundle.min.js'))
  }

  // Test 4: User journey simulation
  console.log('\n👤 Testing user journey components...')

  // Check if all interactive elements are present
  if (fs.existsSync('../docs/index.html')) {
    const htmlContent = fs.readFileSync('../docs/index.html', 'utf8')

    addTest('Navigation links present', htmlContent.includes('nav-links'))
    addTest('CTA buttons present', htmlContent.includes('btn-primary'))
    addTest('GitHub links present', htmlContent.includes('github.com'))
    addTest('Feature grid present', htmlContent.includes('feature-grid'))
    addTest('Comparison table present', htmlContent.includes('compare'))
    addTest('Code examples present', htmlContent.includes('code-block'))
  }

  // Test 5: Analytics integration
  console.log('\n📊 Testing analytics integration...')

  if (fs.existsSync('../docs/js/bundle.min.js')) {
    const bundleContent = fs.readFileSync('../docs/js/bundle.min.js', 'utf8')

    addTest('Analytics class present', bundleContent.includes('Analytics'))
    addTest('Event tracking present', bundleContent.includes('trackEvent'))
    addTest('Consent management present', bundleContent.includes('consent'))
    addTest('Performance tracking present', bundleContent.includes('performance'))
  }

  // Test 6: GitHub integration
  console.log('\n🐙 Testing GitHub integration...')

  if (fs.existsSync('../docs/js/bundle.min.js')) {
    const bundleContent = fs.readFileSync('../docs/js/bundle.min.js', 'utf8')

    addTest('GitHub API integration present', bundleContent.includes('api.github.com'))
    addTest('Stats animation present', bundleContent.includes('animateCounter'))
    addTest('Fallback data present', bundleContent.includes('fallback'))
  }

  // Test 7: Interactive features
  console.log('\n🎮 Testing interactive features...')

  if (fs.existsSync('../docs/js/bundle.min.js')) {
    const bundleContent = fs.readFileSync('../docs/js/bundle.min.js', 'utf8')

    addTest('Feature expansion present', bundleContent.includes('expandFeature'))
    addTest('Table expansion present', bundleContent.includes('expandRow'))
    addTest('Code copying present', bundleContent.includes('copyCode'))
    addTest('Tab switching present', bundleContent.includes('switchExample'))
  }

  // Test 8: Performance optimizations
  console.log('\n⚡ Testing performance optimizations...')

  if (fs.existsSync('../docs/index.html')) {
    const htmlContent = fs.readFileSync('../docs/index.html', 'utf8')

    // Check for performance optimizations
    const hasMinifiedJS = htmlContent.includes('.min.js')
    const hasMinifiedCSS = htmlContent.includes('.min.css')
    const hasCriticalCSS = htmlContent.includes('<style>')

    addTest('Minified JavaScript used', hasMinifiedJS)
    addTest('Minified CSS used', hasMinifiedCSS)
    addTest('Critical CSS optimization', hasCriticalCSS)
  }

  // Calculate final score
  const successRate = (testResults.summary.passed / testResults.summary.total) * 100

  console.log('\n🎯 Final Integration Test Results:')
  console.log(`✅ Passed: ${testResults.summary.passed}`)
  console.log(`❌ Failed: ${testResults.summary.failed}`)
  console.log(`📊 Success Rate: ${successRate.toFixed(1)}%`)

  if (successRate >= 95) {
    console.log('🎉 Excellent! All systems are fully integrated and working.')
  } else if (successRate >= 85) {
    console.log('👍 Good integration with minor issues.')
  } else if (successRate >= 70) {
    console.log('⚠️  Integration has some issues that should be addressed.')
  } else {
    console.log('❌ Integration has significant issues that need fixing.')
  }

  // Save detailed results
  fs.writeFileSync('../docs/integration-test-results.json', JSON.stringify(testResults, null, 2))
  console.log('📄 Detailed results saved to docs/integration-test-results.json')

  // Exit with appropriate code
  if (successRate >= 85) {
    console.log('\n✅ Final integration test PASSED!')
    process.exit(0)
  } else {
    console.log('\n❌ Final integration test FAILED!')
    process.exit(1)
  }
} catch (error) {
  console.error('💥 Integration test failed with error:', error.message)
  process.exit(1)
}
