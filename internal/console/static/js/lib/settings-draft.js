// Helpers for settings drafts, patches, diffs, validation, error mapping and
// summaries. Pure: no DOM, so node --test covers it.

import { SETTINGS_SECTIONS, FIELDS, FIELD_KEYS, BY_KEY } from './settings.js';
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
 * replaces the old one (checked against a live server). Pinned keys are left
 * out whatever the draft holds: a PUT changing one is rejected.
 */
export function buildPatch(base, draft, pinned = {}) {
  const body = {};
  for (const key of changedKeys(base, draft)) {
    if (Object.hasOwn(pinned, key)) continue;
    const [section, name] = key.split('.');
    const v = draft[key];
    (body[section] ||= {})[name] = Array.isArray(v) ? [...v] : v;
  }
  return body;
}

/** ConfigResponse.pinned as {key: env}. */
export function pinnedMap(pinned) {
  const out = {};
  for (const p of Array.isArray(pinned) ? pinned : []) {
    if (p && typeof p.key === 'string' && typeof p.env === 'string') out[p.key] = p.env;
  }
  return out;
}

/** A details.keys entry: "download.temp_dir (AMDL_DOWNLOAD_TEMP_DIR)" or a bare key. */
export function parseLockedKey(entry) {
  const m = /^\s*([a-z_]+\.[a-z0-9_]+)(?:\s*\(([A-Z0-9_]+)\))?\s*$/.exec(String(entry || ''));
  return m ? { key: m[1], env: m[2] || '' } : null;
}

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Every known key named in a message, as a whole dotted word, in `keys` order. */
export function keysInMessage(message, keys = FIELD_KEYS) {
  const text = String(message || '');
  return keys.filter((k) => new RegExp(`(^|[^A-Za-z0-9_])${escapeRe(k)}(?![A-Za-z0-9_])`).test(text));
}

/**
 * Maps a failed PUT (an ApiError) onto the page: {message, fields: {key:
 * text}}. config_key_pinned and config_key_not_mutable name their keys in
 * details.keys; invalid_config and invalid_request name them in the message,
 * where every known key mentioned gets the message (only the keys of the
 * PUT, `sentKeys`, when the message names any of them).
 */
export function fieldErrors(err, sentKeys = []) {
  const message = (err && err.message) || 'The settings were not saved.';
  const fields = {};
  const code = err && err.code;
  const listed = err && err.details && Array.isArray(err.details.keys) ? err.details.keys.map(parseLockedKey).filter(Boolean) : [];
  if (code === 'config_key_pinned' && listed.length) {
    for (const { key, env } of listed) fields[key] = env ? `Set by the environment variable ${env}; unset it and restart to change this.` : 'Set by an environment variable; unset it and restart to change this.';
  } else if (code === 'config_key_not_mutable' && listed.length) {
    for (const { key } of listed) fields[key] = 'Only the config file can change this, followed by a restart.';
  } else if (err && (err.status === 400 || err.status === 422)) {
    // A message may name a key the user did not touch (a related setting);
    // when it also names a key in the PUT, mark only those.
    const named = keysInMessage(message);
    const sent = named.filter((k) => sentKeys.includes(k));
    for (const key of sent.length ? sent : named) fields[key] = message;
  }
  return { message, fields };
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
 * cannot be sent (a number field that is not a whole number); warnings are
 * values the server accepts but that look wrong. Bounds stay with the
 * server, which names the key.
 */
export function validateDraft(draft) {
  const errors = {};
  const warnings = {};
  for (const [key, v] of Object.entries(draft || {})) {
    const f = BY_KEY.get(key);
    if (!f) continue;
    if (f.type === 'number' && !Number.isInteger(v)) errors[key] = 'Enter a whole number.';
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

const labelOf = (key) => (BY_KEY.get(key) || { label: key }).label;
const joinWords = (items) => (items.length < 3 ? items.join(' and ') : `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`);

/** Field labels for a list of keys, at most `max` named: "Albums, Songs and 2 more". */
export function labelList(keys, max = 3) {
  const names = keys.map(labelOf);
  if (names.length <= max) return joinWords(names);
  return joinWords([...names.slice(0, max), `${names.length - max} more`]);
}

/**
 * The outcome of Reload from file from the keys that changed: `short` for a
 * toast, `long` for the page (which settings, and that pinned keys stay put).
 */
export function reloadSummary(changed, pinnedCount = 0) {
  const n = changed.length === 1 ? '1 setting' : `${changed.length} settings`;
  const pinned = pinnedCount > 0 ? ` ${pinnedCount === 1 ? '1 pinned key keeps its environment value.' : `${pinnedCount} pinned keys keep their environment values.`}` : '';
  if (changed.length === 0) return { short: 'Reloaded from the file: no changes', long: `Reloaded from the config file: no changes.${pinned}` };
  return { short: `Reloaded from the file: ${n} changed`, long: `Reloaded from the config file: ${n} changed (${labelList(changed, 4)}).${pinned}` };
}

/** What leaving or discarding would lose, for the confirmation dialogs. */
export function lossSummary(keys) {
  const sections = [...new Set(keys.map((k) => (BY_KEY.get(k) || {}).section).filter(Boolean))];
  const names = sections.map((id) => (SETTINGS_SECTIONS.find((s) => s.id === id) || { label: id }).label);
  return { count: keys.length, fields: labelList(keys, 4), sections: joinWords(names) };
}

/** The polite announcement for a changed unsaved count; '' when there is nothing to say. */
export function unsavedAnnouncement(n) {
  return n > 0 ? changesLabel(n) : 'No unsaved changes';
}

/**
 * The Library sync State badge from GET /library-sync data and whether the
 * media user token is set: on but idle is not "Off".
 */
export function librarySyncState(s, tokenSet) {
  if (!s.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (!tokenSet) return { icon: 'triangle-alert', role: 'warning', label: 'On, idle', detail: 'The media user token is empty' };
  if (s.last_error) return { icon: 'triangle-alert', role: 'warning', label: 'Error', detail: s.last_error };
  if (!s.last_poll_at) return { icon: 'circle-check', role: 'success', label: 'Watching', detail: `Every ${s.interval_minutes} min; first poll pending` };
  return { icon: 'circle-check', role: 'success', label: 'Watching', detail: `Every ${s.interval_minutes} min` };
}

/** Whether Reset anchor does anything, with the reason when it does not. */
export function anchorResetState(s) {
  if (!s) return { allowed: false, reason: 'Loading the status.' };
  if (!(s.anchor_size > 0)) return { allowed: false, reason: 'There is no anchor to reset yet: the next poll sets one.' };
  return { allowed: true, reason: '' };
}

/** Parses a number input: an integer, or the raw text when it is not one. */
export function parseNumberInput(text) {
  const t = String(text).trim();
  return /^-?\d+$/.test(t) ? Number(t) : t;
}

/** "1 unsaved change" / "3 unsaved changes". */
export const changesLabel = (n) => `${n} unsaved ${n === 1 ? 'change' : 'changes'}`;
