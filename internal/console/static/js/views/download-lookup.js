// New download: the link previews behind the rows, a POST lookup per
// previewable link through a shared per-tab cache and a bounded queue, and
// from them which links are flagged and which the submit may send. A second
// queue asks GET downloads whether an album, song or playlist was
// downloaded before (lib/lookup.js priorJob).

import { useState, useEffect, useRef, useMemo } from '../ui/preact.js';
import { lookupLink, listDownloads } from '../api.js';
import { scanLinks, parseLink } from '../lib/links.js';
import { createLookupQueue, priorJob, priorQuery, PREVIEW_KINDS, LOOKUP_LIMIT } from '../lib/lookup.js';
import { linkIssue, submittable } from '../lib/submit.js';

// Previews outlive the page, so coming back does not look links up again.
// An album ?i= link previews as a song or the album depending on
// catalog.album_track_url_mode, so those answers hold only for the mode
// they were read under (LOOKUPS_MODE), and a private playlist's cover only
// for whether Settings held a media-user-token then (LOOKUPS_TOKEN).
const LOOKUPS = new Map();
let LOOKUPS_MODE = null;
let LOOKUPS_TOKEN = null;
// GET downloads?q=<id> pages per link, for the "Downloaded Oct 29" note.
const HISTORY = new Map();
const HISTORY_KINDS = ['album', 'song', 'playlist'];

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
 * entry, the links the submit may send, and each link's earlier job.
 * `settled` is the debounced text, `entries` its scanned links, `serverToken`
 * (whether Settings holds a media-user-token, null until the config loads)
 * and `trackMode` the server's catalog config, `epoch` the shell's reconnect counter (a resync after an
 * outage looks failed previews and the history up again), `storefront` the
 * server's default storefront for links without one (see scanLinks).
 */
export function useLookups({ settled, entries, serverToken, trackMode, epoch, storefront }) {
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

  // Earlier jobs of the wanted albums, songs and playlists, 2 at a time.
  // The search term is the id a job would download (priorQuery), under the
  // track mode in force when it runs.
  const modeRef = useRef(trackMode);
  modeRef.current = trackMode || 'song';
  const history = useRef(null);
  if (!history.current) {
    history.current = createLookupQueue({
      lookup: (url) => listDownloads({ q: priorQuery(parseLink(url), modeRef.current), limit: 50 }),
      cache: HISTORY,
      max: 2,
      onChange: () => setTick((n) => n + 1),
    });
  }
  useEffect(() => () => history.current.dispose(), []);
  const historyWanted = wanted.filter((u) => HISTORY_KINDS.includes((parseLink(u) || {}).kind));
  useEffect(() => history.current.want(historyWanted), [historyWanted.join('\n')]);

  // After an outage the shell resyncs and bumps epoch: failed previews and
  // every history answer are read again; good previews stay.
  const epochSeen = useRef(epoch);
  useEffect(() => {
    if (epochSeen.current === epoch) return;
    epochSeen.current = epoch;
    queue.current.forget([...LOOKUPS.keys()].filter((k) => LOOKUPS.get(k).state === 'error'));
    history.current.forget([...HISTORY.keys()]);
  }, [epoch]);
  // A changed album_track_url_mode reads album ?i= links again.
  useEffect(() => {
    if (trackMode === null) return;
    if (LOOKUPS_MODE !== null && LOOKUPS_MODE !== trackMode) {
      queue.current.forget([...LOOKUPS.keys()].filter(isAlbumTrack));
      history.current.forget([...HISTORY.keys()].filter(isAlbumTrack));
    }
    LOOKUPS_MODE = trackMode;
  }, [trackMode]);
  // A token set or cleared in Settings reads private playlists again.
  useEffect(() => {
    if (serverToken === null) return;
    if (LOOKUPS_TOKEN !== null && LOOKUPS_TOKEN !== serverToken) queue.current.forget([...LOOKUPS.keys()].filter(isPrivate));
    LOOKUPS_TOKEN = serverToken;
  }, [serverToken]);
  const previewSet = new Set(previewable(entries).slice(0, LOOKUP_LIMIT).map((e) => e.url));
  const skipped = Math.max(0, previewable(entries).length - LOOKUP_LIMIT);
  const statusOf = (url) => {
    const s = queue.current.status(url);
    return s.state === 'idle' && previewSet.has(url) ? { state: 'queued' } : s;
  };

  // Until the config has loaded nothing is flagged for a missing token.
  const hasToken = serverToken !== false;
  const issueOf = (e) => linkIssue(e, e.link && PREVIEW_KINDS.includes(e.link.kind) ? queue.current.status(e.url) : null, { hasToken });
  const sendable = submittable(entries, issueOf);
  const flagged = entries.filter((e) => (issueOf(e) || {}).blocking).length;
  const warned = entries.filter((e) => {
    const i = issueOf(e);
    return i && !i.blocking;
  }).length;
  /** The earlier job of a link ({state: 'active' | 'done', job}) or null. */
  const priorOf = (url) => {
    const s = history.current.status(url);
    return s.state === 'ready' && s.data ? priorJob(s.data.downloads, parseLink(url), trackMode || 'song') : null;
  };
  /** Forgets the earlier-job answers of links just submitted. */
  const forgetPrior = (urls) => history.current.forget(urls);

  return { statusOf, issueOf, sendable, flagged, warned, skipped, queue, priorOf, forgetPrior };
}
