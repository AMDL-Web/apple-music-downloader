import AxeBuilder from '@axe-core/playwright';
import { test, expect, open } from './fixtures.mjs';

// axe-core on every main page, in both themes: a serious or critical
// violation fails, unless it is one of the known ones below. Every
// violation, moderate and minor included, is attached to the report. Each
// page is scanned once `ready` shows it loaded, not while it still loads.
const pages = [
  ['Overview', '#/', (p) => p.getByRole('list', { name: 'Active jobs' }).getByRole('listitem').first()],
  ['Activity', '#/queue', (p) => p.getByRole('list', { name: 'Downloads' }).getByRole('listitem').first()],
  ['Job drawer', '#/overview/demo-0005', (p) => p.getByRole('dialog', { name: 'Glass Weather' }).getByRole('region', { name: 'Tracks' })],
  ['New download', '#/new', (p) => p.getByRole('textbox', { name: 'Search or paste links' })],
  ['Library', '#/library', (p) => p.getByRole('button', { name: 'Sync now' })],
  ['Settings', '#/settings', (p) => p.getByRole('link', { name: /^Server, Healthy/ })],
  ['Settings: Hooks', '#/settings?section=hooks', (p) => p.getByRole('switch', { name: 'jellyfin on' })],
  ['Logs', '#/logs', (p) => p.getByRole('heading', { name: 'Logs', level: 1 })],
];

// Known violations, present when this suite was written. Fix them in the
// console and delete their entry; a new violation of the same rule on
// another element still fails.
const KNOWN = [
  // The top bar's search button reads "Search or paste a link… Ctrl K" but
  // is named "Search, paste a link or run a command" (WCAG 2.5.3).
  { id: 'label-content-name-mismatch', target: /^\.search-trigger$/ },
  // Settings' status tiles are named "Account, Ready, US storefront" while
  // their text runs "Account Ready US storefront ..." (WCAG 2.5.3).
  { id: 'label-content-name-mismatch', target: /status-tile/ },
  // The status tiles' storage and library sync meters have no name.
  { id: 'aria-meter-name', target: /viz-meter/ },
  // The hooks.yaml marker is a <span> with aria-label and no role.
  { id: 'aria-prohibited-attr', target: /^\.settings-hook-file$/ },
];

// Matched against the selector axe reports and the start of the element.
const known = (id, node) => KNOWN.some((k) => k.id === id && (k.target.test(node.target.join(' ')) || k.target.test(node.html)));

for (const scheme of ['light', 'dark']) {
  for (const [name, hash, ready] of pages) {
    test(`${name} has no serious accessibility violations (${scheme})`, async ({ page }, testInfo) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      await open(page, hash);
      await expect(ready(page)).toBeVisible();

      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
      await testInfo.attach('axe-violations.json', { body: JSON.stringify(results.violations, null, 2), contentType: 'application/json' });
      const blocking = results.violations
        .filter((v) => v.impact === 'serious' || v.impact === 'critical')
        .flatMap((v) => v.nodes.map((n) => ({ v, n })))
        .filter(({ v, n }) => !known(v.id, n))
        .map(({ v, n }) => `${v.impact} ${v.id}: ${v.help} (${n.target.join(' ')})`);
      expect(blocking).toEqual([]);
    });
  }
}
