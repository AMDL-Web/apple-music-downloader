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
        const running = [...w.jobs.values()].filter((j) => j.status === 'running').length;
        if (running < 3) {
          Object.assign(job, { status: 'running', started_at: iso(Date.now()), updated_at: iso(Date.now()) });
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
