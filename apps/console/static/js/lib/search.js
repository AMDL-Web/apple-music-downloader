// Pure helpers for catalog search on the New download page (GET search and
// GET artists/{id}/albums): the term the text box holds, a searcher that
// keeps one search in flight and pages each group, an artist's album pages,
// what a hit row shows, and which job a hit is in the library or the queue
// through. No DOM; the caller passes the network functions.

import { releaseYear, trackLength, count } from './format.js';

/** The longest term the backend searches (GET search `term`). */
export const SEARCH_MAX_TERM = 200;
/** Typing pauses this long before a search starts. */
export const SEARCH_DEBOUNCE_MS = 300;
/** Hits per group on the first page: six fill one, two, three or six columns. */
export const SEARCH_FIRST = 6;
/** Hits per group a "Show more" adds. */
export const SEARCH_MORE = 25;
/** Albums per page of an artist. */
export const ARTIST_PAGE = 25;
/** Answers kept per tab, oldest dropped first. */
export const SEARCH_CACHE_MAX = 30;

/** The result groups in the order they are shown, with their words. */
export const SEARCH_GROUPS = [
  { id: 'albums', label: 'Albums', one: 'album', many: 'albums' },
  { id: 'songs', label: 'Songs', one: 'song', many: 'songs' },
  { id: 'artists', label: 'Artists', one: 'artist', many: 'artists' },
];

/**
 * What the text box searches for: {term, tooLong}. The term is its words
 * joined by single spaces; it is '' when the box holds a link (`entries`, a
 * scanLinks result, which then previews links as before) or only space, and
 * when it runs past SEARCH_MAX_TERM characters (tooLong).
 */
export function searchTerm(text, entries) {
  if (Array.isArray(entries) && entries.length) return { term: '', tooLong: false };
  const term = String(text || '')
    .split(/\s+/)
    .filter(Boolean)
    .join(' ');
  if ([...term].length > SEARCH_MAX_TERM) return { term: '', tooLong: true };
  return { term, tooLong: false };
}

/** `group` with `page`'s items after its own (each id once) and `page`'s next_offset. */
export function appendPage(group, page) {
  const items = (group && Array.isArray(group.items) ? group.items : []).slice();
  const seen = new Set(items.map((h) => h && h.id));
  for (const hit of (page && page.items) || []) {
    if (!hit || seen.has(hit.id)) continue;
    seen.add(hit.id);
    items.push(hit);
  }
  const next = page && typeof page.next_offset === 'number' ? page.next_offset : undefined;
  return next ? { items, next_offset: next } : { items };
}

const ABORTED = (err) => Boolean(err && (err.name === 'AbortError' || err.code === 'aborted'));

/** Calls `fn` unless `signal` was aborted first (a run typed past before it started). */
const unlessAborted = (signal, fn) => () => (signal.aborted ? Promise.reject(Object.assign(new Error('aborted'), { name: 'AbortError' })) : fn());

function remember(cache, key, value) {
  cache.delete(key);
  cache.set(key, value);
  while (cache.size > SEARCH_CACHE_MAX) cache.delete(cache.keys().next().value);
}

/**
 * A searcher. `search(query, signal)` returns a promise of a SearchResponse
 * for query {term, types?, offset?, limit}; `onChange()` runs whenever the
 * state changes.
 *
 * - run(term): searches term; an answer cached for it comes back at once.
 *   A run aborts the search still in flight and any "more" of the old term.
 *   '' clears the state.
 * - more(group): reads the group's next page (SEARCH_MORE hits) and appends it.
 * - retry(): searches the current term again (after an error).
 * - state(): {term, status: 'idle'|'loading'|'ready'|'error', data, error,
 *   stale, more}. While a new term loads, `data` keeps the last answer and
 *   `stale` is true, so the list keeps its height until the new one lands.
 *   `more` maps a group to 'loading' or 'error' while a page of it is read
 *   or failed.
 * - dispose(): aborts everything.
 *
 * Answers are cached per term in `cache` (shared between mounts), pages
 * read with more() included; an error is never cached.
 */
export function createSearcher({ search, onChange = () => {}, cache = new Map() }) {
  let st = { term: '', status: 'idle', data: null, error: null, stale: false, more: {} };
  let ctrl = null;
  const moreCtrls = new Map();
  let disposed = false;
  const set = (next) => {
    st = next;
    if (!disposed) onChange();
  };
  const abortMore = () => {
    for (const c of moreCtrls.values()) c.abort();
    moreCtrls.clear();
  };

  function start(term) {
    if (ctrl) ctrl.abort();
    const mine = new AbortController();
    ctrl = mine;
    set({ term, status: 'loading', data: st.data, error: null, stale: Boolean(st.data), more: {} });
    Promise.resolve()
      .then(unlessAborted(mine.signal, () => search({ term, limit: SEARCH_FIRST }, mine.signal)))
      .then(
        (data) => {
          if (ctrl !== mine || mine.signal.aborted) return;
          ctrl = null;
          remember(cache, term, data);
          set({ term, status: 'ready', data, error: null, stale: false, more: {} });
        },
        (error) => {
          if (ctrl !== mine || mine.signal.aborted || ABORTED(error)) return;
          ctrl = null;
          set({ term, status: 'error', data: null, error, stale: false, more: {} });
        },
      );
  }

  return {
    run(term) {
      const t = typeof term === 'string' ? term : '';
      if (t === st.term && st.status !== 'idle' && st.status !== 'error') return;
      abortMore();
      if (!t) {
        if (ctrl) ctrl.abort();
        ctrl = null;
        set({ term: '', status: 'idle', data: null, error: null, stale: false, more: {} });
        return;
      }
      if (cache.has(t)) {
        if (ctrl) ctrl.abort();
        ctrl = null;
        const data = cache.get(t);
        remember(cache, t, data);
        set({ term: t, status: 'ready', data, error: null, stale: false, more: {} });
        return;
      }
      start(t);
    },
    retry() {
      if (st.term && st.status === 'error') start(st.term);
    },
    more(group) {
      const data = st.status === 'ready' ? st.data : null;
      const g = data && data[group];
      if (!g || !g.next_offset || moreCtrls.has(group)) return;
      const term = st.term;
      const mine = new AbortController();
      moreCtrls.set(group, mine);
      set({ ...st, more: { ...st.more, [group]: 'loading' } });
      Promise.resolve()
        .then(unlessAborted(mine.signal, () => search({ term, types: group, offset: g.next_offset, limit: SEARCH_MORE }, mine.signal)))
        .then(
          (page) => {
            if (moreCtrls.get(group) !== mine || mine.signal.aborted) return;
            moreCtrls.delete(group);
            const next = { ...st.data, [group]: appendPage(st.data[group], page && page[group]) };
            remember(cache, term, next);
            const more = { ...st.more };
            delete more[group];
            set({ ...st, data: next, more });
          },
          (error) => {
            if (moreCtrls.get(group) !== mine || mine.signal.aborted || ABORTED(error)) return;
            moreCtrls.delete(group);
            set({ ...st, more: { ...st.more, [group]: 'error' } });
          },
        );
    },
    state: () => st,
    dispose() {
      disposed = true;
      if (ctrl) ctrl.abort();
      abortMore();
    },
  };
}

/**
 * An artist's albums, page by page. `load(id, query, signal)` returns a
 * promise of an ArtistAlbumsResponse for query {offset?, limit}.
 *
 * - open(id): shows that artist (a cached one at once); '' closes it.
 * - more(): reads the next page.
 * - retry(): reads the first page again after an error.
 * - state(): {id, status: 'idle'|'loading'|'ready'|'error', artist, albums,
 *   error, more: '' | 'loading' | 'error'}.
 */
export function createArtistPages({ load, onChange = () => {}, cache = new Map() }) {
  let st = { id: '', status: 'idle', artist: null, albums: null, error: null, more: '' };
  let ctrl = null;
  let disposed = false;
  const set = (next) => {
    st = next;
    if (!disposed) onChange();
  };
  const abort = () => {
    if (ctrl) ctrl.abort();
    ctrl = null;
  };

  function first(id) {
    abort();
    const mine = new AbortController();
    ctrl = mine;
    set({ id, status: 'loading', artist: null, albums: null, error: null, more: '' });
    Promise.resolve()
      .then(unlessAborted(mine.signal, () => load(id, { limit: ARTIST_PAGE }, mine.signal)))
      .then(
        (resp) => {
          if (ctrl !== mine || mine.signal.aborted) return;
          ctrl = null;
          const entry = { artist: resp.artist, albums: appendPage(null, resp.albums) };
          remember(cache, id, entry);
          set({ id, status: 'ready', ...entry, error: null, more: '' });
        },
        (error) => {
          if (ctrl !== mine || mine.signal.aborted || ABORTED(error)) return;
          ctrl = null;
          set({ id, status: 'error', artist: null, albums: null, error, more: '' });
        },
      );
  }

  return {
    open(id) {
      const want = typeof id === 'string' ? id : '';
      if (want === st.id && st.status !== 'idle') return;
      abort();
      if (!want) {
        set({ id: '', status: 'idle', artist: null, albums: null, error: null, more: '' });
        return;
      }
      if (cache.has(want)) {
        const entry = cache.get(want);
        set({ id: want, status: 'ready', ...entry, error: null, more: '' });
        return;
      }
      first(want);
    },
    retry() {
      if (st.id && st.status === 'error') first(st.id);
    },
    more() {
      if (st.status !== 'ready' || !st.albums || !st.albums.next_offset || ctrl) return;
      const id = st.id;
      const mine = new AbortController();
      ctrl = mine;
      set({ ...st, more: 'loading' });
      Promise.resolve()
        .then(unlessAborted(mine.signal, () => load(id, { offset: st.albums.next_offset, limit: ARTIST_PAGE }, mine.signal)))
        .then(
          (resp) => {
            if (ctrl !== mine || mine.signal.aborted) return;
            ctrl = null;
            const entry = { artist: st.artist, albums: appendPage(st.albums, resp.albums) };
            remember(cache, id, entry);
            set({ ...st, ...entry, more: '' });
          },
          (error) => {
            if (ctrl !== mine || mine.signal.aborted || ABORTED(error)) return;
            ctrl = null;
            set({ ...st, more: 'error' });
          },
        );
    },
    state: () => st,
    dispose() {
      disposed = true;
      abort();
    },
  };
}

/**
 * Quality hints from a hit's audio_traits, in this order: "Hi-Res Lossless"
 * (or "Lossless" without it), then "Dolby Atmos". Lossy-only hits have none.
 */
export function qualityHints(traits) {
  const list = Array.isArray(traits) ? traits : [];
  const out = [];
  if (list.includes('hi-res-lossless')) out.push('Hi-Res Lossless');
  else if (list.includes('lossless')) out.push('Lossless');
  if (list.includes('atmos')) out.push('Dolby Atmos');
  return out;
}

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * What a hit row shows: {title, facts, explicit, quality}. facts is the meta
 * line in order as {key, text, auto} (auto: Apple's text, shown dir=auto):
 * a song's artist, album and length; an album's artist, year and track
 * count; an artist's genre.
 */
export function hitView(hit) {
  if (!hit || typeof hit !== 'object') return null;
  const facts = [];
  const add = (key, text, auto = false) => {
    if (text) facts.push({ key, text, auto });
  };
  if (hit.type === 'song') {
    add('artist', hit.artist_name, true);
    // A single's album is named after the song: saying it twice says nothing.
    if (hit.album_name !== hit.name) add('album', hit.album_name, true);
    add('length', trackLength(hit.duration_ms));
  } else if (hit.type === 'album') {
    add('artist', hit.artist_name, true);
    add('year', releaseYear(hit));
    add('tracks', typeof hit.track_count === 'number' && hit.track_count > 0 ? plural(hit.track_count, 'track', 'tracks') : '');
  } else if (hit.type === 'artist') {
    add('genre', hit.genre || 'Artist', Boolean(hit.genre));
  }
  return {
    title: hit.name || '',
    facts,
    explicit: hit.content_rating === 'explicit',
    quality: hit.type === 'artist' ? [] : qualityHints(hit.audio_traits),
  };
}

/** The key a hit is tracked by: its type and id, as a job of it is. */
export const hitKey = (hit) => (hit ? `${hit.type}:${hit.id}` : '');

const ACTIVE = new Set(['queued', 'running']);

/**
 * The job a hit is in the library or the queue through, as lib/lookup.js
 * priorJob reports it: {state: 'active'|'done', job} or null. `sent` maps a
 * hitKey to the id of a job this page submitted for it; `jobs` maps a job id
 * to its latest snapshot (submit results and the feed), which wins over the
 * hit's prior_job from the search. A job that failed, was cancelled or was
 * deleted ({deleted: true}) marks nothing.
 */
export function hitPrior(hit, sent = new Map(), jobs = new Map()) {
  if (!hit) return null;
  const prior = hit.prior_job && hit.prior_job.id ? hit.prior_job : null;
  const id = sent.get(hitKey(hit)) || (prior ? prior.id : '');
  if (!id) return null;
  const job = jobs.get(id) || (prior && prior.id === id ? prior : null);
  if (!job || job.deleted) return null;
  if (ACTIVE.has(job.status)) return { state: 'active', job };
  if (job.status === 'completed') return { state: 'done', job };
  return null;
}

/**
 * Applies feed messages to `jobs` (a job id -> snapshot map) for the ids in
 * `watched`: an upsert replaces the snapshot unless it is older, a delete
 * marks it {deleted: true}. Returns `jobs` itself when nothing changed.
 */
export function trackJobs(jobs, batch, watched) {
  let next = jobs;
  const put = (id, value) => {
    if (next === jobs) next = new Map(jobs);
    next.set(id, value);
  };
  for (const msg of batch || []) {
    if (!msg) continue;
    if (msg.type === 'download_deleted' && watched.has(msg.job_id)) {
      put(msg.job_id, { id: msg.job_id, deleted: true });
      continue;
    }
    const job = msg.type === 'download_upserted' ? msg.job : null;
    if (!job || !job.id || !watched.has(job.id)) continue;
    const have = next.get(job.id);
    if (have && !have.deleted && (Date.parse(job.updated_at || '') || 0) < (Date.parse(have.updated_at || '') || 0)) continue;
    put(job.id, job);
  }
  return next;
}

/** The preview a submitted hit's result row shows until its job resolves (lib/submit.js previewSeed). */
export function hitPreview(hit) {
  if (!hit) return null;
  const out = { title: hit.name, type: hit.type };
  for (const key of ['artist_name', 'artwork_url', 'release_date', 'genre', 'artwork_bg_color', 'artwork_text_color1', 'artwork_text_color2', 'artwork_text_color3', 'artwork_text_color4']) {
    if (hit[key]) out[key] = hit[key];
  }
  if (hit.type === 'artist') out.artist_name = hit.name;
  return out;
}

/** The hits of a search answer in the order rows are shown (and the arrow keys move). */
export function shownHits(data) {
  if (!data) return [];
  return SEARCH_GROUPS.flatMap((g) => (data[g.id] && Array.isArray(data[g.id].items) ? data[g.id].items : []));
}

/**
 * The count line while searching, announced: "Searching…", "6 albums, 6
 * songs and 1 artist for “Yorushika”", "Nothing found for “xyz”", the
 * artist's album count in the artist view, '' otherwise.
 */
export function searchCountLine(st, artist = null) {
  if (artist) {
    if (artist.status === 'loading') return 'Loading albums…';
    if (artist.status !== 'ready' || !artist.albums) return '';
    const n = artist.albums.items.length;
    const name = artist.artist && artist.artist.name ? artist.artist.name : 'This artist';
    return `${name}: ${plural(n, 'album', 'albums')}${artist.albums.next_offset ? ' so far' : ''}`;
  }
  if (!st || !st.term) return '';
  if (st.status === 'loading') return 'Searching…';
  if (st.status !== 'ready' || !st.data) return '';
  const parts = SEARCH_GROUPS.map((g) => {
    const n = st.data[g.id] && Array.isArray(st.data[g.id].items) ? st.data[g.id].items.length : 0;
    return n ? plural(n, g.one, g.many) : '';
  }).filter(Boolean);
  if (!parts.length) return `Nothing found for “${st.term}”`;
  const list = parts.length > 1 ? `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}` : parts[0];
  return `${list} for “${st.term}”`;
}

/**
 * The job a submit result puts a hit in the queue with, for hitPrior: the new
 * job of an accepted URL, or the job already downloading it
 * (duplicate_active), as {id, status}; null otherwise.
 */
export function sentJob(result) {
  if (!result) return null;
  if (result.status === 'accepted' && result.job && result.job.id) return result.job;
  if (result.status === 'duplicate_active' && result.existing_job_id) return { id: result.existing_job_id, status: 'queued' };
  return null;
}
