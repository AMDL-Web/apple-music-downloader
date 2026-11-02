// Pure helpers for amdl's records (GET /library/albums, /library/playlists
// and one record with its tracks): what a Library row and the record drawer
// say about a record, a track and a quality slot, the Library's list query,
// and what a disk check or a pending add reads as. No DOM.

import { codecName } from './overrides.js';
import { count } from './format.js';

/** A record's title: its name, or its id while it was never read. */
export const recordTitle = (r) => (r && r.name ? r.name : r ? `Not read yet · ${r.id}` : '');

/** The second line: an album's artist, a playlist's curator. */
export const recordSub = (r) => (r ? r.artist_name || r.curator_name || '' : '');

/** A record's still cover template ({w}x{h}), if any. */
export const recordArt = (r) => (r && r.artwork && r.artwork.url) || '';

/** A record's palette as the format helpers read it (artwork_bg_color, …), from its artwork colors. */
export function recordPalette(r) {
  const c = r && r.artwork && r.artwork.colors;
  if (!c || !c.bg) return {};
  return { artwork_bg_color: c.bg, artwork_text_color1: c.text1, artwork_text_color2: c.text2, artwork_text_color3: c.text3, artwork_text_color4: c.text4 };
}

/**
 * How a record reads in a list: {icon, role, label} for its badge, and the
 * flags it shows (failed, given up, missing), each {key, label}. A run in
 * the queue wins, then paused, then the rollup state.
 */
export function recordView(r) {
  if (!r) return { view: { icon: 'circle-help', role: 'neutral', label: 'Unknown' }, flags: [] };
  const done = r.tracks_done || 0;
  const wanted = r.tracks_wanted || 0;
  let view;
  if (r.active_job_id) view = { icon: 'clock', role: 'info', label: 'In the queue' };
  else if (r.paused && r.state !== 'complete') view = { icon: 'pause', role: 'neutral', label: 'Paused' };
  else if (r.catalog_state && r.catalog_state !== 'ok') view = { icon: 'circle-slash', role: 'warning', label: r.catalog_state === 'gone' ? 'Gone from Apple Music' : 'Not in your storefront' };
  else if (r.state === 'complete') view = { icon: 'circle-check', role: 'success', label: 'Downloaded' };
  else if (r.state === 'partial') view = { icon: 'circle-dashed', role: 'info', label: `${done} of ${wanted}` };
  else if (!r.resolved_at) view = { icon: 'circle-dashed', role: 'neutral', label: 'Not read yet' };
  else view = { icon: 'circle-dashed', role: 'neutral', label: 'Not downloaded' };
  const flags = [];
  if (r.tracks_failed) flags.push({ key: 'failed', label: `${count(r.tracks_failed)} failed` });
  if (r.tracks_given_up) flags.push({ key: 'given_up', label: `${count(r.tracks_given_up)} given up` });
  if (r.tracks_missing) flags.push({ key: 'missing', label: `${count(r.tracks_missing)} missing` });
  return { view, flags };
}

/** "9 tracks", or "9 tracks · 1 not in your region" with withheld ones. */
export function trackCountText(r) {
  if (!r) return '';
  const n = r.tracks_wanted || 0;
  const parts = [`${count(n)} ${n === 1 ? 'track' : 'tracks'}`];
  if (r.withheld_count) parts.push(`${count(r.withheld_count)} not in your region`);
  return parts.join(' · ');
}

const AUDIO = {
  pending: { icon: 'circle-dashed', role: 'neutral', label: 'Waiting' },
  running: { icon: 'download', role: 'info', label: 'Downloading' },
  done: { icon: 'circle-check', role: 'success', label: 'Downloaded' },
  failed: { icon: 'circle-x', role: 'danger', label: 'Failed' },
  given_up: { icon: 'circle-x', role: 'danger', label: 'Given up' },
  unavailable: { icon: 'circle-minus', role: 'skipped', label: 'Not offered' },
  missing: { icon: 'triangle-alert', role: 'warning', label: 'Missing' },
};

/** One slot's audio state as {icon, role, label}. */
export const audioView = (state) => AUDIO[state] || { icon: 'circle-help', role: 'neutral', label: state || 'Unknown' };

/** A slot's quality words: "ALAC 24-bit / 96 kHz", the codec it landed or, before that, its slot. */
export function slotQuality(slot) {
  if (!slot) return '';
  const codec = slot.codec || (slot.slot !== 'main' ? slot.slot : '');
  const name = codec ? codecName(codec) : '';
  return [name, slot.quality_label].filter(Boolean).join(' ');
}

/**
 * How a record track reads: {main, extra, view, note}. `main` is its main
 * slot (or null), `extra` its other qualities, `view` the main slot's
 * state (a track the storefront stopped listing is "Not in your region",
 * one not wanted is "Not added"), `note` the main slot's last error while
 * it waits for a retry or was given up.
 */
export function trackState(t) {
  const files = (t && Array.isArray(t.files) ? t.files : []);
  const main = files.find((f) => f.slot === 'main') || null;
  const extra = files.filter((f) => f.slot !== 'main');
  let view;
  if (t && t.availability === 'unavailable') view = { icon: 'circle-minus', role: 'skipped', label: 'Not in your region' };
  else if (t && !t.wanted) view = { icon: 'circle-dashed', role: 'neutral', label: 'Not added' };
  else view = audioView(main ? main.audio_state : 'pending');
  const note = main && (main.audio_state === 'failed' || main.audio_state === 'given_up') ? main.last_error || '' : '';
  return { main, extra, view, note };
}

/** The song ids of a record's tracks whose main slot is in one of `states`. */
export function songsIn(record, states) {
  const want = new Set(states);
  return (record && Array.isArray(record.tracks) ? record.tracks : []).filter((t) => {
    const m = (t.files || []).find((f) => f.slot === 'main');
    return m && want.has(m.audio_state);
  }).map((t) => t.song_id);
}

/** The GET /library/{kind} query for Library filters (lib/route.js libraryFilters). */
export function recordQuery(f, offset = 0, limit = 50) {
  const q = { limit, offset, sort: 'added' };
  if (f.state) q.state = f.state;
  if (f.flag) q.flag = f.flag;
  if (f.q) q.q = f.q;
  return q;
}

/** A record kind's word: "album" or "playlist", plural with n !== 1. */
export const kindWord = (kind, n = 1) => {
  const one = kind === 'playlists' || kind === 'playlist' ? 'playlist' : 'album';
  return n === 1 ? one : `${one}s`;
};

const CHECK_ABORT = { too_many_missing: 'more than half the files were missing, so nothing was marked', interrupted: 'the server stopped', failed: 'it failed' };

/**
 * A disk check as one line: "Checking… 120 files", "Checked 1,204 files ·
 * 2 missing", or why it marked nothing. `check` is a CheckRun or null.
 */
export function checkText(check) {
  if (!check) return '';
  const n = check.checked || 0;
  if (!check.finished_at) return `Checking… ${count(n)} ${n === 1 ? 'file' : 'files'}`;
  if (check.aborted_reason) return `Check stopped: ${CHECK_ABORT[check.aborted_reason] || check.aborted_reason}`;
  const parts = [`Checked ${count(n)} ${n === 1 ? 'file' : 'files'}`];
  if (check.missing) parts.push(`${count(check.missing)} missing`);
  return parts.join(' · ');
}

/** Whether a disk check is under way. */
export const checkRunning = (check) => Boolean(check && !check.finished_at);

/** Applies feed messages to the last disk check: check_updated replaces it. */
export function applyCheckFeed(check, batch) {
  let next = check;
  for (const msg of batch || []) if (msg && msg.type === 'check_updated' && msg.check) next = msg.check;
  return next;
}

const PENDING_KIND = { song: 'Song', artist: 'Artist', album: 'Album', playlist: 'Playlist' };

/** A pending add's name: its kind and catalog id ("Song 1440857788"). */
export const pendingTitle = (p) => (p ? `${PENDING_KIND[p.kind] || 'Item'} ${p.catalog_id}` : '');

/** The quality most main slots of a record name ("ALAC 16-bit/44.1 kHz"), '' with none. */
export function commonQuality(record) {
  const n = new Map();
  for (const t of record && Array.isArray(record.tracks) ? record.tracks : []) {
    const m = (t.files || []).find((f) => f.slot === 'main');
    const q = m ? slotQuality(m) : '';
    if (q) n.set(q, (n.get(q) || 0) + 1);
  }
  let best = '';
  for (const [q, c] of n) if (!best || c > n.get(best)) best = q;
  return best;
}
