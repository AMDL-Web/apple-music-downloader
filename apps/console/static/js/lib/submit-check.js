// Pure checks before a New download submit: why a link cannot be sent
// (or needs attention), the storefronts the wrapper serves, which links
// are sent, and what the Download button and the count line say. The
// submit itself and its results are lib/submit.js.

import { PREVIEW_KINDS, shortError } from './lookup.js';
import { capitalized } from './format.js';
import { settingsHash } from './route.js';
import { CODES } from './submit.js';

/** Where Settings edits catalog.media_user_token, the only token a download uses. */
export const TOKEN_SETTINGS_HASH = settingsHash('account', 'catalog.media_user_token');

/**
 * Why a scanned link (lib/links.js scanLinks entry) cannot be downloaded or
 * needs attention, or null when it is fine. `lookup` is its preview status
 * (lib/lookup.js), `hasToken` whether Settings holds a media-user-token,
 * `regions` the storefronts the wrapper's account serves (servedRegions;
 * null when unknown, which flags nothing). Returns {code, role, blocking, message, title?, href?, action?}; a blocking
 * issue keeps the link out of the submission, `title` holds a long server
 * message in full, and `href`/`action` name a page that fixes the issue.
 */
export function linkIssue(entry, lookup = null, { hasToken = false, regions = null } = {}) {
  const link = entry && entry.link;
  if (!link) return { code: 'not_apple', role: 'danger', blocking: true, message: 'Not an Apple Music link' };
  if (link.kind === 'music-video') return { code: 'music_video', role: 'danger', blocking: true, message: 'Music videos cannot be downloaded' };
  if (link.kind === 'unknown' && link.noStorefront) {
    return { code: 'no_storefront', role: 'danger', blocking: true, message: 'No storefront in this link: add one, as in music.apple.com/us/album/…' };
  }
  if (!PREVIEW_KINDS.includes(link.kind)) {
    return { code: 'unsupported', role: 'danger', blocking: true, message: 'Not an album, song, playlist, artist or station link' };
  }
  // The same refusal the submit gives (unsupported_storefront), read from
  // the wrapper's status first. The backend still has the last word.
  if (Array.isArray(regions) && regions.length && link.storefront && !regions.includes(link.storefront)) {
    const [label, reason] = CODES.unsupported_storefront;
    return { code: 'storefront_not_served', role: 'danger', blocking: true, message: `${label}. ${reason}. ${servedNote(regions)}` };
  }
  const err = lookup && lookup.state === 'error' ? lookup.error : null;
  const msg = err ? String(err.message || '') : '';
  // Apple answered 404 for the id: a job of this link could only fail. The
  // catalog read never carries the media-user-token (internal/applemusic
  // Playlist), so this holds for private playlists too.
  const notFound = Boolean(err && err.status === 404 && err.code === 'catalog_not_found');
  if (notFound) {
    return { code: 'not_found', role: 'danger', blocking: true, message: 'Apple Music has nothing at this link', title: err.message };
  }
  // Apple refuses a storefront it does not have with a 400, which the lookup
  // passes on as 502 upstream_error; a job of the link could only fail. The
  // row's text is built from the link, not the message.
  const badStorefront = /storefront/i.test(msg) && (/\b400\b/.test(msg) || /unknown storefront|not know the storefront/i.test(msg));
  if (err && (err.status === 502 || err.status === 400) && badStorefront) {
    return { code: 'bad_storefront', role: 'danger', blocking: true, message: `Apple Music has no storefront “${link.storefront.toUpperCase()}”`, title: msg };
  }
  if (err && err.status === 400) return { code: 'refused', role: 'danger', blocking: true, message: capitalized(err.message) || 'The backend does not download this link' };
  // Only a station cannot download without the token; a private playlist
  // uses it for its cover alone.
  const required = link.kind === 'station' || Boolean(lookup && lookup.state === 'ready' && lookup.data && lookup.data.needs_media_user_token);
  if (required && !hasToken) {
    return {
      code: 'needs_token',
      role: 'warning',
      blocking: false,
      message: link.kind === 'station' ? 'Stations need a media-user-token' : 'This link needs a media-user-token',
      href: TOKEN_SETTINGS_HASH,
      action: 'Set it in Settings',
    };
  }
  if (err && err.code !== 'aborted') {
    return { code: 'preview_failed', role: 'warning', blocking: false, message: `No preview: ${shortError(err.message) || 'the lookup failed'}`, title: err.message };
  }
  return null;
}

/**
 * The storefronts a wrapper status result ({data?, error?} from GET
 * wrapper/status) says its account serves, lower case, or null when that
 * is not known: the status is not loaded or failed, the wrapper is not ready
 * or has no account, it lists none, or `simulate` is on (the backend then
 * skips the storefront check).
 */
export function servedRegions(res, simulate = false) {
  const w = res && !res.error ? res.data : null;
  if (simulate || !w || !w.ready || w.logged_in === false) return null;
  const list = Array.isArray(w.regions) ? w.regions.filter((r) => typeof r === 'string' && r).map((r) => r.toLowerCase()) : [];
  return list.length ? list : null;
}

/** "It serves CN, US." for a list of storefronts, '' for none. */
export function servedNote(storefronts) {
  return Array.isArray(storefronts) && storefronts.length ? `It serves ${storefronts.map((s) => s.toUpperCase()).join(', ')}.` : '';
}

/** The URLs to submit: every link without a blocking issue, in order. */
export function submittable(entries, issueOf) {
  return entries.filter((e) => e.link && !(issueOf(e) || {}).blocking).map((e) => e.url);
}

const NOUNS = {
  album: ['album', 'albums'],
  song: ['song', 'songs'],
  playlist: ['playlist', 'playlists'],
  artist: ['artist', 'artists'],
  station: ['station', 'stations'],
};

/**
 * The Download button's text for the kinds of the links it sends, in
 * order: "Download 1 album", "Download 2 songs", "Download 3 items" for
 * mixed kinds. With nothing to send: "Paste a link to start" while the box
 * has no links, "Select a link to download" while some link could be sent
 * but none is checked (`pickable`), else "Download".
 */
export function downloadLabel(kinds, hasLinks = true, pickable = false) {
  const list = Array.isArray(kinds) ? kinds : [];
  if (!list.length) return !hasLinks ? 'Paste a link to start' : pickable ? 'Select a link to download' : 'Download';
  const same = list.every((k) => k === list[0]) ? NOUNS[list[0]] : null;
  const [one, many] = same || ['item', 'items'];
  return `Download ${list.length} ${list.length === 1 ? one : many}`;
}

/**
 * The dock's count line: "4 links · 1 can't download · 1 merged · 1 new · 2
 * in library"; '' with no links. What stops or needs a look comes first, and
 * each part is short, so two lines on a phone hold it. `blocked` counts the
 * links that cannot be sent, `warned` those with a warning, `merged`
 * repeats folded into one link, `fresh` links that can be sent and were
 * never downloaded, `library` and `queued` those with an earlier job done
 * or still in the queue.
 */
export function countLine({ links = 0, merged = 0, fresh = 0, library = 0, queued = 0, blocked = 0, warned = 0 } = {}) {
  if (!links) return '';
  return [
    `${links} ${links === 1 ? 'link' : 'links'}`,
    blocked ? `${blocked} can't download` : '',
    warned ? `${warned} to check` : '',
    merged ? `${merged} merged` : '',
    fresh ? `${fresh} new` : '',
    library ? `${library} in library` : '',
    queued ? `${queued} queued` : '',
  ]
    .filter(Boolean)
    .join(' · ');
}
