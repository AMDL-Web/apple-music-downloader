// The Settings page's sections, the Advanced groups and the section a
// ?section= value opens. Pure: no DOM, so node --test covers it.

import { fieldByKey } from './settings.js';

/**
 * The page's sections in nav order; `id` is the ?section= value. `summary`
 * is the one line the phone's section list shows under the label.
 */
export const SETTINGS_SECTIONS = [
  { id: 'account', label: 'Account', icon: 'key', summary: 'Apple Music login and media token', intro: 'Your Apple Music login, kept by the session service.' },
  { id: 'quality', label: 'Quality & downloads', icon: 'download', summary: 'Codecs, ALAC limits, overwriting', intro: 'Which version of each track is downloaded, and what happens to files already on disk.' },
  { id: 'files', label: 'Files & naming', icon: 'hard-drive', summary: 'Folders and file names', intro: 'Where finished tracks go and how their folders and names are built.' },
  { id: 'lyrics_artwork', label: 'Lyrics & artwork', icon: 'disc', summary: 'Covers and lyrics', intro: 'Covers and lyrics, embedded in each track or saved next to it.' },
  { id: 'advanced', label: 'Advanced', icon: 'settings', summary: 'Retries, library sync, logging, simulate', intro: 'Tuning, library sync, logging, simulate mode and the configuration file.' },
];

/**
 * The sub-headings of Advanced, in order. A group id is also a ?section=
 * value (the sections they were before), which opens Advanced at the group.
 */
export const SETTINGS_GROUPS = [
  { id: 'downloading', section: 'advanced', label: 'Downloading', icon: 'download' },
  { id: 'apple_requests', section: 'advanced', label: 'Apple Music requests', icon: 'music' },
  { id: 'library_sync', section: 'advanced', label: 'Library sync', icon: 'rotate', intro: 'Download the album of every song you add to your Apple Music library.' },
  { id: 'logging', section: 'advanced', label: 'Logging', icon: 'list' },
  { id: 'simulate', section: 'advanced', label: 'Simulate', icon: 'play', intro: 'Run jobs without downloading or writing files.' },
  { id: 'config_file', section: 'advanced', label: 'Configuration file', icon: 'braces' },
];

// Section ids of earlier versions, kept so bookmarks and links still open
// the section that now holds their settings.
const RETIRED_SECTIONS = { downloads: 'quality', artwork: 'lyrics_artwork', lyrics: 'lyrics_artwork', catalog: 'account' };

/** The Advanced group a ?section= value names, or undefined. */
export const groupById = (id) => SETTINGS_GROUPS.find((g) => g.id === id);

/**
 * The section a ?section= value names (a section, an Advanced group or a
 * retired id), or the first one. A known `field` wins: its section opens.
 */
export function sectionById(id, field = '') {
  const f = field ? fieldByKey(field) : undefined;
  const want = f ? f.section : (groupById(id) || {}).section || RETIRED_SECTIONS[id] || id;
  return SETTINGS_SECTIONS.find((s) => s.id === want) || SETTINGS_SECTIONS[0];
}
