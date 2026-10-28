// Pure download options for the New download page: the server's download
// defaults (GET config `download`), the form the page edits, and the
// DownloadOverrides a submission sends, which holds only what differs from
// the defaults. Field names, enums and value lists follow
// internal/api/openapi.yaml (DownloadOverrides) and the comments in
// configs/config.example.yaml.

import { fieldByKey } from './settings.js';

/** localStorage key for options remembered on this device (never the token). */
export const OPTIONS_KEY = 'amdl.console.download.options';

// Labels and value lists come from the Settings fields of the same keys
// (lib/settings.js FIELDS), so both pages use one vocabulary.
const settingsOptions = (key) => fieldByKey(`download.${key}`).options;
const CODEC_HINTS = {
  alac: 'Apple Lossless, up to the sample rate and bit depth below',
  aac: 'The Enhanced HLS AAC stream, not the AAC-LC fallback',
  'aac-binaural': 'Spatial audio rendered for headphones',
  'aac-downmix': 'Spatial audio downmixed to stereo AAC',
  ec3: 'Dolby Atmos in Dolby Digital Plus',
  ac3: 'Dolby Digital surround',
};

/**
 * Codecs of quality_priority, in the order Settings lists them, with
 * Settings' label and a one-line description. AAC-LC is always the last
 * fallback and is not listed.
 */
export const CODECS = settingsOptions('quality_priority').map((o) => ({ value: o.value, label: o.label, hint: CODEC_HINTS[o.value] || '' }));
const CODEC_VALUES = CODECS.map((c) => c.value);

/** Selectable values, as in Settings; a server default outside a list is added by choices(). */
export const SAMPLE_RATES = settingsOptions('alac_max_sample_rate').map((o) => o.value);
export const BIT_DEPTHS = settingsOptions('alac_max_bit_depth').map((o) => o.value);
export const COVER_SIZES = ['5000x5000', '3000x3000', '1400x1400', '600x600'];
// jpeg is accepted as an alias of jpg; the select offers jpg and png.
export const COVER_FORMATS = ['jpg', 'png'];
export const LYRICS_FORMATS = ['lrc', 'ttml'];
export const LYRICS_TYPES = ['lyrics', 'syllable-lyrics'];
export const LYRICS_EXTRAS = ['translation', 'pronunciation'];
export const MAX_ATTEMPTS = 10;

/** The documented defaults (configs/config.example.yaml), used for a key the server leaves out. */
export const OPTION_DEFAULTS = Object.freeze({
  quality_priority: ['alac'],
  codec_alternative: true,
  alac_max_sample_rate: 192000,
  alac_max_bit_depth: 24,
  embed_lyrics: true,
  save_lyrics_file: false,
  lyrics_format: 'lrc',
  lyrics_type: 'lyrics',
  lyrics_extras: [],
  embed_cover: true,
  cover_size: '5000x5000',
  cover_format: 'jpg',
  save_album_cover: true,
  save_artist_cover: true,
  save_playlist_cover: true,
  force_overwrite: false,
  max_attempts: 3,
});

/** The option keys the page edits, in display order. */
export const OPTION_KEYS = Object.keys(OPTION_DEFAULTS);

const BOOL_KEYS = new Set(OPTION_KEYS.filter((k) => typeof OPTION_DEFAULTS[k] === 'boolean'));
const ENUMS = {
  lyrics_format: LYRICS_FORMATS,
  lyrics_type: LYRICS_TYPES,
  cover_format: ['jpg', 'jpeg', 'png'],
};
const COVER_SIZE = /^[1-9]\d{1,4}x[1-9]\d{1,4}$/;

const isPosInt = (v) => Number.isInteger(v) && v > 0;

/** Whether `v` is a valid value for option `key`; lists are checked item by item. */
function validValue(key, v) {
  if (BOOL_KEYS.has(key)) return typeof v === 'boolean';
  switch (key) {
    case 'quality_priority':
      return Array.isArray(v) && v.length > 0 && v.every((c) => CODEC_VALUES.includes(c)) && new Set(v).size === v.length;
    case 'lyrics_extras':
      return Array.isArray(v) && v.every((x) => LYRICS_EXTRAS.includes(x));
    case 'alac_max_sample_rate':
    case 'alac_max_bit_depth':
      return isPosInt(v);
    case 'max_attempts':
      return Number.isInteger(v) && v >= 1 && v <= MAX_ATTEMPTS;
    case 'cover_size':
      return typeof v === 'string' && COVER_SIZE.test(v);
    default:
      return ENUMS[key] ? ENUMS[key].includes(v) : false;
  }
}

/** lyrics_extras in canonical order without duplicates (the order carries no meaning). */
function canonicalExtras(list) {
  return LYRICS_EXTRAS.filter((x) => list.includes(x));
}

/**
 * The defaults the form starts from: the server's download section where it
 * holds a valid value, the documented default otherwise. Max attempts <= 0
 * read as 1 and above 10 as 10, as the backend clamps them.
 */
export function optionDefaults(download) {
  const src = download && typeof download === 'object' ? download : {};
  const out = {};
  for (const key of OPTION_KEYS) {
    let v = src[key];
    if (key === 'max_attempts' && Number.isInteger(v)) v = Math.min(MAX_ATTEMPTS, Math.max(1, v));
    out[key] = validValue(key, v) ? v : OPTION_DEFAULTS[key];
    if (Array.isArray(out[key])) out[key] = key === 'lyrics_extras' ? canonicalExtras(out[key]) : out[key].slice();
  }
  return out;
}

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

function sameValue(key, a, b) {
  if (key === 'lyrics_extras') return sameList(canonicalExtras(a), canonicalExtras(b));
  if (Array.isArray(a) && Array.isArray(b)) return sameList(a, b);
  return a === b;
}

/**
 * The DownloadOverrides for a submission: each option whose value differs
 * from `defaults`, plus
 * - hooks: only when `hooks` (the checked entry names, or null when
 *   untouched) differs from `enabledHooks` (every enabled entry, which is
 *   what omitting the field runs); nothing checked sends [];
 * - media_user_token: only when non-blank (it is never stored).
 * Returns {} when nothing differs.
 */
export function buildOverrides(defaults, values, { hooks = null, enabledHooks = [], token = '' } = {}) {
  const out = {};
  for (const key of OPTION_KEYS) {
    const v = values ? values[key] : undefined;
    if (v === undefined || !validValue(key, v)) continue;
    if (sameValue(key, v, defaults[key])) continue;
    out[key] = key === 'lyrics_extras' ? canonicalExtras(v) : Array.isArray(v) ? v.slice() : v;
  }
  if (Array.isArray(hooks)) {
    const picked = [...new Set(hooks.filter((h) => typeof h === 'string' && h))];
    const all = new Set(enabledHooks);
    const same = picked.length === all.size && picked.every((h) => all.has(h));
    if (!same) out.hooks = picked;
  }
  const t = typeof token === 'string' ? token.trim() : '';
  if (t) out.media_user_token = t;
  return out;
}

/**
 * The stored or remembered part of an overrides object: known option keys
 * with valid values and a hooks list of names. Anything else, the token
 * included, is dropped. Returns {} for anything that is not an object.
 */
export function sanitizeOverrides(obj) {
  const out = {};
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return out;
  for (const key of OPTION_KEYS) {
    if (key in obj && validValue(key, obj[key])) out[key] = Array.isArray(obj[key]) ? obj[key].slice() : obj[key];
  }
  if (Array.isArray(obj.hooks) && obj.hooks.every((h) => typeof h === 'string' && h)) out.hooks = [...new Set(obj.hooks)];
  return out;
}

/** Parses options remembered as JSON (lib/prefs.js readPref normalizer); null when absent or unusable. */
export function parseStoredOptions(raw) {
  if (typeof raw !== 'string' || !raw) return null;
  try {
    const clean = sanitizeOverrides(JSON.parse(raw));
    return Object.keys(clean).length ? clean : null;
  } catch {
    return null;
  }
}

/** The form values for `defaults` with `overrides` (sanitized) applied on top. */
export function applyOverrides(defaults, overrides) {
  const clean = sanitizeOverrides(overrides);
  const out = {};
  for (const key of OPTION_KEYS) out[key] = key in clean ? clean[key] : defaults[key];
  return out;
}

/** A list with the item at `index` moved by `delta` places; out-of-range moves return the list unchanged. */
export function moveItem(list, index, delta) {
  const to = index + delta;
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return list;
  const next = list.slice();
  const [item] = next.splice(index, 1);
  next.splice(to, 0, item);
  return next;
}

/** `list` plus `current` when the server's value is not one of the usual choices. */
export function choices(list, current) {
  if (current === undefined || current === null || list.includes(current)) return list;
  return [...list, current];
}

export const codecName = (value) => (CODECS.find((c) => c.value === value) || { label: value }).label;

/** 192000 -> "192 kHz", 44100 -> "44.1 kHz". */
export function rateLabel(hz) {
  const k = hz / 1000;
  return `${Number.isInteger(k) ? k : Math.round(k * 10) / 10} kHz`;
}

const onOff = (v) => (v ? 'on' : 'off');
const FORMAT_LABELS = { lrc: 'LRC', ttml: 'TTML', jpg: 'JPEG', jpeg: 'JPEG', png: 'PNG' };

/**
 * One short phrase per override, in display order, for the collapsed
 * options line ("Quality: AAC, ALAC", "Lyrics file on", "Hooks: none").
 * The token is never described.
 */
export function describeOverrides(overrides) {
  const o = overrides || {};
  const out = [];
  const add = (key, text) => {
    if (key in o) out.push(text);
  };
  add('quality_priority', `Quality: ${(o.quality_priority || []).map(codecName).join(', ')}`);
  add('codec_alternative', `Fallback to next codec ${onOff(o.codec_alternative)}`);
  add('alac_max_sample_rate', `ALAC up to ${rateLabel(o.alac_max_sample_rate)}`);
  add('alac_max_bit_depth', `ALAC up to ${o.alac_max_bit_depth}-bit`);
  add('embed_lyrics', `Embedded lyrics ${onOff(o.embed_lyrics)}`);
  add('save_lyrics_file', `Lyrics file ${onOff(o.save_lyrics_file)}`);
  add('lyrics_format', `Lyrics as ${FORMAT_LABELS[o.lyrics_format] || o.lyrics_format}`);
  add('lyrics_type', o.lyrics_type === 'syllable-lyrics' ? 'Word-timed lyrics' : 'Line-timed lyrics');
  add('lyrics_extras', o.lyrics_extras && o.lyrics_extras.length ? `Lyrics extras: ${o.lyrics_extras.join(', ')}` : 'No lyrics extras');
  add('embed_cover', `Embedded cover ${onOff(o.embed_cover)}`);
  add('cover_size', `Cover ${String(o.cover_size).replace('x', ' × ')}`);
  add('cover_format', `Cover as ${FORMAT_LABELS[o.cover_format] || o.cover_format}`);
  add('save_album_cover', `Album cover file ${onOff(o.save_album_cover)}`);
  add('save_artist_cover', `Artist cover file ${onOff(o.save_artist_cover)}`);
  add('save_playlist_cover', `Playlist cover file ${onOff(o.save_playlist_cover)}`);
  add('force_overwrite', o.force_overwrite ? 'Overwrite existing files' : 'Keep existing files');
  add('max_attempts', `${o.max_attempts} ${o.max_attempts === 1 ? 'attempt' : 'attempts'}`);
  add('hooks', o.hooks && o.hooks.length ? `Hooks: ${o.hooks.join(', ')}` : 'Hooks: none');
  return out;
}
