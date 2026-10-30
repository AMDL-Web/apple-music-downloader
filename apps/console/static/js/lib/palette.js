// What the command palette lists for a query (views/palette.js renders it):
// pages, actions, filters and themes; with a query, also a download
// action for Apple Music links in it, Settings sections and fields, a
// catalog search for words (New download runs it), and the jobs the search
// found.

import { buildHash, newDownloadHash, newSearchHash, settingsHash } from './route.js';
import { searchTerm } from './search.js';
import { extractLinks } from './links.js';
import { SETTINGS_SECTIONS, SETTINGS_GROUPS, FIELDS, groupById } from './settings.js';
import { jobTitle, jobSubtitle, typeLabel, dateTime } from './format.js';
import { jobStatusView } from './status.js';

const STATIC_ITEMS = [
  { id: 'act-new', group: 'Actions', label: 'New download', icon: 'plus', hash: buildHash('new'), keys: 'n' },
  { id: 'nav-overview', group: 'Navigate', label: 'Overview', icon: 'grid', hash: buildHash('overview'), keys: 'g o' },
  { id: 'nav-queue', group: 'Navigate', label: 'Queue', icon: 'list', hash: buildHash('queue'), keys: 'g q' },
  { id: 'nav-showcase', group: 'Navigate', label: 'Showcase', icon: 'disc', hash: buildHash('showcase'), keys: 'g s' },
  { id: 'nav-logs', group: 'Navigate', label: 'Logs', icon: 'file-text', hash: buildHash('logs'), keys: 'g l' },
  { id: 'nav-system', group: 'Navigate', label: 'System', icon: 'server', hash: buildHash('system'), keys: 'g y' },
  { id: 'nav-settings', group: 'Navigate', label: 'Settings', icon: 'settings', hash: settingsHash(), keys: 'g ,' },
  { id: 'f-failed', group: 'Filters', label: 'Show failed jobs', icon: 'circle-x', hash: buildHash('queue', { tab: 'failed' }) },
  { id: 'f-running', group: 'Filters', label: 'Show active jobs', icon: 'dot', hash: buildHash('queue', { tab: 'active' }) },
  { id: 'f-errors', group: 'Filters', label: 'Show error logs', icon: 'triangle-alert', hash: buildHash('logs', { level: 'warn,error' }) },
];

const PREF_ITEMS = [
  { id: 'theme-system', group: 'Theme', label: 'Use system theme', icon: 'monitor', theme: 'system' },
  { id: 'theme-light', group: 'Theme', label: 'Use light theme', icon: 'sun', theme: 'light' },
  { id: 'theme-dark', group: 'Theme', label: 'Use dark theme', icon: 'moon', theme: 'dark' },
];

/** Settings sections (and Advanced groups) listed before anything is typed. */
const SECTIONS_SHOWN_EMPTY = ['account', 'library_sync'];

/** Words a section or group answers to besides its label and id (what its panels do). */
const SECTION_WORDS = {
  account: 'login log in log out wrapper session apple id media user token',
  quality: 'codec alac atmos lossless overwrite',
  files: 'folder path template naming',
  lyrics_artwork: 'cover artwork lyrics lrc ttml karaoke translation romanization pronunciation',
  advanced: 'tuning',
  library_sync: 'anchor poll',
  config_file: 'reload yaml config locked startup restart parallel concurrency concurrent threads workers pool pools limits keys defaults',
  logging: 'logs',
};

// What the page says about the parallel pools: they are startup settings, so
// no field has them, and a search for them lands here.
const SECTION_SUBS = { config_file: 'Worker pools are set in the file' };

// The five sections, then the Advanced groups, which open Advanced at the group.
const SECTION_ITEMS = [...SETTINGS_SECTIONS, ...SETTINGS_GROUPS].map((s) => ({
  id: `set-${s.id}`,
  group: 'Settings',
  label: `Settings: ${s.label}`,
  sub: SECTION_SUBS[s.id] || (s.section ? 'Advanced' : undefined),
  icon: s.icon,
  hash: settingsHash(s.id),
  section: s.id,
  words: `${s.id.replace(/_/g, ' ')} ${SECTION_WORDS[s.id] || ''}`,
}));

/** Words a field answers to besides its label and key: by type, then by key. */
const TYPE_WORDS = { template: 'template name format', path: 'folder directory' };
const KEY_WORDS = {
  'download.max_attempts': 'retry retries tries attempts',
  'download.memory_mode': 'ram parallel memory mode',
  'catalog.album_track_url_mode': 'song track album link',
  'library_sync.interval_minutes': 'poll interval',
  'download.force_overwrite': 'replace redownload again',
  'download.progress_event_interval_ms': 'throttle',
  'logging.level': 'verbose',
};
const fieldWords = (f) => `${TYPE_WORDS[f.type] || ''} ${KEY_WORDS[f.key] || ''}`;

const SECTION_LABELS = new Map(SETTINGS_SECTIONS.map((s) => [s.id, s]));

/** At most this many Settings fields for one query. */
const FIELD_LIMIT = 6;

const matches = (q, ...texts) => texts.some((t) => typeof t === 'string' && t.toLowerCase().includes(q));

/** One palette row for a job the search found: enough to tell two runs of one album apart. */
export function jobItem(job) {
  const status = jobStatusView(job);
  return {
    id: `job-${job.id}`,
    group: 'Jobs',
    label: jobTitle(job),
    sub: [jobSubtitle(job), typeLabel(job.type), status && status.label, dateTime(job.updated_at || job.created_at)].filter(Boolean).join(' · '),
    icon: 'music',
    hash: buildHash('queue', {}, job.id),
    dir: 'auto',
  };
}

/**
 * The palette's rows for `query` and the jobs its search returned, in
 * display order. Rows carry {id, group, label, icon} and a hash or a
 * theme; `sub`, `keys`, `dir` and, for a Settings field,
 * `field` (its config key, to focus once the section is open) are optional.
 */
export function paletteItems(query, jobs = []) {
  const q = (query || '').trim().toLowerCase();
  if (!q) {
    return [...STATIC_ITEMS, ...SECTIONS_SHOWN_EMPTY.map((id) => SECTION_ITEMS.find((s) => s.section === id)), ...PREF_ITEMS];
  }
  const out = [];
  const links = extractLinks(query);
  if (links.length > 0) {
    out.push({
      id: 'act-download-links',
      group: 'Actions',
      label: links.length === 1 ? 'Download this link' : `Download these ${links.length} links`,
      sub: links.length === 1 ? links[0] : undefined,
      icon: 'download',
      hash: newDownloadHash(links),
    });
  }
  out.push(...STATIC_ITEMS.filter((it) => matches(q, it.label, it.group)));
  out.push(...SECTION_ITEMS.filter((it) => matches(q, it.label, it.words)));
  if (q.length >= 2 && links.length === 0) {
    const fields = FIELDS.filter((f) => matches(q, f.label, f.key, fieldWords(f))).slice(0, FIELD_LIMIT);
    for (const f of fields) {
      const section = SECTION_LABELS.get(f.section);
      out.push({
        id: `field-${f.key}`,
        group: 'Settings',
        label: f.label,
        // The key stays searchable but is not shown: the page hides keys by default.
        sub: [section ? section.label : f.section, f.group ? groupById(f.group).label : ''].filter(Boolean).join(' › '),
        icon: section ? section.icon : 'settings',
        hash: settingsHash(f.section, f.key),
        field: f.key,
      });
    }
  }
  out.push(...PREF_ITEMS.filter((it) => matches(q, it.label, it.group)));
  const term = links.length === 0 ? searchTerm(query, []).term : '';
  if (term) {
    out.push({ id: 'act-search', group: 'Apple Music', label: `Search Apple Music for “${term}”`, icon: 'search', hash: newSearchHash(term), dir: 'auto' });
  }
  out.push(...jobs.map(jobItem));
  return out;
}

/** The polite status the palette announces: "3 results", "No results". */
export function resultCount(n) {
  if (n === 0) return 'No results';
  return n === 1 ? '1 result' : `${n} results`;
}
