import sharp from 'sharp';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const svgPath = join(__dirname, '../src/assets/favicons/favicon.svg');
const outputDir = join(__dirname, '../src/assets/favicons');

async function generateFavicons() {
  try {
    const svgBuffer = readFileSync(svgPath);

    // Generate favicon.ico (32x32)
    console.log('Generating favicon.ico...');
    await sharp(svgBuffer)
      .resize(32, 32)
      .toFile(join(outputDir, 'favicon.ico'));
    console.log('✓ favicon.ico generated');

    // Generate apple-touch-icon.png (180x180)
    console.log('Generating apple-touch-icon.png...');
    await sharp(svgBuffer)
      .resize(180, 180)
      .toFile(join(outputDir, 'apple-touch-icon.png'));
    console.log('✓ apple-touch-icon.png generated');

    console.log('\n✅ All favicons generated successfully!');
  } catch (error) {
    console.error('Error generating favicons:', error);
    process.exit(1);
  }
}

generateFavicons();
