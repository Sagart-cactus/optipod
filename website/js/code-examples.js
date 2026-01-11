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
      max: "8Gi"
  updateStrategy:
    strategy: webhook
    rolloutStrategy: onNextRestart
    allowInPlaceResize: true
    allowRecreate: false
    updateRequestsOnly: true`
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
    workloadTypes:
      include:
        - Deployment
        - StatefulSet
  metricsConfig:
    provider: metrics-server
    rollingWindow: 72h
    percentile: P99
    safetyFactor: 1.5
  resourceBounds:
    cpu:
      min: "200m"
      max: "8000m"
    memory:
      min: "256Mi"
      max: "16Gi"
  updateStrategy:
    strategy: webhook
    rolloutStrategy: immediate
    allowInPlaceResize: true
    allowRecreate: true
    updateRequestsOnly: false
    limitConfig:
      cpuLimitMultiplier: 1.5
      memoryLimitMultiplier: 1.2
    gradualDecreaseConfig:
      enabled: true
      memoryDecreasePercentage: 10
      maximumTotalDecrease: 70
`
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
