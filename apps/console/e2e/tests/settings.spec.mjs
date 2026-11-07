import { test, expect, open } from './fixtures.mjs';

test('a settings edit is marked unsaved, saved, and still there after a reload', async ({ page }) => {
  await open(page, '#/settings');
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Quality' }).click();
  const rate = page.getByRole('combobox', { name: 'Highest ALAC sample rate' });
  await rate.selectOption({ label: '48 kHz' });

  const unsaved = page.getByRole('region', { name: 'Unsaved changes' });
  await expect(unsaved.getByText('1 unsaved change')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Undo the change to Highest ALAC sample rate' })).toBeVisible();
  await unsaved.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved 1 setting' })).toBeVisible();
  await expect(unsaved).toBeHidden();

  await page.reload();
  const after = page.getByRole('combobox', { name: 'Highest ALAC sample rate' });
  await expect(after.getByRole('option', { name: '48 kHz', selected: true })).toHaveCount(1);
});

test('a hook is added from a preset, then edited', async ({ page }) => {
  await open(page, '#/settings?section=hooks');
  const hooks = page.getByRole('region', { name: 'Hooks' });
  await hooks.getByRole('button', { name: 'Add hook' }).click();
  await page.getByRole('dialog', { name: 'Add hook' }).getByRole('button', { name: 'ntfy' }).click();

  const sheet = page.getByRole('dialog', { name: 'ntfy' });
  await sheet.getByRole('textbox', { name: 'Topic' }).fill('amdl-e2e');
  await sheet.getByRole('button', { name: 'Add' }).click();
  await expect(sheet).toBeHidden();
  await expect(hooks.getByRole('switch', { name: 'ntfy on' })).toBeChecked();

  await hooks.getByRole('button', { name: 'More actions for ntfy' }).click();
  await page.getByRole('menuitem', { name: 'Edit…' }).click();
  // The secret fields stay on the server; editing changes when it runs.
  await expect(sheet.getByText('ntfy · ntfy.sh')).toBeVisible();
  await expect(sheet.getByRole('checkbox', { name: 'Cancelled' })).not.toBeChecked();
  await sheet.getByRole('checkbox', { name: 'Cancelled' }).check();
  await sheet.getByRole('button', { name: 'Save' }).click();
  await expect(sheet).toBeHidden();
  await expect(hooks.getByRole('listitem').filter({ hasText: 'ntfy' })).toContainText('Finished · Failed · Cancelled');
});

test('an account logs out, and another logs in with its two-step code', async ({ page }) => {
  await open(page, '#/settings?section=account');
  const accounts = page.getByRole('region', { name: 'Logged-in accounts' });
  await expect(accounts.getByText('demo@example.com · US')).toBeVisible();
  const library = page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: 'Library' });
  await expect(library).toBeVisible();

  await accounts.getByRole('button', { name: 'Log out' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Log out of this account?' });
  await confirm.getByRole('button', { name: 'Log out' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Logged out demo@example.com' })).toBeVisible();
  await expect(accounts).toBeHidden();
  // The Library page follows the account that copies its Apple library.
  await expect(library).toBeHidden();

  const login = page.getByRole('form', { name: 'Log in' });
  await login.getByRole('textbox', { name: 'Apple ID' }).fill('someone@example.com');
  await login.getByRole('textbox', { name: 'Password' }).fill('correct horse');
  await login.getByRole('button', { name: 'Log in' }).click();

  const twoStep = page.getByRole('form', { name: 'Two-step verification' });
  await expect(twoStep.getByText('Apple sent a code to the trusted devices of someone@example.com.')).toBeVisible();
  await twoStep.getByRole('textbox', { name: 'Verification code' }).fill('123456');
  await twoStep.getByRole('button', { name: 'Verify' }).click();
  // The demo backend logs its one account back in, whatever the Apple ID.
  await expect(accounts.getByText('demo@example.com · US')).toBeVisible();
  await expect(library).toBeVisible();
});
