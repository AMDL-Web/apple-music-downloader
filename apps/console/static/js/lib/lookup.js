// Pure helpers for link previews (POST lookup) and quality probes (POST
// quality) on the New download page: a bounded lookup queue with
// cancellation and a cache, and the text a preview row and the quality
// sheet show. No DOM; the caller passes the network function.

import { releaseYear, trackLength, kHz, count } from './format.js';
import { parseLink } from './links.js';

/**
 * Links looked up per paste: one submit request's worth (lib/submit.js
 * MAX_URLS_PER_REQUEST), so a link Apple does not have is flagged before
 * the submit wherever it sits in the list.
 */
export const LOOKUP_LIMIT = 100;
/** Link rows listed before "Show all"; flagged rows past it are listed too. */
export const ROWS_SHOWN = 12;
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
 *   answers, then fetches the wanted ones again (the media user token in
 *   use for private playlist covers, the wrapper's or the one in Settings,
 *   changed).
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
  const title = data.title || '';
  const sub = data.artist_name || data.curator_name || '';
  // An artist link's title is the artist, a single's album is named after the
  // song: the row says each name once.
  return {
    title,
    sub: sub === title ? '' : sub,
    album: data.type === 'song' && data.album_name && data.album_name !== title ? data.album_name : '',
    facts,
    explicit: data.content_rating === 'explicit',
    adm: data.apple_digital_master === true,
  };
}

const NBSP = '\u00a0';

/**
 * The format of one QualityOption as the response gives it: bit depth and
 * sample rate ("24-bit / 96 kHz"), bitrate ("263 kbps"), channels
 * ("6 ch"), each part only when present. A number and its unit are joined
 * by a no-break space, so a wrapped line never splits them.
 */
export function qualitySpec(opt) {
  return qualitySpecParts(opt).join(' · ');
}

/** qualitySpec's facts one by one, for a strip that wraps between them. */
export function qualitySpecParts(opt) {
  if (!opt) return [];
  const parts = [];
  if (opt.bit_depth > 0 && opt.sample_rate > 0) parts.push(`${opt.bit_depth}-bit / ${kHz(opt.sample_rate)}${NBSP}kHz`);
  else if (opt.sample_rate > 0) parts.push(`${kHz(opt.sample_rate)}${NBSP}kHz`);
  if (opt.bitrate > 0) parts.push(`${count(Math.round(opt.bitrate / 1000))}${NBSP}kbps`);
  if (opt.channels > 0) parts.push(`${opt.channels}${NBSP}ch`);
  return parts;
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
  return [...slots.values()].map(({ bestOpt, ...s }) => ({ ...s, best: bestOpt ? qualitySpec(bestOpt) : '', bestParts: qualitySpecParts(bestOpt) }));
}

/**
 * The tracks of a QualityResponse whose formats differ from the album's
 * (the slots any track offers), as indices in order; [] when every track
 * offers the same formats.
 */
export function differingTracks(resp) {
  const tracks = resp && Array.isArray(resp.tracks) ? resp.tracks : [];
  const offered = (t) =>
    ((t && t.qualities) || [])
      .filter((q) => q && q.available)
      .map((q) => q.id)
      .sort()
      .join(',');
  const all = qualityOverview(resp)
    .filter((s) => s.tracks > 0)
    .map((s) => s.id)
    .sort()
    .join(',');
  return tracks.map((t, i) => (offered(t) === all ? -1 : i)).filter((i) => i >= 0);
}

/** A quality slot's label with its codec only when the label does not already say it: "Lossless · ALAC", "AAC". */
export function slotName(slot) {
  const label = (slot && slot.label) || '';
  const codec = (slot && slot.codec) || '';
  return codec && !label.toLowerCase().includes(codec.toLowerCase()) ? { label, codec } : { label, codec: '' };
}

/**
 * What a job of `link` would be, as {type, id}: an album ?i= link is that
 * song unless catalog.album_track_url_mode is "album".
 */
export function linkTarget(link, trackMode = 'song') {
  if (!link || !link.id) return null;
  if (link.kind === 'album' && link.trackId && trackMode !== 'album') return { type: 'song', id: link.trackId };
  return { type: link.kind, id: link.id };
}

/**
 * The id to search GET downloads?q= for when looking for earlier jobs of
 * `link`: the id a job of it would download (a song for an album ?i= link
 * unless catalog.album_track_url_mode is "album"), so a song job made from
 * a /song/ link is found too. '' when the link has no id.
 */
export function priorQuery(link, trackMode = 'song') {
  const target = linkTarget(link, trackMode);
  return target ? target.id : '';
}

const ACTIVE = new Set(['queued', 'running']);

/**
 * Whether a link row stays listed past ROWS_SHOWN: it has an issue (blocking
 * or a warning) or an earlier job still queued or running.
 */
export function needsAttention(issue, prior) {
  return Boolean(issue) || Boolean(prior && prior.state === 'active');
}
const newest = (a, b, key) => (Date.parse(b[key] || '') || 0) - (Date.parse(a[key] || '') || 0);

/**
 * The earlier job of the same album, song or playlist among `jobs` (a GET
 * downloads?q=<id> page, whose q is a substring match, so each job's input
 * is parsed and compared): {state: 'active', job} for one queued or
 * running, else {state: 'done', job} for the latest completed one, else
 * null. Failed and cancelled jobs do not count.
 */
export function priorJob(jobs, link, trackMode = 'song') {
  const want = linkTarget(link, trackMode);
  if (!want) return null;
  const same = (Array.isArray(jobs) ? jobs : []).filter((j) => {
    const got = j && typeof j.input === 'string' ? linkTarget(parseLink(j.input), trackMode) : null;
    return got && got.type === want.type && got.id === want.id;
  });
  const active = same.filter((j) => ACTIVE.has(j.status)).sort((a, b) => newest(a, b, 'created_at'));
  if (active.length) return { state: 'active', job: active[0] };
  const done = same.filter((j) => j.status === 'completed').sort((a, b) => newest(a, b, 'finished_at'));
  return done.length ? { state: 'done', job: done[0] } : null;
}

/**
 * Whether a detected row is checked for the submit: the person's own pick
 * when there is one (`picks`, a Map of url -> boolean), else unchecked when
 * the item has an earlier job still in the queue, or a done one while
 * `overwrite` is off (`prior`, from priorJob), else checked.
 */
export function isPicked(url, prior, picks, overwrite = false) {
  if (picks && picks.has(url)) return picks.get(url) === true;
  if (!prior) return true;
  return overwrite && prior.state === 'done';
}

const sameTarget = (a, b) => Boolean(a && b && a.type === b.type && a.id === b.id);

/**
 * Applies feed messages to one cached GET downloads page (`jobs`) of
 * `link`: a job it holds is replaced by a newer snapshot or dropped when
 * deleted, and a job of the same item that it does not hold yet is added,
 * so "In the queue now" turns into "In your library" when the job
 * completes. Returns `jobs` itself when nothing changed.
 */
export function applyFeedToJobs(jobs, batch, link, trackMode = 'song') {
  const want = linkTarget(link, trackMode);
  let next = Array.isArray(jobs) ? jobs : [];
  const start = next;
  for (const msg of batch || []) {
    if (!msg) continue;
    if (msg.type === 'download_deleted') {
      if (next.some((j) => j && j.id === msg.job_id)) next = next.filter((j) => !j || j.id !== msg.job_id);
      continue;
    }
    const job = msg.type === 'download_upserted' ? msg.job : null;
    if (!job || !job.id) continue;
    const i = next.findIndex((j) => j && j.id === job.id);
    if (i >= 0) {
      if ((Date.parse(job.updated_at || '') || 0) < (Date.parse(next[i].updated_at || '') || 0)) continue;
      next = next.slice();
      next[i] = job;
    } else if (typeof job.input === 'string' && sameTarget(linkTarget(parseLink(job.input), trackMode), want)) {
      next = [job, ...next];
    }
  }
  return next === start ? jobs : next;
}
