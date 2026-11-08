import test from 'node:test';
import assert from 'node:assert/strict';
import {
  tabStreams,
  streamQuery,
  recordOnTab,
  stationOnTab,
  appendItems,
  patchStream,
  dropFromStream,
  mergeStreams,
  liveRow,
  liveMatches,
  withoutLive,
  tabCounts,
  recordMenu,
  rowPlan,
  rowsSummary,
  recordRow,
  stationRow,
  recordLink,
} from '../static/js/lib/queue-rows.js';

const at = (m) => `2026-11-01T10:${String(m).padStart(2, '0')}:00Z`;
const rec = (id, m, extra = {}) => ({ id, name: id, activity_at: at(m), updated_at: at(m), state: 'complete', tracks_done: 3, url: `https://x/${id}`, ...extra });
const st = (id, m, status = 'completed') => ({ id, type: 'station', status, updated_at: at(m), input: `https://x/${id}` });
const ids = (rows) => rows.map((r) => r.id);

test('tabs read their streams with the tab state, by activity', () => {
  assert.deepEqual(tabStreams({ tab: 'all', type: '' }), ['albums', 'playlists', 'stations']);
  assert.deepEqual(tabStreams({ tab: 'done', type: 'playlist' }), ['playlists']);
  assert.deepEqual(tabStreams({ tab: 'active', type: '' }), [], 'Active is the live part alone');
  assert.deepEqual(streamQuery('albums', { tab: 'all' }), { limit: 100, offset: 0, sort: 'activity' });
  assert.deepEqual(streamQuery('playlists', { tab: 'done', q: 'x' }, 100), { limit: 100, offset: 100, sort: 'activity', state: 'complete', q: 'x' });
  assert.deepEqual(streamQuery('albums', { tab: 'failed' }).flag, 'failed,given_up,missing', 'one comma-separated query');
  assert.deepEqual(streamQuery('stations', { tab: 'failed' }), { limit: 100, offset: 0, resource_kind: 'none', sort: 'updated_at', order: 'desc', status: 'failed' });
  assert.equal(streamQuery('stations', { tab: 'all', type: 'station' }).type, 'station', 'the Stations filter keeps the stations of the runs of no record');
  assert.equal(streamQuery('stations', { tab: 'all' }).status, undefined);
});

test('what belongs on a tab', () => {
  assert.ok(recordOnTab(rec('a', 1), 'done'));
  assert.ok(!recordOnTab(rec('a', 1, { state: 'partial' }), 'done'));
  assert.ok(recordOnTab(rec('a', 1, { tracks_missing: 1 }), 'failed'));
  assert.ok(!recordOnTab(rec('a', 1), 'failed'));
  const away = rec('a', 1, { tracks_done: 0, state: 'none', catalog_state: 'not_in_storefront' });
  assert.ok(recordOnTab(away, 'failed'), 'a record Apple does not list is failed, as the backend flag reads');
  assert.ok(recordOnTab(rec('a', 1, { catalog_state: 'gone' }), 'failed'));
  assert.ok(!recordOnTab(rec('a', 1, { catalog_state: 'ok' }), 'failed'));
  assert.ok(menu(away).includes('retry'), 'its Retry reads it again');
  assert.deepEqual(rowPlan([{ kind: 'albums', record: away }]).retry.length, 1);
  assert.ok(stationOnTab(st('s', 1, 'failed'), 'failed'));
  assert.ok(!stationOnTab({ ...st('s', 1), resource: { kind: 'album', id: '1' } }, 'all'), 'a record run is not a station row');
});

test('merge: newest activity first, cut where another stream may still hold newer rows', () => {
  const albums = { items: [rec('a1', 50), rec('a2', 40), rec('a3', 30)], total: 10 };
  const playlists = { items: [rec('p1', 45), rec('p2', 10)], total: 2 };
  const stations = { items: [st('s1', 35)], total: 5 };
  const m = mergeStreams({ albums, playlists, stations });
  // Albums read down to :30, stations to :35: below :35 the stations may hold more.
  assert.deepEqual(ids(m.rows), ['albums:a1', 'playlists:p1', 'albums:a2', 'station:s1']);
  assert.equal(m.next, 'stations', 'the stream whose last row holds the cut reads on');
  assert.equal(m.hidden, 2);
  const done = mergeStreams({ albums: { ...albums, total: 3 }, playlists, stations: { ...stations, total: 1 } });
  assert.deepEqual(ids(done.rows), ['albums:a1', 'playlists:p1', 'albums:a2', 'station:s1', 'albums:a3', 'playlists:p2']);
  assert.equal(done.next, null);
  const unread = mergeStreams({ albums: { items: [], total: 4 }, playlists });
  assert.deepEqual(unread.rows, [], 'a stream not read yet holds everything back');
});

test('pages append without repeats; the feed moves a record to its new activity', () => {
  assert.deepEqual(ids(appendItems([rec('a', 3), rec('b', 2)], [rec('b', 2), rec('c', 1)])), ['a', 'b', 'c']);
  const s = { items: [rec('a', 30), rec('b', 20)], total: 5 };
  const moved = patchStream('albums', s, rec('b', 40), true);
  assert.deepEqual(ids(moved.items), ['b', 'a']);
  assert.equal(moved.total, 5);
  const added = patchStream('albums', s, rec('n', 50), true);
  assert.deepEqual(ids(added.items), ['n', 'a', 'b']);
  assert.equal(added.total, 6);
  const gone = patchStream('albums', s, rec('a', 31, { state: 'partial' }), false);
  assert.deepEqual(ids(gone.items), ['b']);
  assert.equal(gone.total, 4);
  assert.equal(patchStream('albums', s, rec('a', 10), true), s, 'an older snapshot is ignored');
  assert.equal(patchStream('albums', s, rec('z', 10), false), s);
  assert.deepEqual(ids(dropFromStream(s, 'a').items), ['b']);
});

test('a live run is its record row once, in the live part', () => {
  const job = { id: 'j1', status: 'running', title: 'IGOR', input: 'https://x', resource: { kind: 'album', id: 'a2' } };
  const rows = [recordRow('albums', rec('a1', 5)), recordRow('albums', rec('a2', 4)), stationRow(st('s1', 3))];
  assert.deepEqual(ids(withoutLive(rows, [job])), ['albums:a1', 'station:s1']);
  const known = new Map([['albums:a2', rec('a2', 4)]]);
  const row = liveRow(job, known);
  assert.equal(row.id, 'albums:a2');
  assert.equal(row.record.url, 'https://x/a2', 'a record already read lends its fields');
  assert.equal(liveRow(job).record.active_job_id, 'j1');
  assert.equal(liveRow({ id: 's9', status: 'queued', type: 'station' }).id, 'station:s9');
  assert.ok(liveMatches(job, { type: 'album', q: 'igor' }));
  assert.ok(!liveMatches(job, { type: 'station', q: '' }));
});

test('tab counts wait for their parts; Failed is the union total plus failed stations and adds', () => {
  const summary = { albums: { total: 10, states: { complete: 6 } }, playlists: { total: 2, states: { complete: 1 } } };
  const stations = { total: 4, completed: 2, failed: 1 };
  assert.deepEqual(tabCounts({ summary, failedRecords: { albums: 3, playlists: 0 }, stations, pendingFailed: 2, feed: { queued: 2, running: 1 } }), { all: 16, active: 3, done: 9, failed: 6 });
  assert.deepEqual(tabCounts({ summary, stations: null, feed: null }), { all: undefined, active: undefined, done: undefined, failed: undefined });
});

const menu = (r, run, place) => recordMenu(r, run, place).map((it) => (it.separator ? '-' : it.id));

test('a record menu offers what its state allows', () => {
  assert.deepEqual(menu(rec('a', 1)), ['details', 'again', '-', 'copy', '-', 'remove']);
  assert.deepEqual(menu(rec('a', 1, { state: 'partial', tracks_failed: 2 })), ['details', 'pause', 'retry', 'again', '-', 'copy', '-', 'remove']);
  assert.deepEqual(menu(rec('a', 1, { state: 'partial', paused: true })), ['details', 'resume', 'again', '-', 'copy', '-', 'remove']);
  assert.deepEqual(menu(rec('a', 1, { paused: true })), ['details', 'again', '-', 'copy', '-', 'remove'], 'a paused complete record reads Downloaded, without Resume');
  const run = { id: 'j', status: 'queued' };
  assert.deepEqual(menu(rec('a', 1, { active_job_id: 'j' }), run, { first: true }), ['details', 'bottom', 'pause', '-', 'copy'], 'no reset or removal while a run is under way');
  assert.deepEqual(menu({ ...rec('a', 1), url: undefined }), ['details', 'again', '-', 'remove'], 'no link, no Copy link');
  assert.equal(recordLink('albums', { id: '1', pinned_storefront: 'jp' }), 'https://music.apple.com/jp/album/1');
  assert.equal(recordLink('playlists', { id: 'pl.x', storefront_read: 'us' }), 'https://music.apple.com/us/playlist/pl.x');
  assert.equal(recordLink('albums', { id: '1' }), '');
  const labels = recordMenu(rec('a', 1, { tracks_missing: 1 })).filter((it) => !it.separator).map((it) => it.label);
  assert.deepEqual(labels, ['Show details', 'Retry', 'Download again…', 'Copy link', 'Remove from amdl…']);
});

test('a selection plans each action for the rows it applies to', () => {
  const rows = [
    recordRow('albums', rec('done', 5)),
    recordRow('albums', rec('bad', 4, { state: 'partial', tracks_failed: 1 })),
    { ...recordRow('playlists', rec('live', 3, { state: 'partial', active_job_id: 'j' })), live: { id: 'j', status: 'running' } },
    stationRow(st('s1', 2, 'failed')),
    stationRow(st('s2', 1, 'queued')),
  ];
  const plan = rowPlan(rows);
  assert.deepEqual(ids(plan.retry), ['albums:bad', 'station:s1']);
  assert.deepEqual(ids(plan.again), ['albums:done', 'albums:bad']);
  assert.deepEqual(ids(plan.pause), ['albums:bad', 'playlists:live', 'station:s2']);
  assert.deepEqual(ids(plan.remove), ['albums:done', 'albums:bad', 'station:s1']);
});

test('a bulk run says what it did, and names failures by reason', () => {
  const ok = (item) => ({ item, ok: true });
  const bad = (item, status) => ({ item, ok: false, error: { status } });
  const title = (r) => r.id;
  const reason = (e) => (e.status === 409 ? 'running' : 'gone');
  assert.deepEqual(rowsSummary('remove', [ok({ id: 'a' }), bad({ id: 'b' }, 404)], title, reason), { message: 'Removed 2', role: 'success' });
  assert.deepEqual(rowsSummary('retry', [ok({ id: 'a' }), bad({ id: 'b' }, 409)], title, reason), { message: 'Queued 1 of 2 again. 1 failed: b (running).', role: 'danger' });
  assert.deepEqual(rowsSummary('pause', [bad({ id: 'b' }, 409)], title, reason), { message: "Couldn't pause 1: b (running).", role: 'danger' });
});
