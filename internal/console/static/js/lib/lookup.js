// Pure helpers for link previews (POST lookup) and quality probes (POST
// quality) on the New download page: a bounded lookup queue with
// cancellation and a cache, and the text a preview row and the quality
// sheet show. No DOM; the caller passes the network function.

import { releaseYear, trackLength, kHz, count } from './format.js';

/** Links previewed per page view; the rest are listed without a preview. */
export const PREVIEW_LIMIT = 12;
/** Lookups in flight at once. */
export const LOOKUP_CONCURRENCY = 4;
/** Previews kept per tab, oldest dropped first. */
export const LOOKUP_CACHE_MAX = 200;

/** Link kinds the lookup route previews (music videos are refused before any read). */
export const PREVIEW_KINDS = ['album', 'song', 'playlist', 'artist', 'station'];

/**
 * A lookup queue. `lookup(url, signal)` returns a promise of the
 * LookupResponse; `onChange()` runs whenever a status changes.
 *
 * - want(urls): the links to preview, in priority order. At most `max` run
 *   at once; one that is no longer wanted is aborted and forgotten.
 * - status(url): {state: 'loading'} | {state: 'ready', data} |
 *   {state: 'error', error} | {state: 'queued'} (wanted, waiting for a
 *   slot) | {state: 'idle'} (not wanted).
 * - retry(url): forgets a cached answer so want() fetches it again.
 * - forget(urls): aborts these lookups in flight and drops their cached
 *   answers, then fetches the wanted ones again (the media-user-token a
 *   private playlist is read with changed).
 * - dispose(): aborts everything in flight.
 *
 * Answers (errors included) are cached per URL in `cache`, which the caller
 * may share between mounts; an aborted lookup is never cached.
 */
export function createLookupQueue({ lookup, max = LOOKUP_CONCURRENCY, cache = new Map(), onChange = () => {} }) {
  const inflight = new Map();
  let wanted = [];
  let disposed = false;

  // Evicts the oldest answers first, never one that is still wanted (it
  // would be fetched again at once).
  const remember = (url, entry) => {
    cache.delete(url);
    cache.set(url, entry);
    if (cache.size <= LOOKUP_CACHE_MAX) return;
    const keep = new Set(wanted);
    for (const key of cache.keys()) {
      if (cache.size <= LOOKUP_CACHE_MAX) break;
      if (!keep.has(key)) cache.delete(key);
    }
  };

  const settle = (url, ctrl, entry) => {
    if (inflight.get(url) !== ctrl) return;
    inflight.delete(url);
    if (!ctrl.signal.aborted) remember(url, entry);
    pump();
    if (!disposed) onChange();
  };

  const start = (url) => {
    const ctrl = new AbortController();
    inflight.set(url, ctrl);
    Promise.resolve()
      .then(() => lookup(url, ctrl.signal))
      .then(
        (data) => settle(url, ctrl, { state: 'ready', data }),
        (error) => settle(url, ctrl, { state: 'error', error }),
      );
  };

  function pump() {
    if (disposed) return;
    for (const url of wanted) {
      if (inflight.size >= max) break;
      if (!cache.has(url) && !inflight.has(url)) start(url);
    }
  }

  return {
    want(urls) {
      wanted = Array.isArray(urls) ? urls.slice() : [];
      const keep = new Set(wanted);
      for (const [url, ctrl] of inflight) {
        if (!keep.has(url)) {
          ctrl.abort();
          inflight.delete(url);
        }
      }
      pump();
      onChange();
    },
    retry(url) {
      cache.delete(url);
      pump();
      onChange();
    },
    forget(urls) {
      for (const url of urls || []) {
        const ctrl = inflight.get(url);
        if (ctrl) {
          ctrl.abort();
          inflight.delete(url);
        }
        cache.delete(url);
      }
      pump();
      onChange();
    },
    status(url) {
      if (inflight.has(url)) return { state: 'loading' };
      if (cache.has(url)) return cache.get(url);
      return { state: wanted.includes(url) ? 'queued' : 'idle' };
    },
    inflight: () => inflight.size,
    dispose() {
      disposed = true;
      for (const ctrl of inflight.values()) ctrl.abort();
      inflight.clear();
      wanted = [];
    },
  };
}

const KIND_LABELS = {
  album: 'Album',
  song: 'Song',
  playlist: 'Playlist',
  artist: 'Artist',
  station: 'Station',
  'music-video': 'Music video',
  unknown: 'Unknown',
};

/** Label for a parsed link kind; an album link with ?i= reads "Album track" until the lookup says what it downloads. */
export function kindLabel(kind, trackId = null) {
  if (kind === 'album' && trackId) return 'Album track';
  return KIND_LABELS[kind] || 'Link';
}

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * What a preview row shows from a LookupResponse: title, the artist or
 * curator line, the album of a song, and facts in order (type, tracks,
 * year, length, genre) as {key, text}; explicit and Apple Digital Master
 * as flags.
 */
export function previewFacts(data) {
  if (!data || typeof data !== 'object') return null;
  const facts = [];
  const add = (key, text) => {
    if (text) facts.push({ key, text });
  };
  add('type', KIND_LABELS[data.type] || '');
  add('tracks', typeof data.track_count === 'number' && data.type !== 'song' ? plural(data.track_count, 'track', 'tracks') : '');
  add('year', releaseYear(data));
  add('length', data.type === 'song' ? trackLength(data.duration_ms) : '');
  add('genre', typeof data.genre === 'string' ? data.genre : '');
  return {
    title: data.title || '',
    sub: data.artist_name || data.curator_name || '',
    album: data.type === 'song' && data.album_name ? data.album_name : '',
    facts,
    explicit: data.content_rating === 'explicit',
    adm: data.apple_digital_master === true,
  };
}

/**
 * The format of one QualityOption as the response gives it: bit depth and
 * sample rate ("24-bit / 96 kHz"), bitrate ("263 kbps"), channels
 * ("6 ch"), each part only when present.
 */
export function qualitySpec(opt) {
  if (!opt) return '';
  const parts = [];
  if (opt.bit_depth > 0 && opt.sample_rate > 0) parts.push(`${opt.bit_depth}-bit / ${kHz(opt.sample_rate)} kHz`);
  else if (opt.sample_rate > 0) parts.push(`${kHz(opt.sample_rate)} kHz`);
  if (opt.bitrate > 0) parts.push(`${count(Math.round(opt.bitrate / 1000))} kbps`);
  if (opt.channels > 0) parts.push(`${opt.channels} ch`);
  return parts.join(' · ');
}

/**
 * One readable line for a lookup or quality error: Apple's 404 for an id it
 * does not have reads as such, a JSON body after the cause is cut, and the
 * rest is capped at `max` characters. The caller keeps the full message as
 * a tooltip.
 */
export function shortError(message, max = 120) {
  const text = String(message || '').trim();
  if (/catalog/i.test(text) && /\b404\b/.test(text)) return 'Apple Music has nothing at this link (404)';
  const cut = text.replace(/:\s*[{[][\s\S]*$/, '');
  return cut.length > max ? `${cut.slice(0, max - 1)}…` : cut;
}

const rank = (opt) => (opt.bit_depth || 0) * 1e9 + (opt.sample_rate || 0) * 1e3 + (opt.bitrate || 0) / 1e3;

/**
 * Per quality slot (in the response's fixed order): how many tracks offer
 * it and the best format among them, as
 * {id, label, codec, tracks, total, best}. Slots no track offers have
 * tracks 0 and best ''.
 */
export function qualityOverview(resp) {
  const tracks = resp && Array.isArray(resp.tracks) ? resp.tracks : [];
  const slots = new Map();
  for (const t of tracks) {
    for (const q of (t && t.qualities) || []) {
      if (!q || !q.id) continue;
      if (!slots.has(q.id)) slots.set(q.id, { id: q.id, label: q.label || q.id, codec: q.codec || '', tracks: 0, total: tracks.length, bestOpt: null });
      const s = slots.get(q.id);
      if (q.available) {
        s.tracks += 1;
        if (!s.bestOpt || rank(q) > rank(s.bestOpt)) s.bestOpt = q;
      }
    }
  }
  return [...slots.values()].map(({ bestOpt, ...s }) => ({ ...s, best: bestOpt ? qualitySpec(bestOpt) : '' }));
}
