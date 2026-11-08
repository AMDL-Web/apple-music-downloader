// New download: the state behind catalog search (views/download-search.js
// draws it). useSearch runs the searcher and an open artist's pages
// (lib/search.js) and keeps the hits' record marks live (lib/marks.js);
// useHitActions downloads a hit and opens or closes an artist or a group
// shown alone as a history entry; toResults takes the text box's keys to the results.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { searchCatalog, getArtistAlbums } from '../api.js';
import { buildHash } from '../lib/route.js';
import { createSearcher, createArtistPages, searchGroup, hitKey, hitPreview } from '../lib/search.js';
import { resourceMark, applyResourceFeed, applySentSongs, withLatest, submitMark, withSubmitted, markedFiles } from '../lib/marks.js';

// Answers outlive the page for the life of the tab, like link previews.
const SEARCHES = new Map();
const ARTISTS = new Map();

/**
 * The page's search: `term` (lib/search.js searchTerm) is searched once
 * it has been stable for `debounced`; `artistId` (#/new?artist=) opens that
 * artist while there is a term. Returns {search, artist, pending, more,
 * retry, artistMore, artistRetry, runNow, markOf, recordSent}.
 * `subscribeFeed` keeps the record marks live.
 */
export function useSearch({ term, debounced, artistId, epoch, subscribeFeed }) {
  const [, setTick] = useState(0);
  const rerender = () => setTick((n) => n + 1);
  const searcher = useRef(null);
  const artists = useRef(null);
  if (!searcher.current) {
    searcher.current = createSearcher({ search: (q, signal) => searchCatalog(q, { signal }), cache: SEARCHES, onChange: rerender });
    artists.current = createArtistPages({ load: (id, q, signal) => getArtistAlbums(id, q, { signal }), cache: ARTISTS, onChange: rerender });
  }
  useEffect(
    () => () => {
      searcher.current.dispose();
      artists.current.dispose();
    },
    [],
  );
  useEffect(() => searcher.current.run(debounced), [debounced]);
  useEffect(() => artists.current.open(term ? artistId || '' : ''), [artistId, Boolean(term)]);
  // After a feed resync a failed search or artist runs again.
  const firstEpoch = useRef(epoch);
  useEffect(() => {
    if (firstEpoch.current === epoch) return;
    searcher.current.retry();
    artists.current.retry();
  }, [epoch]);

  // The feed's newest record marks, the record each hit this page sent
  // named, and the songs it sent with their run's outcome.
  const [latest, setLatest] = useState(() => new Map());
  const [sentRes, setSentRes] = useState(() => new Map());
  const [songs, setSongs] = useState(() => new Map());
  const search = searcher.current.state();
  const artist = artists.current.state();
  useEffect(
    () =>
      subscribeFeed((batch) => {
        setLatest((m) => applyResourceFeed(m, batch));
        setSongs((m) => applySentSongs(m, batch));
      }),
    [],
  );
  /** A hit's mark: its record (the search's, else the one its submit named) with the feed's news. */
  const markOf = (hit) => {
    if (!hit) return null;
    const r = hit.resource || sentRes.get(hitKey(hit)) || null;
    if (!r) return null;
    if (hit.type !== 'song') return resourceMark(withLatest(r, latest), undefined, hit.track_count);
    const sent = songs.get(hit.id);
    return resourceMark(withLatest(r, latest), sent ? sent.state : r.song_state || null);
  };

  return {
    search,
    artist,
    // Typed but not searched yet (the debounce): the card shows it is coming.
    pending: Boolean(term) && term !== search.term,
    more: (group) => searcher.current.more(group),
    retry: () => searcher.current.retry(),
    artistMore: () => artists.current.more(),
    artistRetry: () => artists.current.retry(),
    runNow: () => searcher.current.run(term),
    markOf,
    /** Takes a submit's or a reset's result for a hit into its mark at once. */
    recordSent: (hit, result) => {
      const m = submitMark(result);
      if (!m) return;
      setSentRes((x) => new Map(x).set(hitKey(hit), { kind: m.kind, id: m.id }));
      setLatest((x) => withSubmitted(x, m));
      if (hit.type === 'song') setSongs((x) => new Map(x).set(hit.id, { jobId: m.active_job_id, state: 'pending' }));
    },
  };
}

/**
 * The page's side of the hits. download(hit) sends a song's, album's or playlist's url
 * alone through the page's `send` and `report` (its result joins the
 * results), after the page's questions (`ask(hit, run, again)`, which
 * calls run(overrides)), and asks first through `setConfirm` for an artist,
 * whose url queues every album as an artist link does. again(hit, mark)
 * downloads a hit on disk again, replacing its files, after the page's
 * confirmation (a reset of its record, or another quality). openArtist(hit) opens the
 * artist over the results as a history entry (#/new?artist=), so Back, the
 * browser's or the card's (closeArtist), returns to them. Returns {busy
 * (urls being sent), download, again, openArtist, closeArtist, openedHit (the hit
 * the open artist came from, for its head until the answer), focusArtist
 * (whether this page opened it), group (#/new?group=, the group shown
 * alone while there is a term), openGroup(id), closeGroup}. openGroup from
 * every group is a history entry, from another group it replaces the
 * entry, so Back, the browser's or the card's (closeGroup), returns to
 * every group.
 */
export function useHitActions({ app, route, term, artistId, search, send, report, ask, setConfirm, clearNotes }) {
  const [busy, setBusy] = useState(() => new Set());
  const sendHit = async (hit, overrides) => {
    if (busy.has(hit.url)) return;
    clearNotes();
    setBusy((b) => new Set(b).add(hit.url));
    const { pairs, fail } = await send([hit.url], overrides);
    setBusy((b) => new Set([...b].filter((u) => u !== hit.url)));
    if (pairs.length) search.recordSent(hit, pairs[0].result);
    report(pairs, fail, () => hitPreview(hit), true, overrides);
  };
  const start = (hit, again = 0, mark = null) => {
    setConfirm(null);
    if (!busy.has(hit.url)) ask(hit, (overrides) => sendHit(hit, overrides), again, mark);
  };
  const download = (hit) => (hit.type === 'artist' ? setConfirm({ names: [hit.name], label: 'Download all', run: () => start(hit) }) : start(hit));
  const downloadAgain = (hit, mark) => start(hit, markedFiles(mark), mark);

  const opened = useRef(null);
  const pushed = useRef(false);
  const openArtist = (hit) => {
    opened.current = hit;
    pushed.current = true;
    app.navigate(buildHash('new', { ...route.params, q: term, artist: hit.id }, route.jobId));
  };
  const closeArtist = () => {
    if (pushed.current) window.history.back();
    else app.navigate(buildHash('new', { ...route.params, artist: '' }, route.jobId), { replace: true });
  };
  useEffect(() => {
    if (!artistId) pushed.current = false;
  }, [artistId]);

  const group = term ? searchGroup(route.params.group) : '';
  const pushedGroup = useRef(false);
  const openGroup = (id) => {
    if (!group) pushedGroup.current = true;
    app.navigate(buildHash('new', { ...route.params, q: term, group: id, artist: '' }, route.jobId), { replace: Boolean(group) });
  };
  const closeGroup = () => {
    if (pushedGroup.current) window.history.back();
    else app.navigate(buildHash('new', { ...route.params, group: '' }, route.jobId), { replace: true });
  };
  useEffect(() => {
    if (!group) pushedGroup.current = false;
  }, [group]);

  return {
    busy,
    download,
    again: downloadAgain,
    openArtist,
    closeArtist,
    openedHit: opened.current && opened.current.id === artistId ? opened.current : null,
    // An artist this page opened (not one a reload or a link opened) takes focus.
    focusArtist: pushed.current,
    group,
    openGroup,
    closeGroup,
  };
}

/**
 * The text box's keys in a search: Return, and ArrowDown on its last line,
 * go to the first hit; Return searches at once while the answer for `term`
 * is not in yet.
 */
export function toResults(e, term, s) {
  if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
  const first = () => document.querySelector('.new-search [data-hit-primary]:not(:disabled):not([aria-disabled="true"])');
  const el = e.currentTarget;
  if (e.key === 'Enter') {
    e.preventDefault();
    const stop = s.search.term === term && s.search.status === 'ready' ? first() : null;
    if (stop) stop.focus();
    else s.runNow();
  } else if (e.key === 'ArrowDown' && !el.value.slice(el.selectionEnd).includes('\n')) {
    const stop = first();
    if (stop) {
      e.preventDefault();
      stop.focus();
    }
  }
}
