import test from 'node:test';
import assert from 'node:assert/strict';
import { createLookupQueue, previewFacts, qualitySpec, qualityOverview, kindLabel, shortError, LOOKUP_CACHE_MAX } from '../static/js/lib/lookup.js';

const tick = () => new Promise((r) => setTimeout(r, 0));

/** A fake lookup whose answers the test releases one by one. */
function fakeLookup() {
  const calls = [];
  const lookup = (url, signal) =>
    new Promise((resolve, reject) => {
      const call = { url, signal, resolve, reject };
      signal.addEventListener('abort', () => reject(Object.assign(new Error('cancelled'), { code: 'aborted' })));
      calls.push(call);
    });
  return { calls, lookup };
}

test('at most max lookups run, in the wanted order', async () => {
  const f = fakeLookup();
  const q = createLookupQueue({ lookup: f.lookup, max: 2 });
  q.want(['a', 'b', 'c']);
  await tick();
  assert.deepEqual(f.calls.map((c) => c.url), ['a', 'b']);
  assert.equal(q.status('a').state, 'loading');
  assert.equal(q.status('c').state, 'queued');
  f.calls[0].resolve({ title: 'A' });
  await tick();
  await tick();
  assert.deepEqual(q.status('a'), { state: 'ready', data: { title: 'A' } });
  assert.deepEqual(f.calls.map((c) => c.url), ['a', 'b', 'c'], 'a free slot starts the next link');
});

test('a link no longer wanted is aborted, forgotten and frees its slot', async () => {
  const f = fakeLookup();
  const q = createLookupQueue({ lookup: f.lookup, max: 1 });
  q.want(['a', 'b']);
  await tick();
  q.want(['b']);
  await tick();
  assert.equal(f.calls[0].signal.aborted, true);
  assert.equal(q.status('a').state, 'idle', 'an aborted lookup is not cached');
  assert.deepEqual(f.calls.map((c) => c.url), ['a', 'b']);
  await tick();
  assert.equal(q.status('b').state, 'loading');
});

test('answers and errors are cached; retry forgets one', async () => {
  const f = fakeLookup();
  const cache = new Map();
  const q = createLookupQueue({ lookup: f.lookup, cache });
  q.want(['a']);
  await tick();
  f.calls[0].reject(Object.assign(new Error('Apple said 404'), { status: 502 }));
  await tick();
  await tick();
  assert.equal(q.status('a').state, 'error');
  q.want(['a']);
  await tick();
  assert.equal(f.calls.length, 1, 'the cached error is not fetched again');
  // A second queue sharing the cache (the page mounted again) reuses it too.
  const q2 = createLookupQueue({ lookup: f.lookup, cache });
  q2.want(['a']);
  await tick();
  assert.equal(f.calls.length, 1);
  q.retry('a');
  await tick();
  assert.equal(f.calls.length, 2);
});

test('the cache keeps the newest LOOKUP_CACHE_MAX answers but never evicts a wanted one', async () => {
  const cache = new Map();
  let calls = 0;
  const q = createLookupQueue({ lookup: async (url) => (calls++, { title: url }), max: 4, cache });
  for (let i = 0; i < LOOKUP_CACHE_MAX + 5; i++) {
    q.want(['u0', `u${i}`]);
    await tick();
    await tick();
  }
  assert.equal(cache.size, LOOKUP_CACHE_MAX);
  assert.equal(cache.has('u0'), true, 'still wanted, so kept');
  assert.equal(cache.has('u1'), false, 'the oldest unwanted answer went first');
  assert.equal(calls, LOOKUP_CACHE_MAX + 5, 'nothing was fetched twice');
});

test('forget aborts in-flight lookups and drops cached answers, so a new token reads them again', async () => {
  const f = fakeLookup();
  const cache = new Map();
  const q = createLookupQueue({ lookup: f.lookup, max: 4, cache });
  q.want(['album', 'mine', 'theirs']);
  await tick();
  f.calls[0].resolve({ title: 'Album' });
  f.calls[1].resolve({ title: 'Mine, read with the old token' });
  await tick();
  await tick();
  assert.equal(q.status('mine').state, 'ready');
  assert.equal(q.status('theirs').state, 'loading');
  q.forget(['mine', 'theirs']);
  await tick();
  assert.equal(f.calls[2].signal.aborted, true, 'the lookup with the old token is cancelled');
  assert.deepEqual(f.calls.slice(3).map((c) => c.url), ['mine', 'theirs'], 'both are looked up again');
  assert.equal(q.status('mine').state, 'loading', 'the old answer is gone');
  assert.deepEqual(q.status('album'), { state: 'ready', data: { title: 'Album' } }, 'other answers stay');
  // The aborted call settling late does not land in the cache.
  f.calls[2].resolve({ title: 'late' });
  f.calls[3].resolve({ title: 'Mine' });
  await tick();
  await tick();
  assert.deepEqual(q.status('mine'), { state: 'ready', data: { title: 'Mine' } });
  assert.equal(q.status('theirs').state, 'loading');
  q.forget(['not-wanted']);
  await tick();
  assert.equal(f.calls.length, 5, 'forgetting an unknown link fetches nothing');
});

test('dispose aborts everything and stops notifying', async () => {
  const f = fakeLookup();
  let changes = 0;
  const q = createLookupQueue({ lookup: f.lookup, onChange: () => changes++ });
  q.want(['a', 'b']);
  await tick();
  const before = changes;
  q.dispose();
  await tick();
  await tick();
  assert.ok(f.calls.every((c) => c.signal.aborted));
  assert.equal(changes, before);
});

test('previewFacts reads a LookupResponse', () => {
  const album = previewFacts({
    type: 'album',
    title: 'IGOR',
    artist_name: 'Tyler, The Creator',
    release_date: '2019-05-17',
    genre: 'Hip-Hop/Rap',
    track_count: 12,
    content_rating: 'explicit',
    apple_digital_master: true,
  });
  assert.equal(album.title, 'IGOR');
  assert.equal(album.sub, 'Tyler, The Creator');
  assert.deepEqual(album.facts.map((f) => f.text), ['Album', '12 tracks', '2019', 'Hip-Hop/Rap']);
  assert.equal(album.explicit, true);
  assert.equal(album.adm, true);
  const song = previewFacts({ type: 'song', title: 'Anti-Hero', artist_name: 'Taylor Swift', album_name: 'Midnights (3am Edition)', track_count: 1, duration_ms: 200690 });
  assert.deepEqual(song.facts.map((f) => f.key), ['type', 'length']);
  assert.equal(song.facts[1].text, '3:21');
  assert.equal(song.album, 'Midnights (3am Edition)');
  const playlist = previewFacts({ type: 'playlist', title: "Today's Hits", curator_name: 'Apple Music Hits', track_count: 50 });
  assert.equal(playlist.sub, 'Apple Music Hits');
  assert.equal(previewFacts(null), null);
});

test('kindLabel names parsed kinds', () => {
  assert.equal(kindLabel('album'), 'Album');
  assert.equal(kindLabel('album', '1650841515'), 'Album track');
  assert.equal(kindLabel('music-video'), 'Music video');
});

const IGOR_TRACK = (alac, aac) => ({
  song: { id: '1', name: 'EARFQUAKE', artist: 'Tyler, The Creator', album: 'IGOR', has_lyrics: true },
  qualities: [
    { id: 'aac', label: 'AAC', available: Boolean(aac), codec: 'AAC', channels: 2, bitrate: aac || undefined },
    { id: 'alac', label: 'Lossless', available: Boolean(alac), codec: 'ALAC', channels: 2, ...(alac || {}) },
    { id: 'ec3', label: 'Dolby Atmos', available: false, codec: 'E-AC-3' },
  ],
});

test('qualitySpec formats what the response gives', () => {
  const sp = (text) => text.replace(/\u00a0/g, ' ');
  assert.equal(sp(qualitySpec({ bit_depth: 24, sample_rate: 44100, bitrate: 1580218, channels: 2 })), '24-bit / 44.1 kHz · 1,580 kbps · 2 ch');
  assert.equal(sp(qualitySpec({ bitrate: 263673, channels: 2 })), '264 kbps · 2 ch');
  assert.equal(qualitySpec({ bitrate: 1633000 }), '1,633\u00a0kbps', 'a number keeps its unit on its line');
  assert.equal(qualitySpec({ codec: 'E-AC-3' }), '');
});

test('qualityOverview counts tracks per slot and keeps the best format', () => {
  const resp = {
    tracks: [IGOR_TRACK({ bit_depth: 24, sample_rate: 44100 }, 263000), IGOR_TRACK({ bit_depth: 24, sample_rate: 96000 }, 256000), IGOR_TRACK(null, 256000)],
  };
  const o = qualityOverview(resp);
  assert.deepEqual(o.map((s) => [s.id, s.tracks, s.total]), [['aac', 3, 3], ['alac', 2, 3], ['ec3', 0, 3]]);
  assert.equal(o.find((s) => s.id === 'alac').best.replace(/\u00a0/g, ' '), '24-bit / 96 kHz · 2 ch');
  assert.equal(o.find((s) => s.id === 'ec3').best, '');
  assert.deepEqual(qualityOverview({}), []);
});

test('shortError keeps lookup failures to one line', () => {
  const apple = 'read Apple Music catalog: catalog request failed: 404 Not Found: {"errors":[{"status":"404"}]}';
  assert.equal(shortError(apple), 'Apple Music has nothing at this link (404)');
  assert.equal(shortError('wrapper: decrypt failed: {"code":5}'), 'wrapper: decrypt failed');
  assert.equal(shortError('x'.repeat(200), 10), `${'x'.repeat(9)}…`);
  assert.equal(shortError(undefined), '');
});
