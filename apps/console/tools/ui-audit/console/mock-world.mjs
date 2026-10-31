// The mock server's world steps: a job as the API sends it, the status
// counts, and one deterministic step of every running or queued job.

import { albumProgress } from '../../../static/js/lib/album-progress.js';
import { CATALOG, CODECS, STAGE_ORDER, stageFields, downloadBytes, makeJob, iso } from './fixtures.mjs';

export const TERMINAL = new Set(['completed', 'failed', 'cancelled']);

/** A job as the API sends it: with progress, derived from its items like the server's internal/progress#Overall. */
export function withProgress(world, job) {
  return { ...job, progress: albumProgress(job.status, world.items.get(job.id) || []).percent / 100 };
}

export function statusCounts(world) {
  const c = { queued: 0, running: 0, completed: 0, failed: 0, cancelled: 0, total: 0 };
  for (const j of world.jobs.values()) {
    c[j.status]++;
    c.total++;
  }
  return c;
}

export function list(v) {
  if (!v) return [];
  return String(v)
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Advances every running or queued job `steps` steps; returns how many items moved. */
export function advance(w, steps = 1) {
  let moved = 0;
  for (let s = 0; s < steps; s++) {
    for (const job of [...w.jobs.values()]) {
      if (job.status === 'queued') {
        // The worker that frees up takes the queued job with the lowest queue_order.
        const running = [...w.jobs.values()].filter((j) => j.status === 'running').length;
        if (running < 3 && queuedInOrder(w)[0] === job) {
          Object.assign(job, { status: 'running', started_at: iso(Date.now()), updated_at: iso(Date.now()) });
          delete job.queue_order;
          w.addEvent({ job_id: job.id, type: 'job_started', message: 'job started', payload: { ...job } });
          moved++;
        }
        continue;
      }
      if (job.status !== 'running') continue;
      const items = w.items.get(job.id);
      if (!job.title) {
        const a = CATALOG.albums[(job.id.charCodeAt(8) + 5) % CATALOG.albums.length];
        const tmp = { now: Date.now(), jobs: new Map(), items: new Map(), addEvent: () => {} };
        const fresh = makeJob(tmp, { key: `${job.id}:resolved`, id: job.id, album: a, status: 'running', tracks: 8, stages: () => 'queued' });
        Object.assign(job, { ...fresh, input: job.input, created_at: job.created_at, started_at: job.started_at, updated_at: iso(Date.now()) });
        w.items.set(job.id, tmp.items.get(job.id));
        w.addEvent({ job_id: job.id, type: 'resolved_input', message: job.type, payload: { ...job } });
        moved++;
        continue;
      }
      let inFlight = items.filter((it) => !TERMINAL.has(it.status) && it.status !== 'skipped_existing' && it.status !== 'queued');
      while (inFlight.length < 2) {
        const next = items.find((it) => it.status === 'queued');
        if (!next) break;
        Object.assign(next, stageFields('resolving', 0), { updated_at: iso(Date.now()) });
        w.addEvent({ job_id: job.id, item_id: next.id, type: 'item_progress', phase: 'resolving', message: next.status_message, payload: { ...next } });
        inFlight = [...inFlight, next];
        moved++;
      }
      for (const it of inFlight) {
        const codecKey = it.codec === 'alac' ? `alac-${it.bit_depth}-${Math.floor(it.sample_rate / 1000)}` : it.codec || 'alac-24-48';
        let stage = it.status;
        let frac = stage === 'downloading' ? it.progress.download : stage === 'decrypting' ? it.progress.decrypt : 0;
        if (stage === 'downloading' && frac < 1) frac = Math.min(1, Math.round((frac + 0.2) * 100) / 100);
        else if (stage === 'decrypting' && frac < 1) frac = Math.min(1, frac + 0.5);
        else {
          stage = STAGE_ORDER[STAGE_ORDER.indexOf(stage) + 1] || 'completed';
          frac = 0;
          if (stage === 'downloading' && !it.codec) {
            Object.assign(it, stageFields('waiting_download', 0, 'alac-24-48'));
            w.addEvent({ job_id: job.id, item_id: it.id, type: 'codec_selected', phase: 'alac', payload: { ...it } });
          }
        }
        Object.assign(it, stageFields(stage, frac, CODECS[codecKey] ? codecKey : 'alac-24-48'), { updated_at: iso(Date.now()) });
        const size = Math.round((it.duration_ms / 1000) * ((it.bitrate || 1800000) / 8));
        it.progress.download_bytes = downloadBytes(stage, frac, size);
        if (stage === 'completed') {
          it.file_size = size;
          it.output_path = `/app/data/downloads/albums/${job.artist_name}/${job.title}/${String(it.index).padStart(2, '0')}. ${it.title}.m4a`;
          w.addEvent({ job_id: job.id, item_id: it.id, type: 'item_completed', message: 'saved', payload: { ...it } });
        } else {
          w.addEvent({ job_id: job.id, item_id: it.id, type: 'item_progress', phase: it.status, message: it.status_message, payload: { ...it } });
        }
        moved++;
      }
      recount(job, items);
      if (items.every((it) => TERMINAL.has(it.status) || it.status === 'skipped_existing')) {
        Object.assign(job, { status: job.failed_items ? 'failed' : 'completed', finished_at: iso(Date.now()), updated_at: iso(Date.now()) });
        w.addEvent({ job_id: job.id, type: job.status === 'failed' ? 'job_failed' : 'job_finished', message: 'job finished', payload: { ...job } });
      } else job.updated_at = iso(Date.now());
    }
  }
  return moved;
}

export function recount(job, items) {
  job.completed_items = items.filter((i) => i.status === 'completed').length;
  job.skipped_items = items.filter((i) => i.status === 'skipped_existing').length;
  job.failed_items = items.filter((i) => i.status === 'failed').length;
  job.cancelled_items = items.filter((i) => i.status === 'cancelled').length;
}

// The spacing of queue_order values, as the real store hands them out
// (apps/site/demo/fake-api.js uses the same).
const QUEUE_GAP = 2 ** 20;

/** The queued jobs in run order. */
export function queuedInOrder(w) {
  return [...w.jobs.values()].filter((j) => j.status === 'queued').sort((a, b) => a.queue_order - b.queue_order);
}

/** A queue_order behind every job's: the place of a new or retried job. */
export function nextOrder(w) {
  w.lastOrder = (w.lastOrder || 0) + QUEUE_GAP;
  return w.lastOrder;
}

/**
 * Gives a fresh world's queued jobs their queue_order, oldest first, after
 * adding `queued` more (a long queue to reorder; the newest last).
 */
export function seedQueue(w, queued = 0) {
  for (let i = 0; i < queued; i++) {
    makeJob(w, { key: `queue${i}`, album: (i * 5 + 60) % CATALOG.albums.length, status: 'queued', tracks: 6 + (i % 7), createdAgo: 8000 - i * 100 });
  }
  const queue = [...w.jobs.values()].filter((j) => j.status === 'queued').sort((a, b) => a.created_at.localeCompare(b.created_at));
  for (const job of queue) job.queue_order = nextOrder(w);
}

/**
 * POST /downloads/{id}/move as the server answers it ({status, body} or
 * {status, error: [code, message]}): only the moved job's queue_order
 * changes, unless no value is left between its new neighbours and the queue
 * is renumbered; each job whose value changed gets job_moved, the moved one
 * last. Every body received is kept in w.moves for GET /__mock/moves.
 */
export function moveJob(w, job, body) {
  body = body || {};
  (w.moves ||= []).push({ id: job.id, body });
  const fields = ['before', 'after', 'to'].filter((k) => typeof body[k] === 'string' && body[k] !== '');
  const unknown = Object.keys(body).filter((k) => !['before', 'after', 'to'].includes(k));
  if (fields.length !== 1 || unknown.length || (body.to && body.to !== 'first' && body.to !== 'last')) {
    return { status: 400, error: ['invalid_request', 'set exactly one of before, after and to (first or last)'] };
  }
  if (job.status !== 'queued') return { status: 409, error: ['job_not_queued', `job ${job.id} is not queued`] };
  const queue = queuedInOrder(w);
  const from = queue.indexOf(job);
  const rest = queue.filter((j) => j !== job);
  let to = rest.length;
  const anchorId = body.before || body.after;
  if (anchorId) {
    let at = rest.findIndex((j) => j.id === anchorId);
    if (anchorId === job.id) at = body.after ? from - 1 : from;
    else if (at < 0) return { status: 409, error: ['job_not_queued', `job ${anchorId} is not queued`] };
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
    w.lastOrder = Math.max(w.lastOrder || 0, job.queue_order);
    for (const j of order) w.addEvent({ job_id: j.id, type: 'job_moved', message: j === job ? 'job moved in the queue' : 'queue renumbered', payload: { ...j } });
  }
  return { status: 200, body: { job: withProgress(w, job) } };
}
