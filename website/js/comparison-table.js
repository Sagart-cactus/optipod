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
