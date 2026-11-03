import assert from 'node:assert/strict';
import { test } from 'node:test';

import { recordView, recordHasWork, slotStates, hasExtraSlots, extraCodecs, runSlot, trackState, trackCountText, checkText, checkRunning, applyCheckFeed, songsIn, recordQuery, slotQuality, pendingTitle, recordTitle, commonQuality, checkScope, checkBody, resetNote, bulkResetText } from '../static/js/lib/records.js';

const rec = (fields = {}) => ({ id: '1', name: 'A', state: 'none', tracks_done: 0, tracks_wanted: 9, withheld_count: 0, paused: false, resolved_at: '2026-11-02T00:00:00Z', catalog_state: 'ok', ...fields });

test('a record reads its run first, then paused, then its state, with its flags', () => {
  assert.equal(recordView(rec({ active_job_id: 'j' })).view.label, 'In the queue');
  assert.equal(recordView(rec({ active_job_id: 'j' }), 'queued').view.label, 'In the queue');
  assert.equal(recordView(rec({ active_job_id: 'j' }), 'running').view.label, 'Running', 'as its row in Activity says');
  assert.equal(recordView(rec({ active_job_id: 'j', state: 'complete' }), 'completed').view.label, 'Downloaded', 'a run that ended, before the record is read again');
  assert.equal(recordView(rec({ paused: true })).view.label, 'Paused');
  assert.equal(recordView(rec({ state: 'complete', tracks_done: 9 })).view.label, 'Downloaded');
  assert.equal(recordView(rec({ state: 'partial', tracks_done: 3 })).view.label, '3 of 9');
  assert.equal(recordView(rec({ resolved_at: undefined })).view.label, 'Not read yet');
  assert.equal(recordView(rec({ catalog_state: 'gone' })).view.label, 'Gone from Apple Music');
  assert.deepEqual(recordView(rec({ tracks_failed: 1, tracks_missing: 2 })).flags.map((f) => f.label), ['1 failed', '2 missing']);
  assert.equal(recordTitle({ id: '9' }), 'Not read yet · 9');
});

test('a track reads its main slot, and the storefront and wanted flag first', () => {
  const t = (files, extra = {}) => ({ song_id: '7', name: 'S', wanted: true, availability: 'available', files, ...extra });
  const done = trackState(t([{ slot: 'main', audio_state: 'done', codec: 'alac', quality_label: '24-bit/96 kHz' }, { slot: 'ec3', audio_state: 'pending' }]));
  assert.equal(done.view.label, 'Downloaded');
  assert.equal(done.extra.length, 1);
  assert.equal(slotQuality(done.main), 'ALAC 24-bit/96 kHz');
  assert.equal(slotQuality(done.extra[0]), 'Dolby Atmos', 'a slot not downloaded yet names its codec');
  assert.equal(trackState(t([], { availability: 'unavailable' })).view.label, 'Not in your region');
  assert.equal(trackState(t([], { wanted: false })).view.label, 'Not added');
  const failed = trackState(t([{ slot: 'main', audio_state: 'given_up', last_error: 'boom' }]));
  assert.deepEqual([failed.view.label, failed.note], ['Given up', 'boom']);
  assert.deepEqual(songsIn({ tracks: [t([{ slot: 'main', audio_state: 'missing' }])] }, ['missing']), ['7']);
});

test('a track with another quality says each quality\'s own state', () => {
  const t = (files) => ({ song_id: '7', name: 'S', wanted: true, availability: 'available', files });
  // Add quality queued AAC: the ALAC file is done, the AAC one is not.
  const queued = t([{ slot: 'main', audio_state: 'done', codec: 'alac', quality_label: '16-bit/44 kHz' }, { slot: 'aac', audio_state: 'pending' }]);
  assert.deepEqual(
    slotStates(queued).map((s) => [s.codec, s.view.label]),
    [['ALAC', 'Downloaded'], ['AAC', 'Waiting']],
  );
  assert.equal(slotStates(queued)[0].label, 'ALAC 16-bit/44 kHz: Downloaded');
  const done = t([{ slot: 'main', audio_state: 'done', codec: 'alac' }, { slot: 'aac', audio_state: 'done', codec: 'aac', quality_label: '262Kbps', last_job_id: 'job_b' }]);
  assert.equal(slotStates(done)[1].label, 'AAC 262 kbps: Downloaded');
  const record = { tracks: [queued, done, t([{ slot: 'main', audio_state: 'done', last_job_id: 'job_a' }])] };
  assert.equal(hasExtraSlots(record), true);
  assert.equal(hasExtraSlots({ tracks: [t([{ slot: 'main', audio_state: 'done' }])] }), false);
  assert.deepEqual(extraCodecs(record), ['AAC']);
  assert.equal(runSlot(record, 'job_b'), 'AAC', 'the run that wrote the AAC files');
  assert.equal(runSlot(record, 'job_a'), '');
  assert.equal(runSlot(record, 'job_x'), '');
});

test('a record has something to pause only while a run could still do something', () => {
  const t = (state, extra = {}) => ({ wanted: true, availability: 'available', files: [{ slot: 'main', audio_state: state }], ...extra });
  assert.equal(recordHasWork(rec({ state: 'complete', tracks: [t('done'), t('done')] })), false);
  assert.equal(recordHasWork(rec({ tracks: [t('unavailable'), t('pending', { availability: 'unavailable' })] })), false, 'nothing offered');
  assert.equal(recordHasWork(rec({ tracks: [t('done'), t('pending')] })), true);
  assert.equal(recordHasWork(rec({ tracks: [t('failed')] })), true, 'a failed slot is retried');
  assert.equal(recordHasWork(rec({ active_job_id: 'j', tracks: [t('done')] })), true);
  assert.equal(recordHasWork(rec({ resolved_at: undefined, tracks: [] })), true);
});

test('counts, checks and queries read as the page shows them', () => {
  assert.equal(trackCountText(rec({ withheld_count: 1 })), '9 tracks · 1 not in your region');
  assert.equal(checkText({ checked: 3, missing: 0 }), 'Checking… 3 files');
  assert.equal(checkRunning({ checked: 3 }), true);
  assert.equal(checkText({ checked: 1204, missing: 2, finished_at: 'x' }), 'Checked 1,204 files · 2 missing');
  assert.match(checkText({ checked: 10, missing: 9, finished_at: 'x', aborted_reason: 'too_many_missing' }), /nothing was marked/);
  assert.equal(checkText(null), '');
  const c = { id: 2, checked: 5, missing: 0 };
  assert.equal(applyCheckFeed(null, [{ type: 'check_updated', check: c }]), c);
  assert.equal(applyCheckFeed(c, [{ type: 'download_upserted', job: {} }]), c);
  assert.deepEqual(recordQuery({ state: 'partial', flag: '', q: 'x' }), { limit: 50, offset: 0, sort: 'added', state: 'partial', q: 'x' });
  assert.equal(pendingTitle({ kind: 'song', catalog_id: '1' }), 'Song 1');
});

test('the usual quality is the one most tracks name', () => {
  const t = (q) => ({ files: [{ slot: 'main', audio_state: 'done', codec: 'alac', quality_label: q }] });
  assert.equal(commonQuality({ tracks: [t('16-bit/44.1 kHz'), t('16-bit/44.1 kHz'), t('24-bit/96 kHz')] }), 'ALAC 16-bit/44.1 kHz');
  assert.equal(commonQuality({ tracks: [] }), '');
});

test('a check is asked again for the scope it named', () => {
  assert.equal(checkScope('albums', '7'), 'album:7');
  assert.equal(checkScope('playlists', 'pl.1'), 'playlist:pl.1');
  assert.deepEqual(checkBody('album:7', true), { kind: 'albums', id: '7', force: true });
  assert.deepEqual(checkBody('playlist:pl.1'), { kind: 'playlists', id: 'pl.1' });
  assert.deepEqual(checkBody('all', true), { force: true });
  assert.deepEqual(checkBody(undefined), {});
});

test('a reset says why nothing downloads yet', () => {
  assert.equal(resetNote({ slots: 2, job: { id: 'j' } }, rec(), 'Downloading'), 'Downloading');
  assert.equal(resetNote({ slots: 2 }, rec({ paused: true }), 'Downloading'), 'Reset “A”. Resume it to download');
  assert.equal(resetNote({ slots: 2 }, rec(), 'Downloading'), 'Reset “A”. It waits for room in the queue');
  assert.equal(resetNote({ slots: 0 }, rec(), 'Downloading'), 'Nothing to download');
});

test('a bulk reset counts what it queued and what it left', () => {
  const o = (item, value) => ({ item, ok: true, value });
  const out = [o(rec({ id: '1' }), { slots: 3, job: { id: 'j' } }), o(rec({ id: '2', paused: true }), { slots: 3 }), o(rec({ id: '3' }), { slots: 0 })];
  assert.equal(bulkResetText(out, 'albums'), 'Queued 1 album · 1 paused: resume to download · 1 with nothing to download');
  assert.equal(bulkResetText([o(rec(), { slots: 1, job: { id: 'j' } }), o(rec(), { slots: 1, job: { id: 'k' } })], 'playlists'), 'Queued 2 playlists');
});
