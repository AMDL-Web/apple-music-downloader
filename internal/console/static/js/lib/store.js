// Pure reducers for live data: the downloads feed, job lists, one job's
// detail driven by its event stream, and the log buffer. Every function
// returns new objects and leaves its inputs untouched.

import { isNewerCursor } from './stream.js';

export const EMPTY_COUNTS = Object.freeze({ queued: 0, running: 0, completed: 0, failed: 0, cancelled: 0, total: 0 });

/**
 * Applies one DownloadFeedMessage to the global feed state
 * {counts, cursor}. status_counts on the feed are always global.
 */
export function applyFeedMessage(state, msg) {
  if (!msg || typeof msg !== 'object') return state;
  const next = { ...state };
  if (msg.status_counts) next.counts = { ...EMPTY_COUNTS, ...msg.status_counts };
  if (isNewerCursor(state.cursor, msg.event_id)) next.cursor = msg.event_id;
  return next;
}

const time = (v) => {
  const t = Date.parse(v || '');
  return Number.isNaN(t) ? 0 : t;
};

/**
 * Whether a job matches a list filter {statuses?, types?, q?}. q mirrors the
 * API's substring search over title and input.
 */
export function jobMatches(job, filter = {}) {
  if (!job) return false;
  if (filter.statuses && filter.statuses.length && !filter.statuses.includes(job.status)) return false;
  if (filter.types && filter.types.length && !filter.types.includes(job.type)) return false;
  const q = (filter.q || '').trim().toLowerCase();
  if (q) {
    const hay = `${job.title || ''}\n${job.input || ''}`.toLowerCase();
    if (!hay.includes(q)) return false;
  }
  return true;
}

/**
 * Upserts a job snapshot into a list. A job that no longer matches the
 * filter leaves the list; a snapshot older than the one held (by updated_at)
 * is ignored, which keeps a late replay from undoing a newer REST read.
 * New jobs go to the front, or the back when `append` is set.
 */
export function upsertJob(list, job, filter = {}, { append = false, limit = Infinity } = {}) {
  if (!job || !job.id) return list;
  const i = list.findIndex((j) => j.id === job.id);
  const matches = jobMatches(job, filter);
  if (i >= 0) {
    if (time(job.updated_at) < time(list[i].updated_at)) return list;
    if (!matches) return list.filter((_, k) => k !== i);
    const next = list.slice();
    next[i] = job;
    return next;
  }
  if (!matches) return list;
  const next = append ? [...list, job] : [job, ...list];
  return next.length > limit ? next.slice(0, limit) : next;
}

/** Removes a job by id. */
export function removeJob(list, id) {
  const next = list.filter((j) => j.id !== id);
  return next.length === list.length ? list : next;
}

/** Appends a page of jobs, dropping ids already present. */
export function appendPage(list, page) {
  const seen = new Set(list.map((j) => j.id));
  return [...list, ...page.filter((j) => !seen.has(j.id))];
}

const JOB_EVENTS = new Set([
  'job_queued',
  'job_recovered',
  'job_retried',
  'job_started',
  'resolved_input',
  'job_finished',
  'job_failed',
  'job_cancelled',
]);

/** Events that change the item set or fields the per-job stream does not carry. */
const REFETCH_EVENTS = new Set(['resolved_input', 'job_retried', 'job_recovered', 'motion_artwork_resolved']);

const HOOK_STATUS = { hook_started: 'running', hook_succeeded: 'succeeded', hook_failed: 'failed' };

const ACTIVITY_LIMIT = 50;

/** Initial drawer state from GET /downloads/{id}. */
export function detailFromSnapshot(body) {
  return {
    job: body.job,
    items: Array.isArray(body.items) ? body.items : [],
    hooks: Array.isArray(body.hooks) ? body.hooks : [],
    cursor: body.event_cursor ?? null,
    activity: [],
    needsRefetch: false,
    deleted: false,
  };
}

function withoutMessage(payload) {
  const { message: _message, ...rest } = payload;
  return rest;
}

/**
 * Applies a batch of per-job events (already JSON-parsed) to the drawer
 * state. Events at or before the cursor are skipped, so a replay after a
 * reconnect is harmless. `payload` is an object: a Job for lifecycle
 * events, a JobItem (plus event-specific fields) for item events.
 */
export function applyJobEvents(state, events) {
  let s = state;
  let items = null;
  let hooks = null;
  let activity = null;
  for (const ev of events) {
    if (!ev || !isNewerCursor(s.cursor, ev.id)) continue;
    s = { ...s, cursor: ev.id };
    const payload = ev.payload && typeof ev.payload === 'object' ? ev.payload : null;
    if (ev.type === 'job_deleted') {
      s = { ...s, deleted: true };
      continue;
    }
    if (JOB_EVENTS.has(ev.type) && payload) {
      s = { ...s, job: { ...s.job, ...withoutMessage(payload) } };
    }
    if (REFETCH_EVENTS.has(ev.type)) s = { ...s, needsRefetch: true };
    if (ev.item_id && payload && payload.id === ev.item_id) {
      items = items || s.items.slice();
      const i = items.findIndex((it) => it.id === ev.item_id);
      if (i >= 0) items[i] = { ...items[i], ...withoutMessage(payload) };
      else s = { ...s, needsRefetch: true };
    }
    if (HOOK_STATUS[ev.type]) {
      const name = (payload && payload.hook) || ev.phase;
      if (name) {
        hooks = hooks || s.hooks.slice();
        const entry = { name, status: HOOK_STATUS[ev.type] };
        if (ev.type === 'hook_failed' && ev.message) entry.error = ev.message;
        const i = hooks.findIndex((h) => h.name === name);
        if (i >= 0) hooks[i] = entry;
        else hooks.push(entry);
      }
    }
    if (ev.type !== 'item_progress') {
      activity = activity || s.activity.slice();
      activity.push({ id: ev.id, type: ev.type, phase: ev.phase, message: ev.message, item_id: ev.item_id, created_at: ev.created_at });
      if (activity.length > ACTIVITY_LIMIT) activity.splice(0, activity.length - ACTIVITY_LIMIT);
    }
  }
  if (items) s = { ...s, items };
  if (hooks) s = { ...s, hooks };
  if (activity) s = { ...s, activity };
  return s;
}

/**
 * Replaces the drawer state with a fresh snapshot while keeping the
 * activity seen so far. The snapshot's cursor wins only when it is newer.
 */
export function mergeSnapshot(state, body) {
  const fresh = detailFromSnapshot(body);
  return {
    ...fresh,
    cursor: isNewerCursor(state.cursor, fresh.cursor) ? fresh.cursor : state.cursor,
    activity: state.activity,
  };
}

export const LOG_CAP = 2000;

/** Empty log buffer state. */
export function emptyLogs() {
  return { entries: [], lastSeq: 0, dropped: 0, truncated: false };
}

/**
 * Appends log entries in sequence order, skipping any sequence already held
 * (a stream replay after reconnect), and keeps at most `cap` rows by
 * dropping the oldest.
 */
export function appendLogs(state, entries, cap = LOG_CAP) {
  const fresh = entries.filter((e) => e && typeof e.sequence === 'number' && e.sequence > state.lastSeq);
  if (fresh.length === 0) return state;
  fresh.sort((a, b) => a.sequence - b.sequence);
  let all = state.entries.concat(fresh);
  let dropped = state.dropped;
  if (all.length > cap) {
    dropped += all.length - cap;
    all = all.slice(all.length - cap);
  }
  return { ...state, entries: all, lastSeq: fresh[fresh.length - 1].sequence, dropped };
}
