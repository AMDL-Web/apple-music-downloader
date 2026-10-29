// Pure download options for the New download page: the server's download
// defaults (GET config `download`), the form the page edits (quality and
// overwrite only; everything else is a Settings value for every download),
// and the DownloadOverrides a submission sends, which holds only what differs
// from the defaults. Field names, enums and value lists follow
// internal/api/openapi.yaml (DownloadOverrides) and the comments in
// configs/config.example.yaml.

import { fieldByKey } from './settings.js';

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

/** The documented defaults (configs/config.example.yaml), used for a key the server leaves out. */
export const OPTION_DEFAULTS = Object.freeze({
  quality_priority: ['alac'],
  codec_alternative: true,
  alac_max_sample_rate: 192000,
  alac_max_bit_depth: 24,
  force_overwrite: false,
});

/** The option keys the page edits, in display order. */
export const OPTION_KEYS = Object.keys(OPTION_DEFAULTS);

const BOOL_KEYS = new Set(OPTION_KEYS.filter((k) => typeof OPTION_DEFAULTS[k] === 'boolean'));

const isPosInt = (v) => Number.isInteger(v) && v > 0;

/** Whether `v` is a valid value for option `key`; lists are checked item by item. */
function validValue(key, v) {
  if (BOOL_KEYS.has(key)) return typeof v === 'boolean';
  switch (key) {
    case 'quality_priority':
      return Array.isArray(v) && v.length > 0 && v.every((c) => CODEC_VALUES.includes(c)) && new Set(v).size === v.length;
    case 'alac_max_sample_rate':
    case 'alac_max_bit_depth':
      return isPosInt(v);
    default:
      return false;
  }
}

/**
 * The defaults the form starts from: the server's download section where it
 * holds a valid value, the documented default otherwise.
 */
export function optionDefaults(download) {
  const src = download && typeof download === 'object' ? download : {};
  const out = {};
  for (const key of OPTION_KEYS) {
    const v = src[key];
    out[key] = validValue(key, v) ? v : OPTION_DEFAULTS[key];
    if (Array.isArray(out[key])) out[key] = out[key].slice();
  }
  return out;
}

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

function sameValue(a, b) {
  if (Array.isArray(a) && Array.isArray(b)) return sameList(a, b);
  return a === b;
}

/**
 * The DownloadOverrides for a submission: each option whose value differs
 * from `defaults`. Returns {} when nothing differs.
 */
export function buildOverrides(defaults, values) {
  const out = {};
  for (const key of OPTION_KEYS) {
    const v = values ? values[key] : undefined;
    if (v === undefined || !validValue(key, v)) continue;
    if (sameValue(v, defaults[key])) continue;
    out[key] = Array.isArray(v) ? v.slice() : v;
  }
  return out;
}

/**
 * The known option keys of an overrides object with valid values; anything
 * else is dropped. Returns {} for anything that is not an object.
 */
export function sanitizeOverrides(obj) {
  const out = {};
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return out;
  for (const key of OPTION_KEYS) {
    if (key in obj && validValue(key, obj[key])) out[key] = Array.isArray(obj[key]) ? obj[key].slice() : obj[key];
  }
  return out;
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

/**
 * One short phrase per override, in display order, for the collapsed
 * options line ("Quality: AAC, ALAC", "Overwrite existing files").
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
  add('force_overwrite', o.force_overwrite ? 'Overwrite existing files' : 'Keep existing files');
  return out;
}
