// New download: the link previews behind the rows, a POST lookup per
// previewable link through a shared per-tab cache and a bounded queue, and
// from them which links are flagged and which the submit may send.

import { useState, useEffect, useRef, useMemo } from '../ui/preact.js';
import { lookupLink } from '../api.js';
import { scanLinks, parseLink } from '../lib/links.js';
import { createLookupQueue, PREVIEW_KINDS, PREVIEW_LIMIT } from '../lib/lookup.js';
import { linkIssue, submittable, needsMediaUserToken } from '../lib/submit.js';
import { useDebounced } from '../ui/hooks.js';

// Previews outlive the page, so coming back does not look links up again.
// An album ?i= link previews as a song or the album depending on
// catalog.album_track_url_mode, so those answers hold only for the mode
// they were read under (LOOKUPS_MODE).
const LOOKUPS = new Map();
let LOOKUPS_MODE = null;

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
 * entry, and the links the submit may send. `settled` is the debounced
 * text, `entries` its scanned links, `token` the typed media-user-token,
 * `serverToken` and `trackMode` the server's catalog config.
 */
export function useLookups({ settled, entries, token, serverToken, trackMode }) {
  // Previews: the first PREVIEW_LIMIT previewable links, 4 at a time. A
  // private playlist is read with the typed token once it settles; answers
  // read with it leave the shared cache with the page, whose field starts
  // empty next time.
  const settledToken = useDebounced(token.trim(), 350);
  const tokenRef = useRef(settledToken);
  tokenRef.current = settledToken;
  const readWithToken = useRef(new Set());
  const [, setTick] = useState(0);
  const queue = useRef(null);
  if (!queue.current) {
    queue.current = createLookupQueue({
      lookup: (url, signal) => {
        const t = isPrivate(url) ? tokenRef.current : '';
        if (t) readWithToken.current.add(url);
        else readWithToken.current.delete(url);
        return lookupLink(url, { signal, mediaUserToken: t });
      },
      cache: LOOKUPS,
      onChange: () => setTick((n) => n + 1),
    });
  }
  useEffect(
    () => () => {
      queue.current.dispose();
      for (const url of readWithToken.current) LOOKUPS.delete(url);
    },
    [],
  );
  const previewable = (list) => list.filter((e) => e.link && PREVIEW_KINDS.includes(e.link.kind));
  const wanted = useMemo(() => previewable(scanLinks(settled)).slice(0, PREVIEW_LIMIT).map((e) => e.url), [settled]);
  useEffect(() => queue.current.want(wanted), [wanted.join('\n')]);
  // A changed album_track_url_mode reads album ?i= links again.
  useEffect(() => {
    if (trackMode === null) return;
    if (LOOKUPS_MODE !== null && LOOKUPS_MODE !== trackMode) queue.current.forget([...LOOKUPS.keys()].filter(isAlbumTrack));
    LOOKUPS_MODE = trackMode;
  }, [trackMode]);
  // A changed token reads private playlists again.
  const tokenSeen = useRef(settledToken);
  useEffect(() => {
    if (tokenSeen.current === settledToken) return;
    tokenSeen.current = settledToken;
    queue.current.forget([...new Set([...wanted, ...LOOKUPS.keys()])].filter(isPrivate));
  }, [settledToken]);
  const previewSet = new Set(previewable(entries).slice(0, PREVIEW_LIMIT).map((e) => e.url));
  const skipped = Math.max(0, previewable(entries).length - PREVIEW_LIMIT);
  const statusOf = (url) => {
    const s = queue.current.status(url);
    return s.state === 'idle' && previewSet.has(url) ? { state: 'queued' } : s;
  };

  // The settled token, like the lookups: a preview read without it is not
  // judged against a token it was never read with.
  const hasToken = settledToken !== '' || serverToken;
  const issueOf = (e) => linkIssue(e, e.link && PREVIEW_KINDS.includes(e.link.kind) ? queue.current.status(e.url) : null, { hasToken });
  const sendable = submittable(entries, issueOf);
  const flagged = entries.filter((e) => issueOf(e)).length;
  const lookupStatus = (url) => queue.current.status(url);
  const needsToken = needsMediaUserToken(sendable, lookupStatus);

  return { statusOf, issueOf, sendable, flagged, skipped, needsToken, lookupStatus, queue };
}
