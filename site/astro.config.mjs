import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: process.env.SITE_URL || 'https://samishal1998.github.io',
  base: process.env.SITE_BASE ?? '/apptrail',
  trailingSlash: 'always',
  integrations: [starlight({
    title: 'apptrail.',
    description: 'Discover, organize, and launch your self-hosted apps. A home for everything you host.',
    social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/samishal1998/apptrail' }],
    editLink: { baseUrl: 'https://github.com/samishal1998/apptrail/edit/main/site/' },
    customCss: ['./src/styles/custom.css'],
    sidebar: [
      { label: 'Start here', items: [
        { label: 'Installation', slug: 'guides/installation' },
        { label: 'Your first dashboard', slug: 'guides/quick-start' },
        { label: 'Background services', slug: 'guides/services' },
      ] },
      { label: 'App guide', items: [
        { label: 'Apps & discovery', slug: 'guides/discovery' },
        { label: 'Dashboards & sharing', slug: 'guides/dashboards' },
        { label: 'Auto-add rules (CEL)', slug: 'guides/auto-add' },
        { label: 'Metadata & health', slug: 'guides/metadata' },
        { label: 'Hosting & maintenance', slug: 'guides/hosting' },
        { label: 'Troubleshooting', slug: 'guides/troubleshooting' },
      ] },
      { label: 'Project', items: [
        { label: 'Releases & contributing', slug: 'project/releases' },
        { label: 'GitHub', link: 'https://github.com/samishal1998/apptrail' },
      ] },
    ],
  })],
});
