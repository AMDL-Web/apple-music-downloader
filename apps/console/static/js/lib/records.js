// Pure helpers for amdl's records (GET /library/albums, /library/playlists
// and one record with its tracks): what an Activity row and the record drawer
// say about a record, a track and a quality slot, and what a disk check or
// a pending add reads as. No DOM.

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
 * the queue wins, then paused, then the rollup state. `runStatus` is the
 * status of its active run where the caller knows it: a running one reads
 * Running, as its row in Activity does, and one that ended no longer
 * counts while the record is read again.
 */
export function recordView(r, runStatus = '') {
  if (!r) return { view: { icon: 'circle-help', role: 'neutral', label: 'Unknown' }, flags: [] };
  const done = r.tracks_done || 0;
  const wanted = r.tracks_wanted || 0;
  let view;
  const active = r.active_job_id && !['completed', 'failed', 'cancelled'].includes(runStatus);
  if (active && runStatus === 'running') view = { icon: 'dot', role: 'info', label: 'Running', pulse: true };
  else if (active) view = { icon: 'clock', role: 'info', label: 'In the queue' };
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

/** A slot's codec: the one it landed or, before that, its slot's ('' for a main slot not downloaded yet). */
const slotCodec = (slot) => (slot ? slot.codec || (slot.slot !== 'main' ? slot.slot : '') : '');

/** A slot's quality words: "ALAC 24-bit / 96 kHz", "AAC 262 kbps", the codec it landed or, before that, its slot. */
export function slotQuality(slot) {
  if (!slot) return '';
  const codec = slotCodec(slot);
  const name = codec ? codecName(codec) : '';
  const label = String(slot.quality_label || '').replace(/(\d)\s*kbps$/i, '$1 kbps');
  return [name, label].filter(Boolean).join(' ');
}

/** Whether any track of a record has a slot besides its main one. */
export const hasExtraSlots = (record) => (record && Array.isArray(record.tracks) ? record.tracks : []).some((t) => (t.files || []).some((f) => f.slot !== 'main'));

/**
 * A track's slots as {key, codec, view, label}, main first, for a record
 * with more than one quality: each says its codec ("ALAC", "AAC") with its
 * own state, so a quality still waiting never reads as downloaded. `label`
 * spells both out for a title and screen readers ("AAC 262 kbps: Waiting").
 */
export function slotStates(t) {
  const files = t && Array.isArray(t.files) ? t.files : [];
  const main = files.filter((f) => f.slot === 'main');
  return [...main, ...files.filter((f) => f.slot !== 'main')].map((f) => {
    const view = audioView(f.audio_state);
    const codec = slotCodec(f);
    return { key: f.slot, codec: codec ? codecName(codec) : 'Main', view, label: `${slotQuality(f) || 'Main'}: ${view.label}` };
  });
}

/** The codec names of a record's other qualities, in first-seen order ("AAC"). */
export function extraCodecs(record) {
  const out = [];
  for (const t of record && Array.isArray(record.tracks) ? record.tracks : []) {
    for (const f of t.files || []) {
      const c = f.slot !== 'main' ? codecName(slotCodec(f)) : '';
      if (c && !out.includes(c)) out.push(c);
    }
  }
  return out;
}

/**
 * The codec name of the other quality a run of this record fetched, from
 * the slots that run wrote last ("AAC"), or '' for a run of the main slots.
 */
export function runSlot(record, jobId) {
  if (!jobId) return '';
  for (const t of record && Array.isArray(record.tracks) ? record.tracks : []) {
    const f = (t.files || []).find((x) => x.last_job_id === jobId);
    if (f) return f.slot !== 'main' ? codecName(slotCodec(f)) : '';
  }
  return '';
}

/**
 * Whether a record has anything a run could still do, so Pause means
 * something: a run is active, it was never read, or a wanted track's slot
 * waits, runs or failed (a failed slot is retried by itself).
 */
export function recordHasWork(r) {
  if (!r) return false;
  if (r.active_job_id || !r.resolved_at) return true;
  return (Array.isArray(r.tracks) ? r.tracks : []).some(
    (t) => t.wanted && t.availability !== 'unavailable' && ((t.files || []).length === 0 || (t.files || []).some((f) => ['pending', 'running', 'failed'].includes(f.audio_state))),
  );
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

/** A record kind's word: "album" or "playlist", plural with n !== 1. */
export const kindWord = (kind, n = 1) => {
  const one = kind === 'playlists' || kind === 'playlist' ? 'playlist' : 'album';
  return n === 1 ? one : `${one}s`;
};

/**
 * The toast after a reset of `record` answered `out` ({slots, job}):
 * `queued` when it queued a run, else why nothing downloads yet. A reset
 * leaves a paused record paused.
 */
export function resetNote(out, record, queued) {
  if (out && out.job) return queued;
  const name = recordTitle(record);
  if (record && record.paused) return `Reset “${name}”. Resume it to download`;
  if (out && out.slots) return `Reset “${name}”. It waits for room in the queue`;
  return 'Nothing to download';
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
