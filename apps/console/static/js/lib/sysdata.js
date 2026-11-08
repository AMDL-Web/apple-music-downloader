// Pure logic behind the Settings status heads: the error-log window, the hourly
// error chart, the library-sync countdown, the codec share and the worker cells.

import { codecLabel } from './format-codec.js';

export const LOG_WINDOW_MS = 24 * 60 * 60 * 1000;
export const LOG_FIRST_LIMIT = 200;
export const WORKER_CELLS_MAX = 24;
/** The days GET /stats is read for (the server caches each window for 30 s). */
export const STATS_DAYS = 14;

const HOUR_MS = 60 * 60 * 1000;
const KNOWN_CODECS = ['alac', 'aac', 'aac-binaural', 'aac-downmix', 'ec3', 'ac3', 'aac-lc'];

const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n));

/** The query for the next GET /logs read of error logs: the 24 h window first, then the cursor. */
export function logsQuery(state, now) {
  if (!state || state.cursor == null) {
    const since = new Date(now - LOG_WINDOW_MS).toISOString().replace(/\.\d{3}Z$/, 'Z');
    return { level: 'error', since, limit: LOG_FIRST_LIMIT };
  }
  return { level: 'error', cursor: state.cursor };
}

/** The new `{entries, cursor, full}` after a page: de-duplicated, sorted, trimmed to the window. */
export function mergeLogs(state, page, now) {
  const first = !state || state.cursor == null;
  const bySequence = new Map();
  for (const e of [...((state && state.entries) || []), ...((page && page.logs) || [])]) {
    bySequence.set(e.sequence, e);
  }
  const floor = now - LOG_WINDOW_MS;
  const entries = [...bySequence.values()]
    .filter((e) => Date.parse(e.time) >= floor)
    .sort((a, b) => a.sequence - b.sequence);
  const next = page && page.next_cursor;
  return {
    entries,
    cursor: typeof next === 'number' ? next : state ? state.cursor : null,
    full: first ? ((page && page.logs) || []).length >= LOG_FIRST_LIMIT : Boolean(state.full),
  };
}

/** The error count as text: "200+" when the first read hit its limit, "" before any read. */
export function errorCountLabel(state) {
  if (!state) return '';
  return state.full ? `${LOG_FIRST_LIMIT}+` : String(state.entries.length);
}

function hourStart(now) {
  const d = new Date(now);
  d.setMinutes(0, 0, 0);
  return d.getTime();
}

/** 24 error counts per local clock hour; index 23 is the hour containing `now`. */
export function hourBuckets(entries, now) {
  const buckets = new Array(24).fill(0);
  const start = hourStart(now);
  for (const e of entries || []) {
    const t = Date.parse(e && e.time);
    if (Number.isNaN(t)) continue;
    const idx = 23 + Math.floor((t - start) / HOUR_MS);
    if (idx >= 0 && idx <= 23) buckets[idx]++;
  }
  return buckets;
}

/** The busiest hour as `{count, at}` (`at` is its start in ms, the latest on a tie), or null when empty. */
export function peakHour(buckets, now) {
  let best = -1;
  buckets.forEach((n, i) => {
    if (n > 0 && n >= (best < 0 ? 0 : buckets[best])) best = i;
  });
  if (best < 0) return null;
  return { count: buckets[best], at: hourStart(now) - (buckets.length - 1 - best) * HOUR_MS };
}

function syncTiming(sync, now) {
  if (!sync || !sync.enabled) return null;
  const last = Date.parse(sync.last_poll_at);
  const interval = sync.interval_minutes * 60000;
  if (Number.isNaN(last) || !(interval > 0)) return null;
  return { last, interval, now };
}

/** How far through the poll interval `now` is, 0 to 1; null when sync is off or never polled. */
export function nextCheckFraction(sync, now) {
  const t = syncTiming(sync, now);
  return t ? clamp((now - t.last) / t.interval, 0, 1) : null;
}

/** Milliseconds until the next library-sync check (<= 0 when past due); null as nextCheckFraction. */
export function nextCheckIn(sync, now) {
  const t = syncTiming(sync, now);
  return t ? t.last + t.interval - now : null;
}

/** The share of tracks on the first-choice codec, or on the most common one when that is not meaningful. */
export function firstChoiceShare(codecs, priority) {
  if (!Array.isArray(codecs)) return null;
  const rows = codecs.filter((c) => c && c.tracks > 0);
  const total = rows.reduce((n, c) => n + c.tracks, 0);
  if (total === 0) return null;
  const first = Array.isArray(priority) ? priority[0] : undefined;
  if (rows.some((c) => !KNOWN_CODECS.includes(c.codec)) || !KNOWN_CODECS.includes(first)) {
    const top = rows.reduce((a, b) => (b.tracks > a.tracks ? b : a));
    return { kind: 'common', label: codecLabel(top), share: top.tracks / total };
  }
  const own = first === 'aac-lc' ? 0 : rows.filter((c) => c.codec === first).reduce((n, c) => n + c.tracks, 0);
  return { kind: 'first', codec: first, share: own / total };
}

/** How to draw job workers: one cell each up to WORKER_CELLS_MAX, else a meter fraction; null if unknown. */
export function workerCells(workers, running) {
  if (!Number.isInteger(workers) || workers <= 0) return null;
  const n = typeof running === 'number' && Number.isFinite(running) ? running : 0;
  if (workers <= WORKER_CELLS_MAX) return { mode: 'cells', total: workers, filled: clamp(n, 0, workers) };
  return { mode: 'meter', fraction: clamp(n / workers, 0, 1) };
}
