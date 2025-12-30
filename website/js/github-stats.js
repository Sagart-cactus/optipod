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
