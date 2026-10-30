// Helpers for settings drafts, patches, diffs, validation, error mapping and
// summaries. Pure: no DOM, so node --test covers it.

import { FIELDS, FIELD_KEYS, BY_KEY } from './settings.js';
import { templateUnknownVars } from './settings-template.js';

/** A ConfigResponse `config` as a flat {'section.key': value} map of its leaves. */
export function flattenConfig(config) {
  const out = {};
  if (!config || typeof config !== 'object') return out;
  for (const [section, body] of Object.entries(config)) {
    if (!body || typeof body !== 'object' || Array.isArray(body)) continue;
    for (const [k, v] of Object.entries(body)) out[`${section}.${k}`] = v;
  }
  return out;
}

/** Equality for config values: scalars by ===, lists by items in order. */
export function sameValue(a, b) {
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((v, i) => v === b[i]);
  }
  return a === b;
}

/** Keys whose draft value differs from the base, in field order. */
export function changedKeys(base, draft) {
  return FIELD_KEYS.filter((k) => Object.hasOwn(draft, k) && !sameValue(draft[k], base[k]));
}

/**
 * The draft left after a save of `sent` (the draft the PUT was built from):
 * a key edited again while the request ran keeps its newer value, every
 * other sent key leaves. Values compare by identity, since every edit
 * stores a new value.
 */
export function draftAfterSave(draft, sent) {
  return Object.fromEntries(Object.entries(draft).filter(([k, v]) => !Object.hasOwn(sent, k) || sent[k] !== v));
}

/**
 * The PUT /api/v1/config body for the changed keys: {section: {key: value}}
 * with only changed leaves and lists sent whole. The server decodes the body
 * onto its current config, so an omitted leaf keeps its value and a list
 * replaces the old one (checked against a live server).
 */
export function buildPatch(base, draft) {
  const body = {};
  for (const key of changedKeys(base, draft)) {
    const [section, name] = key.split('.');
    const v = draft[key];
    (body[section] ||= {})[name] = Array.isArray(v) ? [...v] : v;
  }
  return body;
}

/** A details.keys entry, a dotted key such as "download.max_running_jobs", or null. */
export function parseLockedKey(entry) {
  const m = /^\s*([a-z_]+\.[a-z0-9_]+)\s*$/.exec(String(entry || ''));
  return m ? m[1] : null;
}

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Every known key named in a message, as a whole dotted word, in `keys` order. */
export function keysInMessage(message, keys = FIELD_KEYS) {
  const text = String(message || '');
  return keys.filter((k) => new RegExp(`(^|[^A-Za-z0-9_])${escapeRe(k)}(?![A-Za-z0-9_])`).test(text));
}

/**
 * Maps a failed PUT (an ApiError) onto the page: {message, fields: {key:
 * text}}. config_key_not_mutable names its keys in details.keys; invalid_config and invalid_request name them in the message,
 * where every known key mentioned gets the message (only the keys of the
 * PUT, `sentKeys`, when the message names any of them).
 */
export function fieldErrors(err, sentKeys = []) {
  const raw = (err && err.message) || 'The settings were not saved.';
  const message = keysToLabels(raw);
  const fields = {};
  const code = err && err.code;
  const listed = err && err.details && Array.isArray(err.details.keys) ? err.details.keys.map(parseLockedKey).filter(Boolean) : [];
  if (code === 'config_key_not_mutable' && listed.length) {
    for (const key of listed) fields[key] = 'Only the config file can change this, followed by a restart.';
  } else if (err && (err.status === 400 || err.status === 422)) {
    // A message may name a key the user did not touch (a related setting);
    // when it also names a key in the PUT, mark only those.
    const named = keysInMessage(raw);
    const sent = named.filter((k) => sentKeys.includes(k));
    for (const key of sent.length ? sent : named) fields[key] = message;
  }
  return { message, fields };
}

/** A server message with every known dotted key replaced by its field's label. */
export function keysToLabels(message) {
  let text = String(message || '');
  for (const k of keysInMessage(text)) text = text.replace(new RegExp(`(^|[^A-Za-z0-9_])${escapeRe(k)}(?![A-Za-z0-9_])`, 'g'), `$1${labelOf(k)}`);
  return text;
}

/** The inline error for a whole number outside the field's min/max, or ''. */
export function rangeError(field, v) {
  const { min, max } = field;
  if (min !== undefined && max !== undefined) return v < min || v > max ? `Use a number from ${min} to ${max}` : '';
  if (min !== undefined && v < min) return `Use a number of at least ${min}`;
  if (max !== undefined && v > max) return `Use a number of at most ${max}`;
  return '';
}

/**
 * The warning a value earns, or '' : a value the server accepts but that
 * looks wrong (cover size format, unknown template variables).
 */
export function fieldWarning(field, v) {
  if (!field) return '';
  if (field.pattern && typeof v === 'string' && !new RegExp(field.pattern).test(v)) return field.patternHint;
  if (field.type === 'template') {
    const unknown = templateUnknownVars(v);
    if (unknown.length) return `Not a known variable, kept literally: ${unknown.join(', ')}.`;
  }
  return '';
}

/**
 * Checks the client can make before a PUT. Blocking errors are values that
 * should not be sent (a number that is not a whole number or is outside the
 * field's min/max, an empty list); warnings are values the server accepts
 * but that look wrong. Cross-field rules (the slowest simulated speed under
 * the fastest) stay with the server, which names the keys.
 */
export function validateDraft(draft) {
  const errors = {};
  const warnings = {};
  for (const [key, v] of Object.entries(draft || {})) {
    const f = BY_KEY.get(key);
    if (!f) continue;
    if (f.type === 'number') {
      const range = Number.isInteger(v) ? rangeError(f, v) : 'Enter a whole number.';
      if (range) errors[key] = range;
    }
    if (f.type === 'list' && f.minItems && (!Array.isArray(v) || v.length < f.minItems)) errors[key] = 'Keep at least one entry.';
    const w = fieldWarning(f, v);
    if (w) warnings[key] = w;
  }
  return { errors, warnings };
}

/**
 * Warnings for the values the server holds (a flat config map), so a saved
 * template with an unknown variable keeps its warning after the save and
 * after a reload. `draft` keys are skipped: the edit in progress has its own.
 */
export function savedWarnings(base, draft = {}) {
  const out = {};
  for (const f of FIELDS) {
    if (Object.hasOwn(draft, f.key) || !Object.hasOwn(base || {}, f.key)) continue;
    const w = fieldWarning(f, base[f.key]);
    if (w) out[f.key] = w;
  }
  return out;
}

/** Keys whose value differs between two flat config maps, in field order, then any others. */
export function diffConfig(before, after) {
  const all = new Set([...Object.keys(before || {}), ...Object.keys(after || {})]);
  const differs = (k) => !sameValue((before || {})[k], (after || {})[k]);
  const known = FIELD_KEYS.filter((k) => all.has(k) && differs(k));
  const rest = [...all].filter((k) => !BY_KEY.has(k) && differs(k)).sort();
  return [...known, ...rest];
}

/** A field's label, or the key itself when it is not a field. */
export const labelOf = (key) => (BY_KEY.get(key) || { label: key }).label;
const joinWords = (items) => (items.length < 3 ? items.join(' and ') : `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`);

/** Field labels for a list of keys, at most `max` named: "Albums, Songs and 2 more". */
export function labelList(keys, max = 3) {
  const names = keys.map(labelOf);
  if (names.length <= max) return joinWords(names);
  return joinWords([...names.slice(0, max), `${names.length - max} more`]);
}

/**
 * The outcome of Reload from file from the keys that changed: `short` for a
 * toast, `long` for the page (which settings).
 */
export function reloadSummary(changed) {
  const n = changed.length === 1 ? '1 setting' : `${changed.length} settings`;
  if (changed.length === 0) return { short: 'Reloaded from the file: no changes', long: 'Reloaded from the config file: no changes.' };
  return { short: `Reloaded from the file: ${n} changed`, long: `Reloaded from the config file: ${n} changed (${labelList(changed, 4)}).` };
}

/**
 * A reload_error as {reason, file}: "decode /abs/config.yaml: yaml: line 6:
 * ..." keeps the parser's reason and moves the server's file path aside
 * (the admin needs the line, the path adds nothing). A message without a
 * path is all reason.
 */
export function splitReloadError(message) {
  const text = String(message || '').trim();
  const m = /^[a-z ]*?\s*((?:\/|[A-Za-z]:[\\/]).*?\.ya?ml): (.+)$/is.exec(text);
  return m ? { reason: m[2], file: m[1] } : { reason: text, file: '' };
}

/** What the leave and discard dialogs say was changed: "You changed Tries per file and Cover size." */
export function changedSentence(keys) {
  return `You changed ${labelList(keys, 3)}.`;
}

/** The first key with an error, in field order. */
export function firstErrorKey(errors) {
  return FIELD_KEYS.find((k) => Object.hasOwn(errors || {}, k)) || Object.keys(errors || {})[0] || '';
}

/** The polite announcement for a changed unsaved count; '' when there is nothing to say. */
export function unsavedAnnouncement(n) {
  return n > 0 ? changesLabel(n) : 'No unsaved changes';
}

/** Parses a number input: an integer, or the raw text when it is not one. */
export function parseNumberInput(text) {
  const t = String(text).trim();
  return /^-?\d+$/.test(t) ? Number(t) : t;
}

/** "1 unsaved change" / "3 unsaved changes". */
export const changesLabel = (n) => `${n} unsaved ${n === 1 ? 'change' : 'changes'}`;
