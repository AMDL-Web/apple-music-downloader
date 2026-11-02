import assert from 'node:assert/strict';
import { test } from 'node:test';

import { resourceMark, markFraction, markedFiles, applyResourceFeed, withLatest, submitMark, applySentSongs, kindPath } from '../static/js/lib/marks.js';

const album = (fields = {}) => ({ kind: 'album', id: '1', state: 'none', tracks_done: 0, tracks_wanted: 9, withheld_count: 1, paused: false, ...fields });

test("a record's run wins, then paused, then what its tracks say; nothing done marks nothing", () => {
  assert.equal(resourceMark(null), null);
  assert.equal(resourceMark(album()), null, 'a record with nothing done and no run marks nothing');
  const active = resourceMark(album({ active_job_id: 'job_a', state: 'partial', tracks_done: 3 }));
  assert.equal(active.state, 'active');
  assert.deepEqual(active.job, { id: 'job_a' });
  assert.deepEqual(active.resource, { kind: 'album', id: '1' });
  assert.equal(resourceMark(album({ paused: true, state: 'partial', tracks_done: 3 })).state, 'paused');
  assert.equal(resourceMark(album({ paused: true, state: 'complete', tracks_done: 9 })).state, 'complete', 'a complete record is complete, paused or not');
  const partial = resourceMark(album({ state: 'partial', tracks_done: 3 }));
  assert.deepEqual([partial.state, partial.done, partial.total, partial.withheld, partial.job], ['partial', 3, 9, 1, null]);
  assert.equal(markFraction(partial), '3/9');
  assert.equal(markedFiles(partial), 3);
  assert.equal(markedFiles(active), 0, 'a run in the queue has the tracks already');
});

test("a song is marked by its own track, not by its album's other tracks", () => {
  const r = album({ active_job_id: 'job_a', state: 'partial', tracks_done: 3 });
  assert.equal(resourceMark(r, null), null, 'a song its album record does not list is not in the queue for this run');
  assert.equal(resourceMark(r, 'pending').state, 'active');
  assert.equal(resourceMark(album({ state: 'partial', tracks_done: 3 }), 'done').state, 'complete');
  assert.equal(resourceMark(album({ paused: true }), 'pending').state, 'paused');
  assert.equal(resourceMark(album(), 'given_up'), null);
  assert.equal(resourceMark(album(), 'pending'), null, 'waiting with no run and not paused says nothing');
});

test('the feed replaces a record mark; the newest fields win over an answer', () => {
  const latest = new Map();
  assert.equal(applyResourceFeed(latest, [{ type: 'download_upserted', job: { id: 'j' } }]), latest);
  const msg = { type: 'resource_updated', resource_kind: 'album', resource_id: '1', resource: { state: 'complete', tracks_done: 9, tracks_wanted: 9, withheld_count: 1, paused: false, name: 'x' } };
  const next = applyResourceFeed(latest, [msg]);
  assert.notEqual(next, latest);
  assert.deepEqual(next.get('album:1'), { kind: 'album', id: '1', state: 'complete', tracks_done: 9, tracks_wanted: 9, withheld_count: 1, paused: false });
  const answer = album({ active_job_id: 'job_a', song_state: 'running' });
  const merged = withLatest(answer, next);
  assert.equal(merged.active_job_id, undefined, 'a run the feed says ended is gone');
  assert.equal(merged.song_state, 'running', "the answer's song state stays");
  assert.equal(resourceMark(merged).state, 'complete');
  assert.equal(withLatest(answer, new Map()), answer);
});

test('a submit result names its record and run', () => {
  assert.deepEqual(submitMark({ status: 'accepted', resource: { kind: 'album', id: '1' }, job: { id: 'j' } }), { kind: 'album', id: '1', active_job_id: 'j' });
  assert.deepEqual(submitMark({ status: 'duplicate_active', resource: { kind: 'playlist', id: 'pl.1' }, existing_job_id: 'k' }), { kind: 'playlist', id: 'pl.1', active_job_id: 'k' });
  assert.equal(submitMark({ status: 'accepted', job: { id: 'j' } }), null, 'a station has no record');
  assert.equal(submitMark({ status: 'already_downloaded', resource: { kind: 'album', id: '1' } }), null);
});

test("a sent song takes its run's outcome", () => {
  const sent = new Map([['7', { jobId: 'j', state: 'pending' }]]);
  assert.equal(applySentSongs(sent, [{ type: 'download_upserted', job: { id: 'j', status: 'running' } }]), sent);
  assert.equal(applySentSongs(sent, [{ type: 'download_upserted', job: { id: 'j', status: 'completed' } }]).get('7').state, 'done');
  assert.equal(applySentSongs(sent, [{ type: 'download_upserted', job: { id: 'other', status: 'completed' } }]), sent);
});

test('record kinds map to their /library path', () => {
  assert.equal(kindPath('album'), 'albums');
  assert.equal(kindPath('playlist'), 'playlists');
  assert.equal(kindPath('playlists'), 'playlists');
});
