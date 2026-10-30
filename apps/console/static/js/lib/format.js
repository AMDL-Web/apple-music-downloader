// Pure formatting helpers. No DOM access, so node:test covers them.

export * from './format-codec.js';

const numberFormat = new Intl.NumberFormat('en-US');
const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

const PLACEHOLDER = /\{w\}|\{h\}|\{f\}/;

/** Whether an Apple artwork URL is a {w}x{h}/{f} template. Private playlists
 * carry pre-signed URLs without placeholders, which must be used verbatim. */
export function isArtworkTemplate(url) {
  return typeof url === 'string' && PLACEHOLDER.test(url);
}

/** Fills an artwork template for a square image of `size` pixels. */
export function artworkUrl(url, size, format = 'jpg') {
  if (!url) return '';
  if (!isArtworkTemplate(url)) return url;
  return url.replaceAll('{w}', String(size)).replaceAll('{h}', String(size)).replaceAll('{f}', format);
}

/** Width-descriptor srcset for a template; empty for verbatim URLs. */
export function artworkSrcset(url, widths) {
  if (!isArtworkTemplate(url)) return '';
  return widths.map((w) => `${artworkUrl(url, w)} ${w}w`).join(', ');
}

/** Formats an integer with thousands separators ("1,284"). */
export function count(n) {
  if (typeof n !== 'number' || !Number.isFinite(n)) return '—';
  return numberFormat.format(n);
}

/** Formats a byte count with decimal (SI) units: 1500 -> "1.5 kB". */
export function bytes(n) {
  if (typeof n !== 'number' || !Number.isFinite(n) || n < 0) return '—';
  if (n < 1000) return `${n} B`;
  const units = ['kB', 'MB', 'GB', 'TB', 'PB'];
  let v = n;
  let i = -1;
  do {
    v /= 1000;
    i++;
  } while (v >= 1000 && i < units.length - 1);
  const digits = v >= 100 ? 0 : 1;
  return `${v.toFixed(digits)} ${units[i]}`;
}

/** Formats a track length in milliseconds as m:ss or h:mm:ss. */
export function trackLength(ms) {
  if (typeof ms !== 'number' || !(ms > 0)) return '';
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}

/** Formats an elapsed duration in milliseconds: "45s", "3m 12s", "2h 5m", "3d 4h". */
export function elapsed(ms) {
  if (typeof ms !== 'number' || !Number.isFinite(ms) || ms < 0) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

function toDate(value) {
  if (value instanceof Date) return value;
  if (typeof value !== 'string' || value === '') return null;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Relative time against `now` (ms since epoch): "just now", "5m ago", "3d ago". */
export function relTime(value, now = Date.now()) {
  const d = toDate(value);
  if (!d) return '—';
  const diff = Math.max(0, now - d.getTime());
  const s = Math.floor(diff / 1000);
  if (s < 10) return 'just now';
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const days = Math.floor(h / 24);
  if (days < 30) return `${days}d ago`;
  return shortDate(d);
}

const pad = (n, w = 2) => String(n).padStart(w, '0');

/** Local date and time, "2026-10-28 12:04:05". */
export function absTime(value) {
  const d = toDate(value);
  if (!d) return '';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** Local clock time for log rows, "12:04:05.123" (or without milliseconds). */
export function clockTime(value, withMillis = true) {
  const d = toDate(value);
  if (!d) return '';
  const base = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  return withMillis ? `${base}.${pad(d.getMilliseconds(), 3)}` : base;
}

/** Short local date, "Oct 28". */
export function shortDate(value) {
  const d = toDate(value);
  if (!d) return '';
  return `${monthNames[d.getMonth()]} ${d.getDate()}`;
}

/**
 * A date and time short enough for a row, in local time: "06:21" today,
 * "Oct 28 23:31" earlier this year, "Oct 28, 2025 23:31" before that.
 */
export function dateTime(value, now = new Date()) {
  const d = toDate(value);
  if (!d) return '';
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (d.getFullYear() !== now.getFullYear()) return `${shortDate(d)}, ${d.getFullYear()} ${time}`;
  if (d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) return time;
  return `${shortDate(d)} ${time}`;
}

/**
 * "Downloaded Oct 28 · took 3m 12s" for a finished job; '' when it has no
 * finish time. The duration needs started_at, which a job cancelled while
 * queued never gets.
 */
export function finishedLine(job) {
  if (!job || !job.finished_at) return '';
  const parts = [`Downloaded ${shortDate(job.finished_at)}`];
  const start = toDate(job.started_at);
  const end = toDate(job.finished_at);
  if (start && end && end >= start) parts.push(`took ${elapsed(end - start)}`);
  return parts.join(' · ');
}

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * The line under a page title, from the feed's status counts (null before
 * the first snapshot) and the current time. Showcase has none: the counts are
 * jobs, and its grid shows albums.
 */
export function pageSummary(page, counts, now = new Date()) {
  switch (page) {
    case 'overview':
      return counts ? `${shortDate(now)} · ${plural(counts.total, 'job', 'jobs')}` : shortDate(now);
    case 'queue':
      return counts ? `${count(counts.running)} running · ${count(counts.queued)} queued · ${count(counts.failed)} failed` : '';
    default:
      return '';
  }
}

/** Formats a UTC calendar date ("2026-10-28") as "Oct 28" without a time zone shift. */
export function dayLabel(isoDate) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(isoDate || '');
  if (!m) return isoDate || '';
  return `${monthNames[Number(m[2]) - 1]} ${Number(m[3])}`;
}

/** Display title of a job; before the input resolves, a name derived from the input URL. */
export function jobTitle(job) {
  if (!job) return '';
  if (job.title) return job.title;
  const input = job.input || '';
  try {
    const u = new URL(input);
    const parts = u.pathname.split('/').filter(Boolean);
    // music.apple.com/{storefront}/{type}/{slug}/{id}
    const slug = parts.length >= 4 ? parts[2] : parts[parts.length - 1];
    const name = decodeURIComponent(slug || '').replace(/-/g, ' ').trim();
    return name || input;
  } catch {
    return input;
  }
}

/** Whether Apple has not named the job yet (no title): a queued job before it resolves, a link that never resolved. */
export function isUnresolved(job) {
  return Boolean(job) && !job.title;
}

/** An input URL without its scheme and query, "music.apple.com/us/album/x/1": what an unresolved job shows instead of a guessed title. */
export function inputLabel(input) {
  const text = String(input || '').trim();
  try {
    const u = new URL(text);
    return `${u.host}${u.pathname}`.replace(/\/$/, '') || text;
  } catch {
    return text;
  }
}

/** The line that stands where an unresolved job's artist would be, by status. */
export function unresolvedHint(job) {
  switch (job && job.status) {
    case 'failed':
      return "Couldn't be resolved";
    case 'cancelled':
      return 'Cancelled before it resolved';
    default:
      return 'Not resolved yet';
  }
}

const ERROR_SUMMARY_MAX = 140;

const CATALOG_FAILED = /catalog request failed:\s*(\d{3})\b/i;

/**
 * Jobs stored before the server named Apple's answers keep "catalog request
 * failed: 404 Not Found" as their error. Plain words for those forms, or ''
 * for any other text. The caller keeps the stored text for "Full error".
 */
export function legacyCatalogError(text) {
  const m = CATALOG_FAILED.exec(String(text || ''));
  if (!m) return '';
  const code = Number(m[1]);
  if (code === 404) return 'Apple Music has nothing at this link (404)';
  if (code === 401 || code === 403) return `Apple Music refused access (${code})`;
  if (code === 429) return 'Apple Music is limiting requests, try again later (429)';
  if (code >= 500) return `Apple Music had a server problem (${code})`;
  return `Apple Music rejected the request (${code})`;
}

/**
 * One readable line from a job's error text: the first line, cut before an
 * embedded JSON body ("404 Not Found: {"errors":...}" -> "404 Not Found"), at
 * most 140 characters, with a stored legacy catalog error in plain words
 * (legacyCatalogError). Clean one-line text passes through unchanged; the
 * full text stays available to the caller (a title attribute).
 */
export function errorSummary(error) {
  let text = String(error || '').split(/\r?\n/)[0];
  const brace = text.search(/[{[]/);
  if (brace > 0) text = text.slice(0, brace);
  text = text.replace(/[\s:;,]+$/, '').trim();
  const plain = legacyCatalogError(text);
  if (plain) return plain;
  return text.length > ERROR_SUMMARY_MAX ? `${text.slice(0, ERROR_SUMMARY_MAX - 1).trimEnd()}…` : text;
}

/** Secondary line of a job: artist for songs and albums, curator for playlists. */
export function jobSubtitle(job) {
  if (!job) return '';
  return job.artist_name || job.curator_name || '';
}

/** The URL when it is absolute http(s), else ''. Keeps javascript: and similar out of hrefs. */
export function safeHttpUrl(url) {
  return typeof url === 'string' && /^https?:\/\//i.test(url) ? url : '';
}

/** First 8 characters of an id after its type prefix: "job_a1b2c3d4..." -> "a1b2c3d4". */
/** "database is locked" -> "Database is locked": a server message as a row's text, which carries no end punctuation. */
export function capitalized(s) {
  const t = String(s || '').trim();
  return t ? t[0].toUpperCase() + t.slice(1) : '';
}

/** A message with its closing period: "x must be at most 10" -> "x must be at most 10." (a leading config key keeps its case). */
export function punctuated(s) {
  const t = String(s || '').trim();
  if (!t) return '';
  return /[.!?]$/.test(t) ? t : `${t}.`;
}

/** "database is locked" -> "Database is locked.": a server message as a sentence in running text. */
export function sentence(s) {
  return punctuated(capitalized(s));
}

export function shortId(id) {
  if (typeof id !== 'string') return '';
  const i = id.indexOf('_');
  return (i >= 0 ? id.slice(i + 1) : id).slice(0, 8);
}

/** The id a header shows: 8 hex digits of a real "job_<hex>" id, any other text as typed (a link with a typo). */
export function displayId(id) {
  return typeof id === 'string' && /^job_[0-9a-f]{8,}$/i.test(id) ? shortId(id) : String(id || '');
}

const TYPE_LABELS = { song: 'Song', album: 'Album', playlist: 'Playlist', artist: 'Artist', station: 'Station' };

/** Human label for a job type. */
export function typeLabel(type) {
  return TYPE_LABELS[type] || type || '';
}

// --- Artwork palette ----------------------------------------------------

const HEX6 = /^[0-9a-f]{6}$/i;

/** Parses Apple's "rrggbb" (no leading #) into channels, or null. */
export function parseHex(hex) {
  if (typeof hex !== 'string' || !HEX6.test(hex)) return null;
  return {
    r: parseInt(hex.slice(0, 2), 16),
    g: parseInt(hex.slice(2, 4), 16),
    b: parseInt(hex.slice(4, 6), 16),
  };
}

function channel(c) {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** WCAG relative luminance of "rrggbb", or null when unparseable. */
export function luminance(hex) {
  const c = parseHex(hex);
  if (!c) return null;
  return 0.2126 * channel(c.r) + 0.7152 * channel(c.g) + 0.0722 * channel(c.b);
}

function ratio(l1, l2) {
  const [a, b] = l1 > l2 ? [l1, l2] : [l2, l1];
  return (a + 0.05) / (b + 0.05);
}

/** WCAG contrast ratio between two "rrggbb" colors, or null. */
export function contrastRatio(hexA, hexB) {
  const a = luminance(hexA);
  const b = luminance(hexB);
  if (a === null || b === null) return null;
  return ratio(a, b);
}

/**
 * Text color for Apple-provided `text` over `bg`: the palette color when it
 * reaches 4.5:1, otherwise whichever of the light or dark fallback tokens
 * contrasts more. Returns a CSS value.
 */
export function artTextColor(bg, text) {
  const lb = luminance(bg);
  if (lb === null) return 'var(--text-primary)';
  const r = contrastRatio(bg, text);
  if (r !== null && r >= 4.5) return `#${text}`;
  return ratio(lb, 1) >= ratio(lb, 0) ? 'var(--on-art-light)' : 'var(--on-art-dark)';
}

// In the dark theme a light album color would be a bright slab on a black
// page, so it is drawn at this HSL lightness at most (then darker until white
// text reaches 4.5:1), with the same hue and no more chroma than it had.
const DARK_ART_L = 0.24;

const hex2 = (v) => Math.round(Math.min(1, Math.max(0, v)) * 255).toString(16).padStart(2, '0');

/** "rrggbb" darkened for the dark theme, or null when it is dark enough already. */
export function darkArtColor(hex) {
  const c = parseHex(hex);
  if (!c) return null;
  const v = [c.r / 255, c.g / 255, c.b / 255];
  const hi = Math.max(...v);
  const lo = Math.min(...v);
  const l = (hi + lo) / 2;
  if (l <= DARK_ART_L) return null;
  const chroma = hi - lo;
  const s = chroma === 0 ? 0 : chroma / (1 - Math.abs(2 * l - 1));
  let out = hex;
  for (let to = DARK_ART_L; to > 0; to -= 0.02) {
    const k = chroma === 0 ? 0 : Math.min(chroma, (1 - Math.abs(2 * to - 1)) * s) / chroma;
    out = v.map((x) => hex2(to + (x - l) * k)).join('');
    if (contrastRatio(out, 'ffffff') >= 4.5) break;
  }
  return out;
}

// Over a darkened background: Apple's color when it is the lighter one and
// reaches 4.5:1, else the light fallback.
const darkArtText = (bg, text) =>
  parseHex(text) && luminance(text) > luminance(bg) && contrastRatio(bg, text) >= 4.5 ? `#${text}` : 'var(--on-art-light)';

/**
 * The palette to paint behind and over a job's artwork. With `motion` the
 * square motion asset's own palette is used; Apple pairs each asset with its
 * own colors, and mixing them puts dark text on a dark video. A light
 * background also gets `dark`, the colors for the dark theme.
 */
export function artPalette(job, motion = false) {
  if (!job) return null;
  const p = motion ? 'motion_artwork_' : 'artwork_';
  const bg = job[`${p}bg_color`];
  if (!parseHex(bg)) return null;
  const texts = [1, 2, 3].map((n) => job[`${p}text_color${n}`]);
  const out = {
    bg: `#${bg}`,
    text1: artTextColor(bg, texts[0]),
    text2: artTextColor(bg, texts[1]),
    text3: artTextColor(bg, texts[2]),
  };
  const dark = darkArtColor(bg);
  if (dark) {
    const [text1, text2, text3] = texts.map((t) => darkArtText(dark, t));
    out.dark = { bg: `#${dark}`, text1, text2, text3 };
  }
  return out;
}

/**
 * CSS custom properties for a palette, for a Preact style object: --art-bg
 * and --art-t1..t3 (each text color already reaches 4.5:1 on --art-bg). With
 * a `dark` variant each value is light-dark(), so the theme picks.
 */
export function paletteStyle(palette) {
  if (!palette) return undefined;
  const d = palette.dark;
  const v = (k) => (d ? `light-dark(${palette[k]}, ${d[k]})` : palette[k]);
  return { '--art-bg': v('bg'), '--art-t1': v('text1'), '--art-t2': v('text2'), '--art-t3': v('text3') };
}

/** Year of an Apple release date ("2022-10-21" -> "2022"), or ''. */
export function releaseYear(job) {
  const m = /^(\d{4})/.exec((job && job.release_date) || '');
  return m ? m[1] : '';
}

/**
 * Facts about a job for kicker and meta lines, each '' when unknown: type,
 * genre, year, tracks ("12 tracks"), started (clock time), finished
 * ("Oct 28 00:14") and took (duration).
 */
export function jobFacts(job) {
  if (!job) return { type: '', genre: '', year: '', tracks: '', started: '', finished: '', took: '' };
  const total = typeof job.total_items === 'number' && job.total_items > 0 ? job.total_items : 0;
  const start = toDate(job.started_at);
  const end = toDate(job.finished_at);
  return {
    type: job.type ? typeLabel(job.type) : '',
    genre: typeof job.genre === 'string' ? job.genre : '',
    year: releaseYear(job),
    tracks: total ? plural(total, 'track', 'tracks') : '',
    started: start ? clockTime(start, false) : '',
    finished: end ? `${shortDate(end)} ${pad(end.getHours())}:${pad(end.getMinutes())}` : '',
    took: start && end && end >= start ? elapsed(end - start) : '',
  };
}

/** Log attributes as "key=value" pairs in key order, without the keys in `omit`. */
export function attrPairs(attrs, omit = []) {
  if (!attrs || typeof attrs !== 'object') return [];
  return Object.keys(attrs)
    .filter((k) => !omit.includes(k))
    .sort()
    .map((k) => {
      const v = attrs[k];
      return `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`;
    });
}
