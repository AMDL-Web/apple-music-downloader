import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pick, pickLite, pinFrom, urlOf } from './wrapper-pin.mjs';

// The shapes of GitHub's workflow run and release listings.
const run = { head_branch: 'amdl', conclusion: 'success', id: 41603068335, head_sha: 'b60beea21807c46cd8b788769ae061e173cfd744' };
const asset = (over) => ({ name: 'manager-qemu-guest.zip', size: 30007371,
  digest: 'sha256:71cf59e4bb2b4f3a2f4afd98445b0fe218767136cf464712b196d2d192830499', ...over });
const release = (assets, tag = 'guest-b60beea21807') => ({ tag_name: tag, assets });

test('pick pins the manager-qemu-guest asset of the run\'s release with its digest', () => {
  const p = pick(run, release([asset({ name: 'README.md' }), asset()]));
  assert.deepEqual(p, { artifact: 'manager-qemu-guest', commit: run.head_sha,
    sha256: '71cf59e4bb2b4f3a2f4afd98445b0fe218767136cf464712b196d2d192830499', size: 30007371 });
  assert.equal(urlOf('guest', p), 'https://github.com/AMDL-Web/wrapper/releases/download/guest-b60beea21807/manager-qemu-guest.zip');
});

test('pick refuses a missing asset, one without a digest, or another commit\'s release', () => {
  assert.throws(() => pick(run, release([])), /has no manager-qemu-guest.zip/);
  assert.throws(() => pick(run, release([asset({ digest: null })])), /no sha256 digest/);
  assert.throws(() => pick(run, release([asset()], 'guest-3ff150fdbd27')), /no guest-b60beea21807 release/);
  assert.throws(() => pick(run, undefined), /no guest-b60beea21807 release/);
});

for (const over of [{head_branch:'main'}, {conclusion:'failure'}, {conclusion:'in_progress'}, {head_sha:undefined}, {head_sha:'b60beea'}]) {
  test(`pick refuses a run with ${JSON.stringify(over)}`, () => {
    assert.throws(() => pick({...run, ...over}, release([asset()])), /successful manager run on amdl/);
  });
}

// The build-lite run and its release.
const liteRun = { head_branch: 'amdl', conclusion: 'success', id: 41582282341, head_sha: '97267425669943e9ced5691ee8a311543110a258' };
const liteRelease = (over) => release([asset({ name: 'wrapper-lite-linux-x86_64.zip', size: 50118510,
  digest: 'sha256:0bed5a806f52c71bc2b45f94ed08da3758d22084bc897578ee898673721847c3', ...over })], 'lite-972674256699');

test('pickLite pins the x86_64 payload of the run\'s release', () => {
  const p = pickLite(liteRun, liteRelease());
  assert.deepEqual(p, { artifact: 'wrapper-lite-linux-x86_64', commit: liteRun.head_sha,
    sha256: '0bed5a806f52c71bc2b45f94ed08da3758d22084bc897578ee898673721847c3', size: 50118510 });
  assert.equal(urlOf('lite', p), 'https://github.com/AMDL-Web/wrapper/releases/download/lite-972674256699/wrapper-lite-linux-x86_64.zip');
});

test('pickLite refuses a bare-lite QEMU asset or one without a digest', () => {
  assert.throws(() => pickLite(liteRun, liteRelease({ name: 'wrapper-lite-qemu-linux-x86_64.zip' })), /no wrapper-lite-linux-x86_64.zip/);
  assert.throws(() => pickLite(liteRun, liteRelease({ digest: undefined })), /no sha256 digest/);
});

for (const over of [{head_branch:'lite'}, {conclusion:'failure'}, {conclusion:null}, {head_sha:undefined}]) {
  test(`pickLite refuses a run with ${JSON.stringify(over)}`, () => {
    assert.throws(() => pickLite({...liteRun, ...over}, liteRelease()), /successful build-lite run on amdl/);
  });
}

test('pinFrom writes the guest pin with the payload pin under lite, and needs both runs to be good', () => {
  const p = pinFrom(run, release([asset()]), liteRun, liteRelease());
  assert.equal(p.artifact, 'manager-qemu-guest');
  assert.equal(p.lite.commit, liteRun.head_sha);
  assert.deepEqual(Object.keys(p.lite).sort(), ['artifact', 'commit', 'sha256', 'size']);
  assert.throws(() => pinFrom(run, release([asset()]), { ...liteRun, head_branch: 'main' }, liteRelease()), /build-lite run on amdl/);
  assert.throws(() => pinFrom({ ...run, conclusion: 'failure' }, release([asset()]), liteRun, liteRelease()), /manager run on amdl/);
});
