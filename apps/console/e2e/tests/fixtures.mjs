// The test fixture every spec imports: each test gets a backend of its own
// (a fresh demo world, named by a cookie; see server/main.go), the browser's
// clock stands at the backend's starting time, and nothing leaves the
// machine.
import { test as base, expect } from '@playwright/test';

/** The demo backends' starting time (server/main.go Epoch). */
export const EPOCH = new Date('2026-03-14T15:00:00Z');

export const test = base.extend({
  world: async ({ context, baseURL }, use, testInfo) => {
    const name = `${testInfo.testId}-${testInfo.repeatEachIndex}-${testInfo.retry}`;
    await context.addCookies([{ name: 'e2e-world', value: name, url: baseURL }]);
    // Artwork and links point at Apple; the tests never fetch them.
    await context.route(/^https?:\/\/(?!127\.0\.0\.1[:/])/, (route) => route.abort());
    await use({
      name,
      /** Moves this test's backend (jobs and clock) on by ms. */
      async step(ms) {
        const res = await context.request.post(`/e2e/step?world=${encodeURIComponent(name)}&ms=${ms}`);
        expect(res.status()).toBe(204);
      },
    });
  },
  page: async ({ page, world }, use) => {
    void world;
    await page.clock.setFixedTime(EPOCH);
    await use(page);
  },
});

export { expect };

/** Opens the console at a hash route (#/queue, #/settings?section=hooks). */
export async function open(page, hash = '#/') {
  await page.goto(`/console/${hash}`);
  await expect(page.getByRole('main')).toBeVisible();
}
