import test from 'node:test';
import assert from 'node:assert/strict';
import {
  applyFeedMessage,
  jobMatches,
  upsertJob,
  removeJob,
  appendPage,
  detailFromSnapshot,
  applyJobEvents,
  mergeSnapshot,
  appendLogs,
  emptyLogs,
} from '../static/js/lib/store.js';

const counts = (o) => ({ queued: 0, running: 0, completed: 0, failed: 0, cancelled: 0, total: 0, ...o });

test('feed messages replace counts and only move the cursor forward', () => {
  let s = { counts: null, cursor: 10 };
  s = applyFeedMessage(s, { type: 'download_upserted', event_id: 12, status_counts: counts({ running: 1, total: 1 }) });
  assert.equal(s.cursor, 12);
  assert.equal(s.counts.running, 1);
  const replay = applyFeedMessage(s, { type: 'download_upserted', event_id: 11, status_counts: counts({ total: 1 }) });
  assert.equal(replay.cursor, 12);
});

test('jobMatches filters by status, type and case-insensitive text', () => {
  const job = { status: 'running', type: 'album', title: 'Midnights', artist_name: 'Taylor Swift', input: 'https://music.apple.com/us/album/x/1' };
  assert.equal(jobMatches(job, { statuses: ['running', 'queued'] }), true);
  assert.equal(jobMatches(job, { statuses: ['failed'] }), false);
  assert.equal(jobMatches(job, { types: ['playlist'] }), false);
  assert.equal(jobMatches(job, { q: 'MIDNI' }), true);
  assert.equal(jobMatches(job, { q: 'music.apple.com/us' }), true);
  assert.equal(jobMatches(job, { q: 'folklore' }), false);
  assert.equal(jobMatches(job, { q: 'swift' }), true, 'the artist is searched too');
});

const T = (s) => `2026-10-28T12:00:${String(s).padStart(2, '0')}Z`;

test('upsertJob inserts, updates in place, removes on mismatch and ignores stale snapshots', () => {
  const filter = { statuses: ['running', 'queued'] };
  let list = [{ id: 'a', status: 'running', updated_at: T(1) }];
  list = upsertJob(list, { id: 'b', status: 'queued', updated_at: T(2) }, filter);
  assert.deepEqual(list.map((j) => j.id), ['b', 'a']);
  list = upsertJob(list, { id: 'c', status: 'queued', updated_at: T(2) }, filter, { append: true });
  assert.deepEqual(list.map((j) => j.id), ['b', 'a', 'c']);
  list = upsertJob(list, { id: 'a', status: 'running', title: 'new', updated_at: T(3) }, filter);
  assert.equal(list[1].title, 'new');
  const stale = upsertJob(list, { id: 'a', status: 'queued', updated_at: T(2) }, filter);
  assert.equal(stale, list, 'an older snapshot leaves the list as it was');
  list = upsertJob(list, { id: 'a', status: 'completed', updated_at: T(4) }, filter);
  assert.deepEqual(list.map((j) => j.id), ['b', 'c']);
  assert.equal(upsertJob(list, { id: 'z', status: 'failed', updated_at: T(5) }, filter), list);
  const capped = upsertJob(list, { id: 'd', status: 'queued', updated_at: T(5) }, filter, { limit: 2 });
  assert.deepEqual(capped.map((j) => j.id), ['d', 'b']);
});

test('a motion artwork update with an unchanged updated_at still applies', () => {
  const list = [{ id: 'a', status: 'completed', updated_at: T(1) }];
  const next = upsertJob(list, { id: 'a', status: 'completed', updated_at: T(1), motion_artwork_url: 'https://x/m.m3u8' }, {});
  assert.equal(next[0].motion_artwork_url, 'https://x/m.m3u8');
});

test('removeJob and appendPage', () => {
  const list = [{ id: 'a' }, { id: 'b' }];
  assert.deepEqual(removeJob(list, 'a'), [{ id: 'b' }]);
  assert.equal(removeJob(list, 'x'), list);
  assert.deepEqual(appendPage(list, [{ id: 'b' }, { id: 'c' }]).map((j) => j.id), ['a', 'b', 'c']);
});

function snapshot() {
  return detailFromSnapshot({
    job: { id: 'job_1', status: 'running', total_items: 2, completed_items: 0, updated_at: T(0) },
    items: [
      { id: 'item_1', index: 1, status: 'downloading', progress: { download: 0.2 } },
      { id: 'item_2', index: 2, status: 'queued', progress: {} },
    ],
    hooks: [],
    event_cursor: 50,
  });
}

test('per-job events merge payload objects by item and skip anything at or before the cursor', () => {
  let s = snapshot();
  s = applyJobEvents(s, [
    { id: 49, type: 'item_progress', item_id: 'item_1', payload: { id: 'item_1', status: 'downloading', progress: { download: 0.1 } } },
    { id: 51, type: 'item_progress', item_id: 'item_1', payload: { id: 'item_1', status: 'decrypting', progress: { download: 1, decrypt: 0.3 } } },
    { id: 52, type: 'item_completed', item_id: 'item_2', message: 'done', payload: { id: 'item_2', status: 'completed', codec: 'alac', file_size: 123, message: 'done' } },
  ]);
  assert.equal(s.cursor, 52);
  assert.equal(s.items[0].status, 'decrypting');
  assert.equal(s.items[0].progress.decrypt, 0.3);
  assert.equal(s.items[1].status, 'completed');
  assert.equal(s.items[1].file_size, 123);
  assert.equal(s.items[1].message, undefined, 'the event message is not an item field');
  assert.deepEqual(s.activity, [], 'progress and per-track completion events stay out of the activity list');
});

test('the job counts follow the tracks the stream moves, not the snapshot they started from', () => {
  // The drawer once kept the snapshot's completed_items (0) while a row below
  // already said Completed: only lifecycle events touched the job.
  let s = snapshot();
  s = applyJobEvents(s, [
    { id: 51, type: 'item_completed', item_id: 'item_2', payload: { id: 'item_2', status: 'completed', file_size: 1 } },
  ]);
  assert.equal(s.items[1].status, 'completed');
  assert.equal(s.job.completed_items, 1);
  s = applyJobEvents(s, [
    { id: 52, type: 'item_failed', item_id: 'item_1', payload: { id: 'item_1', status: 'failed', error: 'x' } },
  ]);
  assert.deepEqual(
    [s.job.total_items, s.job.completed_items, s.job.skipped_items, s.job.failed_items, s.job.cancelled_items],
    [2, 1, 0, 1, 0],
  );
});

test('events at or before the cursor fill Activity without touching the job or its tracks', () => {
  let s = snapshot();
  const replay = [
    { id: 10, type: 'job_queued', created_at: T(1), payload: { id: 'job_1', status: 'queued', total_items: 0 } },
    { id: 11, type: 'job_started', created_at: T(2), payload: { id: 'job_1', status: 'queued' } },
    { id: 12, type: 'codec_selected', item_id: 'item_1', payload: { id: 'item_1', status: 'queued' } },
    { id: 13, type: 'item_progress', item_id: 'item_1', payload: { id: 'item_1', status: 'queued', progress: {} } },
  ];
  s = applyJobEvents(s, replay);
  assert.deepEqual(s.activity.map((a) => a.type), ['job_queued', 'job_started']);
  assert.equal(s.job.status, 'running', 'a replayed lifecycle payload must not undo the snapshot');
  assert.equal(s.items[0].status, 'downloading');
  assert.equal(s.cursor, 50);
  const again = applyJobEvents(s, replay);
  assert.equal(again.activity.length, 2, 'a second replay adds nothing');
  assert.equal(again, s, 'and changes nothing');
  const live = applyJobEvents(s, [{ id: 51, type: 'operation_retry', phase: 'download', created_at: T(3) }]);
  assert.deepEqual(live.activity.map((a) => a.id), [10, 11, 51], 'activity stays in id order');
});

test('Activity is capped at 200 events and keeps the newest', () => {
  const lots = Array.from({ length: 250 }, (_, i) => ({ id: i + 1, type: 'operation_retry', phase: 'x' }));
  const s = applyJobEvents(snapshot(), lots);
  assert.equal(s.activity.length, 200);
  assert.equal(s.activity[199].id, 250);
});

test('lifecycle events merge the job; item-set changes ask for a fresh snapshot', () => {
  let s = snapshot();
  s = applyJobEvents(s, [{ id: 60, type: 'job_finished', payload: { id: 'job_1', status: 'completed', completed_items: 2, finished_at: T(9), message: 'ok' } }]);
  assert.equal(s.job.status, 'completed');
  assert.equal(s.job.finished_at, T(9));
  assert.equal(s.job.total_items, 2, 'fields absent from the payload are kept');
  assert.equal(s.job.message, undefined);
  assert.equal(s.needsRefetch, false);
  s = applyJobEvents(s, [{ id: 61, type: 'resolved_input', payload: { id: 'job_1', title: 'Midnights' } }]);
  assert.equal(s.needsRefetch, true);
  const unknownItem = applyJobEvents(snapshot(), [{ id: 70, type: 'item_progress', item_id: 'item_9', payload: { id: 'item_9' } }]);
  assert.equal(unknownItem.needsRefetch, true);
  const motion = applyJobEvents(snapshot(), [{ id: 71, type: 'motion_artwork_resolved', phase: 'motion_artwork' }]);
  assert.equal(motion.needsRefetch, true);
});

test('hook events build the hook list by name', () => {
  let s = snapshot();
  s = applyJobEvents(s, [
    { id: 80, type: 'hook_started', phase: 'notify', payload: { hook: 'notify' } },
    { id: 81, type: 'hook_started', phase: 'scan', payload: { hook: 'scan' } },
    { id: 82, type: 'hook_failed', phase: 'notify', message: 'HTTP 500', payload: { hook: 'notify', attempts: 3 } },
  ]);
  assert.deepEqual(s.hooks, [
    { name: 'notify', status: 'failed', error: 'HTTP 500' },
    { name: 'scan', status: 'running' },
  ]);
});

test('job_deleted marks the drawer deleted', () => {
  assert.equal(applyJobEvents(snapshot(), [{ id: 90, type: 'job_deleted' }]).deleted, true);
});

test('mergeSnapshot keeps the newer cursor and the activity seen so far', () => {
  let s = applyJobEvents(snapshot(), [{ id: 55, type: 'job_started', payload: { id: 'job_1', status: 'running' } }]);
  const merged = mergeSnapshot(s, { job: { id: 'job_1', status: 'running' }, items: [], hooks: [], event_cursor: 53 });
  assert.equal(merged.cursor, 55);
  assert.equal(merged.activity.length, 1);
  assert.equal(mergeSnapshot(s, { job: { id: 'job_1' }, items: [], hooks: [], event_cursor: 70 }).cursor, 70);
});

test('appendLogs drops replayed sequences and caps the buffer', () => {
  const e = (sequence) => ({ sequence, time: T(0), level: 'info', message: `m${sequence}` });
  let s = appendLogs(emptyLogs(), [e(1), e(2), e(3)]);
  s = appendLogs(s, [e(2), e(3), e(4)]);
  assert.deepEqual(s.entries.map((x) => x.sequence), [1, 2, 3, 4]);
  assert.equal(s.lastSeq, 4);
  s = appendLogs(s, [e(6), e(5)], 3);
  assert.deepEqual(s.entries.map((x) => x.sequence), [4, 5, 6]);
  assert.equal(s.dropped, 3);
  assert.equal(appendLogs(s, [e(6)]), s, 'nothing new leaves the state as it was');
});
