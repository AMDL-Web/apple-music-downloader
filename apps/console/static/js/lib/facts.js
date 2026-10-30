// Read-only fact rows for the Settings sections: values fixed at startup that
// GET /system reports, and which saved config keys make the stores refetch. Pure.

import { count } from './format.js';

const num = (n) => (typeof n === 'number' && Number.isFinite(n) ? { text: count(n) } : null);
const str = (s, mono = false) => (typeof s === 'string' && s !== '' ? (mono ? { text: s, mono: true } : { text: s }) : null);

function ffmpeg(system) {
  const tool = Array.isArray(system.tools) ? system.tools.find((t) => t && t.name === 'ffmpeg') : null;
  if (!tool) return null;
  if (tool.available) return tool.path ? { text: tool.path, mono: true } : { text: 'Available' };
  return { text: 'Missing', role: system.simulate === true ? 'neutral' : 'danger' };
}

function logBuffer(system) {
  const n = system.startup && system.startup.log_buffer_size;
  if (typeof n !== 'number' || !Number.isFinite(n)) return null;
  return n === 0 ? { text: 'Off', role: 'warning' } : { text: `${count(n)} lines` };
}

// { id, label, section, configKey?, chip: 'config' | 'builtin' | 'file', read(system) -> {text, mono?, role?} | null, title? }
export const FIXED_ROWS = [
  { id: 'default_storefront', label: 'Default storefront', section: 'account', configKey: 'catalog.default_storefront', chip: 'config', read: (s) => str(typeof s.default_storefront === 'string' ? s.default_storefront.toUpperCase() : null) },
  { id: 'catalog_language', label: 'Catalog language', section: 'account', configKey: 'catalog.language', chip: 'config', read: (s) => str(s.startup && s.startup.catalog_language) },
  { id: 'wrapper_address', label: 'Session service', section: 'account', configKey: 'wrapper.address', chip: 'config', read: (s) => str(s.startup && s.startup.wrapper_address, true) },
  { id: 'max_running_jobs', label: 'Jobs at once', section: 'jobs', configKey: 'download.max_running_jobs', chip: 'config', read: (s) => num(s.jobs && s.jobs.workers) },
  { id: 'max_parallel_downloads', label: 'Parallel track downloads', section: 'jobs', configKey: 'download.max_parallel_downloads', chip: 'config', read: (s) => num(s.pools && s.pools.downloads) },
  { id: 'max_parallel_requests', label: 'Apple Music requests at once', section: 'jobs', configKey: 'catalog.max_parallel_requests', chip: 'config', read: (s) => num(s.startup && s.startup.max_parallel_requests) },
  { id: 'decrypts', label: 'Decrypt slots', section: 'jobs', chip: 'builtin', title: 'One per CPU', read: (s) => num(s.pools && s.pools.decrypts) },
  { id: 'wrapper_requests', label: 'Session-service requests', section: 'jobs', chip: 'builtin', read: (s) => num(s.pools && s.pools.wrapper_requests) },
  { id: 'log_buffer_size', label: 'Log history', section: 'logging', configKey: 'logging.buffer_size', chip: 'config', read: logBuffer },
  { id: 'listen', label: 'Listen address', section: 'server', configKey: 'server.listen', chip: 'config', read: (s) => str(s.startup && s.startup.listen, true) },
  { id: 'database_path', label: 'Database', section: 'server', configKey: 'database.path', chip: 'config', read: (s) => str(s.startup && s.startup.database_path, true) },
  { id: 'ffmpeg', label: 'ffmpeg', section: 'server', configKey: 'tools.ffmpeg', chip: 'config', read: ffmpeg },
  { id: 'hooks_file', label: 'Hooks file', section: 'hooks', chip: 'file', read: () => ({ text: 'hooks.yaml', mono: true }) },
].map((row) => ({ ...row, read: (system) => (system && typeof system === 'object' ? row.read(system) : null) }));

/**
 * The rows of one section, in table order, each with its `value`; rows whose
 * value is unknown are left out. While `pending` (no answer from GET /system
 * yet) every row comes with an empty value, so the rows hold their place.
 */
export function factRows(section, system, pending = false) {
  const out = [];
  for (const row of FIXED_ROWS) {
    if (row.section !== section) continue;
    const value = pending ? { text: '' } : row.read(system);
    if (value) out.push({ ...row, value });
  }
  return out;
}

/** The chip for a fact row: what fixes it and how to change it. */
export function chipOf(row) {
  if (row.chip === 'config') {
    return { label: 'config.yaml', icon: 'lock', title: `Set in configs/config.yaml (${row.configKey}); restart to change` };
  }
  if (row.chip === 'builtin') return { label: 'Built in', icon: '', title: row.title || 'Fixed by the server' };
  return { label: 'At startup', icon: 'lock', title: 'Read at startup; restart to change' };
}

/** [config key prefix, status store keys to refetch] pairs; a prefix ending in "." matches a section. */
export const REFRESH_ON_SAVE = [
  ['simulate.', ['system']],
  ['library_sync.', ['librarySync']],
  ['catalog.media_user_token', ['librarySync']],
  ['catalog.motion_artwork_enabled', ['system']],
  ['download.downloads_dir', ['system']],
  ['download.temp_dir', ['system']],
];

/** The store keys to refetch after saving these dotted config keys, de-duplicated in first-seen order. */
export function refreshKeys(changedKeys) {
  const out = [];
  for (const changed of changedKeys || []) {
    for (const [prefix, keys] of REFRESH_ON_SAVE) {
      if (changed !== prefix && !changed.startsWith(prefix)) continue;
      for (const k of keys) if (!out.includes(k)) out.push(k);
    }
  }
  return out;
}
