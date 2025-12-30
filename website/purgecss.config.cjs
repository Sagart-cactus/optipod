module.exports = {
  content: [
    './index.html',
    './js/*.js'
  ],
  css: [
    './css/*.css'
  ],
  output: process.env.OUTPUT_DIR ? `${process.env.OUTPUT_DIR}/css/` : './dist/css/',
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
