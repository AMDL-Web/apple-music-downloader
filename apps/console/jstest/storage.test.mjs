import test from 'node:test';
import assert from 'node:assert/strict';
import { midTruncate, groupByDisk, diskRole, lowSpaceDisk } from '../static/js/lib/storage.js';

test('a short path stays, a long one is cut in the middle and keeps both ends', () => {
  assert.equal(midTruncate('/data/music'), '/data/music');
  const long = '/private/tmp/some/very/long/location/for/the/downloads/folder/of/this/install/downloads';
  const cut = midTruncate(long, 30);
  assert.equal(cut.length, 30);
  assert.ok(cut.startsWith('/private/tm'));
  assert.ok(cut.endsWith('/downloads'));
  assert.ok(cut.includes('…'));
});

test('folders on one disk share a group; unknown space stays alone', () => {
  const disk = { total_bytes: 100, free_bytes: 20 };
  const groups = groupByDisk([
    { name: 'downloads_dir', path: '/a', ...disk },
    { name: 'temp_dir', path: '/b', ...disk },
    { name: 'other', path: '/c', total_bytes: 100, free_bytes: 50 },
    { name: 'gone', path: '/d', error: 'permission denied' },
  ]);
  assert.deepEqual(groups.map((g) => g.folders.map((f) => f.name)), [['downloads_dir', 'temp_dir'], ['other'], ['gone']]);
  assert.equal(groups[0].known, true);
  assert.equal(groups[2].known, false);
  assert.equal(groups[2].error, 'permission denied');
});

test('a disk is low on space at 85 % used and critical at 95 %; the fullest one is named', () => {
  const [ok, warn, crit, unknown] = groupByDisk([
    { name: 'a', total_bytes: 100, free_bytes: 50 },
    { name: 'b', total_bytes: 100, free_bytes: 15 },
    { name: 'c', total_bytes: 200, free_bytes: 4 },
    { name: 'd', error: 'permission denied' },
  ]);
  assert.deepEqual([ok, warn, crit, unknown].map(diskRole), ['info', 'warning', 'danger', 'neutral']);
  assert.equal(lowSpaceDisk([{ name: 'a', total_bytes: 100, free_bytes: 50 }]), null);
  assert.equal(lowSpaceDisk(undefined), null);
  const fullest = lowSpaceDisk([
    { name: 'b', total_bytes: 100, free_bytes: 15 },
    { name: 'c', total_bytes: 200, free_bytes: 4 },
  ]);
  assert.equal(fullest.folders[0].name, 'c');
});
