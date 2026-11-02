import assert from 'node:assert/strict';
import { test } from 'node:test';

import { recordView, trackState, trackCountText, checkText, checkRunning, applyCheckFeed, songsIn, recordQuery, slotQuality, pendingTitle, recordTitle, commonQuality } from '../static/js/lib/records.js';

const rec = (fields = {}) => ({ id: '1', name: 'A', state: 'none', tracks_done: 0, tracks_wanted: 9, withheld_count: 0, paused: false, resolved_at: '2026-11-02T00:00:00Z', catalog_state: 'ok', ...fields });

test('a record reads its run first, then paused, then its state, with its flags', () => {
  assert.equal(recordView(rec({ active_job_id: 'j' })).view.label, 'In the queue');
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
