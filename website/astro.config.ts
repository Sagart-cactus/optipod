import path from 'path';
import { fileURLToPath } from 'url';

import { defineConfig } from 'astro/config';

import sitemap from '@astrojs/sitemap';
import tailwind from '@astrojs/tailwind';
import mdx from '@astrojs/mdx';
import partytown from '@astrojs/partytown';
import icon from 'astro-icon';
import compress from 'astro-compress';
import starlight from '@astrojs/starlight';
import type { AstroIntegration } from 'astro';

import astrowind from './vendor/integration';

import { readingTimeRemarkPlugin, responsiveTablesRehypePlugin, lazyImagesRehypePlugin } from './src/utils/frontmatter';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const hasExternalScripts = false;
const whenExternalScripts = (items: (() => AstroIntegration) | (() => AstroIntegration)[] = []) =>
  hasExternalScripts ? (Array.isArray(items) ? items.map((item) => item()) : [items()]) : [];

export default defineConfig({
  site: 'https://sagart-cactus.github.io/optipod/',
  base: '/optipod',
  output: 'static',
  trailingSlash: 'ignore',
  devToolbar: {
    enabled: false,
  },

  integrations: [
    starlight({
      title: 'OptiPod Documentation',
      description: 'Open-source Kubernetes resource optimization operator',
      logo: {
        src: './src/assets/images/optipod-logo.svg',
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/Sagart-cactus/optipod',
        },
      ],
      defaultLocale: 'root',
      locales: {
        root: {
          label: 'English',
          lang: 'en',
        },
      },
      expressiveCode: {
        themes: ['github-light', 'github-dark'],
      },
      head: [
        {
          tag: 'link',
          attrs: {
            rel: 'preconnect',
            href: 'https://fonts.googleapis.com',
          },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'preconnect',
            href: 'https://fonts.gstatic.com',
            crossorigin: 'anonymous',
          },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'stylesheet',
            href: 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap',
          },
        },
      ],
      sidebar: [
        {
          label: 'Getting Started',
          collapsed: false,
          items: [
            { label: 'Introduction', link: '/docs/getting-started/introduction' },
            { label: 'Installation', link: '/docs/getting-started/installation' },
            { label: 'Quick Start', link: '/docs/getting-started/quick-start' },
          ],
        },
        {
          label: 'Concepts',
          collapsed: false,
          items: [
            { label: 'Terminology', link: '/docs/concepts/terminology' },
            { label: 'Operational Modes', link: '/docs/concepts/operational-modes' },
            { label: 'Update Strategies', link: '/docs/concepts/update-strategies' },
            { label: 'Safety Model', link: '/docs/concepts/safety-model' },
          ],
        },
        {
          label: 'Guides',
          collapsed: false,
          items: [
            { label: 'Creating Policies', link: '/docs/guides/creating-policies' },
            { label: 'Reviewing Recommendations', link: '/docs/guides/reviewing-recommendations' },
            { label: 'Switching to Auto', link: '/docs/guides/switching-to-auto' },
          ],
        },
        {
          label: 'Advanced',
          collapsed: false,
          items: [
            { label: 'Custom Metrics', link: '/docs/advanced/custom-metrics' },
            { label: 'Multi-Tenant', link: '/docs/advanced/multi-tenant' },
            { label: 'Troubleshooting', link: '/docs/advanced/troubleshooting' },
          ],
        },
        {
          label: 'Reference',
          collapsed: false,
          items: [
            { label: 'API', link: '/docs/reference/api' },
            { label: 'Annotations', link: '/docs/reference/annotations' },
            { label: 'CRD Spec', link: '/docs/reference/crd-spec' },
          ],
        },
      ],
      customCss: [
        './src/assets/styles/docs.css',
      ],
    }),
    tailwind({
      applyBaseStyles: false,
    }),
    sitemap(),
    mdx(),
    icon({
      include: {
        tabler: ['*'],
        'flat-color-icons': [
          'template',
          'gallery',
          'approval',
          'document',
          'advertising',
          'currency-exchange',
          'voice-presentation',
          'business-contact',
          'database',
        ],
      },
    }),

    ...whenExternalScripts(() =>
      partytown({
        config: { forward: ['dataLayer.push'] },
      })
    ),

    compress({
      CSS: true,
      HTML: {
        'html-minifier-terser': {
          removeAttributeQuotes: false,
        },
      },
      Image: false,
      JavaScript: true,
      SVG: false,
      Logger: 1,
    }),

    astrowind({
      config: './src/config.yaml',
    }),
  ],

  image: {
    domains: ['cdn.pixabay.com'],
  },

  markdown: {
    remarkPlugins: [readingTimeRemarkPlugin],
    rehypePlugins: [responsiveTablesRehypePlugin, lazyImagesRehypePlugin],
  },

  vite: {
    resolve: {
      alias: {
        '~': path.resolve(__dirname, './src'),
      },
    },
  },
});
