// Pure download overrides for the New download page: the server's download
// defaults (GET config `download`), the quality presets of the submit row
// (Quality: Settings, Lossless, Dolby Atmos then lossless, AAC, Custom) and the
// Overwrite switch, and the DownloadOverrides a submission sends, which holds
// only what differs from the defaults. Everything else is a Settings value
// for every download. Field names, enums and value lists follow
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

/** `list` plus `current` when the server's value is not one of the usual choices. */
export function choices(list, current) {
  if (current === undefined || current === null || list.includes(current)) return list;
  return [...list, current];
}

/** 192000 -> "192 kHz", 44100 -> "44.1 kHz". */
export function rateLabel(hz) {
  const k = hz / 1000;
  return `${Number.isInteger(k) ? k : Math.round(k * 10) / 10} kHz`;
}

/** The keys a quality preset sets; force_overwrite is the separate switch. */
export const QUALITY_KEYS = ['quality_priority', 'codec_alternative', 'alac_max_sample_rate', 'alac_max_bit_depth'];

/**
 * The quality presets of the submit row, in menu order. "follow" sends no
 * quality keys; the others set the codec order and fallback and leave the
 * ALAC limits to Settings; "custom" (not listed) is the edited values.
 */
export const PRESETS = [
  { id: 'follow', label: 'Quality: Settings', values: {} },
  { id: 'lossless', label: 'Lossless (ALAC)', values: { quality_priority: ['alac'], codec_alternative: true } },
  { id: 'atmos', label: 'Dolby Atmos, then lossless', values: { quality_priority: ['ec3', 'alac'], codec_alternative: true } },
  { id: 'aac', label: 'AAC', values: { quality_priority: ['aac'], codec_alternative: true } },
];

const pickQuality = (obj) => {
  const out = {};
  for (const key of QUALITY_KEYS) if (obj && key in obj) out[key] = obj[key];
  return out;
};

/**
 * The option values a submission resolves to: `defaults` (the server's,
 * optionDefaults), the preset's quality keys on top ("custom": the quality
 * keys of `custom`; an unknown preset follows Settings), and force_overwrite
 * from `overwrite` unless it is null (follow Settings).
 */
export function presetValues(defaults, preset, custom = null, overwrite = null) {
  const found = PRESETS.find((p) => p.id === preset);
  const quality = preset === 'custom' ? pickQuality(custom) : found ? found.values : {};
  const out = applyOverrides(defaults, quality);
  if (typeof overwrite === 'boolean') out.force_overwrite = overwrite;
  return out;
}

/** The DownloadOverrides for a preset: only the keys whose values differ from `defaults`. */
export function presetOverrides(defaults, preset, custom = null, overwrite = null) {
  return buildOverrides(defaults, presetValues(defaults, preset, custom, overwrite));
}

const SHORT = { alac: 'ALAC', ec3: 'Dolby Atmos', ac3: 'Dolby Digital', aac: 'AAC', 'aac-binaural': 'AAC binaural', 'aac-downmix': 'AAC downmix' };
const LONG = { ...SHORT, alac: 'Lossless (ALAC)' };

/** A codec's short name ("Dolby Atmos"); `long` spells ALAC as "Lossless (ALAC)". */
export const codecName = (value, long = false) => (long ? LONG : SHORT)[value] || value;

const limits = (v) => `up to ${v.alac_max_bit_depth}-bit / ${rateLabel(v.alac_max_sample_rate)}`;

/**
 * What a set of option values asks for, in words: the codecs in order, ALAC
 * with its limits ("Dolby Atmos, then Lossless (ALAC) up to 24-bit / 192
 * kHz"), the card's Quality line. Without the fallback only the first codec
 * counts.
 */
export function qualityText(values) {
  const list = values && Array.isArray(values.quality_priority) && values.quality_priority.length ? values.quality_priority : ['alac'];
  const used = values && values.codec_alternative === false ? list.slice(0, 1) : list;
  const parts = used.map((c) => (c === 'alac' ? `${codecName(c, true)} ${limits(values)}` : codecName(c, true)));
  const text = parts.join(', then ');
  return used.length < list.length ? `${text} only` : text;
}

/** The menu item of the custom values: "Custom: Dolby Atmos, ALAC". */
export function customLabel(custom) {
  const list = custom && Array.isArray(custom.quality_priority) ? custom.quality_priority : [];
  return list.length ? `Custom: ${list.map((c) => codecName(c)).join(', ')}` : 'Custom';
}

/**
 * A warning for the Will download line once the qualities of a link are
 * known (lib/lookup.js qualityOverview slots): the first codec of `values`
 * offered by no track ("Dolby Atmos not offered") or by some only ("Dolby
 * Atmos on 3 of 12 tracks"); '' when every track offers it.
 */
export function probeWarning(values, slots) {
  const first = values && Array.isArray(values.quality_priority) ? values.quality_priority[0] : '';
  const slot = (Array.isArray(slots) ? slots : []).find((s) => s.id === first);
  if (!slot || slot.tracks === slot.total) return '';
  const name = codecName(first);
  return slot.tracks === 0 ? `${name} not offered` : `${name} on ${slot.tracks} of ${slot.total} tracks`;
}

/**
 * What a link will download once its qualities are known (`slots`,
 * lib/lookup.js qualityOverview): the first codec of `values` some track
 * offers (only the first without the fallback), with the best format the
 * probe saw, else the AAC-LC every track has; plus probeWarning. Returns
 * {text, warning}.
 */
export function probedQuality(values, slots) {
  const list = values && Array.isArray(values.quality_priority) ? values.quality_priority : [];
  const tried = values && values.codec_alternative === false ? list.slice(0, 1) : list;
  const offered = (Array.isArray(slots) ? slots : []).filter((s) => s.tracks > 0);
  const hit = tried.map((c) => offered.find((s) => s.id === c)).find(Boolean);
  const best = hit && hit.best ? hit.best.split(' · ')[0] : '';
  const text = hit ? `${codecName(hit.id, true)}${best ? `, ${best}` : ''}` : 'AAC (the fallback every track has)';
  return { text, warning: probeWarning(values, slots) };
}

/**
 * The dock's quality line. When the qualities of every link to be sent were
 * read and agree, what each row's Will download line says ("Will download:
 * Lossless (ALAC), 16-bit / 44.1 kHz"); else what the choice asks for, said
 * as an ask ("Asks for Lossless (ALAC) up to 24-bit / 192 kHz"): a ceiling,
 * not what a link has. `probed` holds each link's probedQuality, null while
 * unread.
 */
export function dockQuality(values, probed) {
  if (!values) return '';
  const texts = new Set(probed.map((p) => (p ? p.text : null)));
  if (probed.length && !texts.has(null) && texts.size === 1) return `Will download: ${[...texts][0]}`;
  return `Asks for ${qualityText(values)}`;
}
