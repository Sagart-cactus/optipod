import { getPermalink, getAsset } from './utils/permalinks';

export const headerData = {
  links: [
    {
      text: 'Features',
      href: getPermalink('/#features'),
    },
    {
      text: 'How It Works',
      href: getPermalink('/#how-it-works'),
    },
    {
      text: 'Comparison',
      href: getPermalink('/#comparison'),
    },
    {
      text: 'Quick Start',
      href: getPermalink('/#quick-start'),
    },
    {
      text: 'Documentation',
      href: getPermalink('/docs'),
    },
  ],
  actions: [
    {
      text: 'View on GitHub',
      href: 'https://github.com/Sagart-cactus/optipod',
      target: '_blank',
      icon: 'tabler:brand-github',
    },
  ],
};

export const footerData = {
  links: [
    {
      title: 'Product',
      links: [
        { text: 'Features', href: getPermalink('/#features') },
        { text: 'How It Works', href: getPermalink('/#how-it-works') },
        { text: 'Comparison vs VPA', href: getPermalink('/#comparison') },
        { text: 'Quick Start', href: getPermalink('/#quick-start') },
      ],
    },
    {
      title: 'Resources',
      links: [
        { text: 'GitHub Repository', href: 'https://github.com/Sagart-cactus/optipod', target: '_blank' },
        { text: 'Documentation', href: getPermalink('/docs') },
        { text: 'Design Principles', href: 'https://github.com/Sagart-cactus/optipod/blob/main/DESIGN.md', target: '_blank' },
        { text: 'Roadmap', href: 'https://github.com/Sagart-cactus/optipod/blob/main/ROADMAP.md', target: '_blank' },
      ],
    },
    {
      title: 'Community',
      links: [
        { text: 'Contributing', href: 'https://github.com/Sagart-cactus/optipod/blob/main/CONTRIBUTING.md', target: '_blank' },
        { text: 'Code of Conduct', href: 'https://github.com/Sagart-cactus/optipod/blob/main/CODE_OF_CONDUCT.md', target: '_blank' },
        { text: 'Governance', href: 'https://github.com/Sagart-cactus/optipod/blob/main/GOVERNANCE.md', target: '_blank' },
        { text: 'Issues', href: 'https://github.com/Sagart-cactus/optipod/issues', target: '_blank' },
      ],
    },
  ],
  secondaryLinks: [
    { text: 'Terms', href: getPermalink('/terms') },
    { text: 'Privacy Policy', href: getPermalink('/privacy') },
  ],
  socialLinks: [
    { ariaLabel: 'GitHub', icon: 'tabler:brand-github', href: 'https://github.com/Sagart-cactus/optipod' },
  ],
  footNote: `
    <a class="text-blue-600 underline dark:text-muted" href="https://github.com/Sagart-cactus/optipod">OptiPod</a> · Open source Kubernetes resource optimization
  `,
};
