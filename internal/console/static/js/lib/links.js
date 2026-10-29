// Pure Apple Music link handling for the New download page and the global
// paste: find the links in pasted text and tell what each one points at.
// Hosts, the storefront rule, the kinds and "the id is the last path part"
// follow internal/applemusic (ParseWithAlbumTrackMode), so the console and
// the backend agree on a link.

/** Link kinds the backend downloads ("music-video" only through a collection). */
export const LINK_KINDS = ['album', 'song', 'playlist', 'artist', 'station', 'music-video'];

const HOSTS = new Set(['music.apple.com', 'beta.music.apple.com', 'classical.music.apple.com']);
// Share and embed hosts that serve the same path as music.apple.com, and
// the legacy iTunes Store host, whose album, song, playlist and artist
// paths carry the same storefront, type and id (the id with an "id"
// prefix, dropped by parseLink).
const ALIAS_HOSTS = new Map([
  ['geo.music.apple.com', 'music.apple.com'],
  ['embed.music.apple.com', 'music.apple.com'],
  ['itunes.apple.com', 'music.apple.com'],
]);
// Apple Music Classical has no music videos or stations.
const CLASSICAL_UNSUPPORTED = new Set(['music-video', 'station']);
const STOREFRONT = /^[a-z]{2}$/;

// A candidate is an optional scheme, any subdomains, music.apple.com (or
// itunes.apple.com) and a path, cut at whitespace, quotes, brackets, pipes
// and commas (separators in lists, Markdown and HTML) and before another
// http(s):// (two links pasted without a separator). It must not continue a
// longer host name.
const CANDIDATE = /(?<![\w.@-])(?:https?:\/\/)?(?:[a-z0-9-]+\.)*(?:music|itunes)\.apple\.com\/(?:(?!https?:\/\/)[^\s<>"'`()[\]{}|\\^,])+/gi;
// Sentence punctuation and Markdown emphasis after a link.
const TRAILING = /[.;:!?*_~]+$/;

function toUrl(raw) {
  const s = /^https?:\/\//i.test(raw) ? raw : `https://${raw}`;
  try {
    return new URL(s);
  } catch {
    return null;
  }
}

/**
 * Parses one Apple Music link. Returns null for anything that is not a
 * music.apple.com link (beta. and classical. included; geo., embed. and
 * legacy itunes.apple.com links are read as music.apple.com), else
 * {url, kind, storefront, id, trackId, private, noStorefront}:
 *
 * - url: the normalized link, https://<host>/<path> with the storefront in
 *   lower case, no fragment and no query except an album's ?i=, so variants
 *   with tracking or language parameters compare equal. An iTunes-style id
 *   ("id1463409338") loses its prefix, as the catalog knows it without one.
 * - kind: 'album' | 'song' | 'playlist' | 'artist' | 'station' |
 *   'music-video', or 'unknown' when the backend would reject the link (an
 *   unknown type, a path shorter than storefront/type/id, a storefront that
 *   is not two letters, or a Classical video or station link).
 * - storefront: the two-letter storefront, or '' when there is none.
 * - noStorefront: the path starts with the type, as Apple's share links
 *   without a region do. With `opts.storefront` the link reads as that
 *   storefront's (the backend needs one in the path); without, the kind is
 *   'unknown'.
 * - id: the last path part (the backend's rule), or '' for a short path.
 * - trackId: an album link's ?i= (one song on that album), else null. The
 *   kind stays 'album': the backend downloads that song or the whole album
 *   depending on the runtime catalog.album_track_url_mode ("song" by default).
 * - private: a user playlist (id starts with "pl.u-"), whose cover the
 *   backend reads with catalog.media_user_token.
 */
export function parseLink(input, opts = {}) {
  if (typeof input !== 'string') return null;
  const raw = input.trim();
  if (!raw) return null;
  const u = toUrl(raw);
  if (!u) return null;
  const host = ALIAS_HOSTS.get(u.hostname) || u.hostname;
  if (!HOSTS.has(host)) return null;
  const parts = u.pathname.split('/').filter(Boolean);
  if (parts.length > 0) parts[0] = parts[0].toLowerCase();
  const noStorefront = parts.length >= 2 && LINK_KINDS.includes(parts[0]);
  const fill = opts && typeof opts.storefront === 'string' && STOREFRONT.test(opts.storefront) ? opts.storefront : '';
  if (noStorefront && fill) parts.unshift(fill);
  const storefront = parts.length > 0 && STOREFRONT.test(parts[0]) ? parts[0] : '';
  const type = parts[1] || '';
  if (parts.length >= 3) parts[parts.length - 1] = parts[parts.length - 1].replace(/^id(\d+)$/, '$1');
  let kind = 'unknown';
  if (parts.length >= 3 && storefront && LINK_KINDS.includes(type)) {
    if (!(host === 'classical.music.apple.com' && CLASSICAL_UNSUPPORTED.has(type))) kind = type;
  }
  const id = parts.length >= 3 ? parts[parts.length - 1] : '';
  const i = kind === 'album' ? u.searchParams.get('i') : null;
  const trackId = i ? i : null;
  const isPrivate = kind === 'playlist' && id.startsWith('pl.u-');
  let url = `https://${host}/${parts.join('/')}`;
  if (trackId) url += `?i=${encodeURIComponent(trackId)}`;
  return {
    url,
    kind,
    storefront,
    id,
    trackId,
    private: isPrivate,
    noStorefront,
  };
}

/**
 * The storefront a link without one takes: that of the first link in `text`
 * that has one, else `fallback` (the server's catalog.default_storefront,
 * GET /system `default_storefront`) when it is a storefront, else ''.
 */
export function pasteStorefront(text, fallback = '') {
  const own = typeof fallback === 'string' && STOREFRONT.test(fallback) ? fallback : '';
  if (typeof text !== 'string' || !text) return own;
  for (const m of text.matchAll(CANDIDATE)) {
    const link = parseLink(m[0].replace(TRAILING, ''));
    if (link && link.storefront) return link.storefront;
  }
  return own;
}

/**
 * Finds every Apple Music link in arbitrary text (one per line, separated
 * by spaces or commas, in angle brackets, Markdown links or HTML) and
 * returns their normalized URLs (parseLink().url), first occurrence first,
 * without duplicates. Links of an unknown kind are kept, so the page can say
 * why the backend would refuse them.
 */
export function extractLinks(text, fallback = '') {
  if (typeof text !== 'string' || !text) return [];
  const seen = new Set();
  const out = [];
  const storefront = pasteStorefront(text, fallback);
  for (const m of text.matchAll(CANDIDATE)) {
    const link = parseLink(m[0].replace(TRAILING, ''), { storefront });
    if (!link || seen.has(link.url)) continue;
    seen.add(link.url);
    out.push(link.url);
  }
  return out;
}

// Any other http(s) link, so the page can flag a line that is not Apple Music.
// A scheme starts one even right after another link's last character.
const OTHER_URL = /https?:\/\/(?:(?!https?:\/\/)[^\s<>"'`()[\]{}|\\^,])+/gi;
// What a line may still hold once its links are cut out and it counts as empty.
const LEFTOVER = /^[\s,;:<>()[\]"'`|*_~.-]*$/;

/**
 * Link-shaped spans of one string in order: Apple Music candidates first,
 * then other URLs that do not overlap them. `storefront` fills links
 * without one (pasteStorefront of the whole text).
 */
function spans(text, storefront) {
  const out = [];
  for (const m of text.matchAll(CANDIDATE)) {
    out.push({ start: m.index, raw: m[0].replace(TRAILING, ''), apple: true });
  }
  for (const m of text.matchAll(OTHER_URL)) {
    const end = m.index + m[0].length;
    if (out.some((s) => s.apple && m.index < s.start + s.raw.length && s.start < end)) continue;
    out.push({ start: m.index, raw: m[0].replace(TRAILING, ''), apple: false });
  }
  out.sort((a, b) => a.start - b.start);
  for (const s of out) {
    s.link = parseLink(s.raw, { storefront });
    s.url = s.link ? s.link.url : s.raw;
  }
  return out;
}

/**
 * Every link in arbitrary text, in order and without duplicates:
 * {url, raw, link}. Apple Music links carry parseLink()'s result in `link`
 * and its normalized URL in `url`; any other http(s) URL has `link: null`
 * and `url` equal to the text as written (minus trailing punctuation), so
 * the page can say it is not an Apple Music link. Free text around the
 * links is ignored. `fallback` is pasteStorefront's.
 */
export function scanLinks(text, fallback = '') {
  if (typeof text !== 'string' || !text) return [];
  const seen = new Set();
  const out = [];
  for (const s of spans(text, pasteStorefront(text, fallback))) {
    if (seen.has(s.url)) continue;
    seen.add(s.url);
    out.push({ url: s.url, raw: s.raw, link: s.link });
  }
  return out;
}

/** How many links in `text` repeat an earlier one (scanLinks lists each once). */
export function duplicateCount(text, fallback = '') {
  if (typeof text !== 'string' || !text) return 0;
  const all = spans(text, pasteStorefront(text, fallback));
  return all.length - new Set(all.map((s) => s.url)).size;
}

/**
 * Removes every occurrence of one link (a scanLinks() `url`) from text. A
 * line left with nothing but separators and brackets is dropped; other
 * text on the line stays.
 */
export function removeLink(text, url) {
  if (typeof text !== 'string' || !text || !url) return text || '';
  // A link without a storefront matches under the storefront `url` has,
  // whatever the rest of the text still holds.
  const target = parseLink(url);
  const storefront = target ? target.storefront : '';
  const lines = [];
  for (const line of text.split('\n')) {
    let next = line;
    for (const s of spans(line, storefront).reverse()) {
      if (s.url === url) next = next.slice(0, s.start) + next.slice(s.start + s.raw.length);
    }
    if (next !== line && LEFTOVER.test(next)) continue;
    lines.push(next);
  }
  return lines.join('\n');
}

/** `text` with `urls` appended on new lines (normalized), skipping links it already holds. */
export function mergeLinks(text, urls, fallback = '') {
  const have = new Set(scanLinks(text, fallback).map((e) => e.url));
  const add = [];
  for (const u of urls || []) {
    const e = scanLinks(u, fallback)[0];
    const key = e ? e.url : u;
    if (have.has(key)) continue;
    have.add(key);
    add.push(key);
  }
  if (!add.length) return text;
  const head = text.replace(/\s+$/, '');
  return head ? `${head}\n${add.join('\n')}` : add.join('\n');
}
