// The demo's stand-in for `amdl serve`: it answers the console's /api/v1
// calls from an in-memory world of made-up releases (catalog.js), the event
// and log streams included, and moves running jobs one step per tick().
// Everything happens in the visitor's browser tab; a write changes only this
// world. Shapes follow internal/api/openapi.yaml. Pure apart from the
// injected album progress and timers, so node:test drives it.

import { albums, songs, artists, playlists, find, albumNamed, playlistNamed, albumFor, STOREFRONT } from './catalog.js';
import { createHookStore } from './fake-hooks.js';
import { createRecordStore, resourceOf } from './fake-records.js';
import { CONFIG, RESTART_KEYS } from './fake-config.js';

const MIN = 60000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const iso = (ms) => new Date(ms).toISOString();

function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function hash(s) {
  let h = 2166136261;
  for (const c of String(s)) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  return h >>> 0;
}

function hexId(prefix, key) {
  const r = rng(hash(key));
  let out = '';
  while (out.length < 32) out += Math.floor(r() * 16).toString(16);
  return `${prefix}_${out}`;
}

const CODECS = {
  'alac-24-192': { codec: 'alac', bit_depth: 24, sample_rate: 192000, bitrate: 4608000 },
  'alac-24-96': { codec: 'alac', bit_depth: 24, sample_rate: 96000, bitrate: 2565559 },
  'alac-24-48': { codec: 'alac', bit_depth: 24, sample_rate: 48000, bitrate: 1877679 },
  'alac-16-44': { codec: 'alac', bit_depth: 16, sample_rate: 44100, bitrate: 1011000 },
  ec3: { codec: 'ec3', bit_depth: 0, sample_rate: 48000, bitrate: 768000 },
  aac: { codec: 'aac', bit_depth: 0, sample_rate: 44100, bitrate: 256000 },
};

/** The codec a release downloads in with the default quality priority (ALAC first). */
const codecFor = (src) => (src && src.quality === 'hires' ? (src.genre === 'Classical' ? 'alac-24-192' : 'alac-24-96') : src && src.quality === 'atmos' ? 'alac-24-48' : 'alac-16-44');

const codecKey = (it) => (it.codec === 'alac' ? `alac-${it.bit_depth}-${Math.floor(it.sample_rate / 1000)}` : it.codec);

/** A track's status, progress and status_message at a stage, as the server reports them. */
function stageFields(stage, frac, key) {
  const c = CODECS[key] || CODECS['alac-16-44'];
  const label = c.codec === 'alac' ? 'ALAC' : c.codec.toUpperCase();
  const p = { download: 0, decrypt: 0, download_bytes: 0, resolved: false, remuxed: false, verified: false, tagged: false, saved: false };
  const codec = { codec: c.codec, bit_depth: c.bit_depth, sample_rate: c.sample_rate, bitrate: c.bitrate };
  switch (stage) {
    case 'queued':
      return { status: 'queued', progress: p, status_message: '' };
    case 'resolving':
      return { status: 'resolving', progress: p, status_message: 'resolving metadata' };
    case 'waiting_download':
      return { status: 'waiting_download', progress: { ...p, resolved: true }, status_message: 'waiting for download slot', ...codec };
    case 'downloading':
      return { status: 'downloading', progress: { ...p, resolved: true, download: frac }, status_message: `${label} download ${Math.round(frac * 100)}%`, ...codec };
    case 'waiting_decrypt':
      return { status: 'waiting_decrypt', progress: { ...p, resolved: true, download: 1 }, status_message: 'waiting for decrypt slot', ...codec };
    case 'decrypting':
      return { status: 'decrypting', progress: { ...p, resolved: true, download: 1, decrypt: frac }, status_message: 'decrypting', ...codec };
    case 'remuxing':
      return { status: 'remuxing', progress: { ...p, resolved: true, download: 1, decrypt: 1 }, status_message: 'remuxing', ...codec };
    case 'tagging':
      return { status: 'tagging', progress: { ...p, resolved: true, download: 1, decrypt: 1, remuxed: true }, status_message: 'writing metadata', ...codec };
    case 'saving':
      return { status: 'saving', progress: { ...p, resolved: true, download: 1, decrypt: 1, remuxed: true, tagged: true, verified: true }, status_message: 'saving', ...codec };
    case 'completed':
      return { status: 'completed', progress: { download: 1, decrypt: 1, download_bytes: 0, resolved: true, remuxed: true, verified: true, tagged: true, saved: true }, status_message: 'saved', ...codec };
    case 'skipped_existing':
      return { status: 'skipped_existing', progress: { ...p, resolved: true }, status_message: 'File already exists; skipped', ...codec };
    case 'failed':
      return { status: 'failed', progress: { ...p, resolved: true, download: frac || 0 }, status_message: '', ...codec };
    case 'cancelled':
      return { status: 'cancelled', progress: p, status_message: 'cancelled' };
    default:
      throw new Error(`unknown stage ${stage}`);
  }
}

const STAGE_ORDER = ['queued', 'resolving', 'waiting_download', 'downloading', 'waiting_decrypt', 'decrypting', 'remuxing', 'tagging', 'saving', 'completed'];
const TERMINAL = new Set(['completed', 'failed', 'cancelled']);
const DONE = new Set(['completed', 'failed', 'cancelled', 'skipped_existing']);
const MILESTONES = new Set(['job_queued', 'job_started', 'job_moved', 'resolved_input', 'job_finished', 'job_failed', 'job_cancelled', 'item_completed', 'item_failed', 'item_skipped', 'job_deleted']);
// The spacing of queue_order values, as the real store hands them out.
const QUEUE_GAP = 2 ** 20;
const MAX_RUNNING = 3;
const DECRYPT_ERROR = 'decrypt: wrapper closed the connection while a sample was in flight; 3 attempts used';

const json = (status, body) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const apiError = (status, code, message, details) => json(status, { error: { code, message, ...(details ? { details } : {}) } });
const list = (v) =>
  String(v || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

function parseLink(url) {
  const m = String(url || '').match(/music\.apple\.com\/([a-z]{2})\/(album|song|playlist|artist|station|music-video)\/(?:[^/?#]*\/)?([A-Za-z0-9.-]+)(?:\?i=(\d+))?/);
  return m ? { storefront: m[1], type: m[2], id: m[3], track_id: m[4] } : null;
}

const outputDir = (job) => (job.type === 'playlist' ? `playlists/${job.title}` : `library/${job.artist_name}/${job.title}`);

/**
 * A fresh demo world. `progress(status, items)` is the console's
 * lib/album-progress.js albumProgress, which the server's job progress
 * matches; `now` fixes the clock the history is laid out from.
 */
export function createFakeApi({ progress, now = Date.now(), timers = globalThis } = {}) {
  if (typeof progress !== 'function') throw new Error('createFakeApi needs the album progress function');
  const jobs = new Map();
  const items = new Map();
  const records = createRecordStore({ jobs, items, now });
  const events = [];
  const logs = [];
  const feeds = new Set();
  const jobStreams = new Map();
  const logStreams = new Set();
  const config = structuredClone(CONFIG);
  // The restart keys saved since the last restart.
  const pending = new Set();
  const configBody = () => ({ config, persisted: true, pending_restart: [...pending].sort() });
  const accounts = [
    { id: 'wm_demo_us', name: 'demo@example.com', region: STOREFRONT, position: 0, default: true, library_sync: true, apple_library: true, state: 'active', added_at: iso(now - 30 * 86400000), seen_at: iso(now - 60000) },
    { id: 'wm_demo_jp', name: 'demo.jp@example.com', region: 'jp', position: 1, default: false, library_sync: false, apple_library: false, state: 'active', added_at: iso(now - 9 * 86400000), seen_at: iso(now - 60000) },
  ];
  const sortAccounts = () => {
    accounts.sort((a, b) => a.position - b.position);
    accounts.forEach((a, i) => {
      a.position = i;
      a.default = i === 0;
    });
  };
  const librarySync = { enabled: true, interval_minutes: 15, anchor_size: 0, last_submitted: 0, total_submitted: 0 };
  const librarySyncBody = () => ({
    enabled: librarySync.enabled,
    interval_minutes: librarySync.interval_minutes,
    accounts: accounts.map((a) => ({
      account_id: a.id,
      name: a.name,
      region: a.region,
      enabled: a.library_sync,
      anchor_size: a.default ? librarySync.anchor_size || 50 : 0,
      last_poll_at: iso(now - 4 * 60000),
      last_submitted: a.default ? librarySync.last_submitted : 0,
      total_submitted: a.default ? librarySync.total_submitted : 0,
    })),
  });
  const appleAccount = (a) => {
    const { sync_interval_minutes, full_sync_hours, ...status } = records.appleStatus();
    return { account_id: a.id, name: a.name, region: a.region, default: a.default, enabled: a.apple_library, ...(a.apple_library ? status : { available: false, reason: 'disabled', state: 'idle', counts: { albums: 0, songs: 0, playlists: 0, artists: 0 } }) };
  };
  const appleStatusBody = () => {
    const { sync_interval_minutes, full_sync_hours } = records.appleStatus();
    return { sync_interval_minutes, full_sync_hours, accounts: accounts.map(appleAccount) };
  };
  const hooks = createHookStore({
    enabled: true,
    entries: [
      { name: 'ntfy', type: 'webhook', enabled: true, events: ['job_finished', 'job_failed'], job_types: [], method: 'POST', url: 'https://ntfy.sh/amdl-demo', headers: { Title: 'AMDL', Tags: 'musical_note' }, preset: 'ntfy' },
      { name: 'tag-library', type: 'exec', enabled: true, events: ['job_finished'], job_types: ['album'] },
    ],
  });
  const random = rng(20261031);
  // The codec each track will download in, decided when its job is made.
  const planned = new Map();
  let nextEvent = 1000;
  let nextLog = 1;
  let submitted = 0;
  let lastOrder = 0;
  // A queue_order behind every job's: the place of a new or retried job.
  const nextOrder = () => (lastOrder += QUEUE_GAP);
  const queuedInOrder = () => [...jobs.values()].filter((j) => j.status === 'queued').sort((a, b) => a.queue_order - b.queue_order);

  const withProgress = (job) => ({ ...job, progress: progress(job.status, items.get(job.id) || []).percent / 100 });

  /** The jobs per status: every job (the feed's), or those a list read selects. */
  function statusCounts(list = jobs.values()) {
    const c = { queued: 0, running: 0, completed: 0, failed: 0, cancelled: 0, total: 0 };
    for (const j of list) {
      c[j.status]++;
      c.total++;
    }
    return c;
  }

  function addEvent(ev, live = true, at = Date.now()) {
    const e = { id: nextEvent++, created_at: iso(at), ...ev };
    for (const k of Object.keys(e)) if (e[k] === undefined) delete e[k];
    events.push(e);
    if (!live) return e;
    for (const send of jobStreams.get(e.job_id) || []) send(e.id, e.type, e);
    if (MILESTONES.has(e.type)) {
      const job = jobs.get(e.job_id);
      const msg = job
        ? { type: 'download_upserted', job: withProgress(job), event_id: e.id, status_counts: statusCounts() }
        : { type: 'download_deleted', job_id: e.job_id, event_id: e.id, status_counts: statusCounts() };
      for (const send of feeds) send(e.id, msg.type, msg);
      // A record's run is followed by its record, as GET /library lists it.
      const rec = job && job.resource ? records.get(job.resource.kind, job.resource.id) : null;
      if (rec) {
        const upd = { type: 'resource_updated', resource_kind: rec.kind, resource_id: rec.id, resource: records.record(rec), event_id: e.id, status_counts: statusCounts() };
        for (const send of feeds) send(e.id, upd.type, upd);
      }
    }
    return e;
  }

  function addLog(level, message, attributes, at = Date.now()) {
    const l = { sequence: nextLog++, time: iso(at), level, message, attributes };
    logs.push(l);
    for (const s of logStreams) if (s.match(l)) s.send(l.sequence, 'log', l);
  }

  function recount(job) {
    const list = items.get(job.id) || [];
    job.total_items = list.length;
    job.completed_items = list.filter((i) => i.status === 'completed').length;
    job.skipped_items = list.filter((i) => i.status === 'skipped_existing').length;
    job.failed_items = list.filter((i) => i.status === 'failed').length;
    job.cancelled_items = list.filter((i) => i.status === 'cancelled').length;
  }

  function describe(job, src) {
    Object.assign(job, {
      title: src.name,
      artwork_url: src.artwork_url,
      artwork_bg_color: src.artwork_bg_color,
      artwork_text_color1: src.artwork_text_color1,
      artwork_text_color2: src.artwork_text_color2,
      artwork_text_color3: src.artwork_text_color3,
      artwork_text_color4: src.artwork_text_color4,
    });
    if (src.type === 'playlist') job.curator_name = src.curator_name;
    else {
      const artist = artists.find((a) => a.name === src.artist_name);
      Object.assign(job, { artist_name: src.artist_name, artist_url: artist && artist.url, release_date: src.release_date, genre: src.genre });
    }
  }

  function makeItems(job, src, { stages, fracs, codec, at, finished }) {
    const tracks = src.type === 'song' ? [src] : src.tracks;
    const list = tracks.map((s, i) => {
      const n = tracks.length;
      const stage = stages ? stages(i, n) : job.status === 'completed' ? 'completed' : job.status === 'cancelled' ? 'cancelled' : 'queued';
      const key = codec || codecFor(find(s.album_id));
      const f = stageFields(stage, fracs ? fracs(i) : 0, key);
      const item = {
        id: hexId('item', `${job.id}:${i}`),
        job_id: job.id,
        adam_id: s.id,
        kind: 'song',
        index: i + 1,
        title: s.name,
        artist: s.artist_name,
        album: s.album_name,
        duration_ms: s.duration_ms,
        artwork_url: s.artwork_url,
        has_lyrics: hash(s.id) % 10 > 2,
        lyrics_status: 'fetched',
        ...f,
        file_size: 0,
        attempt: 1,
        max_attempts: config.download.max_attempts,
        created_at: iso(at + 500),
        updated_at: iso(finished ? finished - (n - i) * 1500 : at + i * 1000),
      };
      if (stage === 'completed' || stage === 'skipped_existing') finishItem(job, item);
      setBytes(item);
      if (stage === 'failed') {
        item.error = DECRYPT_ERROR;
        item.attempt = 3;
      }
      planned.set(item.id, key);
      return item;
    });
    items.set(job.id, list);
    recount(job);
  }

  const fileSize = (it) => Math.round((it.duration_ms / 1000) * ((it.bitrate || 1000000) / 8) * (0.96 + (hash(it.id) % 80) / 1000));
  // download_bytes: the share of the file received, all of it once the transfer is over.
  const setBytes = (it) => (it.progress.download_bytes = Math.round(it.progress.download * fileSize(it)));

  function finishItem(job, it) {
    it.file_size = fileSize(it);
    it.output_path = `/app/data/downloads/${outputDir(job)}/${String(it.index).padStart(2, '0')}. ${it.title}.m4a`;
  }

  /** One job with its tracks and the history it would have. */
  function makeJob({ key, src, type = src ? src.type : 'album', status, ago, duration = 4 * MIN, input, live = false, ...rest }) {
    const created = now - ago;
    const started = status === 'queued' ? null : created + 2000;
    const finished = TERMINAL.has(status) ? started + duration : null;
    const id = hexId('job', key);
    const job = {
      id,
      input: input || src.url,
      type,
      storefront: STOREFRONT,
      status,
      total_items: 0,
      completed_items: 0,
      skipped_items: 0,
      failed_items: 0,
      cancelled_items: 0,
      created_at: iso(created),
      updated_at: iso(finished || started || created),
    };
    if (status === 'queued') job.queue_order = nextOrder();
    if (started) job.started_at = iso(started);
    if (finished) job.finished_at = iso(finished);
    if (rest.error) job.error = rest.error;
    if (rest.overrides) job.overrides = rest.overrides;
    // Albums, songs and playlists download for a record, known from the link
    // before it is read; stations and artists do not.
    const link = input ? parseLink(input) : null;
    const target = src || (link && find(link.id));
    const ref = type === 'station' ? null : resourceOf(target);
    if (ref) {
      job.resource = ref;
      records.ensure(ref, created, target.type === 'song' ? target.id : '');
    }
    job.trigger = rest.trigger || 'manual';
    job.priority = 1;
    jobs.set(id, job);
    if (src) {
      describe(job, src);
      makeItems(job, src, { ...rest, at: created, finished });
    } else items.set(id, []);
    seedHistory(job, created, started, finished, live);
    return job;
  }

  function seedHistory(job, created, started, finished, live) {
    const add = (type, extra, at) => addEvent({ job_id: job.id, type, ...extra }, live, at);
    add('job_queued', { message: 'job queued', payload: { ...job, status: 'queued' } }, created);
    if (!started) return;
    add('job_started', { message: 'job started', payload: { ...job, status: 'running' } }, started);
    addLog('info', 'job started', { component: 'jobs', job_id: job.id }, started);
    if (!job.title) return;
    add('resolved_input', { message: job.type, payload: { ...job, status: 'running' } }, started + 800);
    for (const it of items.get(job.id)) {
      const at = Date.parse(it.updated_at);
      if (it.codec) add('codec_selected', { item_id: it.id, phase: it.codec, payload: { ...it } }, at - 900);
      if (it.status === 'completed') {
        add('item_completed', { item_id: it.id, message: 'saved', payload: { ...it } }, at);
        addLog('info', 'track saved', { component: 'jobs', job_id: job.id, path: it.output_path }, at);
      }
      if (it.status === 'skipped_existing') add('item_skipped', { item_id: it.id, message: it.status_message, payload: { ...it } }, at);
      if (it.status === 'failed') {
        add('item_failed', { item_id: it.id, message: it.error, payload: { ...it } }, at);
        addLog('error', 'decrypt failed', { component: 'fairplay', job_id: job.id, error: it.error }, at);
      }
    }
    if (job.status === 'completed') add('job_finished', { message: 'job finished', payload: { ...job } }, finished);
    if (job.status === 'failed') add('job_failed', { message: job.error, payload: { ...job } }, finished);
    if (job.status === 'cancelled') add('job_cancelled', { message: 'job cancelled', payload: { ...job } }, finished);
  }

  // The world a visitor opens: three jobs running, two waiting, and a week of
  // finished, failed and cancelled ones behind them.
  addLog('info', 'amdl backend listening', { component: 'main', addr: '0.0.0.0:18080' }, now - 3 * DAY);
  const history = [
    { key: 'rainy', src: playlistNamed('Rainy Window'), status: 'completed', ago: 4 * DAY, duration: 22 * MIN },
    { key: 'arcade', src: albumNamed('High Score Summer'), status: 'completed', ago: 3 * DAY },
    { key: 'lanterns', src: albumNamed('Lanterns for the Long Walk'), status: 'cancelled', ago: 2 * DAY + 5 * HOUR, stages: (i) => (i < 4 ? 'completed' : 'cancelled') },
    { key: 'orchard', src: albumNamed('Orchard Light'), status: 'completed', ago: 2 * DAY },
    { key: 'boats', src: albumNamed('Paper Boats'), status: 'completed', ago: 26 * HOUR, stages: (i) => (i === 6 ? 'skipped_existing' : 'completed') },
    { key: 'machinery', src: albumNamed('Soft Machinery - Single').tracks[0], type: 'song', status: 'completed', ago: 20 * HOUR, duration: 40000 },
    { key: 'trains', src: albumNamed('Late Trains'), status: 'failed', ago: 6 * HOUR, error: DECRYPT_ERROR, stages: (i) => (i === 3 || i === 7 || i === 8 ? 'failed' : 'completed') },
    { key: 'nocturnes', src: albumNamed('Studies in Blue: Nocturnes for Strings'), status: 'completed', ago: 5 * HOUR, duration: 9 * MIN },
    { key: 'hikari', src: albumNamed('ひかりの地図'), status: 'completed', ago: 2 * HOUR },
    { key: 'tide', src: albumNamed('潮汐来信'), status: 'completed', ago: 40 * MIN },
    { key: 'velvet', src: albumNamed('Velvet Hours'), status: 'running', ago: 3 * MIN, stages: (i) => (i < 5 ? 'completed' : i === 5 ? 'downloading' : i === 6 ? 'decrypting' : 'queued'), fracs: (i) => (i === 5 ? 0.42 : 0.5) },
    { key: 'signal', src: albumNamed('Signal / Noise'), status: 'running', ago: 90000, codec: 'ec3', stages: (i) => (i < 2 ? 'completed' : i === 2 ? 'downloading' : 'queued'), fracs: () => 0.18 },
    { key: 'desk', src: null, type: 'playlist', status: 'running', ago: 20000, input: playlistNamed('Late Night Desk').url },
    { key: 'garden', src: albumNamed('Satellite Garden'), status: 'queued', ago: 12000 },
    { key: 'cedar', src: albumNamed('Cedar Hours - EP'), status: 'queued', ago: 8000 },
  ];
  for (const spec of history) makeJob(spec);
  logs.sort((a, b) => Date.parse(a.time) - Date.parse(b.time)).forEach((l, i) => (l.sequence = i + 1));
  nextLog = logs.length + 1;

  /** Advances every running or queued job one step; returns how many things moved. */
  function tick() {
    let moved = 0;
    // A free slot takes the first queued job in run order.
    const started = new Set();
    for (const job of queuedInOrder()) {
      if ([...jobs.values()].filter((j) => j.status === 'running').length >= MAX_RUNNING) break;
      Object.assign(job, { status: 'running', started_at: iso(Date.now()), updated_at: iso(Date.now()) });
      delete job.queue_order;
      addEvent({ job_id: job.id, type: 'job_started', message: 'job started', payload: { ...job } });
      addLog('info', 'job started', { component: 'jobs', job_id: job.id });
      started.add(job);
      moved++;
    }
    for (const job of [...jobs.values()]) {
      if (job.status !== 'running' || started.has(job)) continue;
      if (!job.title) {
        const link = parseLink(job.input);
        const src = (link && find(link.id)) || albumFor(job.id);
        describe(job, src);
        makeItems(job, src, { at: Date.now() });
        job.updated_at = iso(Date.now());
        addEvent({ job_id: job.id, type: 'resolved_input', message: job.type, payload: { ...job } });
        moved++;
        continue;
      }
      const list = items.get(job.id);
      let inFlight = list.filter((it) => !DONE.has(it.status) && it.status !== 'queued');
      while (inFlight.length < 2) {
        const next = list.find((it) => it.status === 'queued');
        if (!next) break;
        Object.assign(next, stageFields('resolving', 0), { updated_at: iso(Date.now()) });
        addEvent({ job_id: job.id, item_id: next.id, type: 'item_progress', phase: 'resolving', message: next.status_message, payload: { ...next } });
        inFlight = [...inFlight, next];
        moved++;
      }
      for (const it of inFlight) {
        const key = planned.get(it.id) || codecKey(it);
        let stage = it.status;
        let frac = stage === 'downloading' ? it.progress.download : stage === 'decrypting' ? it.progress.decrypt : 0;
        if (stage === 'downloading' && frac < 1) frac = Math.min(1, Math.round((frac + 0.1 + random() * 0.18) * 100) / 100);
        else if (stage === 'decrypting' && frac < 1) frac = Math.min(1, frac + 0.5);
        else {
          stage = STAGE_ORDER[STAGE_ORDER.indexOf(stage) + 1] || 'completed';
          frac = 0;
          if (stage === 'waiting_download') {
            Object.assign(it, stageFields(stage, 0, key));
            addEvent({ job_id: job.id, item_id: it.id, type: 'codec_selected', phase: it.codec, payload: { ...it } });
          }
        }
        Object.assign(it, stageFields(stage, frac, key), { updated_at: iso(Date.now()) });
        setBytes(it);
        if (stage === 'completed') {
          finishItem(job, it);
          addEvent({ job_id: job.id, item_id: it.id, type: 'item_completed', message: 'saved', payload: { ...it } });
          addLog('info', 'track saved', { component: 'jobs', job_id: job.id, path: it.output_path });
        } else {
          addEvent({ job_id: job.id, item_id: it.id, type: 'item_progress', phase: it.status, message: it.status_message, payload: { ...it } });
        }
        moved++;
      }
      recount(job);
      if (list.every((it) => DONE.has(it.status))) {
        Object.assign(job, { status: job.failed_items ? 'failed' : 'completed', finished_at: iso(Date.now()), updated_at: iso(Date.now()) });
        addEvent({ job_id: job.id, type: job.status === 'failed' ? 'job_failed' : 'job_finished', message: 'job finished', payload: { ...job } });
        addLog('info', 'job finished', { component: 'jobs', job_id: job.id, tracks: job.completed_items });
      } else job.updated_at = iso(Date.now());
    }
    return moved;
  }

  /** An SSE response whose body stays open until the request is aborted or `end()` runs. */
  function stream(signal, open) {
    const enc = new TextEncoder();
    let cleanup = () => {};
    let ctl;
    const stop = () => {
      const c = cleanup;
      cleanup = () => {};
      c();
    };
    const body = new ReadableStream({
      start(c) {
        ctl = c;
        const write = (s) => {
          try {
            c.enqueue(enc.encode(s));
          } catch {
            stop();
          }
        };
        const send = (id, name, data) => write(`id: ${id}\nevent: ${name}\ndata: ${JSON.stringify(data)}\n\n`);
        const end = () => {
          stop();
          try {
            c.close();
          } catch {}
        };
        write(': open\n\n');
        // The console drops a stream that has said nothing for 45 s.
        const ping = timers.setInterval(() => write(': keepalive\n\n'), 15000);
        const close = open({ send, end }) || (() => {});
        cleanup = () => {
          timers.clearInterval(ping);
          close();
        };
      },
      cancel: stop,
    });
    signal?.addEventListener(
      'abort',
      () => {
        stop();
        try {
          ctl.error(new DOMException('The request was aborted.', 'AbortError'));
        } catch {}
      },
      { once: true },
    );
    return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
  }

  function logMatcher(q) {
    const levels = list(q.get('level'));
    const comp = q.get('component');
    const jobId = q.get('job_id');
    const since = Date.parse(q.get('since') || '') || 0;
    const term = (q.get('q') || '').toLowerCase();
    return (l) =>
      (!levels.length || levels.includes(l.level)) &&
      (!comp || l.attributes.component === comp) &&
      (!jobId || l.attributes.job_id === jobId) &&
      Date.parse(l.time) >= since &&
      (!term || JSON.stringify(l).toLowerCase().includes(term));
  }

  function filteredJobs(q) {
    const statuses = q.getAll('status').flatMap(list);
    const types = q.getAll('type').flatMap(list);
    const term = (q.get('q') || '').toLowerCase();
    const all = [...jobs.values()];
    // current=true: no newer run of the same record (a station: link), and a record not complete.
    const sameTarget = (a, b) => (a.resource ? Boolean(b.resource) && a.resource.kind === b.resource.kind && a.resource.id === b.resource.id : !b.resource && a.input === b.input);
    const current = (j, i) => {
      const rec = j.resource ? records.get(j.resource.kind, j.resource.id) : null;
      return !all.slice(i + 1).some((n) => sameTarget(j, n)) && !(rec && records.record(rec).state === 'complete');
    };
    const out = all.filter(
      (j, i) =>
        (!statuses.length || statuses.includes(j.status)) &&
        (!types.length || types.includes(j.type)) &&
        (q.get('current') !== 'true' || current(j, i)) &&
        (!term || `${j.title || ''}\n${j.input}\n${j.artist_name || ''}`.toLowerCase().includes(term)),
    );
    const dir = q.get('order') === 'asc' ? 1 : -1;
    if (q.get('sort') === 'queue_order') {
      // The queue in run order, then every other job in submission order.
      const rank = (j) => (j.status === 'queued' ? 0 : 1);
      return out.sort((a, b) => rank(a) - rank(b) || (rank(a) === 0 ? dir * (a.queue_order - b.queue_order) : Date.parse(a.created_at) - Date.parse(b.created_at)));
    }
    const sort = ['created_at', 'updated_at', 'finished_at'].includes(q.get('sort')) ? q.get('sort') : 'created_at';
    return out.sort((a, b) => dir * ((Date.parse(a[sort]) || 0) - (Date.parse(b[sort]) || 0)) || dir * a.id.localeCompare(b.id));
  }

  /**
   * Places a queued job before or after another, or first or last, as the
   * real move does: only its queue_order changes, unless no value is left
   * between its new neighbors and the queue is renumbered. Each job whose
   * value changed gets job_moved, the moved one last.
   */
  function move(job, body) {
    const fields = ['before', 'after', 'to'].filter((k) => typeof body[k] === 'string' && body[k] !== '');
    const unknown = Object.keys(body).filter((k) => !['before', 'after', 'to'].includes(k));
    if (fields.length !== 1 || unknown.length || (body.to && body.to !== 'first' && body.to !== 'last')) {
      return apiError(400, 'invalid_request', 'set exactly one of before, after and to (first or last)');
    }
    if (job.status !== 'queued') return apiError(409, 'job_not_queued', `job ${job.id} is not queued`);
    const queue = queuedInOrder();
    const from = queue.indexOf(job);
    const rest = queue.filter((j) => j !== job);
    let to = rest.length;
    const anchorId = body.before || body.after;
    if (anchorId) {
      let at = rest.findIndex((j) => j.id === anchorId);
      if (anchorId === job.id) at = body.after ? from - 1 : from;
      else if (at < 0) return apiError(409, 'job_not_queued', `job ${anchorId} is not queued`);
      to = body.after ? at + 1 : at;
    } else if (body.to === 'first') to = 0;
    if (to !== from && rest.length) {
      const changed = new Map();
      if (to === 0) changed.set(job, rest[0].queue_order - QUEUE_GAP);
      else if (to === rest.length) changed.set(job, rest[rest.length - 1].queue_order + QUEUE_GAP);
      else if (rest[to].queue_order - rest[to - 1].queue_order >= 2) changed.set(job, rest[to - 1].queue_order + Math.floor((rest[to].queue_order - rest[to - 1].queue_order) / 2));
      else [...rest.slice(0, to), job, ...rest.slice(to)].forEach((j, i) => (i + 1) * QUEUE_GAP !== j.queue_order && changed.set(j, (i + 1) * QUEUE_GAP));
      const order = [...changed.keys()].filter((j) => j !== job).sort((a, b) => a.queue_order - b.queue_order);
      if (changed.has(job)) order.push(job);
      for (const j of order) j.queue_order = changed.get(j);
      lastOrder = Math.max(lastOrder, job.queue_order);
      for (const j of order) addEvent({ job_id: j.id, type: 'job_moved', message: j === job ? 'job moved in the queue' : 'queue renumbered', payload: { ...j } });
    }
    return json(200, { job: withProgress(job) });
  }

  function jobRoute(method, id, action, q, signal, body) {
    const job = jobs.get(id);
    if (!job) return apiError(404, 'not_found', 'job not found');
    const list = items.get(id) || [];
    if (!action && method === 'GET') return json(200, { job: withProgress(job), items: list, hooks: [], event_cursor: nextEvent - 1 });
    if (!action && method === 'DELETE') {
      if (!TERMINAL.has(job.status)) return apiError(409, 'job_active', 'job is still active');
      jobs.delete(id);
      items.delete(id);
      addEvent({ job_id: id, type: 'job_deleted', message: 'job deleted' });
      return json(200, { deleted: true });
    }
    if (action === 'cancel' && method === 'POST') {
      if (TERMINAL.has(job.status)) return apiError(409, 'job_finished', 'job has already finished');
      // Cancel on a record's run pauses the record.
      const rec = job.resource ? records.get(job.resource.kind, job.resource.id) : null;
      if (rec) rec.paused = true;
      Object.assign(job, { status: 'cancelled', finished_at: iso(Date.now()), updated_at: iso(Date.now()) });
      delete job.queue_order;
      for (const it of list) if (!DONE.has(it.status)) Object.assign(it, stageFields('cancelled', 0));
      recount(job);
      addEvent({ job_id: id, type: 'job_cancelled', message: 'job cancelled', payload: { ...job } });
      return json(200, withProgress(job));
    }
    // A retry is a new run of the same link.
    if (action === 'retry' && method === 'POST') {
      if (job.status !== 'failed' && job.status !== 'cancelled') return apiError(409, 'job_not_retryable', 'only failed or cancelled jobs can be retried');
      const rec = job.resource ? records.get(job.resource.kind, job.resource.id) : null;
      if (rec) rec.paused = false;
      const src = sourceOf(job.input);
      const next = queueRun(src, job.input, job.overrides, 'retry');
      return json(202, { status: 'queued', job: withProgress(next) });
    }
    if (action === 'move' && method === 'POST') return move(job, body);
    if (action === 'events' && method === 'GET') {
      const after = Number(q.get('cursor')) || 0;
      const backlog = events.filter((e) => e.job_id === id && e.id > after);
      // A finished job's stream says what it has left and ends; with nothing left, 204.
      if (TERMINAL.has(job.status) && !backlog.length) return new Response(null, { status: 204 });
      return stream(signal, ({ send, end }) => {
        for (const e of backlog) send(e.id, e.type, e);
        if (TERMINAL.has(job.status)) return end();
        const set = jobStreams.get(id) || new Set();
        set.add(send);
        jobStreams.set(id, set);
        return () => set.delete(send);
      });
    }
    return apiError(405, 'method_not_allowed', 'method not allowed');
  }

  /** The catalog object a job's or a link's input names. */
  function sourceOf(url) {
    const link = parseLink(url);
    if (!link) return null;
    const known = find(link.track_id || link.id);
    return known && known.type !== 'artist' ? known : albumFor(link.id);
  }

  /** Queues a run of `src`, as a submit, a retry or a reset does. */
  function queueRun(src, input, overrides, trigger = 'manual') {
    return makeJob({ key: `submit${submitted++}:${input}`, src, type: src.type, status: 'queued', ago: 0, input, live: true, overrides, trigger });
  }

  function submit(body) {
    const results = [];
    for (const u of Array.isArray(body.urls) ? body.urls : []) {
      const link = parseLink(u);
      if (!link) {
        results.push({ url: u, status: 'invalid', error: { code: 'invalid_url', message: 'not an Apple Music link' } });
        continue;
      }
      const src = sourceOf(u);
      const ref = resourceOf(src);
      const m = ref ? records.mark(ref, src.type === 'song' ? src.id : '') : null;
      if (m && m.active_job_id) {
        results.push({ url: u, status: 'duplicate_active', resource: ref, existing_job_id: m.active_job_id, error: { code: 'duplicate_active', message: 'already queued or running' } });
        continue;
      }
      const done = m && (src.type === 'song' ? m.song_state === 'done' : m.state === 'complete' && records.get(ref.kind, ref.id).wanted === 'all');
      if (done) {
        results.push({ url: u, status: 'already_downloaded', resource: ref });
        continue;
      }
      if (ref) records.get(ref.kind, ref.id) && (records.get(ref.kind, ref.id).paused = false);
      const job = queueRun(src, u, body.overrides);
      results.push({ url: u, status: 'accepted', ...(ref ? { resource: ref } : {}), job: withProgress(job) });
    }
    const accepted = results.filter((r) => ['accepted', 'pending', 'already_downloaded'].includes(r.status)).length;
    const out = { accepted, rejected: results.length - accepted, results };
    return accepted ? json(202, out) : apiError(422, 'no_urls_accepted', 'No URL was accepted.', out);
  }

  /** /library/{kind}/{id}[/action]: one record and what can be done to it. */
  function recordRoute(method, kind, id, action, body) {
    const rec = records.get(kind, id);
    if (!rec) return apiError(404, 'resource_not_found', `no ${kind === 'playlists' ? 'playlist' : 'album'} record ${id}`);
    const src = find(rec.id);
    const run = records.activeRun(rec);
    if (!action && method === 'GET') return json(200, records.detail(rec));
    if (!action && method === 'DELETE') {
      if (run) return apiError(409, 'run_active', 'the record has a queued or running run; pause it first');
      records.drop(rec);
      return new Response(null, { status: 204 });
    }
    if (action === 'pause' && method === 'POST') {
      rec.paused = true;
      if (run) jobRoute('POST', run.id, 'cancel');
      return json(200, records.record(rec));
    }
    if (action === 'resume' && method === 'POST') {
      rec.paused = false;
      const x = records.record(rec);
      if (x.state !== 'complete' && !x.active_job_id && src) queueRun(src, src.url, undefined, 'retry');
      return json(200, records.record(rec));
    }
    if ((action === 'reset' || action === 'download') && method === 'POST') {
      if (run) return apiError(409, 'run_active', 'the record has a queued or running run; pause it first');
      if (!src) return json(200, { status: 'already_downloaded' });
      rec.paused = false;
      const job = queueRun(src, src.url, undefined, action === 'reset' ? 'reset' : 'manual');
      return json(202, action === 'reset' ? { slots: records.tracksOf(rec).length, job: withProgress(job) } : { status: 'queued', job: withProgress(job) });
    }
    return apiError(405, 'method_not_allowed', 'method not allowed');
  }

  function lookup(body) {
    const link = parseLink(body.url);
    if (!link) return apiError(400, 'invalid_url', 'Not an Apple Music link.');
    const base = { input: body.url, type: link.type === 'music-video' ? 'song' : link.type, storefront: link.storefront, id: link.id, ...(link.track_id ? { track_id: link.track_id } : {}) };
    const known = find(link.track_id || link.id);
    const src = known || albumFor(link.id);
    const out = { ...base, title: src.name, artwork_url: src.artwork_url };
    for (const k of ['artwork_bg_color', 'artwork_text_color1', 'artwork_text_color2', 'artwork_text_color3', 'artwork_text_color4', 'release_date', 'genre', 'audio_traits', 'duration_ms']) if (src[k]) out[k] = src[k];
    if (src.type === 'playlist') out.curator_name = src.curator_name;
    else if (src.type === 'artist') out.artist_name = src.name;
    else out.artist_name = src.artist_name;
    if (src.type === 'album') out.track_count = src.track_count;
    if (src.type === 'song') out.album_name = src.album_name;
    const ref = resourceOf(src);
    const mark = ref ? records.mark(ref, src.type === 'song' ? src.id : '') : null;
    if (mark) out.resource = mark;
    return json(200, out);
  }

  function quality(body) {
    const link = parseLink(body.url);
    if (!link) return apiError(400, 'invalid_url', 'Not an Apple Music link.');
    const known = find(link.track_id || link.id);
    const src = known && known.type !== 'artist' ? known : albumFor(link.id);
    const tracks = (src.type === 'song' ? [src] : src.tracks).slice(0, 30).map((s) => {
      const album = find(s.album_id);
      const c = CODECS[codecFor(album)];
      const opt = (id, label, extra, available = true) => ({ id, label, available, ...(available ? extra : {}) });
      return {
        song: { id: s.id, name: s.name, artist: s.artist_name, album: s.album_name, has_lyrics: hash(s.id) % 10 > 2 },
        qualities: [
          opt('aac', 'AAC', { codec: 'aac', codec_id: 'audio-stereo-256', channels: 2, bitrate: 256000, sample_rate: 44100 }),
          opt('aac-binaural', 'AAC Binaural', {}, false),
          opt('aac-downmix', 'AAC Downmix', {}, false),
          opt('alac', 'Lossless (ALAC)', { codec: 'alac', codec_id: `audio-alac-stereo-${c.sample_rate}-${c.bit_depth}`, channels: 2, bit_depth: c.bit_depth, sample_rate: c.sample_rate, bitrate: c.bitrate }),
          opt('ec3', 'Dolby Atmos', { codec: 'ec3', codec_id: 'audio-atmos-2768', channels: 16, bitrate: 768000, sample_rate: 48000 }, album.quality === 'atmos'),
          opt('ac3', 'Dolby Digital', {}, false),
        ],
      };
    });
    return json(200, { input: body.url, storefront: link.storefront, type: src.type, adam_id: src.id, tracks });
  }

  // Search matches names, artists (a playlist's curator) and albums; a term with no match answers
  // with the whole catalog, so every search in the demo has something to show.
  function search(q) {
    const term = (q.get('term') || '').trim().replace(/\s+/g, ' ');
    const t = term.toLowerCase();
    const types = list(q.get('types'));
    const want = (k) => !types.length || types.includes(k);
    const limit = Math.min(25, Number(q.get('limit')) || 10);
    const offset = Number(q.get('offset')) || 0;
    const hit = (x) => [x.name, x.artist_name, x.album_name].some((v) => v && v.toLowerCase().includes(t));
    // Hits on the name come before hits on the artist or album.
    const ranked = (arr) => arr.filter(hit).sort((a, b) => !a.name.toLowerCase().includes(t) - !b.name.toLowerCase().includes(t));
    // A playlist hit names its curator as artist_name, as GET /search does.
    const lists = playlists.map(({ tracks, track_count, curator_name, ...p }) => ({ ...p, artist_name: curator_name }));
    let groups = { songs: ranked(songs), albums: ranked(albums), playlists: ranked(lists), artists: ranked(artists) };
    if (!Object.values(groups).some((g) => g.length)) groups = { songs, albums, playlists: lists, artists };
    const page = (arr, strip) => {
      const rows = arr.slice(offset, offset + limit).map(strip);
      return { items: rows, ...(offset + limit < arr.length ? { next_offset: offset + limit } : {}) };
    };
    const marked = (x, hit) => {
      const ref = resourceOf(x);
      const mark = ref ? records.mark(ref, x.type === 'song' ? x.id : '') : null;
      return mark ? { ...hit, resource: mark } : hit;
    };
    const song = ({ album_id, track_number, quality, ...s }) => marked({ ...s, album_id }, s);
    const album = ({ tracks, quality, artist_id, ...a }) => marked(a, a);
    const out = { term, storefront: STOREFRONT };
    if (want('songs')) out.songs = page(groups.songs, song);
    if (want('albums')) out.albums = page(groups.albums, album);
    if (want('playlists')) out.playlists = page(groups.playlists, (p) => marked(p, p));
    if (want('artists')) out.artists = page(groups.artists, (a) => a);
    return json(200, out);
  }

  function artistAlbums(id, q) {
    const artist = find(id);
    if (!artist || artist.type !== 'artist') return apiError(404, 'catalog_not_found', `Apple Music has no artist ${id}`);
    const all = albums.filter((a) => a.artist_name === artist.name).map(({ tracks, quality, artist_id, ...a }) => {
      const mark = records.mark({ kind: 'album', id: a.id });
      return mark ? { ...a, resource: mark } : a;
    });
    const offset = Number(q.get('offset')) || 0;
    const limit = Number(q.get('limit')) || 25;
    return json(200, { storefront: STOREFRONT, artist, albums: { items: all.slice(offset, offset + limit), ...(offset + limit < all.length ? { next_offset: offset + limit } : {}) } });
  }

  function stats(q) {
    const days = Math.min(90, Number(q.get('days')) || 14);
    const r = rng(7);
    const daily = [];
    for (let d = days - 1; d >= 0; d--) {
      const t = d === 0 ? 0 : Math.floor(r() * 48) * (r() > 0.3 ? 1 : 0);
      daily.push({ date: iso(now - d * DAY).slice(0, 10), tracks_completed: t, bytes_written: t * 41000000, jobs_completed: Math.ceil(t / 11), jobs_failed: t && r() > 0.85 ? 1 : 0 });
    }
    for (const j of jobs.values()) {
      if (!j.finished_at) continue;
      const day = daily.find((x) => x.date === j.finished_at.slice(0, 10));
      if (!day) continue;
      day.tracks_completed += j.completed_items;
      day.bytes_written += (items.get(j.id) || []).reduce((s, it) => s + (it.file_size || 0), 0);
      if (j.status === 'completed') day.jobs_completed++;
      if (j.status === 'failed') day.jobs_failed++;
    }
    const tracks = daily.reduce((s, x) => s + x.tracks_completed, 0);
    return json(200, {
      generated_at: iso(Date.now()),
      days,
      totals: {
        jobs: daily.reduce((s, x) => s + x.jobs_completed + x.jobs_failed, 0),
        tracks_completed: tracks,
        tracks_skipped: 1,
        tracks_failed: 3,
        bytes_written: daily.reduce((s, x) => s + x.bytes_written, 0),
        duration_ms: tracks * 236000,
      },
      daily,
      hooks: [],
      codecs: [
        { codec: 'alac', bit_depth: 16, sample_rate: 44100, tracks: Math.round(tracks * 0.52) },
        { codec: 'alac', bit_depth: 24, sample_rate: 48000, tracks: Math.round(tracks * 0.24) },
        { codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: Math.round(tracks * 0.14) },
        { codec: 'ec3', bit_depth: 0, sample_rate: 48000, tracks: Math.round(tracks * 0.1) },
      ],
    });
  }

  function system() {
    const all = [...jobs.values()];
    const up = Math.round((Date.now() - (now - 3 * DAY)) / 1000);
    return json(200, {
      version: '2.0.0',
      commit: 'd3a0d3a0d3a0d3a0d3a0d3a0d3a0d3a0d3a0d3a0',
      build_date: '2026-10-30T00:00:00Z',
      go_version: 'go1.27.3',
      os: 'linux',
      arch: 'amd64',
      started_at: iso(now - 3 * DAY),
      uptime_seconds: up,
      simulate: false,
      motion_artwork_enabled: false,
      default_storefront: STOREFRONT,
      jobs: { workers: 3, active: all.filter((j) => j.status === 'running').length, queue_depth: all.filter((j) => j.status === 'queued').length, queue_capacity: 256, recovery_backlog: 0 },
      pools: { downloads: 16, decrypts: 4, wrapper_requests: 24 },
      tools: [{ name: 'ffmpeg', available: true, path: '/usr/bin/ffmpeg' }],
      storage: [
        { name: 'downloads_dir', path: '/app/data/downloads', free_bytes: 1214000000000, total_bytes: 3840000000000 },
        { name: 'temp_dir', path: '/app/data/tmp', free_bytes: 1214000000000, total_bytes: 3840000000000 },
      ],
      startup: {
        wrapper_address: 'http://wrapper:12340',
        listen: '0.0.0.0:18080',
        database_path: 'data/db/amdl.db',
        log_buffer_size: 2000,
        catalog_language: 'en-US',
        max_parallel_requests: 16,
      },
      pending_restart: [...pending].sort(),
    });
  }

  async function readBody(req) {
    if (req.body === undefined || req.body === null) return {};
    if (typeof req.body === 'object') return req.body;
    try {
      return JSON.parse(req.body) || {};
    } catch {
      return {};
    }
  }

  /**
   * Answers one request: {method, url, body, signal}, url being anything URL
   * parses, body a JSON string or object. Resolves to a Response; a path
   * outside /api/v1 answers 404.
   */
  async function handle(req) {
    const method = (req.method || 'GET').toUpperCase();
    const url = new URL(req.url, 'http://demo.invalid/');
    const at = url.pathname.indexOf('/api/v1/');
    if (at < 0) return apiError(404, 'not_found', 'not found');
    const path = url.pathname.slice(at);
    const q = url.searchParams;
    const record = path.match(/^\/api\/v1\/library\/(albums|playlists)\/([^/]+)(?:\/(download|reset|pause|resume))?$/);
    if (record) return recordRoute(method, record[1], decodeURIComponent(record[2]), record[3], await readBody(req));
    const appleKind = path.match(/^\/api\/v1\/apple-library\/(albums|songs|playlists|artists)$/);
    if (appleKind && method === 'GET') return json(200, records.appleList(appleKind[1], q));
    const appleTracks = path.match(/^\/api\/v1\/apple-library\/playlists\/([^/]+)\/tracks$/);
    if (appleTracks && method === 'GET') return json(200, records.appleTracks(decodeURIComponent(appleTracks[1])));
    const pendingAdd = path.match(/^\/api\/v1\/library\/pending\/([^/]+)(\/retry)?$/);
    if (pendingAdd) return apiError(404, 'resource_not_found', 'no pending add with this id');
    const account = path.match(/^\/api\/v1\/accounts\/([^/]+)$/);
    if (account && (method === 'PATCH' || method === 'DELETE')) {
      const acc = accounts.find((a) => a.id === decodeURIComponent(account[1]));
      if (!acc) return apiError(404, 'account_not_found', 'no account with this id');
      if (method === 'DELETE') {
        accounts.splice(accounts.indexOf(acc), 1);
        sortAccounts();
        return json(200, { deleted: acc.id, logged_out: true });
      }
      const body = await readBody(req);
      if (typeof body.library_sync === 'boolean') acc.library_sync = body.library_sync;
      if (typeof body.apple_library === 'boolean') acc.apple_library = body.apple_library;
      if (Number.isInteger(body.position) && body.position >= 0) {
        acc.position = body.position - 0.5;
        sortAccounts();
      }
      return json(200, acc);
    }
    const job = path.match(/^\/api\/v1\/downloads\/(job_[0-9a-f]+)(?:\/(cancel|retry|move|events))?$/);
    if (job) return jobRoute(method, job[1], job[2], q, req.signal, job[2] === 'move' ? await readBody(req) : undefined);
    const artist = path.match(/^\/api\/v1\/artists\/([^/]+)\/albums$/);
    if (artist && method === 'GET') return artistAlbums(decodeURIComponent(artist[1]), q);
    if (/^\/api\/v1\/wrapper\/(login|logout|login\/[^/]+\/2fa)$/.test(path) && method === 'POST') {
      return apiError(501, 'login_unsupported', 'this wrapper is wrapper-lite, which has no login API');
    }
    if (path === '/api/v1/hooks' || path.startsWith('/api/v1/hooks/')) {
      const out = hooks.handle(method, path, method === 'PUT' ? await readBody(req) : null);
      return json(out.status, out.body);
    }
    switch (`${method} ${path}`) {
      case 'GET /api/v1/health':
        return json(200, { status: 'ok' });
      case 'GET /api/v1/system':
        return system();
      case 'GET /api/v1/stats':
        return stats(q);
      case 'GET /api/v1/config':
        return json(200, configBody());
      case 'PUT /api/v1/config': {
        const body = await readBody(req);
        for (const [sec, vals] of Object.entries(body)) {
          if (!config[sec] || !vals || typeof vals !== 'object') continue;
          for (const [k, v] of Object.entries(vals)) if (RESTART_KEYS.has(`${sec}.${k}`) && JSON.stringify(config[sec][k]) !== JSON.stringify(v)) pending.add(`${sec}.${k}`);
          config[sec] = { ...config[sec], ...vals };
        }
        return json(200, configBody());
      }
      case 'POST /api/v1/config/reload':
        return json(200, configBody());
      case 'POST /api/v1/system/restart':
        pending.clear();
        return json(202, { restarting: true });
      case 'GET /api/v1/accounts':
        return json(200, { accounts });
      case 'GET /api/v1/library-sync':
        return json(200, librarySyncBody());
      case 'POST /api/v1/library-sync/reset': {
        const body = await readBody(req);
        const acc = body.account ? accounts.find((a) => a.id === body.account) : accounts[0];
        if (!acc) return apiError(404, 'account_not_found', 'no account with this id');
        const cleared = acc.default ? librarySync.anchor_size : 0;
        if (acc.default) librarySync.anchor_size = 0;
        return json(200, { account: acc.id, cleared, status: librarySyncBody() });
      }
      case 'GET /api/v1/wrapper/status':
        return json(200, { ready: true, logged_in: true, regions: [...new Set(accounts.map((a) => a.region))], login_supported: false, token_set: false });
      case 'GET /api/v1/logs': {
        const match = logMatcher(q);
        const after = Number(q.get('cursor')) || 0;
        const limit = Math.min(1000, Number(q.get('limit')) || 200);
        const rows = logs.filter((l) => l.sequence > after && match(l)).slice(-limit);
        return json(200, { logs: rows, next_cursor: nextLog - 1, oldest_sequence: 1, truncated: false });
      }
      case 'GET /api/v1/logs/stream': {
        const match = logMatcher(q);
        const after = Number(q.get('cursor')) || 0;
        return stream(req.signal, ({ send }) => {
          for (const l of logs) if (l.sequence > after && match(l)) send(l.sequence, 'log', l);
          const s = { match, send };
          logStreams.add(s);
          return () => logStreams.delete(s);
        });
      }
      case 'GET /api/v1/downloads': {
        const all = filteredJobs(q);
        const limit = Math.min(200, Number(q.get('limit')) || 50);
        const offset = Number(q.get('offset')) || 0;
        return json(200, { downloads: all.slice(offset, offset + limit).map(withProgress), total: all.length, limit, offset, status_counts: statusCounts(all), event_cursor: nextEvent - 1 });
      }
      case 'POST /api/v1/downloads':
        return submit(await readBody(req));
      case 'GET /api/v1/downloads/events':
        return stream(req.signal, ({ send }) => {
          feeds.add(send);
          return () => feeds.delete(send);
        });
      case 'POST /api/v1/lookup':
        return lookup(await readBody(req));
      case 'POST /api/v1/quality':
        return quality(await readBody(req));
      case 'GET /api/v1/search':
        return search(q);
      case 'GET /api/v1/library/summary':
        return json(200, records.summary());
      case 'GET /api/v1/library/albums':
        return json(200, records.list('albums', q));
      case 'GET /api/v1/library/playlists':
        return json(200, records.list('playlists', q));
      case 'POST /api/v1/library/add':
        return submit(await readBody(req));
      case 'POST /api/v1/library/reset': {
        const body = await readBody(req);
        let reset = 0;
        let skipped = 0;
        for (const rec of records.all()) {
          const x = records.record(rec);
          const flags = (body.filter && body.filter.flag) || [];
          if (flags.length && !(flags.includes('failed') && x.tracks_failed) && !(flags.includes('paused') && rec.paused)) continue;
          if (x.active_job_id) skipped++;
          else if (recordRoute('POST', `${rec.kind}s`, rec.id, 'reset').status === 202) reset++;
        }
        return json(200, { reset, skipped_active: skipped });
      }
      case 'GET /api/v1/library/pending':
        return json(200, { items: [] });
      case 'GET /api/v1/library/check':
        return json(200, { check: records.check() });
      case 'POST /api/v1/library/check':
        return json(202, { check: records.startCheck(await readBody(req)) });
      case 'GET /api/v1/apple-library/status':
        return json(200, appleStatusBody());
      case 'POST /api/v1/apple-library/sync': {
        const body = await readBody(req);
        const acc = body.account ? accounts.find((a) => a.id === body.account) : accounts[0];
        if (!acc) return apiError(404, 'account_not_found', 'no account with this id');
        return json(202, appleAccount(acc));
      }
    }
    return apiError(404, 'not_found', `no route ${method} ${path}`);
  }

  return { handle, tick, jobs, items };
}

/**
 * A fetch for the demo page: /api/v1 on this origin goes to `api`, other
 * same-origin requests (the console's own files) to the real fetch, and any
 * other host fails as a network error would, so nothing leaves the page.
 */
export function createDemoFetch({ api, fetch, origin }) {
  return async function demoFetch(input, init = {}) {
    const isRequest = typeof Request !== 'undefined' && input instanceof Request;
    const url = new URL(isRequest ? input.url : String(input), origin);
    if (url.origin !== new URL(origin).origin) throw new TypeError(`The AMDL demo does not reach other hosts (${url.host}).`);
    if (!url.pathname.includes('/api/v1/')) return fetch(input, init);
    const method = init.method || (isRequest ? input.method : 'GET');
    const body = init.body !== undefined ? init.body : isRequest && method !== 'GET' ? await input.text() : undefined;
    const signal = init.signal || (isRequest ? input.signal : undefined);
    if (signal && signal.aborted) throw new DOMException('The request was aborted.', 'AbortError');
    return api.handle({ method, url: url.href, body, signal });
  };
}
