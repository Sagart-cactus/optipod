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
