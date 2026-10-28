import sharp from 'sharp';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const logoPath = join(__dirname, '../src/assets/favicons/favicon.svg');
const outputPath = join(__dirname, '../public/og-image.png');

async function generateOGImage() {
  try {
    console.log('Generating Open Graph image...');

    // Create a blue gradient background (1200x628)
    const background = await sharp({
      create: {
        width: 1200,
        height: 628,
        channels: 4,
        background: { r: 37, g: 99, b: 235, alpha: 1 } // #2563eb
      }
    })
    .png()
    .toBuffer();

    // Read and resize logo
    const logoBuffer = readFileSync(logoPath);
    const logo = await sharp(logoBuffer)
      .resize(300, 300)
      .toBuffer();

    // Create SVG text overlay
    const textSvg = `
      <svg width="1200" height="628">
        <style>
          .title {
            fill: white;
            font-size: 72px;
            font-weight: bold;
            font-family: Arial, sans-serif;
          }
          .subtitle {
            fill: rgba(255, 255, 255, 0.9);
            font-size: 36px;
            font-family: Arial, sans-serif;
          }
        </style>
        <text x="600" y="480" text-anchor="middle" class="title">OptiPod</text>
        <text x="600" y="540" text-anchor="middle" class="subtitle">Kubernetes Resource Optimization</text>
      </svg>
    `;

    // Composite everything together
    await sharp(background)
      .composite([
        {
          input: logo,
          top: 80,
          left: 450
        },
        {
          input: Buffer.from(textSvg),
          top: 0,
          left: 0
        }
      ])
      .toFile(outputPath);

    console.log('✅ Open Graph image generated at public/og-image.png');
  } catch (error) {
    console.error('Error generating OG image:', error);
    process.exit(1);
  }
}

generateOGImage();
