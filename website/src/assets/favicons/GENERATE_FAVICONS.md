# Generate OptiPod Favicons

The `favicon.svg` has been updated with the OptiPod logo, but `favicon.ico` and `apple-touch-icon.png` still need to be regenerated.

## Quick Fix for Development

To see the OptiPod favicon immediately in your browser:

1. **Clear browser cache** (Ctrl+Shift+Delete or Cmd+Shift+Delete)
2. **Hard refresh** the page (Ctrl+F5 or Cmd+Shift+R)
3. The SVG favicon should now show the OptiPod logo

## Generate Production Favicons

Use one of these methods to generate proper `.ico` and `.png` files:

### Option 1: Online Tool (Easiest)

1. Visit https://realfavicongenerator.net/
2. Upload `favicon.svg`
3. Adjust settings:
   - iOS: Background color #2563eb (blue)
   - Android: Background color #2563eb
   - Windows: Background color #2563eb
4. Download the package
5. Replace files in this directory

### Option 2: ImageMagick (Command Line)

```bash
# Install ImageMagick if needed
brew install imagemagick

# Generate favicon.ico (16x16, 32x32, 48x48)
magick favicon.svg -background none -define icon:auto-resize=16,32,48 favicon.ico

# Generate apple-touch-icon.png (180x180)
magick favicon.svg -background "#2563eb" -gravity center -resize 180x180 -extent 180x180 apple-touch-icon.png
```

### Option 3: Manual with Design Tool

1. Open `favicon.svg` in Figma/Sketch/Illustrator
2. Export as PNG at these sizes:
   - 16x16, 32x32, 48x48 (for favicon.ico)
   - 180x180 (for apple-touch-icon.png)
3. Use an online ICO converter to create favicon.ico from the PNGs
4. Replace files in this directory

## Files to Generate

- [ ] `favicon.ico` - Multi-resolution (16x16, 32x32, 48x48)
- [ ] `apple-touch-icon.png` - 180x180px

## Current Status

- ✅ `favicon.svg` - Updated with OptiPod logo (blue hexagon with cube)
- ⚠️ `favicon.ico` - Still using old Astro favicon
- ⚠️ `apple-touch-icon.png` - Still using old Astro icon

## Testing

After generating new favicons:

1. Replace the files in this directory
2. Clear browser cache
3. Hard refresh the page
4. Check favicon in:
   - Browser tab
   - Bookmarks
   - iOS home screen (if applicable)
   - Android home screen (if applicable)

## Colors

OptiPod brand colors for favicon generation:
- Primary: #2563eb (blue-600)
- Background: #2563eb or transparent
- Foreground: #ffffff (white)
