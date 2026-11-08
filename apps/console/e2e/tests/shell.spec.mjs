import { test, expect, open } from './fixtures.mjs';

test('the overview loads the live queue, job counts and throughput', async ({ page }) => {
  await open(page, '#/');
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await expect(page.getByText('Live updates on')).toBeAttached();

  const live = page.getByRole('list', { name: 'Active jobs' });
  await expect(live.getByRole('listitem')).toHaveCount(3);
  await expect(live.getByRole('link', { name: /^Pop·2025·.*Glass Weather Mira Solenne 5 of 12 tracks, 57% done/ })).toBeVisible();

  const jobs = page.getByRole('region', { name: 'Jobs' });
  await expect(jobs.getByRole('link', { name: 'Running 2' })).toBeVisible();
  await expect(jobs.getByRole('link', { name: 'Failed 1' })).toBeVisible();
  await expect(page.getByRole('table', { name: /Completed tracks and bytes written per day/ })).toBeAttached();

  const primary = page.getByRole('navigation', { name: 'Primary' });
  await expect(primary.getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
  await primary.getByRole('link', { name: /^Activity/ }).click();
  await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible();
  await expect(primary.getByRole('link', { name: /^Activity/ })).toHaveAttribute('aria-current', 'page');
  await expect(primary.getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current', 'page');
});

// The demo backend does not filter the Apple library, so the search box is
// not exercised here.
test('the library lists the Apple Music library by kind', async ({ page }) => {
  await open(page, '#/library');
  await expect(page.getByRole('heading', { name: 'Library', level: 1 })).toBeVisible();
  const kind = page.getByRole('group', { name: 'Kind' });
  await expect(kind.getByRole('button', { name: 'Albums 5' })).toHaveAttribute('aria-pressed', 'true');
  const main = page.getByRole('main');
  // Albums amdl already has link to them; the one it lacks offers Add.
  const salt = main.getByRole('listitem').filter({ hasText: 'Salt & Static' });
  await expect(salt.getByRole('button', { name: 'Add' })).toBeVisible();
  const glass = main.getByRole('listitem').filter({ hasText: 'Glass Weather' });
  await expect(glass.getByRole('link', { name: 'In Library' })).toBeVisible();

  await kind.getByRole('button', { name: /^Playlists/ }).click();
  await expect(kind.getByRole('button', { name: /^Playlists/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(main.getByText('Favourite Songs')).toBeVisible();
  await expect(main.getByText('Salt & Static')).toBeHidden();
});

test('the command palette opens with Ctrl+K and runs a filter', async ({ page }) => {
  await open(page, '#/');
  await page.keyboard.press('Control+k');
  const palette = page.getByRole('dialog', { name: 'Command palette' });
  await expect(palette).toBeVisible();
  const input = palette.getByRole('combobox', { name: 'Search or paste a link' });
  await expect(input).toBeFocused();

  await input.fill('failed');
  await expect(palette.getByRole('option', { name: 'Show failed jobs' })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(palette).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Show' }).getByRole('button', { name: /^Failed/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('list', { name: 'Downloads' }).getByRole('link', { name: /^Tidal Rooms / })).toBeVisible();
});

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('a tab bar replaces the sidebar, New download in the middle, and marks the current page', async ({ page }) => {
    await open(page, '#/queue');
    const tabs = page.getByRole('navigation', { name: 'Primary' }).getByRole('link');
    // The sidebar is gone from the accessibility tree; only the tab bar is left.
    await expect(page.getByRole('navigation', { name: 'Primary' })).toHaveCount(1);
    await expect(tabs).toHaveText(['Overview', 'Activity', '', 'Library', 'Settings']);
    await expect(tabs.nth(2)).toHaveAccessibleName('New download');
    await expect(page.getByRole('link', { name: 'Activity' })).toHaveAttribute('aria-current', 'page');

    await page.getByRole('link', { name: 'Settings' }).click();
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('link', { name: 'Activity' })).not.toHaveAttribute('aria-current', 'page');
  });
});
