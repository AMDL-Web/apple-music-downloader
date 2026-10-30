// What a job list row and the job drawer say about a job. Pure: the views
// (views/queue-row.js, views/drawer*.js) only draw what these return.

import { jobTitle, jobSubtitle, typeLabel, isUnresolved, inputLabel, unresolvedHint, errorSummary, codecLabel, codecMeaning, bytes } from './format.js';
import { jobCounts, jobStatusView, activityView } from './status.js';

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** Whether a job's error says Apple has nothing at the link (a 404 in the text before any JSON body). */
export function isNotFound(error) {
  return /\b404\b/.test(String(error || '').split(/[{\n]/)[0]);
}

/**
 * The jobs of a list with running ones first, then queued ones, then the rest,
 * each group in the order given.
 */
export function activeFirst(jobs) {
  const rank = (j) => (j.status === 'running' ? 0 : j.status === 'queued' ? 1 : 2);
  if (jobs.every((j) => rank(j) === 2) || jobs.every((j) => rank(j) === rank(jobs[0]))) return jobs;
  return jobs
    .map((job, i) => ({ job, i, r: rank(job) }))
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .map((x) => x.job);
}

/**
 * One row of a job list.
 * {title, mono, sub, reason, progress, iconOnly, view}: `mono` is a link
 * standing in for a title; `sub` the second line ("Artist", plus the type
 * unless it is an album, the storefront unless it is `defaultStorefront`, and
 * the track count of a finished job); `reason` a failed job's one-line error;
 * `progress` whether the row draws the progress bar (queued and running only);
 * `iconOnly` whether the status is the icon alone (a plain completed job).
 */
export function rowView(job, defaultStorefront = '') {
  const c = jobCounts(job);
  const view = jobStatusView(job);
  const failed = job.status === 'failed';
  const progress = job.status === 'queued' || job.status === 'running';
  const unresolved = isUnresolved(job);
  const parts = [];
  let title;
  let mono = false;
  if (unresolved && failed) {
    title = isNotFound(job.error) ? 'Link not found' : "Couldn't resolve this link";
    parts.push(inputLabel(job.input));
  } else if (unresolved) {
    title = inputLabel(job.input) || jobTitle(job);
    mono = true;
    parts.push(unresolvedHint(job));
  } else {
    title = jobTitle(job);
    if (jobSubtitle(job)) parts.push(jobSubtitle(job));
    if (job.type && job.type !== 'album') parts.push(typeLabel(job.type));
    if (job.storefront && defaultStorefront && job.storefront.toLowerCase() !== defaultStorefront.toLowerCase()) parts.push(job.storefront.toUpperCase());
    if (!progress && c.total > 0) {
      if (job.status === 'completed' && c.failed === 0) parts.push(plural(c.total, 'track', 'tracks'));
      else if (c.done > 0 || c.failed > 0) parts.push(`${c.done} of ${plural(c.total, 'track', 'tracks')}${c.failed > 0 ? `, ${c.failed} failed` : ''}`);
    }
  }
  return {
    title,
    mono,
    sub: parts.join(' · '),
    reason: failed ? errorSummary(job.error) : '',
    progress,
    iconOnly: job.status === 'completed' && view.label === 'Completed' && c.failed === 0,
    view,
  };
}

/** The most common codec chip text of a track list ("ALAC 24/44.1"), '' when none carries a codec. */
export function albumCodec(items) {
  const n = new Map();
  for (const it of items || []) {
    const label = codecLabel(it);
    if (label) n.set(label, (n.get(label) || 0) + 1);
  }
  let best = '';
  let top = 0;
  for (const [label, k] of n) if (k > top) [best, top] = [label, k];
  return best;
}

/**
 * The track list's key to its chips: the codec most tracks carry, spelled out
 * ({label: "ALAC 24/48", text: "Lossless 24-bit / 48 kHz"}); the rows mark only
 * the tracks that differ. null when no track carries a codec.
 */
export function codecNote(items) {
  const label = albumCodec(items);
  if (!label) return null;
  return { label, text: codecMeaning((items || []).find((it) => codecLabel(it) === label)) };
}

/** The distinct codec chips of a track list, in first-seen order. */
export function codecKinds(items) {
  return [...new Set((items || []).map(codecLabel).filter(Boolean))];
}

/**
 * The drawer's one-line summary of a job from its tracks: "ALAC 24/44.1 ·
 * 14 tracks · 692 MB". The format is "Mixed formats" when tracks differ, and
 * the size is left out until a file exists. '' for a job without tracks.
 */
export function drawerSummary(items) {
  if (!items || items.length === 0) return '';
  const kinds = codecKinds(items);
  const size = items.reduce((n, it) => n + (it.file_size > 0 ? it.file_size : 0), 0);
  return [kinds.length === 1 ? kinds[0] : kinds.length > 1 ? 'Mixed formats' : '', plural(items.length, 'track', 'tracks'), size > 0 ? bytes(size) : '']
    .filter(Boolean)
    .join(' · ');
}

const SETTLED = ['completed'];

/**
 * What a track row shows beyond number, title, length and size: `artist`
 * when it differs from the album artist, `codec` when its chip differs from
 * the album's, `status` while the track is working or has anything but a
 * plain completion to report (failed, skipped, cancelled, waiting), and for
 * every track while `keepStatus` is set (a job that was live in this drawer,
 * so finished rows keep their height instead of collapsing one by one).
 */
export function trackView(item, albumArtist, modeCodec, keepStatus = false) {
  const chip = codecLabel(item);
  return {
    artist: item.artist && item.artist !== albumArtist ? item.artist : '',
    codec: chip && chip !== modeCodec ? chip : '',
    status: keepStatus || !SETTLED.includes(item.status),
  };
}

/**
 * The fraction of the measurable stage a working track is in, as "35%", for
 * its size column until the file exists; '' while the stage has no fraction
 * (resolving, remuxing, tagging, saving, a transfer of unknown size).
 */
export function stagePercent(item) {
  const p = (item && item.progress) || {};
  const status = item && item.status;
  const f = status === 'downloading' ? p.download : status === 'decrypting' ? p.decrypt : undefined;
  return typeof f === 'number' && f > 0 && f < 1 ? `${Math.round(f * 100)}%` : '';
}

const WORKING = ['resolving', 'downloading', 'decrypting', 'remuxing', 'tagging', 'saving'];

/**
 * The status message beside a track's status, or ''. A finished track has
 * none, nor has a message that only repeats the status label, or that only
 * restates the progress of a working track ("decrypting 35%", "ALAC
 * download 53%" under Downloading: the bar and the size column show it).
 */
export function trackNote(item, statusLabel) {
  if (!item || ['completed', 'skipped_existing'].includes(item.status)) return '';
  const msg = String(item.status_message || '').trim();
  const label = String(statusLabel || '').trim().toLowerCase();
  const plain = msg.replace(/[.\s]+$/, '').toLowerCase();
  if (!msg || plain === label) return '';
  if (WORKING.includes(item.status) && ((label && plain.startsWith(label)) || /\d%$/.test(plain))) return '';
  return msg;
}

/**
 * The drawer's Activity as {entry, view} rows, oldest first: "Gave up" right
 * before the "Failed" that carries the same text is one failure, so it is
 * dropped and only "Failed" stays.
 */
export function activityRows(activity) {
  const rows = (activity || []).map((entry) => ({ entry, view: activityView(entry) }));
  return rows.filter((r, i) => {
    const next = rows[i + 1];
    return !(r.entry.type === 'operation_exhausted' && next && next.entry.type === 'job_failed' && r.view.text === next.view.text);
  });
}
