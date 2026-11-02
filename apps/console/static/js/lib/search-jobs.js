// Pure helpers for a catalog search hit's (lib/search.js) queue entry and
// what its card shows: the key a hit is tracked by, its earlier or
// just-submitted job kept live by the feed, how much of it is on disk, and
// what a submitted hit's result row shows until its job resolves. No DOM.

import { diskMark } from './on-disk.js';

/** The key a hit is tracked by: its type and id, as a job of it is. */
export const hitKey = (hit) => (hit ? `${hit.type}:${hit.id}` : '');

const ACTIVE = new Set(['queued', 'running']);

/**
 * A hit's queue entry, as lib/lookup.js priorJob reports it: {state: 'active'|'done', job} or null. `sent` maps a
 * hitKey to the id of a job this page submitted for it; `jobs` maps a job id
 * to its latest snapshot (submit results and the feed), which wins over the
 * hit's prior_job from the search. A job that failed, was cancelled or was
 * deleted ({deleted: true}) marks nothing.
 */
export function hitPrior(hit, sent = new Map(), jobs = new Map()) {
  if (!hit) return null;
  const prior = hit.prior_job && hit.prior_job.id ? hit.prior_job : null;
  const id = sent.get(hitKey(hit)) || (prior ? prior.id : '');
  if (!id) return null;
  const job = jobs.get(id) || (prior && prior.id === id ? prior : null);
  if (!job || job.deleted) return null;
  if (ACTIVE.has(job.status)) return { state: 'active', job };
  if (job.status === 'completed') return { state: 'done', job };
  return null;
}

/**
 * The library state a job leaves behind, for a run this page saw end: its
 * finished tracks (downloaded or already there) are on disk, out of
 * total_items. Null for a job with no tracks.
 */
export function jobLibrary(job) {
  const total = (job && job.total_items) || 0;
  if (!total) return null;
  const n = Math.min(total, (job.completed_items || 0) + (job.skipped_items || 0));
  return { state: n === total ? 'complete' : n > 0 ? 'partial' : 'none', tracks_on_disk: n, tracks_total: total };
}

/**
 * What a hit's card shows (lib/on-disk.js diskMark): its job while queued
 * or running, else how much of it is on disk. That is the hit's `library`
 * from the search (album hits only), unless this page watched its job
 * finish since (a snapshot in `jobs` that completed), whose tracks are
 * newer news.
 */
export function hitMark(hit, sent = new Map(), jobs = new Map()) {
  const prior = hitPrior(hit, sent, jobs);
  const seen = prior && prior.state === 'done' && jobs.has(prior.job.id) ? jobLibrary(prior.job) : null;
  return diskMark(prior, seen || (hit && hit.library));
}

/**
 * Applies feed messages to `jobs` (a job id -> snapshot map) for the ids in
 * `watched`: an upsert replaces the snapshot unless it is older, a delete
 * marks it {deleted: true}. Returns `jobs` itself when nothing changed.
 */
export function trackJobs(jobs, batch, watched) {
  let next = jobs;
  const put = (id, value) => {
    if (next === jobs) next = new Map(jobs);
    next.set(id, value);
  };
  for (const msg of batch || []) {
    if (!msg) continue;
    if (msg.type === 'download_deleted' && watched.has(msg.job_id)) {
      put(msg.job_id, { id: msg.job_id, deleted: true });
      continue;
    }
    const job = msg.type === 'download_upserted' ? msg.job : null;
    if (!job || !job.id || !watched.has(job.id)) continue;
    const have = next.get(job.id);
    if (have && !have.deleted && (Date.parse(job.updated_at || '') || 0) < (Date.parse(have.updated_at || '') || 0)) continue;
    put(job.id, job);
  }
  return next;
}

/** The preview a submitted hit's result row shows until its job resolves (lib/submit.js previewSeed). */
export function hitPreview(hit) {
  if (!hit) return null;
  const out = { title: hit.name, type: hit.type };
  for (const key of ['artist_name', 'artwork_url', 'release_date', 'genre', 'artwork_bg_color', 'artwork_text_color1', 'artwork_text_color2', 'artwork_text_color3', 'artwork_text_color4']) {
    if (hit[key]) out[key] = hit[key];
  }
  if (hit.type === 'artist') out.artist_name = hit.name;
  return out;
}

/**
 * The job a submit result puts a hit in the queue with, for hitPrior: the new
 * job of an accepted URL, or the job already downloading it
 * (duplicate_active), as {id, status}; null otherwise.
 */
export function sentJob(result) {
  if (!result) return null;
  if (result.status === 'accepted' && result.job && result.job.id) return result.job;
  if (result.status === 'duplicate_active' && result.existing_job_id) return { id: result.existing_job_id, status: 'queued' };
  return null;
}
