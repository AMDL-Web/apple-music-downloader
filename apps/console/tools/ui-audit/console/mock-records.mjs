// The mock server's records: amdl's albums and playlists as the API lists
// them (GET /library/...), derived from the world's runs. A run of an album,
// song or playlist names its record in `resource`; a record's rollup, state,
// active run and activity_at come from its runs, the newest last. Pause,
// resume, reset and removal change only what the mock keeps.

import { list, TERMINAL, statusCounts } from './mock-world.mjs';

const STATES = ['none', 'partial', 'complete'];
const FLAGS = ['failed', 'given_up', 'missing', 'waiting', 'paused', 'unresolved'];
const time = (v) => Date.parse(v || '') || 0;
const keyOf = (kind, id) => `${kind}:${id}`;
const kindOf = (route) => (route === 'playlists' ? 'playlist' : 'album');

/** The world's records by key, each {kind, id, runs}, oldest run first. */
function groups(w) {
  w.recordState ||= { paused: new Set(), removed: new Set(), missing: new Set() };
  const out = new Map();
  for (const j of w.jobs.values()) {
    if (!j.resource) continue;
    const k = keyOf(j.resource.kind, j.resource.id);
    if (w.recordState.removed.has(k)) continue;
    if (!out.has(k)) out.set(k, { kind: j.resource.kind, id: j.resource.id, runs: [] });
    out.get(k).runs.push(j);
  }
  for (const g of out.values()) g.runs.sort((a, b) => time(a.created_at) - time(b.created_at));
  return out;
}

/** A record as GET /library/{kind} lists it. */
function record(w, g) {
  const runs = g.runs;
  const named = [...runs].reverse().find((j) => j.title) || runs[runs.length - 1];
  const last = runs[runs.length - 1];
  const active = runs.find((j) => !TERMINAL.has(j.status));
  const k = keyOf(g.kind, g.id);
  const wanted = Math.max(0, ...runs.map((j) => j.total_items || 0));
  const doneRun = runs.some((j) => j.status === 'completed');
  let done = doneRun ? wanted : (last.completed_items || 0) + (last.skipped_items || 0);
  const missing = w.recordState.missing.has(k) ? Math.min(1, done) : 0;
  done -= missing;
  const failed = !active && last.status === 'failed' ? last.failed_items || 0 : 0;
  const colors = named.artwork_bg_color ? { bg: named.artwork_bg_color, text1: named.artwork_text_color1, text2: named.artwork_text_color2, text3: named.artwork_text_color3, text4: named.artwork_text_color4 } : undefined;
  const r = {
    id: g.id,
    name: named.title || '',
    artwork: { url: named.artwork_url || '', ...(colors ? { colors } : {}) },
    catalog_state: 'ok',
    paused: w.recordState.paused.has(k),
    priority: 1,
    cover_state: 'done',
    pinned_storefront: named.storefront || 'cn',
    storefront_read: named.storefront || 'cn',
    added_at: runs[0].created_at,
    updated_at: last.updated_at,
    activity_at: new Date(Math.max(...runs.map((j) => time(j.finished_at || j.updated_at)))).toISOString(),
    tracks_wanted: wanted,
    tracks_done: done,
    tracks_failed: failed,
    tracks_given_up: 0,
    tracks_missing: missing,
    withheld_count: 0,
    state: wanted && done >= wanted ? 'complete' : done ? 'partial' : 'none',
  };
  if (named.title) r.resolved_at = runs[0].created_at;
  if (active) r.active_job_id = active.id;
  if (g.kind === 'playlist') return { ...r, curator_name: named.curator_name || 'Apple Music' };
  return { ...r, artist_name: named.artist_name || '', release_date: named.release_date, genre: named.genre, wanted_all: true, artist_cover_state: 'done', release_track_count: wanted };
}

const flagged = (r, f) => (f === 'failed' ? r.tracks_failed > 0 : f === 'missing' ? r.tracks_missing > 0 : f === 'paused' ? r.paused : f === 'unresolved' ? !r.resolved_at : false);

/** One record with its tracks (each its main slot), from its newest run's items. */
function detail(w, g) {
  const r = record(w, g);
  const last = g.runs[g.runs.length - 1];
  const tracks = (w.items.get(last.id) || []).map((it, i) => {
    const audio = ['completed', 'skipped_existing'].includes(it.status) ? 'done' : it.status === 'failed' ? 'failed' : TERMINAL.has(it.status) ? 'pending' : it.status === 'queued' ? 'pending' : 'running';
    const file = { slot: 'main', audio_state: i === 0 && r.tracks_missing ? 'missing' : audio, failures: audio === 'failed' ? 1 : 0, last_job_id: last.id };
    return { song_id: it.adam_id, name: it.title, artist_name: it.artist, duration_ms: it.duration_ms, wanted: true, availability: 'available', files: [file], disc_number: 1, track_number: i + 1, song_number: i + 1 };
  });
  return { ...r, sources: ['manual'], in_apple_library: false, tracks };
}

/** Whether a run is one of the record a downloads read names (resource_kind and resource_id), or any without them. */
export const ofResource = (j, q) =>
  (!q.get('resource_kind') || (q.get('resource_kind') === 'none' ? !j.resource : j.resource && j.resource.kind === q.get('resource_kind'))) &&
  (!q.get('resource_id') || (j.resource && j.resource.id === q.get('resource_id')));

/** The feed's resource_updated after a record's run's download_upserted, as the server sends it, or null. */
export function recordMessage(w, job, eventId) {
  const g = job && job.resource ? groups(w).get(keyOf(job.resource.kind, job.resource.id)) : null;
  return g ? { type: 'resource_updated', resource_kind: g.kind, resource_id: g.id, resource: record(w, g), event_id: eventId, status_counts: statusCounts(w) } : null;
}

function listRecords(w, route, q) {
  const kind = kindOf(route);
  const states = list(q.get('state')).filter((s) => STATES.includes(s));
  const flags = list(q.get('flag')).filter((s) => FLAGS.includes(s));
  const term = (q.get('q') || '').toLowerCase();
  const all = [...groups(w).values()]
    .filter((g) => g.kind === kind)
    .map((g) => record(w, g))
    .filter((r) => (!states.length || states.includes(r.state)) && (!flags.length || flags.some((f) => flagged(r, f))))
    .filter((r) => !term || [r.name, r.artist_name, r.curator_name].some((v) => v && v.toLowerCase().includes(term)))
    .sort((a, b) => (q.get('sort') === 'activity' ? time(b.activity_at) - time(a.activity_at) : time(b.added_at) - time(a.added_at)) || (a.id < b.id ? -1 : 1));
  const limit = Math.min(500, Number(q.get('limit')) || 50);
  const offset = Number(q.get('offset')) || 0;
  return { items: all.slice(offset, offset + limit), total: all.length, limit, offset };
}

function counts(w, kind) {
  const out = { total: 0, states: { none: 0, partial: 0, complete: 0 }, flags: Object.fromEntries(FLAGS.map((f) => [f, 0])) };
  for (const g of groups(w).values()) {
    if (g.kind !== kind) continue;
    const r = record(w, g);
    out.total++;
    out.states[r.state]++;
    for (const f of FLAGS) if (flagged(r, f)) out.flags[f]++;
  }
  return out;
}

/**
 * Answers a /api/v1/library route of the world: {status, body}, or null for
 * a route it does not serve. `body` is the request's parsed JSON body.
 */
export function libraryRoute(w, method, path, q, body) {
  w.pending ||= [];
  if (method === 'GET' && path === '/api/v1/library/summary') {
    return { status: 200, body: { albums: counts(w, 'album'), playlists: counts(w, 'playlist'), pending: { pending: 0, failed: w.pending.length }, check: w.check || null } };
  }
  if (method === 'GET' && path === '/api/v1/library/pending') return { status: 200, body: { items: q.get('state') === 'failed' || !q.get('state') ? w.pending : [] } };
  const pm = path.match(/^\/api\/v1\/library\/pending\/(\d+)(\/retry)?$/);
  if (pm) {
    w.pending = w.pending.filter((p) => String(p.id) !== pm[1]);
    return { status: 200, body: {} };
  }
  if (path === '/api/v1/library/check') {
    if (method === 'POST') w.check = { id: (w.check ? w.check.id : 0) + 1, trigger: 'manual', scope: body && body.id ? `${kindOf(body.kind)}:${body.id}` : 'all', started_at: new Date().toISOString(), finished_at: new Date(Date.now() + 1000).toISOString(), checked: 120, missing: 0 };
    return { status: method === 'POST' ? 202 : 200, body: { check: w.check || null } };
  }
  if (method === 'POST' && path === '/api/v1/library/reset') return { status: 200, body: { reset: 0, skipped_active: 0 } };
  const lm = path.match(/^\/api\/v1\/library\/(albums|playlists)$/);
  if (lm && method === 'GET') return { status: 200, body: listRecords(w, lm[1], q) };
  const rm = path.match(/^\/api\/v1\/library\/(albums|playlists)\/([^/]+)(?:\/(pause|resume|reset|download))?$/);
  if (!rm) return null;
  const k = keyOf(kindOf(rm[1]), decodeURIComponent(rm[2]));
  const g = groups(w).get(k);
  if (!g) return { status: 404, body: { error: { code: 'resource_not_found', message: 'no such record' } } };
  if (!rm[3] && method === 'GET') return { status: 200, body: detail(w, g) };
  if (!rm[3] && method === 'DELETE') {
    w.recordState.removed.add(k);
    return { status: 200, body: {} };
  }
  if (rm[3] === 'pause') w.recordState.paused.add(k);
  if (rm[3] === 'resume') w.recordState.paused.delete(k);
  return { status: 200, body: record(w, g) };
}
