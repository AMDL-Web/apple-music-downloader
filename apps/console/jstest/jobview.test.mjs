import test from 'node:test';
import assert from 'node:assert/strict';
import { isNotFound, activeFirst, rowView, albumCodec, codecKinds, codecNote, drawerSummary, trackView, stagePercent, trackNote, activityRows } from '../static/js/lib/jobview.js';

const done = { id: 'a', status: 'completed', type: 'album', storefront: 'us', title: 'folklore', artist_name: 'Taylor Swift', total_items: 16, completed_items: 16 };

test('isNotFound reads the status before any JSON body', () => {
  assert.equal(isNotFound('catalog request failed: 404 Not Found: {"errors":[{"id":"X404Y"}]}'), true);
  assert.equal(isNotFound('catalog request failed: 500 Server Error: {"detail":"404"}'), false);
  assert.equal(isNotFound(''), false);
});

test('activeFirst lifts running, then queued, keeping each group in order', () => {
  const jobs = [{ id: 'a', status: 'completed' }, { id: 'b', status: 'queued' }, { id: 'c', status: 'running' }, { id: 'd', status: 'failed' }, { id: 'e', status: 'running' }, { id: 'f', status: 'queued' }];
  assert.deepEqual(activeFirst(jobs).map((j) => j.id), ['c', 'e', 'b', 'f', 'a', 'd']);
  const same = [{ status: 'completed' }, { status: 'failed' }];
  assert.equal(activeFirst(same), same, 'nothing to lift keeps the list');
});

test('a completed album row has no progress, an icon-only status and the track count', () => {
  const v = rowView(done, 'us');
  assert.equal(v.progress, false);
  assert.equal(v.iconOnly, true);
  assert.equal(v.sub, 'Taylor Swift · 16 tracks');
});

test('the type shows unless it is an album, the storefront unless it is the default', () => {
  assert.equal(rowView({ ...done, type: 'playlist', artist_name: '', curator_name: 'Apple Music Hits' }, 'us').sub, 'Apple Music Hits · Playlist · 16 tracks');
  assert.equal(rowView({ ...done, storefront: 'jp' }, 'us').sub, 'Taylor Swift · JP · 16 tracks');
  assert.equal(rowView({ ...done, storefront: 'jp' }, '').sub, 'Taylor Swift · 16 tracks', 'default unknown: no storefront');
});

test('a running row keeps its progress and no track count', () => {
  const v = rowView({ ...done, status: 'running', completed_items: 3 }, 'us');
  assert.equal(v.progress, true);
  assert.equal(v.iconOnly, false);
  assert.equal(v.sub, 'Taylor Swift');
});

test('a failed link reads Link not found for a 404, with the link under it and the reason', () => {
  const job = { id: 'x', status: 'failed', type: 'album', storefront: 'us', input: 'https://music.apple.com/us/album/not-a-real-album/1', error: 'catalog request failed: 404 Not Found: {"errors":[]}' };
  const v = rowView(job, 'us');
  assert.equal(v.title, 'Link not found');
  assert.equal(v.sub, 'music.apple.com/us/album/not-a-real-album/1');
  assert.equal(v.reason, 'Apple Music has nothing at this link (404)');
  assert.equal(v.progress, false);
  assert.equal(v.mono, false);
  assert.equal(rowView({ ...job, error: 'network down' }, 'us').title, "Couldn't resolve this link");
});

test('a queued job before Apple names it shows its link in mono', () => {
  const v = rowView({ id: 'q', status: 'queued', type: 'album', input: 'https://music.apple.com/us/album/x/1' }, 'us');
  assert.equal(v.mono, true);
  assert.equal(v.progress, true);
  assert.equal(v.sub, 'Not resolved yet');
});

test('a completed job with failed tracks keeps a word', () => {
  const v = rowView({ ...done, completed_items: 14, failed_items: 2 }, 'us');
  assert.equal(v.iconOnly, false);
  assert.equal(v.sub, 'Taylor Swift · 14 of 16 tracks, 2 failed');
});

const t = (o) => ({ status: 'completed', artist: 'Taylor Swift', codec: 'alac', bit_depth: 24, sample_rate: 44100, file_size: 40e6, ...o });

test('drawerSummary names the format, the tracks and the size', () => {
  assert.equal(drawerSummary([t(), t()]), 'ALAC 24/44.1 · 2 tracks · 80.0 MB');
  assert.equal(drawerSummary([t(), t({ codec: 'aac', bit_depth: 0, sample_rate: 0 })]), 'Mixed formats · 2 tracks · 80.0 MB');
  assert.equal(drawerSummary([t({ file_size: 0, codec: '' })]), '1 track');
  assert.equal(drawerSummary([]), '');
});

test('codecNote spells out the codec most tracks carry, once', () => {
  const items = [t({ sample_rate: 48000 }), t({ sample_rate: 48000 }), t(), t({ sample_rate: 96000 })];
  assert.deepEqual(codecNote(items), { label: 'ALAC 24/48', text: 'Lossless 24-bit / 48\u00a0kHz' });
  assert.equal(codecNote([t({ codec: '' })]), null);
  assert.equal(codecNote([]), null);
});

test('trackView shows an artist, codec or status only when it adds something', () => {
  const items = [t(), t(), t({ codec: 'aac', bit_depth: 0, sample_rate: 0, artist: 'Bon Iver' })];
  const mode = albumCodec(items);
  assert.equal(mode, 'ALAC 24/44.1');
  assert.deepEqual(trackView(items[0], 'Taylor Swift', mode), { artist: '', codec: '', status: false });
  assert.deepEqual(trackView(items[2], 'Taylor Swift', mode), { artist: 'Bon Iver', codec: 'AAC', status: false });
  assert.equal(trackView(t({ status: 'skipped_existing' }), 'Taylor Swift', mode).status, true);
  assert.equal(trackView(t({ status: 'downloading' }), 'Taylor Swift', mode).status, true);
  assert.equal(trackView(items[0], 'Taylor Swift', mode, true).status, true, 'a job seen live keeps every row its stage line');
});

test('stagePercent reads the fraction of the measurable stage a track is in', () => {
  assert.equal(stagePercent({ status: 'downloading', progress: { download: 0.454 } }), '45%');
  assert.equal(stagePercent({ status: 'decrypting', progress: { download: 1, decrypt: 0.35 } }), '35%');
  assert.equal(stagePercent({ status: 'downloading', progress: { download: 0 } }), '', 'unknown size');
  assert.equal(stagePercent({ status: 'remuxing', progress: { download: 1, decrypt: 1 } }), '');
  assert.equal(stagePercent({ status: 'completed', progress: { download: 1, decrypt: 1 } }), '');
  assert.equal(stagePercent({ status: 'queued' }), '');
});

test('trackNote drops messages that restate the stage, keeps the ones that explain', () => {
  assert.equal(trackNote({ status: 'decrypting', status_message: 'decrypting 35%' }, 'Decrypting'), '');
  assert.equal(trackNote({ status: 'downloading', status_message: 'downloading encrypted AAC-LC media' }, 'Downloading'), '');
  assert.equal(trackNote({ status: 'downloading', status_message: 'ALAC download 53%' }, 'Downloading'), '');
  assert.equal(trackNote({ status: 'decrypting', status_message: 'opening decrypt session' }, 'Decrypting'), 'opening decrypt session');
  const retry = 'download attempt 2/3 failed; retrying in 2s: connection reset';
  assert.equal(trackNote({ status: 'downloading', status_message: retry }, 'Downloading'), retry);
  assert.equal(trackNote({ status: 'cancelled', status_message: 'Cancelled.' }, 'Cancelled'), '');
  assert.equal(trackNote({ status: 'cancelled', status_message: 'Cancelled by the user' }, 'Cancelled'), 'Cancelled by the user', 'only a working stage drops a longer message');
  assert.equal(trackNote({ status: 'completed', status_message: 'ALAC download completed' }, 'Completed'), '');
  assert.equal(trackNote({ status: 'skipped_existing', status_message: 'File already exists; skipped' }, 'Already on disk'), '');
});

test('activityRows folds Gave up into the Failed that says the same', () => {
  const e = (id, type, message) => ({ id, type, message });
  const err = 'catalog request failed: 404 Not Found: {"errors":[]}';
  const rows = activityRows([e(1, 'job_queued', 'job queued'), e(2, 'operation_exhausted', err), e(3, 'job_failed', err)]);
  assert.deepEqual(rows.map((r) => r.view.label), ['Queued', 'Failed']);
  const apart = activityRows([e(1, 'operation_exhausted', 'a'), e(2, 'job_failed', 'b')]);
  assert.deepEqual(apart.map((r) => r.view.label), ['Gave up', 'Failed'], 'different text stays');
});

test('a track named its codec but not yet its numbers is not a second kind', () => {
  // item_progress names the codec while the track is matched; codec_selected
  // adds bit depth and rate a moment later (recorded on a real album).
  const set = [
    { codec: 'alac', bit_depth: 24, sample_rate: 96000 },
    { codec: 'alac' },
    { codec: '' },
  ];
  assert.deepEqual(codecKinds(set), ['ALAC 24/96']);
  assert.equal(albumCodec(set), 'ALAC 24/96');
  assert.equal(drawerSummary(set), 'ALAC 24/96 · 3 tracks');
  // With no numbered track the bare name stands, and other codecs still count.
  assert.deepEqual(codecKinds([{ codec: 'alac' }]), ['ALAC']);
  assert.deepEqual(codecKinds([...set, { codec: 'aac-lc' }, { codec: 'alac', bit_depth: 24, sample_rate: 48000 }]), ['ALAC 24/96', 'AAC-LC', 'ALAC 24/48']);
});
