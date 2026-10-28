// Pure formatting helpers. No DOM access, so node:test covers them.

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

/** Formats a UTC calendar date ("2026-10-28") as "Oct 28" without a time zone shift. */
export function dayLabel(isoDate) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(isoDate || '');
  if (!m) return isoDate || '';
  return `${monthNames[Number(m[2]) - 1]} ${Number(m[3])}`;
}

const codecNames = {
  alac: 'ALAC',
  aac: 'AAC',
  'aac-lc': 'AAC-LC',
  'aac-binaural': 'AAC Binaural',
  'aac-downmix': 'AAC Downmix',
  ec3: 'Dolby Atmos',
  ac3: 'Dolby Audio',
};

/** Sample rate in Hz as kHz text: 96000 -> "96", 44100 -> "44.1". */
export function kHz(sampleRate) {
  if (!(sampleRate > 0)) return '';
  const k = sampleRate / 1000;
  return Number.isInteger(k) ? String(k) : String(Math.round(k * 10) / 10);
}

/** Codec chip text from codec, bit_depth and sample_rate: "ALAC 24/96", "AAC". */
export function codecLabel(item) {
  if (!item || !item.codec) return '';
  const name = codecNames[item.codec] || item.codec.toUpperCase();
  if (item.bit_depth > 0 && item.sample_rate > 0) return `${name} ${item.bit_depth}/${kHz(item.sample_rate)}`;
  return name;
}

/** Whether an item's format is high resolution (more than 16-bit/48 kHz). */
export function isHiRes(item) {
  return Boolean(item && item.bit_depth >= 24 && item.sample_rate > 48000);
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
export function shortId(id) {
  if (typeof id !== 'string') return '';
  const i = id.indexOf('_');
  return (i >= 0 ? id.slice(i + 1) : id).slice(0, 8);
}

const TYPE_LABELS = { song: 'Song', album: 'Album', playlist: 'Playlist', artist: 'Artist', station: 'Station' };

/** Human label for a job type. */
export function typeLabel(type) {
  return TYPE_LABELS[type] || type || '';
}

/** Masks an account identifier such as an email: "jane@example.com" -> "j•••@example.com". */
export function maskAccount(value) {
  if (typeof value !== 'string' || value === '') return '';
  const at = value.indexOf('@');
  if (at > 0) return `${value[0]}•••${value.slice(at)}`;
  return value.length <= 2 ? '•••' : `${value[0]}•••${value[value.length - 1]}`;
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

/**
 * The palette to paint behind and over a job's artwork. With `motion` the
 * square motion asset's own palette is used; Apple pairs each asset with its
 * own colors, and mixing them puts dark text on a dark video.
 */
export function artPalette(job, motion = false) {
  if (!job) return null;
  const p = motion ? 'motion_artwork_' : 'artwork_';
  const bg = job[`${p}bg_color`];
  if (!parseHex(bg)) return null;
  return {
    bg: `#${bg}`,
    text1: artTextColor(bg, job[`${p}text_color1`]),
    text2: artTextColor(bg, job[`${p}text_color2`]),
  };
}

/** CSS custom properties for a palette, for a Preact style object. */
export function paletteStyle(palette) {
  if (!palette) return undefined;
  return { '--art-bg': palette.bg, '--art-text1': palette.text1, '--art-text2': palette.text2 };
}
