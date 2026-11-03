// Pure rules for the Queue's rows: amdl's records (albums and catalog
// playlists, one row each) and station runs, which have no record. The
// queued and running runs head the list (lib/queue-order.js); the rest are
// read as streams (GET /library/albums and /library/playlists by
// activity_at, GET /downloads?type=station by updated_at, newest first),
// each paged on its own, and merged here. No DOM.

/** The flags a record on the Failed tab has (one comma-separated query). */
export const FAILED_FLAGS = 'failed,given_up,missing';

/** A stream page: what one read of a stream asks for. */
export const STREAM_PAGE = 100;

const time = (v) => {
  const t = Date.parse(v || '');
  return Number.isNaN(t) ? 0 : t;
};

/** A record's place in the activity order: activity_at (else its last update). */
export const recordAt = (r) => time((r && (r.activity_at || r.updated_at)) || '');

/** A station run's place in the activity order: its last update, as the server sorts it. */
export const stationAt = (j) => time((j && j.updated_at) || '');

/** The streams a tab reads, by the type filter: albums, playlists, stations. */
export function tabStreams(filters) {
  if (!filters || filters.tab === 'active') return [];
  const all = ['albums', 'playlists', 'stations'];
  const only = { album: 'albums', playlist: 'playlists', station: 'stations' }[filters.type];
  return only ? [only] : all;
}

/**
 * The GET query of one stream of a tab, `limit` items from `offset`:
 * records sort=activity with the tab's state or flags, stations
 * type=station by updated_at with the tab's status.
 */
export function streamQuery(name, filters, offset = 0, limit = STREAM_PAGE) {
  const q = { limit, offset };
  if (name === 'stations') {
    Object.assign(q, { type: 'station', sort: 'updated_at', order: 'desc' });
    if (filters.tab === 'done') q.status = 'completed';
    if (filters.tab === 'failed') q.status = 'failed';
  } else {
    q.sort = 'activity';
    if (filters.tab === 'done') q.state = 'complete';
    if (filters.tab === 'failed') q.flag = FAILED_FLAGS;
  }
  if (filters.q) q.q = filters.q;
  return q;
}

/** Whether a record belongs on a tab's stream (the search is the server's to judge). */
export function recordOnTab(r, tab) {
  if (!r) return false;
  if (tab === 'done') return r.state === 'complete';
  if (tab === 'failed') return Boolean(r.tracks_failed || r.tracks_given_up || r.tracks_missing);
  return tab === 'all';
}

/** Whether a station run belongs on a tab's stream. */
export function stationOnTab(j, tab) {
  if (!j || j.resource) return false;
  if (tab === 'done') return j.status === 'completed';
  if (tab === 'failed') return j.status === 'failed';
  return tab === 'all';
}

const atOf = (name) => (name === 'stations' ? stationAt : recordAt);

/** A stream's items newest first, ties kept in their order. */
export function sortStream(name, items) {
  const at = atOf(name);
  return items
    .map((x, i) => ({ x, i, t: at(x) }))
    .sort((a, b) => b.t - a.t || a.i - b.i)
    .map((e) => e.x);
}

/** Adds a later page to a stream's items, dropping ids already there. */
export function appendItems(items, page) {
  const seen = new Set(items.map((x) => x.id));
  const out = items.slice();
  for (const x of page || []) {
    if (!seen.has(x.id)) {
      seen.add(x.id);
      out.push(x);
    }
  }
  return out;
}

/**
 * A stream after the feed brought `item` (a record from resource_updated, a
 * station run from download_upserted): replaced where it is, added when it
 * belongs (`belongs`), dropped when it no longer does, and the stream sorted
 * again so it moves to its new activity. {items, total}; the same object
 * when nothing changed. An older snapshot (by updated_at) is ignored.
 */
export function patchStream(name, stream, item, belongs) {
  if (!item || !item.id) return stream;
  const i = stream.items.findIndex((x) => x.id === item.id);
  if (i >= 0 && time(item.updated_at) < time(stream.items[i].updated_at)) return stream;
  if (!belongs) {
    if (i < 0) return stream;
    return { ...stream, items: stream.items.filter((_, k) => k !== i), total: Math.max(0, stream.total - 1) };
  }
  if (i >= 0) {
    const items = stream.items.slice();
    items[i] = item;
    return { ...stream, items: sortStream(name, items) };
  }
  return { ...stream, items: sortStream(name, [item, ...stream.items]), total: stream.total + 1 };
}

/** A stream without the item `id` (a deleted run, a removed record). */
export function dropFromStream(stream, id) {
  const items = stream.items.filter((x) => x.id !== id);
  return items.length === stream.items.length ? stream : { ...stream, items, total: Math.max(0, stream.total - 1) };
}

/** Whether every item of a stream has been read. */
export const exhausted = (s) => !s || s.items.length >= s.total;

/**
 * A record's Apple Music link: its own `url`, else one built from its kind,
 * id and storefront (pinned, else last read), '' without a storefront.
 */
export function recordLink(kind, r) {
  if (!r) return '';
  if (r.url) return r.url;
  const sf = r.pinned_storefront || r.storefront_read || '';
  return sf ? `https://music.apple.com/${sf}/${kind === 'playlists' ? 'playlist' : 'album'}/${encodeURIComponent(r.id)}` : '';
}

/** A record kind's word for routes and calls: albums or playlists. */
export const recordKind = (name) => (name === 'playlists' ? 'playlists' : 'albums');

/** A row's key: albums:<id>, playlists:<id> or station:<job id>. */
export const rowKey = (row) => row.id;

/** The row of a record of kind (albums|playlists). */
export const recordRow = (kind, r) => ({ id: `${kind}:${r.id}`, kind, record: r, at: recordAt(r) });

/** The row of a station run. */
export const stationRow = (j) => ({ id: `station:${j.id}`, kind: 'station', job: j, at: stationAt(j) });

/**
 * The rows read so far in activity order, newest first, cut where the
 * order is not yet sure: a stream that has more to read has an unread item
 * at most as new as its last read one, so below the newest of those
 * last-read times another stream's rows wait. {rows, next, hidden}: `next`
 * names the stream to read on (the one whose last read row holds the cut),
 * null when every stream is read; `hidden` counts rows read but not shown.
 * `streams` maps a name to {items, total}.
 */
export function mergeStreams(streams) {
  let cut = -Infinity;
  let next = null;
  const rows = [];
  for (const [name, s] of Object.entries(streams)) {
    if (!s) continue;
    for (const x of s.items) rows.push(name === 'stations' ? stationRow(x) : recordRow(name, x));
    if (exhausted(s)) continue;
    const last = s.items.length ? atOf(name)(s.items[s.items.length - 1]) : Infinity;
    if (last > cut) {
      cut = last;
      next = name;
    }
  }
  const order = rows.map((r, i) => ({ r, i })).sort((a, b) => b.r.at - a.r.at || a.i - b.i);
  const shown = order.filter((e) => e.r.at >= cut).map((e) => e.r);
  return { rows: shown, next, hidden: rows.length - shown.length };
}

/**
 * The record of a live run, as far as the run says (kind, id, its active
 * run), for a record that no stream has read yet.
 */
export function liveRecord(job) {
  return { id: job.resource.id, active_job_id: job.id, name: job.title || '', url: job.input, paused: false };
}

/** Whether a queued or running run shows under the type filter and search (the search as the API's). */
export function liveMatches(job, filters) {
  if (!job) return false;
  const type = job.resource ? job.resource.kind : 'station';
  if (filters.type && filters.type !== type) return false;
  const q = (filters.q || '').trim().toLowerCase();
  return !q || `${job.title || ''}\n${job.input || ''}\n${job.artist_name || ''}`.toLowerCase().includes(q);
}

/** The row of a queued or running run: its record's row, or a station run's. */
export function liveRow(job, records = new Map()) {
  if (!job.resource) return { ...stationRow(job), live: job };
  const kind = job.resource.kind === 'playlist' ? 'playlists' : 'albums';
  const id = `${kind}:${job.resource.id}`;
  return { id, kind, record: records.get(id) || liveRecord(job), live: job, at: Infinity };
}

/** The streamed rows that are not live: a live run's record or station run shows once, in the live part. */
export function withoutLive(rows, live) {
  if (!live.length) return rows;
  const keys = new Set(live.map((j) => liveRow(j).id));
  return rows.filter((r) => !keys.has(r.id));
}

/**
 * The tab counts: {all, active, done, failed}, each undefined until its
 * parts are known. `summary` is GET /library/summary, `failedRecords` the
 * union total of the Failed record reads ({albums, playlists}), `stations`
 * the status_counts of GET /downloads?type=station, `pendingFailed` the
 * adds that could not be read, `feed` the feed's global status_counts.
 */
export function tabCounts({ summary, failedRecords, stations, pendingFailed, feed }) {
  const has = (v) => v !== undefined && v !== null;
  const rec = (k, f) => (summary && summary[k] ? f(summary[k]) || 0 : 0);
  const out = { all: undefined, active: undefined, done: undefined, failed: undefined };
  if (feed) out.active = (feed.queued || 0) + (feed.running || 0);
  if (summary && stations) {
    out.all = rec('albums', (s) => s.total) + rec('playlists', (s) => s.total) + (stations.total || 0);
    out.done = rec('albums', (s) => s.states && s.states.complete) + rec('playlists', (s) => s.states && s.states.complete) + (stations.completed || 0);
  }
  if (failedRecords && stations && has(pendingFailed)) out.failed = (failedRecords.albums || 0) + (failedRecords.playlists || 0) + (stations.failed || 0) + pendingFailed;
  return out;
}

const MENU = {
  details: 'Show details',
  top: 'Run next',
  bottom: 'Run last',
  pause: 'Pause',
  resume: 'Resume',
  retry: 'Retry',
  again: 'Download again…',
  check: 'Check files',
  copy: 'Copy link',
  remove: 'Remove from amdl…',
};

/**
 * A record row's menu: Show details; for its queued run Run next and Run
 * last (`place` {first, last}, when known, leaves out the one that would not
 * move it); Pause while it runs or has something to download, else Resume
 * when paused; Retry while tracks failed, were given up or are missing, and
 * Download again… once tracks are downloaded (neither while a run is under
 * way); Check files; Copy link; and Remove from amdl… (not while a run is
 * under way) after a separator. `run` is its live run, if any; `link` its
 * Apple Music link where the record has no `url`.
 */
export function recordMenu(r, run = null, place = {}, link = '') {
  const ids = ['details'];
  const active = Boolean(run || (r && r.active_job_id));
  if (run && run.status === 'queued') {
    if (!place.first) ids.push('top');
    if (!place.last) ids.push('bottom');
  }
  if (r && r.paused && !active) ids.push('resume');
  else if (active || (r && r.state !== 'complete')) ids.push('pause');
  if (!active && r && (r.tracks_failed || r.tracks_given_up || r.tracks_missing)) ids.push('retry');
  if (!active && r && r.tracks_done) ids.push('again');
  ids.push('check');
  if (link || (r && r.url)) ids.push('-', 'copy');
  if (!active) ids.push('-', 'remove');
  return ids.map((id) => (id === '-' ? { separator: true } : { id, label: MENU[id] }));
}

/** The labels of the Run next / Run last entries a station run's menu takes too. */
export const MOVE_LABELS = { top: MENU.top, bottom: MENU.bottom };

/**
 * What a selection of rows allows, each the rows it applies to:
 * {retry, again, pause, remove}. Retry: failed or cancelled station runs,
 * records whose tracks failed, were given up or are missing (not while a
 * run is under way). Download again: records with downloaded tracks and no
 * run under way. Pause: records running or with something to download that
 * are not paused, and queued or running station runs (a cancel). Remove:
 * records with no run under way, and finished station runs (their history).
 */
export function rowPlan(rows) {
  const plan = { retry: [], again: [], pause: [], remove: [] };
  for (const row of rows) {
    if (row.kind === 'station') {
      const s = row.job.status;
      if (s === 'failed' || s === 'cancelled') plan.retry.push(row);
      if (s === 'queued' || s === 'running') plan.pause.push(row);
      else plan.remove.push(row);
      continue;
    }
    const r = row.record;
    const active = Boolean(row.live || r.active_job_id);
    if (!active && (r.tracks_failed || r.tracks_given_up || r.tracks_missing)) plan.retry.push(row);
    if (!active && r.tracks_done) plan.again.push(row);
    if (active || (!r.paused && r.state !== 'complete')) plan.pause.push(row);
    if (!active) plan.remove.push(row);
  }
  return plan;
}

const BULK = {
  retry: { did: 'Queued', ing: 'Retrying', fail: 'retry', after: ' again' },
  again: { did: 'Queued', ing: 'Queueing', fail: 'queue', after: ' again' },
  pause: { did: 'Paused', ing: 'Pausing', fail: 'pause', after: '' },
  remove: { did: 'Removed', ing: 'Removing', fail: 'remove', after: '' },
};

/** The progress line of a bulk run over rows: "Removing 3 of 12". */
export const rowsProgressText = (action, done, total) => `${(BULK[action] || { ing: 'Working on' }).ing} ${done} of ${total}`;

/**
 * The toast after a bulk run of `action` over rows (runLimited outcomes):
 * {message, role}. "Removed 12", or "Removed 10 of 12. 2 failed: IGOR,
 * folklore (still running)." naming at most three per reason, where
 * `reason(error)` words a failure and `titleOf(row)` names a row; a row
 * already gone (404) on remove counts as done.
 */
export function rowsSummary(action, outcomes, titleOf, reason) {
  const w = BULK[action] || { did: 'Updated', fail: 'update', after: '' };
  const gone = (o) => action === 'remove' && o.error && o.error.status === 404;
  const failed = outcomes.filter((o) => !o.ok && !gone(o));
  const ok = outcomes.length - failed.length;
  if (!failed.length) return { message: `${w.did} ${ok}${w.after}`, role: outcomes.length ? 'success' : 'neutral' };
  const groups = new Map();
  for (const o of failed) {
    const why = reason(o.error);
    if (!groups.has(why)) groups.set(why, []);
    groups.get(why).push(titleOf(o.item));
  }
  const parts = [...groups].map(([why, names]) => `${names.slice(0, 3).join(', ')}${names.length > 3 ? ` and ${names.length - 3} more` : ''} (${why})`).join('; ');
  if (ok === 0) return { message: `Couldn't ${w.fail} ${outcomes.length}: ${parts}.`, role: 'danger' };
  return { message: `${w.did} ${ok} of ${outcomes.length}${w.after}. ${failed.length} failed: ${parts}.`, role: 'danger' };
}
