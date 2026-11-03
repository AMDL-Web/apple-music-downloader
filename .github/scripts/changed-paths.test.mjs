import assert from 'node:assert/strict';
import { test } from 'node:test';
import { classify } from './changed-paths.mjs';

test('the guest boot runs for the wrapper host, the pin and its own files', () => {
  for (const f of ['internal/wrapperhost/qemu.go', 'internal/wrapperhost/pin.go', '.github/scripts/wrapper-pin.mjs',
    '.github/scripts/wrapper-pin.test.mjs', '.github/actions/guestboot/action.yml', '.github/workflows/guestboot.yml',
    'internal/ext4/ext4.go', 'internal/config/config.go', 'internal/client/types.go', 'internal/domain/ids.go',
    'internal/limits/limits.go', 'internal/fairplay/rounds_gen.go', 'go.mod']) {
    assert.equal(classify([f]).guestboot, true, f);
  }
});

test('the guest boot skips a change that cannot reach it', () => {
  assert.equal(classify(['README.md', 'docs/cli.md', 'apps/console/web/app.js', 'internal/jobs/jobs.go', 'internal/configs/x.go']).guestboot, false);
});

test('one matching path among many is enough', () => {
  assert.equal(classify(['README.md', 'internal/wrapperhost/guest.go', 'docs/api.md']).guestboot, true);
});

test('an unknown change list runs every job', () => {
  assert.deepEqual(classify([]), { guestboot: true });
  assert.deepEqual(classify(['', '\n']), { guestboot: true });
});
