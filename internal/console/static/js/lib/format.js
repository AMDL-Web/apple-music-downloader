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
 * the first snapshot) and the current time.
 */
export function pageSummary(page, counts, now = new Date()) {
  switch (page) {
    case 'overview':
      return counts ? `${shortDate(now)} · ${plural(counts.total, 'job', 'jobs')}` : shortDate(now);
    case 'queue':
      return counts ? `${count(counts.running)} running · ${count(counts.queued)} queued · ${count(counts.failed)} failed` : '';
    case 'showcase':
      return counts ? `${count(counts.completed)} completed` : '';
    case 'logs':
      return 'Structured backend log, newest last';
    case 'system':
      return 'Build, runtime, health and configuration';
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

/**
 * The codec mix from /stats `codecs` (most tracks first) as shares of all
 * completed tracks. Past `max` formats, the tail is folded into "Other".
 * @returns {{key: string, label: string, tracks: number, share: number}[]}
 */
export function formatShares(codecs, max = 4) {
  const rows = (Array.isArray(codecs) ? codecs : []).filter((c) => c && c.tracks > 0);
  const total = rows.reduce((n, c) => n + c.tracks, 0);
  if (total === 0) return [];
  const head = rows.length > max ? rows.slice(0, max - 1) : rows;
  const out = head.map((c) => ({
    key: [c.codec, c.bit_depth, c.sample_rate].filter((v) => v).join('-') || 'unknown',
    label: codecLabel(c) || 'Unknown',
    tracks: c.tracks,
    share: c.tracks / total,
  }));
  if (rows.length > max) {
    const tracks = rows.slice(max - 1).reduce((n, c) => n + c.tracks, 0);
    out.push({ key: 'other', label: 'Other', tracks, share: tracks / total });
  }
  return out;
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
    text3: artTextColor(bg, job[`${p}text_color3`]),
  };
}

/**
 * CSS custom properties for a palette, for a Preact style object: --art-bg
 * and --art-t1..t3 (each text color already reaches 4.5:1 on --art-bg).
 */
export function paletteStyle(palette) {
  if (!palette) return undefined;
  return { '--art-bg': palette.bg, '--art-t1': palette.text1, '--art-t2': palette.text2, '--art-t3': palette.text3 };
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

/**
 * Health-adjacent facts from GET /system: workers, queue, downloads disk and
 * uptime as [key, label, value] rows; rows without data are left out.
 */
export function systemFacts(sys) {
  if (!sys || typeof sys !== 'object') return [];
  const out = [];
  const jobs = sys.jobs || {};
  if (typeof jobs.workers === 'number') out.push(['workers', 'Workers', `${count(jobs.active || 0)} of ${count(jobs.workers)} busy`]);
  if (typeof jobs.queue_capacity === 'number') out.push(['queue', 'Queue', `${count(jobs.queue_depth || 0)} of ${count(jobs.queue_capacity)}`]);
  const disk = (sys.storage || []).find((d) => d && d.name === 'downloads_dir');
  if (disk && typeof disk.free_bytes === 'number') out.push(['storage', 'Storage', `${bytes(disk.free_bytes)} free of ${bytes(disk.total_bytes)}`]);
  if (typeof sys.uptime_seconds === 'number') out.push(['uptime', 'Uptime', elapsed(sys.uptime_seconds * 1000)]);
  return out;
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
