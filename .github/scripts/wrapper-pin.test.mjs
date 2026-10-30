import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pick } from './wrapper-pin.mjs';

// The shapes of GitHub's workflow run and artifacts listings.
const run = { id: 40194181305, head_sha: 'bde3335f47a30518e1f53f79cbae94bf76b27fb3' };
const art = (over) => ({ id: 12598758939, name: 'qemu-assets', expired: false, size_in_bytes: 64451213,
  digest: 'sha256:d2c150c07e20a42d0978420f2d43a1a7b43c1e843575ff48075ef89940aef292', expires_at: '2027-01-23T12:40:50Z', ...over });

test('pick pins the run\'s qemu-assets artifact with its digest', () => {
  const p = pick(run, [art({ id: 1, name: 'wrapper-lite-qemu-linux-x86_64' }), art()]);
  assert.deepEqual(p, { commit: run.head_sha, run_id: run.id, artifact_id: 12598758939,
    sha256: 'd2c150c07e20a42d0978420f2d43a1a7b43c1e843575ff48075ef89940aef292', size: 64451213, expires_at: '2027-01-23T12:40:50Z' });
});

test('pick refuses an expired artifact or one without a digest', () => {
  assert.throws(() => pick(run, [art({ expired: true })]), /no live qemu-assets/);
  assert.throws(() => pick(run, [art({ digest: null })]), /no sha256 digest/);
});
