# OptiPod Favicons

## Current Status

- `favicon.svg` - ✅ Updated with OptiPod logo
- `favicon.ico` - ⚠️ Needs regeneration
- `apple-touch-icon.png` - ⚠️ Needs regeneration

## Generating Favicons

### Option 1: Using Favicon Generator (Recommended)

1. Visit [RealFaviconGenerator](https://realfavicongenerator.net/)
2. Upload the `favicon.svg` file
3. Customize settings:
   - iOS: Use solid background color (#2563eb)
   - Android: Use solid background color
   - Windows: Use solid background color
4. Download the generated package
5. Replace files in this directory

### Option 2: Using ImageMagick

```bash
# Install ImageMagick if needed
brew install imagemagick

# Generate favicon.ico from SVG
convert -background none favicon.svg -define icon:auto-resize=16,32,48 favicon.ico

# Generate apple-touch-icon.png
convert -background "#2563eb" -gravity center -extent 180x180 favicon.svg apple-touch-icon.png
```

### Option 3: Using Figma/Design Tool

1. Create a 512x512px artboard
2. Add OptiPod logo centered
3. Export as PNG at 2x resolution
4. Use online tools to generate all sizes

## Required Sizes

- `favicon.ico` - 16x16, 32x32, 48x48 (multi-resolution)
- `favicon.svg` - Vector (already done)
- `apple-touch-icon.png` - 180x180px

## Design Specifications

### Colors
- Background: #2563eb (blue-600)
- Foreground: #ffffff (white)

### Logo
- Kubernetes-inspired hexagon
- Optimization arrows (< >)
- Center dot

### Style
- Simple and recognizable at small sizes
- High contrast for visibility
- Works on both light and dark backgrounds

## Testing

After generating new favicons:

1. Clear browser cache
2. Test in multiple browsers:
   - Chrome/Edge
   - Firefox
   - Safari
3. Test on mobile devices
4. Check dark mode appearance

## Notes

- The current `favicon.svg` is a placeholder
- For production, consider using a favicon generator service
- Ensure all sizes are optimized for web
- Test visibility at 16x16px (smallest size)
