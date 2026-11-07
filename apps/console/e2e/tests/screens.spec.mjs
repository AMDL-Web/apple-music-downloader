import { test, expect, open } from './fixtures.mjs';

// Screenshot comparisons, last line of defence for layout and theme. The
// baselines in __screenshots__ are made in Chromium in the Playwright Linux
// image only, where CI compares them (fonts and anti-aliasing differ from
// machine to machine), so these run only there: docker.sh sets
// CONSOLE_E2E_SCREENSHOTS. After an intended change: npm run test:update
// (see README.md).
test.skip(process.env.CONSOLE_E2E_SCREENSHOTS !== '1', 'screenshots are compared in the Playwright image only (npm run test:docker)');
test.use({ contextOptions: { reducedMotion: 'reduce' } });

const shots = [
  { name: 'overview-light', hash: '#/', scheme: 'light', heading: 'Overview' },
  { name: 'overview-dark', hash: '#/', scheme: 'dark', heading: 'Overview' },
  { name: 'activity-dark', hash: '#/queue', scheme: 'dark', heading: 'Activity' },
  { name: 'settings-quality-light', hash: '#/settings?section=quality', scheme: 'light', heading: 'Settings' },
];

for (const s of shots) {
  test(`${s.name} looks as it did`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: s.scheme });
    await open(page, s.hash);
    await expect(page.getByRole('heading', { name: s.heading, level: 1 })).toBeVisible();
    await expect(page).toHaveScreenshot(`${s.name}.png`);
  });
}

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('activity-phone-light looks as it did', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' });
    await open(page, '#/queue');
    await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible();
    await expect(page).toHaveScreenshot('activity-phone-light.png');
  });
});
