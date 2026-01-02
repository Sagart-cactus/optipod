const outputDir = process.env.OUTPUT_DIR || './dist'

module.exports = {
  content: [
    './index.html',
    './docs/**/*.html',
    './js/*.js'
  ],
  css: [
    './css/*.css'
  ],
  output: `${outputDir}/css/`,
  safelist: [
    // Keep animation classes
    /^reveal/,
    /^delay-/,
    // Keep dynamic classes that might be added by JavaScript
    /^github-/,
    /^feature-/,
    /^comparison-/,
    /^code-/,
    // Keep hover and focus states
    /:hover/,
    /:focus/,
    /:active/,
    // Keep responsive classes
    /@media/
  ],
  defaultExtractor: content => content.match(/[\w-/:]+(?<!:)/g) || [],
  fontFace: true,
  keyframes: true,
  variables: true
}
