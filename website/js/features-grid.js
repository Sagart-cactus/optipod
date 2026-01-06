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
          <li><strong>Recommend:</strong> Generate recommendations for review</li>
          <li><strong>Auto:</strong> Apply changes automatically with policy-driven safety</li>
          <li><strong>Disabled:</strong> Stop processing workloads under the policy</li>
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
          <li><strong>Prometheus:</strong> Fully supported and production-ready</li>
          <li><strong>Per-policy providers:</strong> Planned for next release</li>
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
          <li>Per-workload recommendations stored as workload annotations</li>
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
