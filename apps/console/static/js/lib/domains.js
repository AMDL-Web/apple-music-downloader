// What each Settings domain reports, from the status store's entries: one
// view per domain that the board tile, the section head, the nav mark, the
// phone list and Overview's status line all draw, so every place says the
// same thing. Pure.
//
// `s` holds the store entries ({data, error, at, okAt} or undefined) by key:
// health, system, wrapper, librarySync, hooks, stats, logs, config. `ctx` is
// {now, counts, conn, simulate}: the feed's counts and connection state.

import { count, bytes, elapsed, relTime, clockTime, errorSummary } from './format.js';
import { formatShares } from './format-codec.js';
import { summarizeError } from './health.js';
import { groupByDisk, diskRole } from './storage.js';
import { HOUR, pct, plural, savedConfig, frame, roleOf } from './domain-frame.js';
import { account } from './domain-account.js';
import { keysToLabels } from './settings-draft.js';
import { firstChoiceShare, nextCheckFraction, nextCheckIn, workerCells, hourBuckets, peakHour, errorCountLabel, STATS_DAYS } from './sysdata.js';
import { settingsHash } from './route.js';

/**
 * @typedef {{
 *   id: string,
 *   state: 'loading'|'failed'|'missing'|'ok',
 *   role: 'danger'|'warning'|null,
 *   tag: {icon: string, role: string, label: string, title?: string}|null,
 *   figure: {value: string, unit?: string, word?: boolean, title?: string, second?: string}|null,
 *   viz: object|null,
 *   note: {text: string, role?: string, title?: string, link?: {href: string, label: string}}|null,
 *   detail: string,
 *   asOf: number,
 *   summary: {text: string, role?: string}|null,
 * }} DomainView
 * `role` drives the nav mark, the tile's icon and the Overview line (null:
 * nothing to act on); `detail` is the short reason the Overview line adds;
 * `asOf` is when the shown values were read, set only while they are stale.
 */

/** The formats bar: the top three formats and Other, each with its share. */
function formatsViz(codecs) {
  const shares = formatShares(codecs);
  return { kind: 'shares', parts: shares.map((f, i) => ({ key: f.key, label: f.label, share: f.share, tone: `series-${i + 1}` })) };
}

// Quality: how often the first codec in the saved order was what got downloaded.
function quality(s, ctx) {
  const st = s.stats;
  const v = frame('quality', st, ctx);
  if (v.state !== 'ok') return v;
  const codecs = st.data.codecs || [];
  const share = firstChoiceShare(codecs, savedConfig(s).priority);
  if (!share) {
    return { ...v, tag: { icon: 'circle-minus', role: 'neutral', label: 'No downloads yet' }, figure: { value: 'No tracks yet', word: true }, viz: { kind: 'shares', parts: [] }, summary: { text: 'No tracks yet' } };
  }
  const viz = formatsViz(codecs);
  const figure =
    share.kind === 'first'
      ? { value: pct(share.share), unit: 'first choice', title: 'Of all retained tracks' }
      : { value: pct(share.share), unit: share.label, title: 'The most common format of all retained tracks' };
  return {
    ...v,
    figure,
    viz,
    note: { text: viz.parts.map((p) => `${p.label} ${pct(p.share)}`).join(' · ') },
    summary: { text: share.kind === 'first' ? `${pct(share.share)} first choice` : `${pct(share.share)} ${share.label}` },
  };
}

/** The disk group the head shows: the fullest one whose space is known. */
function fullestDisk(groups) {
  const known = groups.filter((g) => g.known);
  if (known.length === 0) return null;
  return known.reduce((a, b) => (b.free / b.total < a.free / a.total ? b : a));
}

const FOLDER_NAMES = { downloads_dir: 'Downloads folder', temp_dir: 'Temporary folder' };

/** "Downloaded 412 GB · 9,812 tracks · 27 days of music"; zero parts left out. */
function downloadedNote(totals) {
  if (!totals) return '';
  const parts = [];
  if (totals.bytes_written > 0) parts.push(`Downloaded ${bytes(totals.bytes_written)}`);
  if (totals.tracks_completed > 0) parts.push(plural(totals.tracks_completed, 'track', 'tracks'));
  const days = Math.floor((totals.duration_ms || 0) / (24 * HOUR));
  const hours = Math.floor((totals.duration_ms || 0) / HOUR);
  if (days > 0) parts.push(`${plural(days, 'day', 'days')} of music`);
  else if (hours > 0) parts.push(`${plural(hours, 'hour', 'hours')} of music`);
  return parts.length ? parts.join(' · ') : 'Nothing downloaded yet';
}

// Storage & files: free space on the disk of the configured folders.
function files(s, ctx) {
  const sys = s.system;
  const v = frame('files', sys, ctx);
  if (v.state !== 'ok') return v;
  const storage = Array.isArray(sys.data.storage) ? sys.data.storage : [];
  const groups = groupByDisk(storage);
  const unreadable = storage.find((d) => d && d.error);
  const disk = fullestDisk(groups);
  const role = disk ? diskRole(disk) : 'neutral';
  const lowRole = role === 'danger' || role === 'warning' ? role : null;
  const tag = unreadable
    ? { icon: 'circle-x', role: 'danger', label: "Can't read folder", title: `${FOLDER_NAMES[unreadable.name] || unreadable.name}: ${unreadable.error}` }
    : lowRole
      ? { icon: 'triangle-alert', role: lowRole, label: 'Low space' }
      : null;
  const others = disk ? groups.filter((g) => g !== disk && g.known) : [];
  const other = others[0];
  const totals = s.stats && s.stats.data ? s.stats.data.totals : null;
  const note = other
    ? { text: `${other.folders.map((f) => FOLDER_NAMES[f.name] || f.name).join(', ')}: ${bytes(other.free)} free on another disk` }
    : totals
      ? { text: downloadedNote(totals) }
      : null;
  return {
    ...v,
    role: unreadable ? 'danger' : lowRole,
    tag,
    figure: disk ? { value: bytes(disk.free), unit: `free of ${bytes(disk.total)}` } : { value: 'Space unknown', word: true },
    viz: disk ? { kind: 'meter', fraction: 1 - disk.free / disk.total, role: lowRole || 'info', label: pct(1 - disk.free / disk.total), valuetext: `${pct(1 - disk.free / disk.total)} used` } : null,
    note,
    detail: unreadable ? `can't read ${FOLDER_NAMES[unreadable.name] || unreadable.name}` : disk ? `${bytes(disk.free)} free` : '',
    summary: { text: disk ? `${bytes(disk.free)} free${tag ? ` · ${tag.label}` : ''}` : tag ? tag.label : 'Space unknown', role: tag ? tag.role : undefined },
  };
}

// Jobs: running and queued from the feed, workers and the queue from /system.
function jobs(s, ctx) {
  const c = ctx.counts;
  if (!c) return frame('jobs', undefined, ctx);
  const sys = s.system && s.system.data;
  const j = (sys && sys.jobs) || null;
  const v = { ...frame('jobs', { data: {}, okAt: 0 }, ctx), asOf: 0 };
  const offline = ctx.conn && ctx.conn.state === 'offline';
  const workers = j && j.workers > 0 ? j.workers : 0;
  const tag = offline ? { icon: 'dot', role: 'neutral', label: 'Paused' } : { icon: 'dot', role: 'success', label: 'Live' };
  const queueShare = j && j.queue_capacity > 0 ? (j.queue_depth || 0) / j.queue_capacity : 0;
  const queueRole = queueShare >= 0.9 ? 'danger' : queueShare >= 0.6 ? 'warning' : null;
  const stats = s.stats && s.stats.data;
  let note = null;
  if (j && j.recovery_backlog > 0) note = { text: `Recovering ${plural(j.recovery_backlog, 'job', 'jobs')} from the last run` };
  else if (queueRole) note = { text: `Queue ${count(j.queue_depth)} of ${count(j.queue_capacity)} slots`, role: queueRole };
  else if (stats) {
    const tracks = (stats.totals && stats.totals.tracks_completed) || 0;
    const failed = (stats.daily || []).reduce((n, d) => n + (d.jobs_failed || 0), 0);
    note = {
      text:
        tracks === 0 && failed === 0
          ? `No downloads in ${STATS_DAYS} days`
          : [`${plural(tracks, 'track', 'tracks')} in ${STATS_DAYS} days`, failed ? plural(failed, 'failed job', 'failed jobs') : ''].filter(Boolean).join(' · '),
    };
  }
  const running = c.running || 0;
  const queued = c.queued || 0;
  return {
    ...v,
    asOf: offline ? ctx.conn.downSince || ctx.now : 0,
    role: queueRole,
    tag,
    figure: { value: workers ? `${count(running)}/${count(workers)}` : count(running), unit: 'running', second: queued > 0 ? `${count(queued)} queued` : undefined },
    viz: workers ? { kind: 'cells', ...workerCells(workers, running) } : null,
    note,
    detail: queueRole ? note.text : '',
    summary: { text: running || queued ? [running ? `${count(running)} running` : '', queued ? `${count(queued)} queued` : ''].filter(Boolean).join(' · ') : 'Idle', role: queueRole || undefined },
  };
}

/**
 * Library sync's state, shared with Overview: Off, Needs token (on, but the
 * saved media user token is empty), Error, or Watching. `tokenSet` undefined
 * (the config not read yet) counts as set.
 */
export function librarySyncState(sync, tokenSet) {
  if (!sync.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (tokenSet === false) return { icon: 'triangle-alert', role: 'warning', label: 'Needs token' };
  if (sync.last_error) return { icon: 'circle-x', role: 'danger', label: 'Error' };
  return { icon: 'circle-check', role: 'success', label: 'Watching' };
}

const TOKEN_HREF = settingsHash('account', 'catalog.media_user_token');

// Library sync: tracks seen, and when the next check is.
function librarySync(s, ctx) {
  const e = s.librarySync;
  const v = frame('library_sync', e, ctx);
  if (v.state !== 'ok') return v;
  const sync = e.data;
  const tag = librarySyncState(sync, savedConfig(s).tokenSet);
  const anchor = sync.anchor_size || 0;
  const figure = anchor > 0 ? { value: count(anchor), unit: anchor === 1 ? 'track seen' : 'tracks seen' } : { value: 'Not anchored yet', word: true };
  const fraction = tag.label === 'Off' ? 0 : nextCheckFraction(sync, ctx.now);
  const viz = { kind: 'meter', fraction: fraction || 0, role: 'neutral', valuetext: fraction === null ? 'No check yet' : `${pct(fraction || 0)} of the way to the next check` };
  let note;
  let summary;
  if (tag.label === 'Off') {
    // The figure already says what was recorded.
    note = null;
    summary = { text: 'Off' };
  } else if (tag.label === 'Needs token') {
    note = { text: 'Needs the media user token', role: 'warning', link: { href: TOKEN_HREF, label: 'Set token' } };
    summary = { text: 'Needs token', role: 'warning' };
  } else if (tag.label === 'Error') {
    const text = keysToLabels(sync.last_error);
    note = { text, role: 'danger', title: sync.last_error };
    summary = { text: 'Error', role: 'danger' };
  } else {
    const due = nextCheckIn(sync, ctx.now);
    const albums = sync.total_submitted > 0 ? plural(sync.total_submitted, 'album', 'albums') + ' since start' : '';
    const when = due === null ? `First check within ${plural(sync.interval_minutes || 0, 'min', 'min')}` : due <= 0 ? 'Checking…' : `Next check in ${elapsedShort(due)}`;
    note = { text: [when, albums].filter(Boolean).join(' · ') };
    summary = { text: when };
  }
  return {
    ...v,
    role: roleOf(tag.role),
    tag,
    figure,
    viz,
    note,
    detail: tag.label === 'Error' ? keysToLabels(sync.last_error) : '',
    summary,
  };
}

/** "9 min", "2 h", "45 s": the wait before something, rounded up to its unit. */
function elapsedShort(ms) {
  const s = Math.ceil(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.ceil(s / 60);
  if (m < 60) return `${m} min`;
  return `${Math.round(m / 60)} h`;
}

// Hooks: what is set up in hooks.yaml and how its runs went over the stats window.
function hooks(s, ctx) {
  const e = s.hooks;
  const v = frame('hooks', e, ctx);
  if (v.state !== 'ok') return v;
  const h = e.data;
  const entries = Array.isArray(h.entries) ? h.entries : [];
  const outcomes = (s.stats && s.stats.data && s.stats.data.hooks) || [];
  const ok = outcomes.reduce((n, o) => n + (o.succeeded || 0), 0);
  const bad = outcomes.reduce((n, o) => n + (o.failed || 0), 0);
  const lastFail = outcomes
    .filter((o) => o.last_failed_at && !Number.isNaN(Date.parse(o.last_failed_at)))
    .reduce((a, b) => (!a || Date.parse(b.last_failed_at) > Date.parse(a.last_failed_at) ? b : a), null);
  const recent = lastFail && ctx.now - Date.parse(lastFail.last_failed_at) < 24 * HOUR;
  if (!h.enabled || entries.length === 0) {
    // Off says so once, in the tag; the figure is what is defined.
    const off = !h.enabled;
    return {
      ...v,
      tag: { icon: 'circle-minus', role: 'neutral', label: off ? 'Off' : 'On' },
      figure: entries.length ? { value: count(entries.length), unit: entries.length === 1 ? 'hook defined' : 'hooks defined' } : { value: 'No hooks', word: true },
      viz: { kind: 'shares', parts: [] },
      note: null,
      summary: { text: off ? 'Off' : 'No hooks' },
    };
  }
  const on = entries.filter((x) => x.enabled).length;
  const runs = ok + bad;
  return {
    ...v,
    role: recent ? 'warning' : null,
    tag: recent ? { icon: 'triangle-alert', role: 'warning', label: 'Failed recently' } : { icon: 'circle-check', role: 'success', label: `${count(on)} on` },
    figure: runs > 0 ? { value: pct(ok / runs), unit: `succeeded · ${STATS_DAYS} d` } : { value: 'No runs', word: true },
    viz: { kind: 'shares', parts: runs > 0 ? [{ key: 'ok', label: 'Succeeded', share: ok / runs, tone: 'success' }, { key: 'failed', label: 'Failed', share: bad / runs, tone: 'danger' }] : [] },
    note: { text: lastFail ? `Last failure: ${lastFail.name} · ${relTime(lastFail.last_failed_at, ctx.now)}` : `No failures in ${STATS_DAYS} days` },
    detail: recent ? `${lastFail.name} failed ${relTime(lastFail.last_failed_at, ctx.now)}` : '',
    summary: { text: runs > 0 ? `${count(on)} on · ${pct(ok / runs)}` : `${count(on)} on`, role: recent ? 'warning' : undefined },
  };
}

// Logs: errors kept over the last 24 hours, by hour.
function logging(s, ctx) {
  const e = s.logs;
  if (e && e.error && e.error.status === 503 && e.error.code === 'feature_unavailable') {
    return {
      ...frame('logging', { data: {}, okAt: 0 }, ctx),
      asOf: 0,
      role: 'warning',
      tag: { icon: 'triangle-alert', role: 'warning', label: 'Retention off' },
      figure: { value: 'Off', word: true },
      viz: { kind: 'bars', buckets: [], label: 'No log history' },
      note: { text: 'logging.buffer_size is 0 · config.yaml' },
      detail: 'logging.buffer_size is 0',
      summary: { text: 'Retention off', role: 'warning' },
    };
  }
  const v = frame('logging', e, ctx);
  if (v.state !== 'ok') return v;
  const state = e.data;
  const entries = state.entries || [];
  const buckets = hourBuckets(entries, ctx.now);
  const peak = peakHour(buckets, ctx.now);
  const n = buckets.reduce((a, b) => a + b, 0);
  const last = entries.length ? entries[entries.length - 1] : state.last || null;
  const label = errorCountLabel(state);
  // A failed job logs "job finished"; its reason is the error attribute.
  const cause = last && last.attributes && typeof last.attributes.error === 'string' ? last.attributes.error : '';
  return {
    ...v,
    tag: { icon: 'circle-check', role: 'neutral', label: 'On' },
    figure: n > 0 ? { value: label, unit: `${n === 1 && !state.full ? 'error' : 'errors'} · 24 h` } : { value: 'No errors', unit: 'in 24 h', word: true },
    viz: { kind: 'bars', buckets, label: peak ? `Errors per hour, last 24 hours, peak ${peak.count} at ${clockTime(new Date(peak.at).toISOString(), false).slice(0, 5)}` : 'Errors per hour, last 24 hours: none' },
    note: last ? { text: `Last error ${relTime(last.time, ctx.now)}: ${cause ? errorSummary(cause) : last.message}`, title: cause ? `${last.message}: ${cause}` : last.message } : null,
    summary: { text: n > 0 ? `${label} ${n === 1 && !state.full ? 'error' : 'errors'} in 24 h` : 'No errors in 24 h' },
  };
}

// Server: the database, ffmpeg and the config file, and how long it has run.
function server(s, ctx) {
  const sysE = s.system;
  const v = frame('server', sysE, ctx);
  const health = s.health;
  const degradedBody = health && health.error && health.error.status === 503 && health.error.body && health.error.body.status === 'degraded' ? health.error.body : null;
  const degraded = degradedBody || (health && health.data && health.data.status !== 'ok' ? health.data : null);
  if (v.state !== 'ok' && !degraded) return v;
  const sys = sysE && sysE.data;
  const tools = sys && Array.isArray(sys.tools) ? sys.tools : [];
  const ffmpeg = tools.find((t) => t && t.name === 'ffmpeg');
  const ffmpegMissing = Boolean(ffmpeg && ffmpeg.available === false);
  const persisted = savedConfig(s).persisted;
  const offline = ctx.conn && ctx.conn.state === 'offline';
  let tag;
  if (degraded) tag = { icon: 'circle-x', role: 'danger', label: 'Degraded' };
  else if (offline) tag = { icon: 'circle-x', role: 'danger', label: 'Offline' };
  else if (ffmpegMissing && !ctx.simulate) tag = { icon: 'circle-x', role: 'danger', label: 'ffmpeg missing' };
  else if (ctx.simulate) tag = { icon: 'circle-minus', role: 'neutral', label: 'Simulate' };
  else tag = { icon: 'circle-check', role: 'success', label: 'Healthy' };
  const dbError = degraded ? degraded.database_error || 'Database unavailable' : '';
  const checks = [
    degraded
      ? { label: 'Database', ok: false, role: 'danger', title: dbError }
      : health && health.data
        ? { label: 'Database', ok: true, role: 'success', title: 'Healthy' }
        : { label: 'Database', ok: null, role: 'neutral', title: 'Not checked' },
    ffmpeg ? { label: 'ffmpeg', ok: !ffmpegMissing, role: ffmpegMissing ? (ctx.simulate ? 'neutral' : 'danger') : 'success', title: ffmpegMissing ? ffmpeg.error || 'Not found' : ffmpeg.path || 'Available' } : null,
    persisted === undefined ? null : { label: 'Config file', ok: persisted, role: persisted ? 'success' : 'warning', title: persisted ? 'Saved to configs/config.yaml' : 'Changes are not saved to a file' },
  ].filter(Boolean);
  const started = sys && Date.parse(sys.started_at);
  const version = sys ? [sys.version, sys.go_version, sys.os && sys.arch ? `${sys.os}/${sys.arch}` : ''].filter(Boolean).join(' · ') : '';
  const role = roleOf(tag.role) || (persisted === false ? 'warning' : null);
  return {
    ...v,
    state: 'ok',
    role,
    tag,
    figure: started > 0 ? { value: elapsed(Math.max(0, ctx.now - started)).replace(/ \d+s$/, ''), unit: 'up' } : null,
    viz: { kind: 'checks', items: checks },
    note: degraded ? { text: summarizeError(dbError) || dbError, role: 'danger', title: dbError } : version ? { text: version } : null,
    detail: degraded ? summarizeError(dbError) || dbError : ffmpegMissing && !ctx.simulate ? 'needed for every download' : '',
    summary: { text: [started > 0 ? `Up ${elapsed(Math.max(0, ctx.now - started)).split(' ')[0]}` : '', sys && sys.version ? sys.version : ''].filter(Boolean).join(' · ') || tag.label, role: role || undefined },
  };
}

const VIEWS = { account, quality, files, jobs, library_sync: librarySync, hooks, logging, server };

/** The view of one domain (a Settings section id with a status). */
export function domainView(id, s, ctx) {
  const fn = VIEWS[id];
  if (!fn) return null;
  return fn(s || {}, { now: Date.now(), counts: null, conn: null, simulate: false, ...ctx });
}

/** The accessible name of a tile or list row: "Storage & files, Low space, 34.8 GB free of 494 GB". */
export function domainLabel(view, label) {
  if (!view) return label;
  const parts = [label];
  if (view.tag) parts.push(view.tag.label);
  if (view.figure) parts.push([view.figure.value, view.figure.unit].filter(Boolean).join(' '));
  return parts.join(', ');
}

/**
 * Overview's status line: the domains that need action, each {id, label,
 * text, role, href}. Danger from any of them, plus a warning from Account or
 * Library sync (something to do there); a disk at 85–94 % stays in Settings.
 */
export function attentionRows(s, ctx, sections) {
  const rows = [];
  for (const id of ['server', 'account', 'files', 'library_sync']) {
    const v = domainView(id, s, ctx);
    if (!v || !v.role) continue;
    if (v.role === 'warning' && (id === 'files' || id === 'server')) continue;
    const section = sections.find((x) => x.id === id);
    rows.push({ id, label: section ? section.label : id, role: v.role, text: `${v.tag.label.charAt(0).toLowerCase()}${v.tag.label.slice(1)}${v.detail ? `: ${v.detail}` : ''}`, href: settingsHash(id) });
  }
  return rows;
}

/**
 * The status line's notices: attentionRows grouped by severity, danger
 * first, as [{role, rows}], so a warning (no account) keeps its color beside
 * a danger (a full disk).
 */
export function attentionGroups(rows) {
  return ['danger', 'warning'].map((role) => ({ role, rows: rows.filter((r) => r.role === role) })).filter((g) => g.rows.length);
}

