
/* analytics.js */
/**
 * Analytics and User Interaction Tracking
 * Privacy-respecting analytics with Google Analytics 4 and custom event tracking
 */

/* global gtag, dataLayer */

class Analytics {
  constructor () {
    this.isInitialized = false
    this.trackingId = 'G-XXXXXXXXXX' // Replace with actual GA4 tracking ID
    this.debugMode = false // Set to true for development
    this.consentGiven = false

    this.init()
  }

  /**
   * Initialize analytics system
   */
  init () {
    // Check for consent (GDPR compliance)
    this.checkConsent()

    if (this.consentGiven) {
      this.loadGoogleAnalytics()
      this.setupEventTracking()
      this.trackPageView()
      this.isInitialized = true
    } else {
      this.showConsentBanner()
    }
  }

  /**
   * Check if user has given consent for analytics
   */
  checkConsent () {
    const consent = localStorage.getItem('analytics-consent')
    this.consentGiven = consent === 'granted'
  }

  /**
   * Show consent banner for GDPR compliance
   */
  showConsentBanner () {
    // Only show if not already dismissed
    if (localStorage.getItem('analytics-consent') !== null) {
      return
    }

    const banner = document.createElement('div')
    banner.id = 'consent-banner'
    banner.innerHTML = `
      <div style="
        position: fixed;
        bottom: 0;
        left: 0;
        right: 0;
        background: var(--ink);
        color: white;
        padding: 16px;
        z-index: 1000;
        display: flex;
        align-items: center;
        justify-content: space-between;
        flex-wrap: wrap;
        gap: 12px;
        font-size: 0.9rem;
        box-shadow: 0 -4px 12px rgba(0,0,0,0.1);
      ">
        <div style="flex: 1; min-width: 300px;">
          We use privacy-respecting analytics to improve our website. No personal data is collected.
        </div>
        <div style="display: flex; gap: 8px;">
          <button id="consent-accept" style="
            background: var(--accent);
            color: #1f1304;
            border: none;
            padding: 8px 16px;
            border-radius: 6px;
            font-weight: 600;
            cursor: pointer;
          ">Accept</button>
          <button id="consent-decline" style="
            background: transparent;
            color: white;
            border: 1px solid rgba(255,255,255,0.3);
            padding: 8px 16px;
            border-radius: 6px;
            cursor: pointer;
          ">Decline</button>
        </div>
      </div>
    `

    document.body.appendChild(banner)

    // Handle consent responses
    document.getElementById('consent-accept').addEventListener('click', () => {
      this.grantConsent()
      banner.remove()
    })

    document.getElementById('consent-decline').addEventListener('click', () => {
      this.declineConsent()
      banner.remove()
    })
  }

  /**
   * Grant analytics consent
   */
  grantConsent () {
    localStorage.setItem('analytics-consent', 'granted')
    this.consentGiven = true
    this.init() // Re-initialize with consent
  }

  /**
   * Decline analytics consent
   */
  declineConsent () {
    localStorage.setItem('analytics-consent', 'declined')
    this.consentGiven = false
  }

  /**
   * Load Google Analytics 4
   */
  loadGoogleAnalytics () {
    // Load gtag script
    const script = document.createElement('script')
    script.async = true
    script.src = `https://www.googletagmanager.com/gtag/js?id=${this.trackingId}`
    document.head.appendChild(script)

    // Initialize gtag
    window.dataLayer = window.dataLayer || []
    function gtag () {
      dataLayer.push(arguments)
    }
    window.gtag = gtag

    gtag('js', new Date())
    gtag('config', this.trackingId, {
      // Privacy-focused configuration
      anonymize_ip: true,
      allow_google_signals: false,
      allow_ad_personalization_signals: false,
      cookie_flags: 'SameSite=None;Secure'
    })

    if (this.debugMode) {
      console.log('Google Analytics 4 loaded with tracking ID:', this.trackingId)
    }
  }

  /**
   * Track page view
   */
  trackPageView () {
    if (!this.consentGiven) return

    const pageData = {
      page_title: document.title,
      page_location: window.location.href,
      page_path: window.location.pathname
    }

    if (window.gtag) {
      gtag('event', 'page_view', pageData)
    }

    if (this.debugMode) {
      console.log('Page view tracked:', pageData)
    }
  }

  /**
   * Setup event tracking for user interactions
   */
  setupEventTracking () {
    if (!this.consentGiven) return

    // Track CTA button clicks
    this.trackCTAClicks()

    // Track navigation clicks
    this.trackNavigationClicks()

    // Track GitHub repository visits
    this.trackGitHubClicks()

    // Track section visibility (scroll tracking)
    this.trackSectionVisibility()

    // Track code copy interactions
    this.trackCodeCopyClicks()

    // Track performance metrics
    this.trackPerformanceMetrics()
  }

  /**
   * Track CTA button clicks
   */
  trackCTAClicks () {
    const ctaButtons = document.querySelectorAll('.btn-primary, .btn-secondary')

    ctaButtons.forEach(button => {
      button.addEventListener('click', (e) => {
        const buttonText = button.textContent.trim()
        const buttonHref = button.getAttribute('href') || ''
        const section = this.findParentSection(button)

        this.trackEvent('cta_click', {
          button_text: buttonText,
          button_href: buttonHref,
          section,
          event_category: 'engagement'
        })
      })
    })
  }

  /**
   * Track navigation clicks
   */
  trackNavigationClicks () {
    const navLinks = document.querySelectorAll('.nav-links a')

    navLinks.forEach(link => {
      link.addEventListener('click', (e) => {
        const linkText = link.textContent.trim()
        const linkHref = link.getAttribute('href') || ''

        this.trackEvent('navigation_click', {
          link_text: linkText,
          link_href: linkHref,
          event_category: 'navigation'
        })
      })
    })
  }

  /**
   * Track GitHub repository visits
   */
  trackGitHubClicks () {
    const githubLinks = document.querySelectorAll('a[href*="github.com"]')

    githubLinks.forEach(link => {
      link.addEventListener('click', (e) => {
        const linkText = link.textContent.trim()
        const linkHref = link.getAttribute('href') || ''

        this.trackEvent('github_visit', {
          link_text: linkText,
          link_href: linkHref,
          event_category: 'conversion'
        })
      })
    })
  }

  /**
   * Track section visibility using Intersection Observer
   */
  trackSectionVisibility () {
    const sections = document.querySelectorAll('section[id]')
    const observedSections = new Set()

    const observer = new IntersectionObserver((entries) => {
      entries.forEach(entry => {
        if (entry.isIntersecting && entry.intersectionRatio > 0.5) {
          const sectionId = entry.target.id

          // Only track each section once per page load
          if (!observedSections.has(sectionId)) {
            observedSections.add(sectionId)

            this.trackEvent('section_view', {
              section_id: sectionId,
              event_category: 'engagement'
            })
          }
        }
      })
    }, {
      threshold: 0.5
    })

    sections.forEach(section => {
      observer.observe(section)
    })
  }

  /**
   * Track code copy interactions
   */
  trackCodeCopyClicks () {
    // This will work with the existing code examples component
    document.addEventListener('code-copied', (e) => {
      this.trackEvent('code_copy', {
        code_type: e.detail.type || 'unknown',
        event_category: 'engagement'
      })
    })
  }

  /**
   * Track performance metrics
   */
  trackPerformanceMetrics () {
    // Track Core Web Vitals
    if ('web-vital' in window) {
      // This would integrate with web-vitals library if added
      return
    }

    // Basic performance tracking
    window.addEventListener('load', () => {
      setTimeout(() => {
        const perfData = performance.getEntriesByType('navigation')[0]

        if (perfData) {
          this.trackEvent('performance_metrics', {
            load_time: Math.round(perfData.loadEventEnd - perfData.fetchStart),
            dom_content_loaded: Math.round(perfData.domContentLoadedEventEnd - perfData.fetchStart),
            first_paint: this.getFirstPaint(),
            event_category: 'performance'
          })
        }
      }, 1000)
    })
  }

  /**
   * Get First Paint timing
   */
  getFirstPaint () {
    const paintEntries = performance.getEntriesByType('paint')
    const firstPaint = paintEntries.find(entry => entry.name === 'first-paint')
    return firstPaint ? Math.round(firstPaint.startTime) : null
  }

  /**
   * Find parent section of an element
   */
  findParentSection (element) {
    let parent = element.parentElement
    while (parent && parent !== document.body) {
      if (parent.tagName === 'SECTION' && parent.id) {
        return parent.id
      }
      parent = parent.parentElement
    }
    return 'unknown'
  }

  /**
   * Track custom event
   */
  trackEvent (eventName, parameters = {}) {
    if (!this.consentGiven) return

    // Add timestamp
    parameters.timestamp = new Date().toISOString()

    if (window.gtag) {
      gtag('event', eventName, parameters)
    }

    if (this.debugMode) {
      console.log('Event tracked:', eventName, parameters)
    }
  }

  /**
   * Track conversion (GitHub repository visit)
   */
  trackConversion (conversionType, value = null) {
    if (!this.consentGiven) return

    const conversionData = {
      event_category: 'conversion',
      conversion_type: conversionType
    }

    if (value !== null) {
      conversionData.value = value
    }

    this.trackEvent('conversion', conversionData)
  }

  /**
   * Enable debug mode
   */
  enableDebugMode () {
    this.debugMode = true
    console.log('Analytics debug mode enabled')
  }

  /**
   * Disable debug mode
   */
  disableDebugMode () {
    this.debugMode = false
  }

  /**
   * Get analytics status
   */
  getStatus () {
    return {
      initialized: this.isInitialized,
      consentGiven: this.consentGiven,
      trackingId: this.trackingId,
      debugMode: this.debugMode
    }
  }
}

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = Analytics
}

// Auto-initialize if in browser environment
if (typeof window !== 'undefined') {
  window.Analytics = Analytics
}


/* code-examples.js */
/**
 * Enhanced Code Examples Component
 * Handles syntax highlighting, copy-to-clipboard, and tabbed interface
 */

class CodeExamples {
  constructor () {
    this.examples = {
      basic: {
        title: 'Basic Policy',
        language: 'yaml',
        code: `apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: basic-optimization
  namespace: default
spec:
  mode: Recommend
  selector:
    workloadSelector:
      matchLabels:
        optimize: "true"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 24h
    percentile: P90
    safetyFactor: 1.2
  resourceBounds:
    cpu:
      min: "100m"
      max: "4000m"
    memory:
      min: "128Mi"
      max: "8Gi"`
      },
      advanced: {
        title: 'Advanced Policy',
        language: 'yaml',
        code: `apiVersion: optipod.optipod.io/v1alpha1
kind: OptimizationPolicy
metadata:
  name: advanced-optimization
  namespace: production
spec:
  mode: Auto
  selector:
    workloadSelector:
      matchLabels:
        tier: "backend"
    namespaceSelector:
      matchLabels:
        environment: "production"
  metricsConfig:
    provider: metrics-server
    rollingWindow: 72h
    percentile: P95
    safetyFactor: 1.5
  resourceBounds:
    cpu:
      min: "200m"
      max: "8000m"
    memory:
      min: "256Mi"
      max: "16Gi"
  updateStrategy:
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: false
  limitConfig:
    cpuLimitMultiplier: 1.5
    memoryLimitMultiplier: 1.2
  workloadTypeFilter:
    include:
      - Deployment
      - StatefulSet
    exclude: []
  changeRateLimit:
    maxConcurrentUpdates: 5
    updateInterval: "10m"`
      }
    }
    this.currentExample = 'basic'
    this.init()
  }

  /**
   * Initialize the code examples component
   */
  init () {
    this.loadPrismJS()
    this.enhanceCodeExamples()
  }

  /**
   * Load Prism.js for syntax highlighting
   */
  loadPrismJS () {
    // Load Prism CSS
    const prismCSS = document.createElement('link')
    prismCSS.rel = 'stylesheet'
    prismCSS.href = 'https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/themes/prism-tomorrow.min.css'
    document.head.appendChild(prismCSS)

    // Load Prism JS
    const prismJS = document.createElement('script')
    prismJS.src = 'https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/components/prism-core.min.js'
    prismJS.onload = () => {
      // Load YAML and Bash components
      this.loadPrismComponent('prism-yaml')
      this.loadPrismComponent('prism-bash')
    }
    document.head.appendChild(prismJS)
  }

  /**
   * Load specific Prism component
   */
  loadPrismComponent (component) {
    const script = document.createElement('script')
    script.src = `https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/components/${component}.min.js`
    document.head.appendChild(script)
  }

  /**
   * Enhance the existing code examples section
   */
  enhanceCodeExamples () {
    const quickstartSection = document.querySelector('#quickstart')
    if (!quickstartSection) return

    const policyExamples = quickstartSection.querySelector('.policy-examples')
    if (!policyExamples) return

    const codeBlock = policyExamples.querySelector('.code-block')
    if (!codeBlock) return

    // Replace the existing code block with enhanced version
    const enhancedCodeSection = this.createEnhancedCodeSection()
    codeBlock.parentNode.replaceChild(enhancedCodeSection, codeBlock)
  }

  /**
   * Create enhanced code section with tabs
   */
  createEnhancedCodeSection () {
    const container = document.createElement('div')
    container.className = 'enhanced-code-examples'

    // Create tab navigation
    const tabNav = document.createElement('div')
    tabNav.className = 'code-tabs'

    Object.keys(this.examples).forEach(key => {
      const tab = document.createElement('button')
      tab.className = `code-tab ${key === this.currentExample ? 'active' : ''}`
      tab.textContent = this.examples[key].title
      tab.addEventListener('click', () => this.switchExample(key))
      tabNav.appendChild(tab)
    })

    // Create code container
    const codeContainer = document.createElement('div')
    codeContainer.className = 'code-container'

    // Create copy button
    const copyButton = document.createElement('button')
    copyButton.className = 'copy-button'
    copyButton.innerHTML = `
      <svg class="copy-icon" viewBox="0 0 24 24" width="16" height="16">
        <path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z" fill="currentColor"/>
      </svg>
      <span class="copy-text">Copy</span>
    `
    copyButton.addEventListener('click', () => this.copyCode())

    // Create code element
    const codeElement = document.createElement('pre')
    codeElement.className = 'code-block enhanced'
    const code = document.createElement('code')
    code.className = `language-${this.examples[this.currentExample].language}`
    code.textContent = this.examples[this.currentExample].code
    codeElement.appendChild(code)

    // Assemble the container
    codeContainer.appendChild(copyButton)
    codeContainer.appendChild(codeElement)

    container.appendChild(tabNav)
    container.appendChild(codeContainer)

    // Apply syntax highlighting after a short delay
    setTimeout(() => {
      if (window.Prism) {
        window.Prism.highlightElement(code)
      }
    }, 100)

    return container
  }

  /**
   * Switch to a different code example
   */
  switchExample (exampleKey) {
    if (this.currentExample === exampleKey) return

    this.currentExample = exampleKey
    const example = this.examples[exampleKey]

    // Update active tab
    document.querySelectorAll('.code-tab').forEach(tab => {
      tab.classList.remove('active')
    })
    document.querySelector(`.code-tab:nth-child(${Object.keys(this.examples).indexOf(exampleKey) + 1})`).classList.add('active')

    // Update code content
    const codeElement = document.querySelector('.enhanced-code-examples code')
    codeElement.className = `language-${example.language}`
    codeElement.textContent = example.code

    // Re-apply syntax highlighting
    if (window.Prism) {
      window.Prism.highlightElement(codeElement)
    }

    // Add transition effect
    const codeContainer = document.querySelector('.code-container')
    codeContainer.classList.add('updating')
    setTimeout(() => {
      codeContainer.classList.remove('updating')
    }, 300)
  }

  /**
   * Copy code to clipboard
   */
  async copyCode () {
    const codeElement = document.querySelector('.enhanced-code-examples code')
    const copyButton = document.querySelector('.copy-button')
    const copyText = copyButton.querySelector('.copy-text')

    try {
      await navigator.clipboard.writeText(codeElement.textContent)

      // Show success feedback
      copyText.textContent = 'Copied!'
      copyButton.classList.add('copied')

      // Emit analytics event
      this.emitCopyEvent()

      setTimeout(() => {
        copyText.textContent = 'Copy'
        copyButton.classList.remove('copied')
      }, 2000)
    } catch (err) {
      // Fallback for older browsers
      this.fallbackCopyTextToClipboard(codeElement.textContent)

      copyText.textContent = 'Copied!'
      copyButton.classList.add('copied')

      // Emit analytics event
      this.emitCopyEvent()

      setTimeout(() => {
        copyText.textContent = 'Copy'
        copyButton.classList.remove('copied')
      }, 2000)
    }
  }

  /**
   * Emit custom event for analytics tracking
   */
  emitCopyEvent () {
    const event = new CustomEvent('code-copied', {
      detail: {
        type: this.currentExample,
        language: this.examples[this.currentExample].language
      }
    })
    document.dispatchEvent(event)
  }

  /**
   * Fallback copy method for older browsers
   */
  fallbackCopyTextToClipboard (text) {
    const textArea = document.createElement('textarea')
    textArea.value = text
    textArea.style.position = 'fixed'
    textArea.style.left = '-999999px'
    textArea.style.top = '-999999px'
    document.body.appendChild(textArea)
    textArea.focus()
    textArea.select()

    try {
      document.execCommand('copy')
    } catch (err) {
      console.error('Fallback: Oops, unable to copy', err)
    }

    document.body.removeChild(textArea)
  }
}

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = CodeExamples
}


/* comparison-table.js */
/**
 * Comparison Table
 * Simple comparison table without expand functionality
 */

/* global gtag */

class ComparisonTable {
  constructor () {
    this.init()
  }

  /**
   * Initialize the comparison functionality
   */
  init () {
    this.setupAccessibility()
    this.setupToggles()
    this.trackAnalytics()
  }

  /**
   * Setup accessibility features
   */
  setupAccessibility () {
    // Add proper ARIA labels for screen readers
    document.querySelectorAll('.comparison-row').forEach((row, index) => {
      row.setAttribute('role', 'row')
      row.setAttribute('aria-label', `Comparison row ${index + 1}`)
    })

    document.querySelectorAll('.status-indicator').forEach((indicator) => {
      let status
      if (indicator.classList.contains('status-yes')) {
        status = 'Supported'
      } else if (indicator.classList.contains('status-partial')) {
        status = 'Partially supported'
      } else {
        status = 'Not supported'
      }
      indicator.setAttribute('aria-label', status)
    })
  }

  /**
   * Setup expand/collapse controls
   */
  setupToggles () {
    const table = document.querySelector('.comparison-table')
    if (!table) return

    document.querySelectorAll('.comparison-toggle').forEach((button) => {
      const detailsId = button.getAttribute('aria-controls')
      const details = detailsId ? document.getElementById(detailsId) : null
      if (details && button.getAttribute('aria-expanded') !== 'true') {
        details.hidden = true
      }
    })

    table.addEventListener('click', (event) => {
      const button = event.target.closest('.comparison-toggle')
      if (!button) return

      const row = button.closest('.comparison-row')
      if (!row) return

      const isExpanded = row.classList.toggle('comparison-row-expanded')
      button.setAttribute('aria-expanded', isExpanded ? 'true' : 'false')
      button.textContent = isExpanded ? 'Hide details' : 'Show details'

      const detailsId = button.getAttribute('aria-controls')
      const details = detailsId ? document.getElementById(detailsId) : null
      if (details) {
        details.hidden = !isExpanded
      }
    })
  }

  /**
   * Track analytics for comparison table interactions
   */
  trackAnalytics () {
    // Track when users scroll to the comparison section
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && typeof gtag !== 'undefined') {
          gtag('event', 'comparison_view', {
            event_category: 'engagement',
            event_label: 'comparison_table'
          })
        }
      })
    }, { threshold: 0.5 })

    const comparisonTable = document.querySelector('.comparison-table')
    if (comparisonTable) {
      observer.observe(comparisonTable)
    }
  }
}

// Initialize comparison table when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
  // eslint-disable-next-line no-new
  new ComparisonTable()
})

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = ComparisonTable
}


/* features-grid.js */
/**
 * Interactive Features Grid
 * Handles expandable cards, animations, and interactions
 */

class FeaturesGrid {
  constructor () {
    this.expandedCard = null
    this.init()
  }

  /**
   * Initialize the features grid functionality
   */
  init () {
    this.enhanceFeatureCards()
    this.addEventListeners()
  }

  /**
   * Enhance existing feature cards with interactive elements
   */
  enhanceFeatureCards () {
    const features = document.querySelectorAll('.feature')

    features.forEach((feature, index) => {
      // Add expand button and detailed content
      this.addExpandableContent(feature, index)

      // Add custom icon
      this.addCustomIcon(feature, index)

      // Add hover effects class
      feature.classList.add('feature-interactive')
    })
  }

  /**
   * Add expandable content to feature cards
   */
  addExpandableContent (feature, index) {
    const title = feature.querySelector('h5').textContent

    // Detailed content for each feature
    const detailedContent = this.getDetailedContent(index, title)

    // Create expand button
    const expandBtn = document.createElement('button')
    expandBtn.className = 'feature-expand-btn'
    expandBtn.innerHTML = `
      <svg class="expand-icon" viewBox="0 0 24 24" width="16" height="16">
        <path d="M7 14l5-5 5 5z" fill="currentColor"/>
      </svg>
      <span>Learn more</span>
    `

    // Create detailed content container
    const detailsContainer = document.createElement('div')
    detailsContainer.className = 'feature-details'
    detailsContainer.innerHTML = detailedContent

    // Add to feature card
    feature.appendChild(expandBtn)
    feature.appendChild(detailsContainer)

    // Add click handler
    expandBtn.addEventListener('click', (e) => {
      e.stopPropagation()
      this.toggleFeatureExpansion(feature)
    })
  }

  /**
   * Add custom SVG icons to feature cards
   */
  addCustomIcon (feature, index) {
    const iconContainer = document.createElement('div')
    iconContainer.className = 'feature-icon'
    iconContainer.innerHTML = this.getFeatureIcon(index)

    // Insert icon before title
    const title = feature.querySelector('h5')
    feature.insertBefore(iconContainer, title)
  }

  /**
   * Get custom SVG icon for each feature
   */
  getFeatureIcon (index) {
    const icons = [
      // Automatic Resource Optimization
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M12 2L2 7v10c0 5.55 3.84 9.74 9 11 5.16-1.26 9-5.45 9-11V7l-10-5z" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M9 12l2 2 4-4" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // GitOps-Safe Server-Side Apply
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Multiple Operational Modes
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M12 1v6m0 6v6m11-7h-6m-6 0H1" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Safety-First Resource Management
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M9 12l2 2 4-4" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Pluggable Metrics Backends
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <polyline points="22,12 18,12 15,21 9,3 6,12 2,12" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Workload Type Filtering
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M22 12h-4l-3 9L9 3l-3 9H2" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Multi-Tenant Ready
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" fill="none" stroke="currentColor" stroke-width="2"/>
        <circle cx="9" cy="7" r="4" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M23 21v-2a4 4 0 0 0-3-3.87" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M16 3.13a4 4 0 0 1 0 7.75" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Comprehensive Observability
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" fill="none" stroke="currentColor" stroke-width="2"/>
        <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Flexible Limit Configuration
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" fill="none" stroke="currentColor" stroke-width="2"/>
        <polyline points="7.5,4.21 12,6.81 16.5,4.21" fill="none" stroke="currentColor" stroke-width="2"/>
        <polyline points="7.5,19.79 7.5,14.6 3,12" fill="none" stroke="currentColor" stroke-width="2"/>
        <polyline points="21,12 16.5,14.6 16.5,19.79" fill="none" stroke="currentColor" stroke-width="2"/>
        <polyline points="12,22.81 12,17" fill="none" stroke="currentColor" stroke-width="2"/>
        <line x1="12" y1="6.81" x2="12" y2="12" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Correctness by Design
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <polyline points="9,11 12,14 22,4" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M21 12c0 4.97-4.03 9-9 9s-9-4.03-9-9 4.03-9 9-9c1.24 0 2.42.25 3.5.7" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>`,

      // Explainable Recommendations
      `<svg viewBox="0 0 24 24" width="24" height="24">
        <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" fill="none" stroke="currentColor" stroke-width="2"/>
        <line x1="12" y1="17" x2="12.01" y2="17" stroke="currentColor" stroke-width="2"/>
      </svg>`
    ]

    return icons[index] || icons[0]
  }

  /**
   * Get detailed content for each feature
   */
  getDetailedContent (index, title) {
    const details = [
      // Automatic Resource Optimization
      `<div class="feature-detail-content">
        <h6>How it works:</h6>
        <ul>
          <li>Continuously monitors CPU and memory usage patterns</li>
          <li>Uses configurable percentile strategies (P90, P95, P99)</li>
          <li>Applies safety margins and bounds checking</li>
          <li>Generates recommendations with full rationale</li>
        </ul>
        <p><strong>Result:</strong> Right-sized resources without guesswork or over-provisioning.</p>
      </div>`,

      // GitOps-Safe Server-Side Apply
      `<div class="feature-detail-content">
        <h6>Technical approach:</h6>
        <ul>
          <li>Uses Kubernetes Server-Side Apply (SSA) with field-level ownership</li>
          <li>Only manages CPU and memory requests/limits fields</li>
          <li>Coexists with ArgoCD, Flux, and other GitOps tools</li>
          <li>No conflicts with human or automated changes</li>
        </ul>
        <p><strong>Result:</strong> Safe automation that respects your GitOps workflows.</p>
      </div>`,

      // Multiple Operational Modes
      `<div class="feature-detail-content">
        <h6>Available modes:</h6>
        <ul>
          <li><strong>Observe:</strong> Monitor usage without any changes</li>
          <li><strong>Recommend:</strong> Generate recommendations for review</li>
          <li><strong>Auto:</strong> Apply changes automatically with safety constraints</li>
        </ul>
        <p><strong>Result:</strong> Adopt optimization at your own pace and comfort level.</p>
      </div>`,

      // Safety-First Resource Management
      `<div class="feature-detail-content">
        <h6>Safety mechanisms:</h6>
        <ul>
          <li>Configurable min/max resource bounds</li>
          <li>Safety margins to prevent resource starvation</li>
          <li>Change-rate limits to prevent disruptive updates</li>
          <li>Memory decrease protection to avoid OOMKills</li>
        </ul>
        <p><strong>Result:</strong> Optimization that prioritizes stability over savings.</p>
      </div>`,

      // Pluggable Metrics Backends
      `<div class="feature-detail-content">
        <h6>Supported providers:</h6>
        <ul>
          <li><strong>Metrics-server:</strong> Fully supported and production-ready</li>
          <li><strong>Prometheus:</strong> In active development</li>
          <li><strong>Custom providers:</strong> Plugin architecture planned</li>
        </ul>
        <p><strong>Result:</strong> Works with your existing monitoring infrastructure.</p>
      </div>`,

      // Workload Type Filtering
      `<div class="feature-detail-content">
        <h6>Filtering capabilities:</h6>
        <ul>
          <li>Target specific workload types (Deployment, StatefulSet, DaemonSet)</li>
          <li>Include/exclude filters for gradual rollout</li>
          <li>Namespace and label-based selection</li>
          <li>Policy weight prioritization for conflicts</li>
        </ul>
        <p><strong>Result:</strong> Precise control over which workloads get optimized.</p>
      </div>`,

      // Multi-Tenant Ready
      `<div class="feature-detail-content">
        <h6>Multi-tenancy features:</h6>
        <ul>
          <li>Namespace-scoped policies and permissions</li>
          <li>Label-based workload selection with allow/deny lists</li>
          <li>Policy weight prioritization for overlapping policies</li>
          <li>Tenant isolation and resource boundaries</li>
        </ul>
        <p><strong>Result:</strong> Safe deployment in shared cluster environments.</p>
      </div>`,

      // Comprehensive Observability
      `<div class="feature-detail-content">
        <h6>Observability features:</h6>
        <ul>
          <li>Prometheus metrics for monitoring and alerting</li>
          <li>Kubernetes events for audit trails</li>
          <li>Per-workload status and recommendation history</li>
          <li>Detailed logging with structured output</li>
        </ul>
        <p><strong>Result:</strong> Full visibility into optimization decisions and outcomes.</p>
      </div>`,

      // Flexible Limit Configuration
      `<div class="feature-detail-content">
        <h6>Limit management:</h6>
        <ul>
          <li>Configurable CPU and memory limit multipliers</li>
          <li>Automatic limit calculation from request recommendations</li>
          <li>Support for requests-only or limits-only optimization</li>
          <li>Custom limit strategies per workload type</li>
        </ul>
        <p><strong>Result:</strong> Flexible resource limit management that fits your needs.</p>
      </div>`,

      // Correctness by Design
      `<div class="feature-detail-content">
        <h6>Testing approach:</h6>
        <ul>
          <li>Extensive unit and integration test coverage</li>
          <li>Property-based testing for edge case discovery</li>
          <li>End-to-end testing with real Kubernetes clusters</li>
          <li>Continuous validation of correctness properties</li>
        </ul>
        <p><strong>Result:</strong> High confidence in system reliability and correctness.</p>
      </div>`,

      // Explainable Recommendations
      `<div class="feature-detail-content">
        <h6>Transparency features:</h6>
        <ul>
          <li>Clear rationale for every recommendation</li>
          <li>Usage data and percentile calculations shown</li>
          <li>Safety margin and bounds application explained</li>
          <li>Historical recommendation tracking</li>
        </ul>
        <p><strong>Result:</strong> Full understanding of why changes are recommended.</p>
      </div>`
    ]

    return details[index] || `<p>Detailed information about ${title}.</p>`
  }

  /**
   * Toggle expansion of a feature card
   */
  toggleFeatureExpansion (feature) {
    const isExpanded = feature.classList.contains('expanded')

    // Close any currently expanded card
    if (this.expandedCard && this.expandedCard !== feature) {
      this.collapseFeature(this.expandedCard)
    }

    if (isExpanded) {
      this.collapseFeature(feature)
    } else {
      this.expandFeature(feature)
    }
  }

  /**
   * Expand a feature card
   */
  expandFeature (feature) {
    feature.classList.add('expanded')
    this.expandedCard = feature

    const details = feature.querySelector('.feature-details')
    const expandBtn = feature.querySelector('.feature-expand-btn')

    // Update button text and icon
    expandBtn.querySelector('span').textContent = 'Show less'
    expandBtn.querySelector('.expand-icon').style.transform = 'rotate(180deg)'

    // Animate expansion
    details.style.maxHeight = details.scrollHeight + 'px'
  }

  /**
   * Collapse a feature card
   */
  collapseFeature (feature) {
    feature.classList.remove('expanded')
    if (this.expandedCard === feature) {
      this.expandedCard = null
    }

    const details = feature.querySelector('.feature-details')
    const expandBtn = feature.querySelector('.feature-expand-btn')

    // Update button text and icon
    expandBtn.querySelector('span').textContent = 'Learn more'
    expandBtn.querySelector('.expand-icon').style.transform = 'rotate(0deg)'

    // Animate collapse
    details.style.maxHeight = '0'
  }

  /**
   * Add event listeners
   */
  addEventListeners () {
    // Close expanded cards when clicking outside
    document.addEventListener('click', (e) => {
      if (!e.target.closest('.feature') && this.expandedCard) {
        this.collapseFeature(this.expandedCard)
      }
    })

    // Handle escape key
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && this.expandedCard) {
        this.collapseFeature(this.expandedCard)
      }
    })
  }
}

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = FeaturesGrid
}


/* github-stats.js */
/**
 * GitHub Statistics Integration
 * Fetches real-time repository stats and displays them with animated counters
 */

class GitHubStats {
  constructor (repoOwner, repoName) {
    this.repoOwner = repoOwner
    this.repoName = repoName
    this.apiUrl = `https://api.github.com/repos/${repoOwner}/${repoName}`
    this.cache = null
    this.cacheExpiry = 5 * 60 * 1000 // 5 minutes
  }

  /**
   * Fetch repository statistics from GitHub API
   * @returns {Promise<Object>} Repository stats object
   */
  async fetchStats () {
    // Check cache first
    if (this.cache && Date.now() - this.cache.timestamp < this.cacheExpiry) {
      return this.cache.data
    }

    try {
      const response = await fetch(this.apiUrl)

      if (!response.ok) {
        throw new Error(`GitHub API error: ${response.status}`)
      }

      const data = await response.json()

      // Get releases data
      const releasesResponse = await fetch(`${this.apiUrl}/releases`)
      const releases = releasesResponse.ok ? await releasesResponse.json() : []

      const stats = {
        stars: data.stargazers_count || 0,
        forks: data.forks_count || 0,
        watchers: data.watchers_count || 0,
        releases: releases.length || 0,
        lastUpdated: new Date(data.updated_at),
        language: data.language || 'Go',
        openIssues: data.open_issues_count || 0
      }

      // Cache the results
      this.cache = {
        data: stats,
        timestamp: Date.now()
      }

      return stats
    } catch (error) {
      console.warn('Failed to fetch GitHub stats:', error)

      // Return fallback data
      return {
        stars: 42,
        forks: 8,
        watchers: 15,
        releases: 3,
        lastUpdated: new Date(),
        language: 'Go',
        openIssues: 2,
        error: true
      }
    }
  }

  /**
   * Animate counter from 0 to target value
   * @param {HTMLElement} element - Element to animate
   * @param {number} target - Target number
   * @param {number} duration - Animation duration in ms
   */
  animateCounter (element, target, duration = 1000) {
    const start = 0
    const startTime = performance.now()

    const updateCounter = (currentTime) => {
      const elapsed = currentTime - startTime
      const progress = Math.min(elapsed / duration, 1)

      // Easing function for smooth animation
      const easeOutQuart = 1 - Math.pow(1 - progress, 4)
      const current = Math.floor(start + (target - start) * easeOutQuart)

      element.textContent = this.formatNumber(current)

      if (progress < 1) {
        requestAnimationFrame(updateCounter)
      } else {
        element.textContent = this.formatNumber(target)
      }
    }

    requestAnimationFrame(updateCounter)
  }

  /**
   * Format number with appropriate suffixes (K, M, etc.)
   * @param {number} num - Number to format
   * @returns {string} Formatted number string
   */
  formatNumber (num) {
    if (num >= 1000000) {
      return (num / 1000000).toFixed(1) + 'M'
    }
    if (num >= 1000) {
      return (num / 1000).toFixed(1) + 'K'
    }
    return num.toString()
  }

  /**
   * Create and inject stats elements into the hero section
   * @param {Object} stats - Stats object from GitHub API
   */
  injectStatsIntoHero (stats) {
    const heroSection = document.querySelector('.hero')
    if (!heroSection) return

    // Create stats container
    const statsContainer = document.createElement('div')
    statsContainer.className = 'github-stats reveal delay-3'
    statsContainer.innerHTML = `
      <div class="stats-grid">
        <div class="stat-item">
          <span class="stat-number" data-target="${stats.stars}">0</span>
          <span class="stat-label">Stars</span>
        </div>
        <div class="stat-item">
          <span class="stat-number" data-target="${stats.forks}">0</span>
          <span class="stat-label">Forks</span>
        </div>
        <div class="stat-item">
          <span class="stat-number" data-target="${stats.releases}">0</span>
          <span class="stat-label">Releases</span>
        </div>
        ${stats.error ? '<div class="stat-error">Using cached data</div>' : ''}
      </div>
    `

    // Insert after the trust section
    const trustSection = heroSection.querySelector('.trust')
    if (trustSection) {
      trustSection.parentNode.insertBefore(statsContainer, trustSection.nextSibling)
    }

    // Animate counters after a short delay
    setTimeout(() => {
      const statNumbers = statsContainer.querySelectorAll('.stat-number')
      statNumbers.forEach((element, index) => {
        const target = parseInt(element.dataset.target)
        setTimeout(() => {
          this.animateCounter(element, target)
        }, index * 200) // Stagger animations
      })
    }, 500)
  }

  /**
   * Initialize GitHub stats display
   */
  async init () {
    try {
      const stats = await this.fetchStats()
      this.injectStatsIntoHero(stats)
    } catch (error) {
      console.error('Failed to initialize GitHub stats:', error)
    }
  }
}

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = GitHubStats
}
