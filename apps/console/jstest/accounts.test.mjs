import { test } from 'node:test';
import assert from 'node:assert/strict';
import { accountLabel, accountList, appleLibraryOn, defaultAppleAccount, accountRegions, regionTag, flattenLibrarySync, librarySyncLines } from '../static/js/lib/accounts.js';
import { wrapperErrorText } from '../static/js/lib/settings-account.js';

test('accountLabel: name and region, the region alone, or the name alone', () => {
  assert.equal(accountLabel({ name: 'a@icloud.com', region: 'jp' }), 'a@icloud.com · JP');
  assert.equal(accountLabel({ name: '', region: 'us' }), 'US');
  assert.equal(accountLabel({ name: 'a@icloud.com', region: '' }), 'a@icloud.com');
});

test('accountList reads GET /accounts, whatever came back', () => {
  assert.deepEqual(accountList({ accounts: [{ id: 'x' }] }), [{ id: 'x' }]);
  assert.deepEqual(accountList(undefined), []);
  assert.deepEqual(accountList({ accounts: null }), []);
});

test('the Apple library view reads the default account, else the first', () => {
  const a = { account_id: 'a', default: false, state: 'idle' };
  const b = { account_id: 'b', default: true, state: 'running' };
  assert.equal(defaultAppleAccount({ accounts: [a, b] }), b);
  assert.equal(defaultAppleAccount({ accounts: [a] }), a);
  assert.equal(defaultAppleAccount({ accounts: [] }), null);
  assert.equal(defaultAppleAccount(undefined), null);
});

const entry = (over) => ({ account_id: 'a', name: 'a@icloud.com', region: 'us', enabled: true, anchor_size: 10, last_poll_at: '2026-11-03T01:00:00Z', last_error: '', last_submitted: 1, total_submitted: 3, ...over });

test('library sync: one account reads as before, several are summed', () => {
  const one = flattenLibrarySync({ enabled: true, interval_minutes: 15, accounts: [entry()] });
  assert.deepEqual(one, { enabled: true, interval_minutes: 15, anchor_size: 10, last_submitted: 1, total_submitted: 3, last_poll_at: '2026-11-03T01:00:00.000Z', last_error: '' });
  const two = flattenLibrarySync({
    enabled: true,
    interval_minutes: 15,
    accounts: [entry(), entry({ account_id: 'b', anchor_size: 5, total_submitted: 2, last_poll_at: '2026-11-03T02:00:00Z', last_error: 'boom' }), entry({ account_id: 'c', enabled: false, last_error: 'ignored', last_poll_at: '2026-11-03T03:00:00Z' })],
  });
  assert.equal(two.anchor_size, 25);
  assert.equal(two.total_submitted, 8);
  assert.equal(two.last_poll_at, '2026-11-03T02:00:00.000Z', 'the latest check of an account that polls');
  assert.equal(two.last_error, 'boom', 'a switched-off account does not raise an error');
  assert.deepEqual(flattenLibrarySync({ enabled: true, interval_minutes: 15, accounts: [] }).last_poll_at, undefined);
});

test('library sync lines: none for one account, one per account for several', () => {
  assert.deepEqual(librarySyncLines({ accounts: [entry()] }), []);
  const lines = librarySyncLines({ accounts: [entry(), entry({ account_id: 'b', name: '', region: 'jp', last_error: 'boom' }), entry({ account_id: 'c', enabled: false })] });
  assert.deepEqual(lines.map((l) => [l.label, l.text, l.role]), [
    ['a@icloud.com · US', '10 tracks seen · 3 albums since start', undefined],
    ['JP', 'boom', 'danger'],
    ['a@icloud.com · US', 'off', undefined],
  ]);
});

test('login at the account limit shows the server message', () => {
  const t = wrapperErrorText({ status: 409, code: 'account_limit_reached', message: 'The session service holds 10 accounts, the most it keeps.' }, 'login');
  assert.equal(t.message, 'The session service holds 10 accounts, the most it keeps.');
});

test('a region tag shows only while the accounts serve two regions or more', () => {
  const one = accountRegions({ accounts: [{ id: 'a', region: 'us', default: true, state: 'active' }, { id: 'b', region: 'US', state: 'active' }] });
  assert.deepEqual(one, { regions: ['us'], fallback: 'us' });
  assert.equal(regionTag('jp', one), '', 'one region: no tag');
  assert.equal(regionTag('', one), '');
  const two = accountRegions({ accounts: [{ id: 'b', region: 'jp', state: 'active' }, { id: 'a', region: 'us', default: true, state: 'active' }, { id: 'c', region: 'cn', state: 'missing' }] });
  assert.deepEqual(two, { regions: ['jp', 'us'], fallback: 'us' }, 'a missing account serves no region');
  assert.equal(regionTag('jp', two), 'JP');
  assert.equal(regionTag('', two), 'US', 'an empty storefront is the region of the default account');
  assert.equal(regionTag('jp', accountRegions(undefined)), '', 'no accounts read yet: no tag');
});

test('appleLibraryOn is true only while an account copies its Apple library', () => {
  assert.equal(appleLibraryOn({ accounts: [{ id: 'a', apple_library: false }, { id: 'b' }] }), false);
  assert.equal(appleLibraryOn({ accounts: [{ id: 'a' }, { id: 'b', apple_library: true }] }), true);
  assert.equal(appleLibraryOn(undefined), false);
});
