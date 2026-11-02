// The demo's records (GET /library/*, the `resource` marks of search,
// lookup and the album page, the Apple library): what the fake server
// keeps per album and playlist, derived from the jobs that downloaded for
// them. A track is done when a run of its record finished it. Shapes follow
// internal/api/openapi.yaml (AlbumResource, RecordDetail, ResourceMark,
// LibrarySummary, AppleLibraryPage). Pure: fake-api.js passes its jobs and
// items in.

import { albums, songs, artists, playlists, find, STOREFRONT } from './catalog.js';

const ACTIVE = new Set(['queued', 'running']);
const DONE_ITEM = new Set(['completed', 'skipped_existing']);
const STATES = ['none', 'partial', 'complete'];
const FLAGS = ['failed', 'given_up', 'missing', 'waiting', 'paused', 'unresolved'];
const iso = (ms) => new Date(ms).toISOString();

/** The record a catalog object downloads into: an album (a song's album) or a playlist; null for others. */
export function resourceOf(src) {
  if (!src) return null;
  if (src.type === 'album') return { kind: 'album', id: src.id };
  if (src.type === 'song' && src.album_id) return { kind: 'album', id: src.album_id };
  if (src.type === 'playlist') return { kind: 'playlist', id: src.id };
  return null;
}

const keyOf = (ref) => `${ref.kind}:${ref.id}`;
const artwork = (src) => ({
  url: src.artwork_url,
  colors: { bg: src.artwork_bg_color, text1: src.artwork_text_color1, text2: src.artwork_text_color2, text3: src.artwork_text_color3, text4: src.artwork_text_color4 },
});

export function createRecordStore({ jobs, items, now }) {
  const records = new Map();
  let check = null;
  let checks = 0;

  /** Keeps a record for `ref`; a song adds only itself to an album not wanted whole. */
  function ensure(ref, at, song = '') {
    if (!ref) return null;
    let r = records.get(keyOf(ref));
    if (!r) {
      r = { ...ref, added_at: iso(at), updated_at: iso(at), paused: false, wanted: song ? new Set([song]) : 'all' };
      records.set(keyOf(ref), r);
    } else if (!song) r.wanted = 'all';
    else if (r.wanted !== 'all') r.wanted.add(song);
    return r;
  }

  const runsOf = (r) => [...jobs.values()].filter((j) => j.resource && j.resource.kind === r.kind && j.resource.id === r.id);
  const activeRun = (r) => runsOf(r).find((j) => ACTIVE.has(j.status));
  const srcOf = (r) => find(r.id);
  const tracksOf = (r) => {
    const src = srcOf(r);
    const all = src ? src.tracks : [];
    return r.wanted === 'all' ? all : all.filter((t) => r.wanted.has(t.id));
  };
  /** Each wanted track's item state from the newest run that planned it. */
  function trackStates(r) {
    const out = new Map();
    const runs = runsOf(r).sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
    for (const j of runs) for (const it of items.get(j.id) || []) out.set(it.adam_id, { it, job: j });
    return out;
  }

  function rollup(r) {
    const tracks = tracksOf(r);
    const st = trackStates(r);
    let done = 0;
    let failed = 0;
    for (const t of tracks) {
      const s = st.get(t.id);
      if (s && DONE_ITEM.has(s.it.status)) done++;
      else if (s && s.it.status === 'failed') failed++;
    }
    const wanted = tracks.length;
    return { tracks_wanted: wanted, tracks_done: done, tracks_failed: failed, tracks_given_up: 0, tracks_missing: 0, state: wanted && done >= wanted ? 'complete' : done ? 'partial' : 'none' };
  }

  /** A record as GET /library/{kind} lists it. */
  function record(r) {
    const src = srcOf(r) || {};
    const run = activeRun(r);
    const base = {
      id: r.id,
      name: src.name || '',
      artwork: artwork(src),
      resolved_at: r.added_at,
      storefront_read: STOREFRONT,
      catalog_state: 'ok',
      paused: r.paused,
      priority: 1,
      cover_state: 'done',
      added_at: r.added_at,
      updated_at: r.updated_at,
      ...rollup(r),
      ...(run ? { active_job_id: run.id } : {}),
    };
    if (r.kind === 'playlist') return { ...base, curator_name: src.curator_name };
    return { ...base, artist_name: src.artist_name || '', release_date: src.release_date, genre: src.genre, release_track_count: src.track_count || 0, withheld_count: 0, wanted_all: r.wanted === 'all', artist_cover_state: 'done' };
  }

  /** One record with its tracks and their main slots (RecordDetail). */
  function detail(r) {
    const st = trackStates(r);
    const src = srcOf(r) || { tracks: [] };
    const wanted = r.wanted === 'all' ? null : r.wanted;
    const tracks = src.tracks.map((t, i) => {
      const s = st.get(t.id);
      const it = s && s.it;
      const audio = !it ? 'pending' : DONE_ITEM.has(it.status) ? 'done' : it.status === 'failed' ? 'failed' : ACTIVE.has(s.job.status) && it.status !== 'queued' ? 'running' : 'pending';
      const file = { slot: 'main', audio_state: audio, failures: audio === 'failed' ? 1 : 0, ...(s ? { last_job_id: s.job.id } : {}) };
      if (it && it.codec) Object.assign(file, { codec: it.codec, bit_depth: it.bit_depth, sample_rate: it.sample_rate, quality_label: it.bit_depth ? `${it.bit_depth}-bit/${it.sample_rate / 1000} kHz` : '' });
      if (it && it.output_path) file.path = it.output_path;
      if (audio === 'failed' && it.error) file.last_error = it.error;
      const row = { song_id: t.id, name: t.name, artist_name: t.artist_name, duration_ms: t.duration_ms, has_lyrics: true, wanted: !wanted || wanted.has(t.id), availability: 'available', files: wanted && !wanted.has(t.id) ? [] : [file] };
      return r.kind === 'playlist' ? { ...row, position: i, album_name: t.album_name } : { ...row, disc_number: 1, track_number: t.track_number || i + 1, song_number: t.track_number || i + 1 };
    });
    return { ...record(r), sources: ['manual'], in_apple_library: true, tracks, ...(check && check.finished_at ? { last_checked_at: check.finished_at } : {}) };
  }

  /** The ResourceMark search, lookup and the album page carry; a song adds its own state. */
  function mark(ref, songId = '') {
    const r = ref && records.get(keyOf(ref));
    if (!r) return null;
    const full = record(r);
    const out = { kind: r.kind, id: r.id, state: full.state, tracks_done: full.tracks_done, tracks_wanted: full.tracks_wanted, withheld_count: 0, paused: r.paused };
    if (full.active_job_id) out.active_job_id = full.active_job_id;
    if (songId) {
      const s = trackStates(r).get(songId);
      if (s) out.song_state = DONE_ITEM.has(s.it.status) ? 'done' : s.it.status === 'failed' ? 'failed' : 'pending';
    }
    return out;
  }

  const get = (kind, id) => records.get(`${kind === 'playlists' ? 'playlist' : kind === 'albums' ? 'album' : kind}:${id}`) || null;
  const flagged = (r, flag) => {
    const x = record(r);
    if (flag === 'failed') return x.tracks_failed > 0;
    if (flag === 'paused') return r.paused;
    return false;
  };

  /** GET /library/{kind}: filters state, flag, q; newest added first. */
  function list(kind, q) {
    const k = kind === 'playlists' ? 'playlist' : 'album';
    const states = (q.get('state') || '').split(',').filter((s) => STATES.includes(s));
    const flags = (q.get('flag') || '').split(',').filter((s) => FLAGS.includes(s));
    const term = (q.get('q') || '').toLowerCase();
    const all = [...records.values()]
      .filter((r) => r.kind === k)
      .filter((r) => !states.length || states.includes(record(r).state))
      .filter((r) => !flags.length || flags.some((f) => flagged(r, f)))
      .filter((r) => {
        const x = record(r);
        return !term || [x.name, x.artist_name, x.curator_name].some((v) => v && v.toLowerCase().includes(term));
      })
      .sort((a, b) => Date.parse(b.added_at) - Date.parse(a.added_at));
    const limit = Math.min(500, Number(q.get('limit')) || 50);
    const offset = Number(q.get('offset')) || 0;
    return { items: all.slice(offset, offset + limit).map(record), total: all.length, limit, offset };
  }

  function counts(k) {
    const out = { total: 0, states: { none: 0, partial: 0, complete: 0 }, flags: Object.fromEntries(FLAGS.map((f) => [f, 0])) };
    for (const r of records.values()) {
      if (r.kind !== k) continue;
      const x = record(r);
      out.total++;
      out.states[x.state]++;
      for (const f of FLAGS) if (flagged(r, f)) out.flags[f]++;
    }
    return out;
  }

  const summary = () => ({ albums: counts('album'), playlists: counts('playlist'), pending: { pending: 0, failed: 0 }, check });

  /** A disk check: every done slot is "found" in the demo. */
  function startCheck(body, at = Date.now()) {
    const scope = body && body.kind && body.id ? `${body.kind}:${body.id}` : 'all';
    let n = 0;
    for (const r of records.values()) if (scope === 'all' || get(body.kind, body.id) === r) n += record(r).tracks_done;
    check = { id: ++checks, trigger: 'manual', scope, started_at: iso(at), finished_at: iso(at + 1200), checked: n, missing: 0 };
    return check;
  }

  // The Apple library: the catalog stands in for the account's.
  const apple = {
    albums: () => albums.map((a) => ({ id: `l.${a.id}`, catalog_id: a.id, url: a.url, name: a.name, artist_name: a.artist_name, artwork_url: a.artwork_url, genre: a.genre, release_date: a.release_date, track_count: a.track_count, date_added: a.release_date })),
    songs: () => songs.map((s) => ({ id: `i.${s.id}`, catalog_id: s.id, url: s.url, name: s.name, artist_name: s.artist_name, album_name: s.album_name, artwork_url: s.artwork_url, duration_ms: s.duration_ms })),
    playlists: () => [
      { id: 'p.favorites', name: 'Favourite Songs', playlist_kind: 'favorites', artwork_url: playlists[0].artwork_url },
      ...playlists.map((p) => ({ id: `p.${p.id}`, catalog_id: p.id, url: p.url, name: p.name, artwork_url: p.artwork_url, playlist_kind: 'editorial' })),
    ],
    artists: () => artists.map((a) => ({ id: `r.${a.id}`, catalog_id: a.id, url: a.url, name: a.name, artwork_url: a.artwork_url, genre: a.genre })),
  };
  const appleStatus = () => ({
    available: true,
    state: 'idle',
    last_sync: { id: 'sync_1', mode: 'full', trigger: 'schedule', started_at: iso(now - 40 * 60000), finished_at: iso(now - 39 * 60000), ok: true, requests: 18, added: 0, removed: 0, changed: 0 },
    last_full_sync_at: iso(now - 39 * 60000),
    counts: { albums: albums.length, songs: songs.length, playlists: playlists.length + 1, artists: artists.length },
    sync_interval_minutes: 30,
    full_sync_hours: 24,
  });
  function appleList(kind, q) {
    const make = apple[kind];
    if (!make) return null;
    const term = (q.get('q') || '').toLowerCase();
    const all = make().filter((x) => !term || [x.name, x.artist_name, x.album_name].some((v) => v && v.toLowerCase().includes(term)));
    const limit = Math.min(200, Number(q.get('limit')) || 50);
    const offset = Number(q.get('offset')) || 0;
    return { kind, items: all.slice(offset, offset + limit), total: all.length, offset, synced_at: iso(now - 39 * 60000) };
  }
  function appleTracks(id) {
    const src = id === 'p.favorites' ? { tracks: songs.slice(0, 12) } : find(String(id).replace(/^p\./, ''));
    const tracks = (src && src.tracks) || [];
    return { playlist_id: id, items: tracks.map((s, i) => ({ position: i, song_id: `i.${s.id}`, catalog_id: s.id, url: s.url, name: s.name, artist_name: s.artist_name, album_name: s.album_name, duration_ms: s.duration_ms })), total: tracks.length, read_at: iso(now - 39 * 60000) };
  }

  /** Removes a record; its runs stay. */
  const drop = (r) => records.delete(keyOf(r));

  return { ensure, drop, get, record, detail, mark, list, summary, startCheck, check: () => check, activeRun, tracksOf, appleStatus, appleList, appleTracks, all: () => [...records.values()] };
}
