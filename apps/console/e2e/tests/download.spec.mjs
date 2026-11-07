import { test, expect, open } from './fixtures.mjs';

test('search the catalog and start an album download', async ({ page }) => {
  await open(page, '#/');
  await page.getByRole('link', { name: 'New download' }).click();
  await expect(page.getByRole('heading', { name: 'New download', level: 1 })).toBeVisible();

  await page.getByRole('textbox', { name: 'Search or paste links' }).fill('Salt');
  await expect(page.getByRole('status').filter({ hasText: '1 album and 6 songs for “Salt”' })).toBeVisible();
  const albums = page.getByRole('list', { name: 'Albums' });
  await albums.getByRole('button', { name: 'Download Salt & Static' }).click();

  const submitted = page.getByRole('region', { name: 'Submitted' });
  await expect(submitted.getByText('Started 1 download')).toBeVisible();
  await expect(submitted.getByRole('link', { name: /^Salt & Static Nightjar Coast 0 of 9 tracks/ })).toBeVisible();

  // The new job is in Activity, queued behind the two that run.
  await page.getByRole('link', { name: 'View in Activity' }).click();
  await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible();
  await expect(page.getByRole('list', { name: 'Downloads' }).getByRole('link', { name: /^Salt & Static .* Queued/ })).toBeVisible();
});

test('a submitted job runs to done while Activity watches', async ({ page, world }) => {
  const res = await page.request.post('/api/v1/downloads', {
    data: { urls: ['https://music.apple.com/us/album/salt-static/920004'] },
  });
  expect(res.ok()).toBeTruthy();
  await open(page, '#/queue');
  const job = page.getByRole('list', { name: 'Downloads' }).getByRole('link', { name: /^Salt & Static / });
  await expect(job).toContainText('Queued');

  // The page never reloads: the downloads feed moves the row on as the
  // backend's jobs move. It waits for the two running jobs and the one queued
  // before it, then runs, then lands as downloaded.
  await expect(async () => {
    await world.step(20_000);
    await expect(job).toContainText('Running', { timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
  await expect(job.getByRole('progressbar', { name: 'Tracks' })).toBeVisible();
  await expect(async () => {
    await world.step(20_000);
    await expect(job).toContainText('Downloaded', { timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
  await expect(job).not.toContainText('Running');
});

// An album row in Activity opens the album's record, which the demo backend
// does not serve; the Overview's live queue opens the job itself.
test('the job drawer opens from the live queue, shows the tracks and closes back to the card', async ({ page }) => {
  await open(page, '#/');
  const row = page.getByRole('list', { name: 'Active jobs' }).getByRole('link', { name: /Glass Weather/ });
  await row.click();

  const drawer = page.getByRole('dialog', { name: 'Glass Weather' });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText('Album · 2025 · Pop · US')).toBeVisible();
  await expect(drawer.getByRole('region', { name: 'Tracks' }).getByRole('listitem')).toHaveCount(12);
  await expect(drawer.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(page).toHaveURL(/#\/overview\/demo-0005$/);

  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await expect(page).toHaveURL(/#\/$/);
  await expect(row).toBeFocused();
});
