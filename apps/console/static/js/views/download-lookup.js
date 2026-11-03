// New download: the link previews behind the rows, a POST lookup per
// previewable link through a shared per-tab cache and a bounded queue, and
// from them which links are flagged and which the submit may send, and the
// record each one has (the lookup's `resource`, lib/marks.js). The feed's
// resource_updated keeps the marks live; a song link is looked up again
// when its album's run ends, for its own track's state.

import { useState, useEffect, useRef, useMemo } from '../ui/preact.js';
import { lookupLink } from '../api.js';
import { scanLinks, parseLink } from '../lib/links.js';
import { createLookupQueue, PREVIEW_KINDS, LOOKUP_LIMIT } from '../lib/lookup.js';
import { linkIssue, submittable } from '../lib/submit-check.js';
import { resourceMark, applyResourceFeed, withLatest, submitMark, resourceKey } from '../lib/marks.js';

// Previews outlive the page, so coming back does not look links up again.
// An album ?i= link previews as a song or the album depending on
// catalog.album_track_url_mode, so those answers hold only for the mode
// they were read under (LOOKUPS_MODE), and a private playlist's cover only
// for the sign-in state then (LOOKUPS_TOKEN, tokenState's key).
const LOOKUPS = new Map();
let LOOKUPS_MODE = null;
let LOOKUPS_TOKEN = null;
// The feed's newest record marks (lib/marks.js applyResourceFeed), for the tab.
let LATEST = new Map();
// Links this tab submitted, with the record each answer named: url ->
// {kind, id}. A song link's own track waits until its album's run ends.
const SENT = new Map();

/** The record a link's answer or its submit named, or null. */
function recordOf(url, entry) {
  const r = entry && entry.state === 'ready' && entry.data ? entry.data.resource : null;
  return r || SENT.get(url) || null;
}

const isPrivate = (url) => {
  const link = parseLink(url);
  return Boolean(link && link.private);
};

const isAlbumTrack = (url) => {
  const link = parseLink(url);
  return Boolean(link && link.trackId);
};

/**
 * The page's link previews: the lookup queue, the status and issue of each
 * entry, the links the submit may send, and each link's mark (markOf).
 * `settled` is the debounced text, `entries` its scanned links, `token`
 * whether a signed-in account gives a download its media-user-token (lib/user-token.js tokenState)
 * and `trackMode` the server's catalog config, `epoch` the shell's reconnect counter (a resync after an
 * outage looks failed previews and the history up again), `storefront` the
 * server's default storefront for links without one (see scanLinks),
 * `subscribeFeed` the shell's feed (app.subscribeFeed), `regions` the
 * storefronts the wrapper serves (null while unknown).
 */
export function useLookups({ settled, entries, token, regions, trackMode, epoch, storefront, subscribeFeed }) {
  // Previews: the first LOOKUP_LIMIT previewable links, 4 at a time.
  const [, setTick] = useState(0);
  const queue = useRef(null);
  if (!queue.current) {
    queue.current = createLookupQueue({
      lookup: (url, signal) => lookupLink(url, { signal }),
      cache: LOOKUPS,
      onChange: () => setTick((n) => n + 1),
    });
  }
  useEffect(() => () => queue.current.dispose(), []);
  const previewable = (list) => list.filter((e) => e.link && PREVIEW_KINDS.includes(e.link.kind));
  const wanted = useMemo(() => previewable(scanLinks(settled, storefront)).slice(0, LOOKUP_LIMIT).map((e) => e.url), [settled, storefront]);
  useEffect(() => queue.current.want(wanted), [wanted.join('\n')]);

  // The feed's record marks: a run that ends reads its song links again,
  // whose own track's state the record's counts do not say. The feed is
  // heard only while the page is open, so coming back reads the answers
  // that name a record again.
  const stale = useRef(null);
  if (stale.current === null) {
    stale.current = [...LOOKUPS.keys()].filter((url) => SENT.has(url) || recordOf(url, LOOKUPS.get(url)));
    LATEST = new Map();
    SENT.clear();
  }
  useEffect(() => {
    if (stale.current.length) queue.current.refresh(stale.current);
  }, []);
  useEffect(
    () =>
      subscribeFeed((batch) => {
        const next = applyResourceFeed(LATEST, batch);
        if (next === LATEST) return;
        const ended = [];
        for (const [url, entry] of LOOKUPS) {
          const r = recordOf(url, entry);
          if (!r || !entry.data || entry.data.type !== 'song') continue;
          const was = withLatest(r, LATEST);
          const now = withLatest(r, next);
          if (was.active_job_id && !now.active_job_id) ended.push(url);
        }
        LATEST = next;
        for (const url of ended) SENT.delete(url);
        if (ended.length) queue.current.refresh(ended);
        setTick((n) => n + 1);
      }),
    [],
  );

  // After an outage the shell resyncs and bumps epoch: failed previews and
  // every history answer are read again; good previews stay.
  const epochSeen = useRef(epoch);
  useEffect(() => {
    if (epochSeen.current === epoch) return;
    epochSeen.current = epoch;
    queue.current.forget([...LOOKUPS.keys()].filter((k) => LOOKUPS.get(k).state === 'error' || (LOOKUPS.get(k).data && LOOKUPS.get(k).data.resource)));
    LATEST = new Map();
  }, [epoch]);
  // A changed album_track_url_mode reads album ?i= links again.
  useEffect(() => {
    if (trackMode === null) return;
    if (LOOKUPS_MODE !== null && LOOKUPS_MODE !== trackMode) {
      queue.current.forget([...LOOKUPS.keys()].filter(isAlbumTrack));
    }
    LOOKUPS_MODE = trackMode;
  }, [trackMode]);
  // A sign-in or sign-out reads private playlists again.
  useEffect(() => {
    if (!token.known) return;
    if (LOOKUPS_TOKEN !== null && LOOKUPS_TOKEN !== token.key) queue.current.forget([...LOOKUPS.keys()].filter(isPrivate));
    LOOKUPS_TOKEN = token.key;
  }, [token.known, token.key]);
  const previewSet = new Set(previewable(entries).slice(0, LOOKUP_LIMIT).map((e) => e.url));
  const skipped = Math.max(0, previewable(entries).length - LOOKUP_LIMIT);
  const statusOf = (url) => {
    const s = queue.current.status(url);
    return s.state === 'idle' && previewSet.has(url) ? { state: 'queued' } : s;
  };

  // Until the token is known nothing is flagged for a missing one.
  const hasToken = !token.known || token.present;
  const issueOf = (e) => linkIssue(e, e.link && PREVIEW_KINDS.includes(e.link.kind) ? queue.current.status(e.url) : null, { hasToken, regions });
  const sendable = submittable(entries, issueOf);
  const flagged = entries.filter((e) => (issueOf(e) || {}).blocking).length;
  const warned = entries.filter((e) => {
    const i = issueOf(e);
    return i && !i.blocking;
  }).length;
  /**
   * What a link's row shows (lib/marks.js resourceMark): the record its
   * lookup or its submit named, with the feed's news; a song by its own
   * track, which waits from its submit until its album's run ends.
   */
  const markOf = (url) => {
    const s = queue.current.status(url);
    const r = recordOf(url, s);
    if (!r) return null;
    const song = s.state === 'ready' && s.data && s.data.type === 'song';
    const songState = !song ? undefined : SENT.has(url) ? 'pending' : r.song_state || null;
    return resourceMark(withLatest(r, LATEST), songState);
  };
  /** Takes the records a submit named into the marks at once, before the feed says so. */
  const noteSubmitted = (pairs) => {
    let changed = false;
    for (const p of pairs) {
      const m = submitMark(p.result);
      if (!m) continue;
      if (p.url) SENT.set(p.url, { kind: m.kind, id: m.id });
      const key = resourceKey(m);
      LATEST = new Map(LATEST).set(key, { ...(LATEST.get(key) || {}), ...m });
      changed = true;
    }
    if (changed) setTick((n) => n + 1);
  };

  return { statusOf, issueOf, sendable, flagged, warned, skipped, queue, markOf, noteSubmitted };
}
