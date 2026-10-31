// A stand-in for `amdl serve` that the audit drives: it serves
// apps/console/static under /console/ with the Go server's CSP, and answers
// /api/v1 from an in-memory world (fixtures.mjs), streams included. Worlds,
// latency and faults are switched per scenario, and `tick()` moves running
// jobs one deterministic step, so a live page reaches the same states every
// run.
//
//   node apps/console/tools/ui-audit/console/mock-server.mjs [--port 18571] [--world active]
//
// opens it for a look by hand; POST /__mock/{world,tick,fault} steer it. A
// fault or latency keyed `GET /api/v1/search?offset` hits only the later
// pages of a search group shown alone. GET /__mock/hook-writes lists the
// hooks writes it received.

import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
import { WORLDS, CATALOG, stageFields, findAlbum, makeJob, iso } from './fixtures.mjs';
import { TERMINAL, withProgress, statusCounts, list, advance, recount } from './mock-world.mjs';
import { search, artistAlbums } from './mock-search.mjs';
import { stats, system } from './fixtures-status.mjs';
import { json, apiError, readBody, parseLink } from './mock-http.mjs';
import { lookup, quality } from './mock-links.mjs';
import { createHookStore } from '../../../../site/demo/fake-hooks.js';

const STATIC_DIR = fileURLToPath(new URL('../../../static/', import.meta.url));
const CONSOLE_GO = fileURLToPath(new URL('../../../console.go', import.meta.url));

/** The CSP the Go server sends, read from console.go so the two cannot drift. */
export function readCsp(src = readFileSync(CONSOLE_GO, 'utf8')) {
  const m = src.match(/const ContentSecurityPolicy = ((?:"[^"]*"\s*\+?\s*)+)/);
  if (!m) throw new Error('ContentSecurityPolicy not found in console.go');
  return [...m[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]).join('');
}

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.webmanifest': 'application/manifest+json',
};

const MILESTONES = new Set(['job_queued', 'job_started', 'resolved_input', 'job_finished', 'job_failed', 'job_cancelled', 'job_retried', 'item_completed', 'item_failed', 'item_skipped', 'job_deleted']);

export async function startMockServer({ port = 0, host = '127.0.0.1', world = 'active', staticDir = STATIC_DIR } = {}) {
  const csp = readCsp();
  const feeds = new Set();
  const jobStreams = new Map();
  const logStreams = new Set();
  const state = { world: null, latency: 0, latencies: {}, faults: {} };

  function sse(res) {
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', 'X-Accel-Buffering': 'no' });
    res.write(': open\n\n');
  }
  const send = (res, id, name, data) => res.write(`id: ${id}\nevent: ${name}\ndata: ${JSON.stringify(data)}\n\n`);

  function setWorld(name, opts = {}) {
    const make = typeof name === 'function' ? name : Object.hasOwn(WORLDS, name) ? WORLDS[name] : undefined;
    if (!make) throw new Error(`unknown world ${name}`);
    const w = make(Date.now());
    Object.assign(w, opts);
    w.onEvent = (ev) => {
      for (const res of jobStreams.get(ev.job_id) || []) send(res, ev.id, ev.type, ev);
      if (MILESTONES.has(ev.type)) {
        const job = w.jobs.get(ev.job_id);
        const msg = job
          ? { type: 'download_upserted', job: withProgress(w, job), event_id: ev.id, status_counts: statusCounts(w) }
          : { type: 'download_deleted', job_id: ev.job_id, event_id: ev.id, status_counts: statusCounts(w) };
        for (const res of feeds) send(res, ev.id, msg.type, msg);
      }
    };
    w.onLog = (l) => {
      for (const s of logStreams) if (s.match(l)) send(s.res, l.sequence, 'log', l);
    };
    // Streams opened on the previous world end, as on a server restart.
    for (const res of feeds) res.end();
    for (const set of jobStreams.values()) for (const res of set) res.end();
    for (const s of logStreams) s.res.end();
    feeds.clear();
    jobStreams.clear();
    logStreams.clear();
    state.world = w;
    state.faults = { ...(opts.faults || {}) };
    state.latencies = { ...(opts.latencies || {}) };
    state.latency = opts.latency ?? 0;
    return w;
  }

  /** Advances every running or queued job one step; returns how many items moved. */
  function tick(steps = 1) {
    const w = state.world;
    const moved = advance(w, steps);
    // The feed pushes a running job again every 2 s while its progress moves,
    // between the milestones.
    const last = w.events.length ? w.events[w.events.length - 1].id : 0;
    for (const job of w.jobs.values()) {
      if (job.status !== 'running' || !job.title) continue;
      const msg = { type: 'download_upserted', job: withProgress(w, job), event_id: last, status_counts: statusCounts(w) };
      for (const res of feeds) send(res, last, msg.type, msg);
    }
    return moved;
  }

  async function serveStatic(req, res, path) {
    let rel = path.replace(/^\/console\/?/, '');
    if (rel === '' || rel.endsWith('/')) rel += 'index.html';
    const file = normalize(join(staticDir, decodeURIComponent(rel)));
    if (!file.startsWith(normalize(staticDir))) return apiError(res, 404, 'not_found', 'not found');
    try {
      const body = await readFile(file);
      res.writeHead(200, {
        'Content-Type': TYPES[extname(file)] || 'application/octet-stream',
        'Content-Security-Policy': csp,
        'X-Content-Type-Options': 'nosniff',
        'Cache-Control': 'no-cache',
      });
      res.end(body);
    } catch {
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
    }
  }

  function filteredJobs(q) {
    const w = state.world;
    const statuses = q.getAll('status').flatMap(list);
    const types = q.getAll('type').flatMap(list);
    const term = (q.get('q') || '').toLowerCase();
    let out = [...w.jobs.values()].filter((j) => {
      if (statuses.length && !statuses.includes(j.status)) return false;
      if (types.length && !types.includes(j.type)) return false;
      if (term && !`${j.title || ''}\n${j.input}\n${j.artist_name || ''}`.toLowerCase().includes(term)) return false;
      return true;
    });
    const sort = q.get('sort') || 'created_at';
    const dir = q.get('order') === 'asc' ? 1 : -1;
    out.sort((a, b) => dir * (Date.parse(a[sort]) - Date.parse(b[sort])) || dir * a.id.localeCompare(b.id));
    return out;
  }

  async function api(req, res, url) {
    const w = state.world;
    const path = url.pathname;
    const q = url.searchParams;
    const key = `${req.method} ${path.replace(/job_[0-9a-f]{32}/, '{id}').replace(/^\/api\/v1\/hooks\/(?!presets$)[^/]+/, '/api/v1/hooks/{name}')}`;
    // `GET /api/v1/search?offset` names a later page of a search alone.
    const paged = q.get('offset') ? `${key}?offset` : '';
    const fault = (paged && state.faults[paged]) || state.faults[key] || state.faults[`${req.method} ${path}`];
    // A scenario's latency, capped so that no request can hold the server.
    const wait = Math.min(Number((paged ? state.latencies[paged] : undefined) ?? state.latencies[key] ?? state.latency) || 0, 60_000);
    if (wait > 0) await new Promise((r) => setTimeout(r, wait));
    if (fault) {
      if (fault.hang) return;
      return apiError(res, fault.status || 500, fault.code || 'internal_error', fault.message || 'Injected failure', fault.details);
    }
    const m = path.match(/^\/api\/v1\/downloads\/(job_[0-9a-f]+)(\/(cancel|retry|events))?$/);
    if (m) return jobRoute(req, res, m[1], m[3], q);
    if (path === '/api/v1/hooks' || path.startsWith('/api/v1/hooks/')) {
      // hooks.yaml as the server keeps it (fake-hooks.js); the world's
      // `hooks` seeds it, and every write is kept for /__mock/hook-writes.
      w.hookStore ||= createHookStore(w.hooks);
      const out = w.hookStore.handle(req.method, path, req.method === 'PUT' ? await readBody(req) : null);
      return json(res, out.status, out.body);
    }
    const am = path.match(/^\/api\/v1\/artists\/([^/]+)\/albums$/);
    if (am) return artistAlbums(res, am[1], q);
    switch (`${req.method} ${path}`) {
      case 'GET /api/v1/health':
        return json(res, w.health.status === 'ok' ? 200 : 503, w.health);
      case 'GET /api/v1/system':
        return json(res, 200, system(w));
      case 'GET /api/v1/stats':
        return json(res, 200, stats(w, Number(q.get('days')) || 14));
      case 'GET /api/v1/config':
        return json(res, 200, { config: w.config, persisted: true });
      case 'PUT /api/v1/config': {
        const body = (await readBody(req)) || {};
        for (const [sec, vals] of Object.entries(body)) w.config[sec] = { ...w.config[sec], ...vals };
        return json(res, 200, { config: w.config, persisted: true });
      }
      case 'POST /api/v1/config/reload':
        return json(res, 200, { config: w.config, persisted: true, ...(w.reloadError ? { reload_error: w.reloadError } : {}) });
      case 'GET /api/v1/library-sync':
        return json(res, 200, w.librarySync);
      case 'POST /api/v1/library-sync/reset':
        w.librarySync.anchor_size = 0;
        return json(res, 200, w.librarySync);
      case 'GET /api/v1/wrapper/status':
        return json(res, 200, w.wrapper);
      case 'POST /api/v1/wrapper/login':
        return json(res, 200, { status: 'needs_2fa', login_id: 'login_1' });
      case 'POST /api/v1/wrapper/logout':
        return json(res, 200, { status: 'logged_out' });
      case 'GET /api/v1/logs': {
        const entries = logMatcher(q);
        const limit = Number(q.get('limit')) || 200;
        const rows = w.logs.filter(entries).slice(-limit);
        return json(res, 200, { logs: rows, next_cursor: w.logs.length, oldest_sequence: 1, truncated: false });
      }
      case 'GET /api/v1/logs/stream': {
        sse(res);
        const s = { res, match: logMatcher(q) };
        const after = Number(q.get('cursor')) || 0;
        for (const l of w.logs) if (l.sequence > after && s.match(l)) send(res, l.sequence, 'log', l);
        logStreams.add(s);
        req.on('close', () => logStreams.delete(s));
        return;
      }
      case 'GET /api/v1/downloads': {
        const all = filteredJobs(q);
        const limit = Math.min(200, Number(q.get('limit')) || 50);
        const offset = Number(q.get('offset')) || 0;
        return json(res, 200, { downloads: all.slice(offset, offset + limit).map((j) => withProgress(w, j)), total: all.length, limit, offset, status_counts: statusCounts(w), event_cursor: w.cursor });
      }
      case 'POST /api/v1/downloads':
        return submit(req, res);
      case 'GET /api/v1/downloads/events': {
        sse(res);
        feeds.add(res);
        req.on('close', () => feeds.delete(res));
        return;
      }
      case 'POST /api/v1/lookup':
        return lookup(req, res);
      case 'POST /api/v1/quality':
        return quality(req, res);
      case 'GET /api/v1/search':
        return search(state.world, res, q);
    }
    return apiError(res, 404, 'not_found', `no route ${req.method} ${path}`);
  }

  function logMatcher(q) {
    const levels = list(q.get('level'));
    const comp = q.get('component');
    const jobId = q.get('job_id');
    const term = (q.get('q') || '').toLowerCase();
    return (l) =>
      (!levels.length || levels.includes(l.level)) &&
      (!comp || l.attributes.component === comp) &&
      (!jobId || l.attributes.job_id === jobId) &&
      (!term || JSON.stringify(l).toLowerCase().includes(term));
  }

  async function jobRoute(req, res, id, action, q) {
    const w = state.world;
    const job = w.jobs.get(id);
    if (!job) return apiError(res, 404, 'not_found', 'job not found');
    if (!action && req.method === 'GET') {
      return json(res, 200, { job: withProgress(w, job), items: w.items.get(id) || [], hooks: [], event_cursor: w.cursor });
    }
    if (!action && req.method === 'DELETE') {
      if (!TERMINAL.has(job.status)) return apiError(res, 409, 'job_active', 'job is still active');
      w.jobs.delete(id);
      w.addEvent({ job_id: id, type: 'job_deleted', message: 'job deleted' });
      return json(res, 200, { deleted: true });
    }
    if (action === 'cancel') {
      Object.assign(job, { status: 'cancelled', finished_at: iso(Date.now()), updated_at: iso(Date.now()) });
      for (const it of w.items.get(id) || []) if (!TERMINAL.has(it.status) && it.status !== 'skipped_existing') Object.assign(it, stageFields('cancelled', 0));
      recount(job, w.items.get(id) || []);
      w.addEvent({ job_id: id, type: 'job_cancelled', message: 'job cancelled', payload: { ...job } });
      return json(res, 200, job);
    }
    if (action === 'retry') {
      Object.assign(job, { status: 'queued', updated_at: iso(Date.now()) });
      delete job.error;
      delete job.finished_at;
      for (const it of w.items.get(id) || []) if (it.status === 'failed' || it.status === 'cancelled') Object.assign(it, stageFields('queued', 0));
      recount(job, w.items.get(id) || []);
      w.addEvent({ job_id: id, type: 'job_retried', message: 'job retried', payload: { ...job } });
      return json(res, 202, job);
    }
    if (action === 'events') {
      sse(res);
      const after = Number(q.get('cursor')) || 0;
      for (const ev of w.events) if (ev.job_id === id && ev.id > after) send(res, ev.id, ev.type, ev);
      if (TERMINAL.has(job.status)) return res.end();
      const set = jobStreams.get(id) || new Set();
      set.add(res);
      jobStreams.set(id, set);
      req.on('close', () => set.delete(res));
      return;
    }
    return apiError(res, 405, 'method_not_allowed', 'method not allowed');
  }

  async function submit(req, res) {
    const w = state.world;
    const body = (await readBody(req)) || {};
    const results = [];
    let accepted = 0;
    for (const u of body.urls || []) {
      const link = parseLink(u);
      if (!link) {
        results.push({ url: u, status: 'invalid', error: { code: 'invalid_url', message: 'not an Apple Music link' } });
        continue;
      }
      const a = findAlbum(link.id) || CATALOG.albums[0];
      const job = makeJob({ ...w, addEvent: () => {} }, { key: `submit:${u}:${w.events.length}`, album: a, status: 'queued', tracks: a.track_count || 8, createdAgo: 0, input: u });
      w.addEvent({ job_id: job.id, type: 'job_queued', message: 'job queued', payload: { ...job } });
      results.push({ url: u, status: 'accepted', job });
      accepted++;
    }
    const out = { accepted, rejected: results.length - accepted, results };
    if (!accepted) return apiError(res, 422, 'no_urls_accepted', 'No URL was accepted.', out);
    return json(res, 202, out);
  }

  async function control(req, res, url) {
    const body = (await readBody(req)) || {};
    if (url.pathname === '/__mock/world') {
      setWorld(body.name || 'active', body.opts || {});
      return json(res, 200, { ok: true, cursor: state.world.cursor });
    }
    if (url.pathname === '/__mock/hook-writes') return json(res, 200, state.world.hookStore ? state.world.hookStore.writes : []);
    if (url.pathname === '/__mock/tick') return json(res, 200, { moved: tick(body.steps || 1) });
    if (url.pathname === '/__mock/fault') {
      state.faults = body.faults || {};
      state.latencies = body.latencies || state.latencies;
      if (body.latency !== undefined) state.latency = body.latency;
      return json(res, 200, { ok: true });
    }
    return apiError(res, 404, 'not_found', 'unknown control');
  }

  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://x');
    if (url.pathname.startsWith('/__mock/')) return control(req, res, url);
    if (url.pathname.startsWith('/api/')) return api(req, res, url).catch((e) => apiError(res, 500, 'internal_error', `mock failed: ${e && e.message}`));
    if (url.pathname === '/' || url.pathname === '/console') {
      res.writeHead(302, { Location: '/console/' });
      return res.end();
    }
    if (url.pathname.startsWith('/console/')) return serveStatic(req, res, url.pathname);
    res.writeHead(404);
    res.end();
  });
  // Streams stay open; keep-alive pings stop the console's 45 s idle timer.
  const ping = setInterval(() => {
    for (const r of feeds) r.write(': keepalive\n\n');
    for (const set of jobStreams.values()) for (const r of set) r.write(': keepalive\n\n');
    for (const s of logStreams) s.res.write(': keepalive\n\n');
  }, 15000);
  setWorld(world);
  await new Promise((r) => server.listen(port, host, r));
  const addr = server.address();
  return {
    url: `http://${host}:${addr.port}/console/`,
    origin: `http://${host}:${addr.port}`,
    setWorld,
    tick,
    setFaults(faults = {}, { latency, latencies } = {}) {
      state.faults = faults;
      if (latency !== undefined) state.latency = latency;
      if (latencies) state.latencies = latencies;
    },
    get world() {
      return state.world;
    },
    close() {
      clearInterval(ping);
      for (const r of feeds) r.end();
      for (const set of jobStreams.values()) for (const r of set) r.end();
      for (const s of logStreams) s.res.end();
      server.closeAllConnections?.();
      return new Promise((r) => server.close(r));
    },
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const opt = { port: 18571, world: 'active' };
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    const v = args[i + 1];
    if ((a === '--port' || a === '--world' || a === '--host') && v && !v.startsWith('--')) {
      opt[a.slice(2)] = a === '--port' ? Number(v) : v;
      i++;
    } else {
      process.stderr.write('usage: mock-server.mjs [--port N] [--host H] [--world empty|idle|active|many]\n');
      process.exit(2);
    }
  }
  if (!WORLDS[opt.world] || !Number.isInteger(opt.port)) {
    process.stderr.write('usage: mock-server.mjs [--port N] [--host H] [--world empty|idle|active|many]\n');
    process.exit(2);
  }
  const s = await startMockServer(opt);
  process.stdout.write(`mock amdl at ${s.url} (world ${opt.world})\n`);
}
