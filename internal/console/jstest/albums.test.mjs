import test from 'node:test';
import assert from 'node:assert/strict';
import { jobTargetKey, uniqueByTarget } from '../static/js/lib/albums.js';

const job = (id, input) => ({ id, input });
const folklore = 'https://music.apple.com/us/album/folklore/1524801260';

test('the same album under different spellings has one target', () => {
  assert.equal(jobTargetKey(job('a', folklore)), 'album:1524801260');
  assert.equal(jobTargetKey(job('b', 'https://music.apple.com/gb/album/folklore/id1524801260')), 'album:1524801260');
  assert.equal(jobTargetKey(job('c', 'not a link')), null);
});

test('the first job of a target is kept, other targets and non-links stay', () => {
  const jobs = [
    job('new', folklore),
    job('deluxe', 'https://music.apple.com/us/album/folklore/1528112358'),
    job('old', folklore),
    job('x', 'junk'),
    job('y', 'junk'),
  ];
  assert.deepEqual(uniqueByTarget(jobs).map((j) => j.id), ['new', 'deluxe', 'x', 'y']);
});

test('a song of an album link is its own target', () => {
  const song = job('s', `${folklore}?i=99`);
  assert.deepEqual(uniqueByTarget([job('a', folklore), song]).map((j) => j.id), ['a', 's']);
});
