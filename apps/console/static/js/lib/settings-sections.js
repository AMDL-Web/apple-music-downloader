// The Settings page's sections, their sub-groups and the section a
// ?section= value opens. Pure: no DOM, so node --test covers it.

import { fieldByKey } from './settings.js';

/**
 * The page's sections in nav order; `id` is the ?section= value. `board`
 * sections have a status tile on the board; `split` starts the nav's second
 * group (a hairline above it).
 */
export const SETTINGS_SECTIONS = [
  { id: 'account', label: 'Account', icon: 'key', board: true },
  { id: 'quality', label: 'Quality', icon: 'download', board: true },
  { id: 'files', label: 'Storage & files', icon: 'hard-drive', board: true },
  { id: 'lyrics_artwork', label: 'Lyrics & artwork', icon: 'disc', board: false },
  { id: 'jobs', label: 'Jobs', icon: 'list', board: true },
  { id: 'library_sync', label: 'Library sync', icon: 'rotate', board: true, split: true },
  { id: 'hooks', label: 'Hooks', icon: 'zap', board: true },
  { id: 'logging', label: 'Logs', icon: 'file-text', board: true },
  { id: 'server', label: 'Server', icon: 'server', board: true },
];

/** The sections with a board tile, in nav order. */
export const BOARD_SECTIONS = SETTINGS_SECTIONS.filter((s) => s.board);

/**
 * Sub-headings inside a section, in order. A group id is also a ?section=
 * value, which opens its section scrolled to the group.
 */
export const SETTINGS_GROUPS = [
  { id: 'simulate', section: 'server', label: 'Simulate', icon: 'play' },
  { id: 'config_file', section: 'server', label: 'Configuration file', icon: 'braces' },
];

// Section ids of earlier versions, kept so bookmarks and links still open
// the section that now holds their settings.
const RETIRED_SECTIONS = {
  advanced: 'jobs',
  downloading: 'jobs',
  apple_requests: 'lyrics_artwork',
  downloads: 'quality',
  artwork: 'lyrics_artwork',
  lyrics: 'lyrics_artwork',
  catalog: 'account',
};

/** The sub-group a ?section= value names, or undefined. */
export const groupById = (id) => SETTINGS_GROUPS.find((g) => g.id === id);

/** The section with this exact id, or undefined. */
export const sectionWithId = (id) => SETTINGS_SECTIONS.find((s) => s.id === id);

/**
 * The section a ?section= value names (a section, a sub-group or a retired
 * id), or the first one. A known `field` wins: its section opens.
 */
export function sectionById(id, field = '') {
  const f = field ? fieldByKey(field) : undefined;
  const want = f ? f.section : (groupById(id) || {}).section || RETIRED_SECTIONS[id] || id;
  return sectionWithId(want) || SETTINGS_SECTIONS[0];
}
