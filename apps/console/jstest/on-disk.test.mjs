import assert from 'node:assert/strict';
import { test } from 'node:test';

import { diskMark, diskFraction, filesOnDisk } from '../static/js/lib/on-disk.js';

test('a job in the queue comes first, then the files; none on disk marks nothing', () => {
  const job = { id: 'j', status: 'running' };
  assert.deepEqual(diskMark({ state: 'active', job }, { state: 'complete', tracks_on_disk: 2, tracks_total: 2 }), { state: 'active', job });
  assert.deepEqual(diskMark(null, { state: 'partial', tracks_on_disk: 3, tracks_total: 13 }), { state: 'partial', onDisk: 3, total: 13, job: null });
  assert.deepEqual(diskMark({ state: 'done', job }, { state: 'complete', tracks_on_disk: 1, tracks_total: 1 }), { state: 'complete', onDisk: 1, total: 1, job });
  assert.equal(diskMark({ state: 'done', job }, { state: 'none', tracks_on_disk: 0, tracks_total: 12 }), null, 'a finished job whose files are gone');
  assert.equal(diskMark(null, undefined), null, 'no library state: unknown, nothing shown');
});

test('a partial mark reads as its count, and Download again replaces what is there', () => {
  const partial = diskMark(null, { state: 'partial', tracks_on_disk: 9, tracks_total: 12 });
  assert.equal(diskFraction(partial), '9/12');
  assert.equal(filesOnDisk(partial), 9);
  assert.equal(filesOnDisk({ state: 'active', job: {} }), 0);
  assert.equal(filesOnDisk(null), 0);
});
