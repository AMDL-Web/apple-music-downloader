// Pure helpers for what a search hit, a link row or an album shows about
// amdl's records: the `resource` (ResourceMark) of search hits, lookups and
// the album page, kept live by the feed's resource_updated messages. The
// records decide; nothing here reads the disk. No DOM.

/** A record's key: "album:<id>" or "playlist:<id>"; '' without one. */
export const resourceKey = (r) => (r && r.kind && r.id ? `${r.kind}:${r.id}` : '');

/** The /library path segment of a record kind: 'albums' or 'playlists'. */
export const kindPath = (kind) => (kind === 'playlist' || kind === 'playlists' ? 'playlists' : 'albums');

const WAITING = new Set(['pending', 'running', 'failed']);

/**
 * The mark of an item from its record `resource`, or null when there is
 * nothing to show: {state, done, total, withheld, job, resource}. state is
 * 'active' while the record has a queued or running run (job {id}),
 * 'paused', 'complete' or 'partial'. A song passes `songState`, its main
 * slot in its album's record: done is complete, a waiting slot is active or
 * paused with its album, anything else marks nothing.
 */
export function resourceMark(resource, songState) {
  const r = resource && typeof resource === 'object' && resource.kind && resource.id ? resource : null;
  if (!r) return null;
  const ref = { kind: r.kind, id: r.id };
  const job = r.active_job_id ? { id: r.active_job_id } : null;
  const withheld = Math.max(0, Number(r.withheld_count) || 0);
  if (songState !== undefined) {
    if (songState === 'done') return { state: 'complete', done: 1, total: 1, withheld: 0, job: null, resource: ref, song: true };
    if (!WAITING.has(songState)) return null;
    if (job) return { state: 'active', done: 0, total: 1, withheld: 0, job, resource: ref, song: true };
    if (r.paused) return { state: 'paused', done: 0, total: 1, withheld: 0, job: null, resource: ref, song: true };
    return null;
  }
  const done = Math.max(0, Number(r.tracks_done) || 0);
  const total = Math.max(done, Number(r.tracks_wanted) || 0);
  const base = { done, total, withheld, job, resource: ref };
  if (job) return { state: 'active', ...base };
  if (r.paused && r.state !== 'complete') return { state: 'paused', ...base };
  if (r.state === 'complete' || r.state === 'partial') return { state: r.state, ...base };
  return null;
}

/** "9/12": a partial mark's done tracks of its wanted ones. */
export const markFraction = (mark) => `${mark.done}/${mark.total}`;

/** The files a Download again of a marked item replaces; 0 when it has none. */
export const markedFiles = (mark) => (mark && (mark.state === 'complete' || mark.state === 'partial') ? mark.done : 0);

/** The record fields a mark reads, from a record as GET /library/{kind} lists it. */
export function markFields(kind, id, record) {
  const r = record || {};
  return {
    kind,
    id,
    state: r.state || 'none',
    tracks_done: r.tracks_done || 0,
    tracks_wanted: r.tracks_wanted || 0,
    withheld_count: r.withheld_count || 0,
    paused: Boolean(r.paused),
    ...(r.active_job_id ? { active_job_id: r.active_job_id } : {}),
  };
}

/**
 * Applies feed messages to `latest` (a resourceKey -> mark fields map):
 * resource_updated replaces its record's entry. Returns `latest` itself
 * when nothing changed.
 */
export function applyResourceFeed(latest, batch) {
  let next = latest;
  for (const msg of batch || []) {
    if (!msg || msg.type !== 'resource_updated' || !msg.resource_kind || !msg.resource_id || !msg.resource) continue;
    const fields = markFields(msg.resource_kind, msg.resource_id, msg.resource);
    if (next === latest) next = new Map(latest);
    next.set(resourceKey(fields), fields);
  }
  return next;
}

/**
 * A record mark with the newest news: the feed's fields over the ones an
 * answer carried (a song's song_state stays from the answer).
 */
export function withLatest(resource, latest) {
  if (!resource) return null;
  const fresh = latest && latest.get(resourceKey(resource));
  if (!fresh) return resource;
  const out = { ...resource, ...fresh };
  if (!fresh.active_job_id) delete out.active_job_id;
  return out;
}

/**
 * The resource a submit result names, with its queued run, as mark
 * fields to merge into `latest`; null when it names none.
 */
export function submitMark(result) {
  if (!result || !result.resource || !result.resource.kind) return null;
  const { kind, id } = result.resource;
  if (result.status === 'accepted' && result.job && result.job.id) return { kind, id, active_job_id: result.job.id };
  if (result.status === 'duplicate_active' && result.existing_job_id) return { kind, id, active_job_id: result.existing_job_id };
  return null;
}

const SONG_AFTER = { completed: 'done', failed: 'failed', cancelled: 'pending' };

/**
 * Applies feed messages to the songs a page sent (a song id -> {jobId,
 * state} map): when the run a song was sent with ends, its state is what
 * the run left (completed: done, failed, cancelled: pending again). The
 * record's counts cannot say which song a run finished. Returns `sent`
 * itself when nothing changed.
 */
export function applySentSongs(sent, batch) {
  let next = sent;
  for (const msg of batch || []) {
    const job = msg && msg.type === 'download_upserted' ? msg.job : null;
    if (!job || !SONG_AFTER[job.status]) continue;
    for (const [song, entry] of next) {
      if (entry.jobId !== job.id || entry.state === SONG_AFTER[job.status]) continue;
      if (next === sent) next = new Map(sent);
      next.set(song, { ...entry, state: SONG_AFTER[job.status] });
    }
  }
  return next;
}
