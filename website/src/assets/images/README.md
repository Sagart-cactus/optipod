# OptiPod Images

## Current Assets

- `optipod-logo.svg` - OptiPod logo (Kubernetes-inspired hexagon with optimization arrows)
- `default.png` - Placeholder Open Graph image (needs replacement)

## Assets Needed

### High Priority

1. **Open Graph Image** (`optipod-og.png`)
   - Size: 1200x628px
   - Should include: OptiPod logo, tagline, key features
   - Used for social media sharing
   - Replace `default.png` or create new file

2. **Hero Image** (optional)
   - Kubernetes/DevOps themed illustration
   - Could show: cluster optimization, resource management, GitOps workflow
   - Suggested size: 1200x800px

### Medium Priority

3. **Feature Icons** (optional)
   - Custom icons for key features
   - Could replace Tabler icons with branded versions
   - SVG format preferred

4. **Diagram Images** (optional)
   - How OptiPod works diagram
   - Architecture diagram
   - Comparison visualization

## Design Guidelines

### Colors
- Primary: Blue (#2563eb) - Kubernetes blue
- Secondary: Green (#10b981) - Success/GitOps
- Accent: Light Blue (#3b82f6)

### Style
- Clean, modern, technical
- Kubernetes/cloud-native aesthetic
- Professional but approachable
- Dark mode compatible

## Temporary Assets to Remove

- `app-store.png` - Not needed for OptiPod
- `google-play.png` - Not needed for OptiPod
- `hero-image.png` - Generic, should be replaced

## Creating Assets

### Tools
- Figma (recommended for OG images)
- Canva (quick social media graphics)
- Inkscape (SVG editing)
- GIMP/Photoshop (raster images)

### Templates
Consider using:
- Kubernetes color palette
- CNCF design guidelines
- Cloud-native project aesthetics

## Usage

Images are referenced in:
- `src/config.yaml` - Open Graph image
- `src/pages/index.astro` - Hero and content images
- `src/components/Logo.astro` - Logo SVG (inline)
