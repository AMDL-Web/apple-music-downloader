import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pick, pickLite, pinFrom, liteURL } from './wrapper-pin.mjs';

// The shapes of GitHub's workflow run and artifacts listings.
const run = { head_branch: 'lite', conclusion: 'success', id: 40194181305, head_sha: 'bde3335f47a30518e1f53f79cbae94bf76b27fb3' };
const art = (over) => ({ id: 12598758939, name: 'manager-qemu-guest', expired: false, size_in_bytes: 64451213,
  digest: 'sha256:d2c150c07e20a42d0978420f2d43a1a7b43c1e843575ff48075ef89940aef292', expires_at: '2027-01-23T12:40:50Z', ...over });

test('pick pins the run\'s manager-qemu-guest artifact with its digest', () => {
  const p = pick(run, [art({ id: 1, name: 'wrapper-lite-qemu-linux-x86_64' }), art()]);
  assert.deepEqual(p, { artifact: 'manager-qemu-guest', commit: run.head_sha, run_id: run.id, artifact_id: 12598758939,
    sha256: 'd2c150c07e20a42d0978420f2d43a1a7b43c1e843575ff48075ef89940aef292', size: 64451213, expires_at: '2027-01-23T12:40:50Z' });
});

test('pick refuses an expired artifact or one without a digest', () => {
  assert.throws(() => pick(run, [art({ expired: true })]), /no live manager-qemu-guest/);
  assert.throws(() => pick(run, [art({ digest: null })]), /no sha256 digest/);
});

for (const over of [{head_branch:'main'}, {conclusion:'failure'}, {conclusion:'in_progress'}, {id:0}, {id:undefined}, {head_sha:undefined}]) {
  test(`pick refuses a run with ${JSON.stringify(over)}`, () => {
    assert.throws(() => pick({...run, ...over}, [art()]), /successful manager run on lite/);
  });
}
test('pick refuses a bare-lite artifact', () => {
 assert.throws(() => pick(run, [art({name:'qemu-assets'})]), /no live manager-qemu-guest/);
});

// The shapes of the build-lite run and its artifacts.
const liteRun = { head_branch: 'lite', conclusion: 'success', id: 41582282341, head_sha: '97267425669943e9ced5691ee8a311543110a258' };
const liteArt = (over) => ({ id: 13206063271, name: 'wrapper-lite-linux-x86_64', expired: false, size_in_bytes: 50118510,
  digest: 'sha256:0bed5a806f52c71bc2b45f94ed08da3758d22084bc897578ee898673721847c3', expires_at: '2027-02-01T05:09:59Z', ...over });

test('pickLite pins the run\'s x86_64 payload, not the other build-lite artifacts', () => {
  const p = pickLite(liteRun, [liteArt({ id: 1, name: 'wrapper-lite-qemu-linux-x86_64' }), liteArt({ id: 2, name: 'qemu-assets' }), liteArt()]);
  assert.deepEqual(p, { artifact: 'wrapper-lite-linux-x86_64', commit: liteRun.head_sha, run_id: 41582282341,
    sha256: '0bed5a806f52c71bc2b45f94ed08da3758d22084bc897578ee898673721847c3', size: 50118510, expires_at: '2027-02-01T05:09:59Z' });
  assert.equal(liteURL(p), 'https://nightly.link/AMDL-Web/wrapper/actions/runs/41582282341/wrapper-lite-linux-x86_64.zip');
});

test('pickLite refuses an expired payload, one without a digest or a bare-lite QEMU one', () => {
  assert.throws(() => pickLite(liteRun, [liteArt({ expired: true })]), /no live wrapper-lite-linux-x86_64/);
  assert.throws(() => pickLite(liteRun, [liteArt({ digest: undefined })]), /no sha256 digest/);
  assert.throws(() => pickLite(liteRun, [liteArt({ name: 'wrapper-lite-qemu-linux-x86_64' })]), /no live wrapper-lite-linux-x86_64/);
});

for (const over of [{head_branch:'lite-account-name'}, {conclusion:'failure'}, {conclusion:null}, {id:0}, {head_sha:undefined}]) {
  test(`pickLite refuses a run with ${JSON.stringify(over)}`, () => {
    assert.throws(() => pickLite({...liteRun, ...over}, [liteArt()]), /successful build-lite run on lite/);
  });
}

test('pinFrom writes the guest pin with the payload pin under lite, and needs both runs to be good', () => {
  const p = pinFrom(run, [art()], liteRun, [liteArt()]);
  assert.equal(p.artifact, 'manager-qemu-guest');
  assert.equal(p.lite.run_id, liteRun.id);
  assert.deepEqual(Object.keys(p.lite).sort(), ['artifact', 'commit', 'expires_at', 'run_id', 'sha256', 'size']);
  assert.throws(() => pinFrom(run, [art()], { ...liteRun, head_branch: 'main' }, [liteArt()]), /build-lite run on lite/);
  assert.throws(() => pinFrom({ ...run, conclusion: 'failure' }, [art()], liteRun, [liteArt()]), /manager run on lite/);
});
