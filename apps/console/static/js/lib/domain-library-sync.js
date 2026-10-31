// Library sync's view for lib/domains.js: tracks seen, the next check, and
// whether it waits for a token. Pure.

import { count } from './format.js';
import { pct, plural, savedConfig, frame, roleOf } from './domain-frame.js';
import { keysToLabels } from './settings-draft.js';
import { nextCheckFraction, nextCheckIn } from './sysdata.js';
import { settingsHash } from './route.js';

/**
 * Library sync's state, shared with Overview: Off, Needs token (on, but the
 * saved media user token is empty), Error, or Watching. `tokenSet` undefined
 * (the config not read yet) counts as set.
 */
export function librarySyncState(sync, tokenSet) {
  if (!sync.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (tokenSet === false) return { icon: 'triangle-alert', role: 'warning', label: 'Needs token' };
  if (sync.last_error) return { icon: 'circle-x', role: 'danger', label: 'Error' };
  return { icon: 'circle-check', role: 'success', label: 'Watching' };
}

const TOKEN_HREF = settingsHash('account', 'catalog.media_user_token');

// Library sync: tracks seen, and when the next check is.
export function librarySync(s, ctx) {
  const e = s.librarySync;
  const v = frame('library_sync', e, ctx);
  if (v.state !== 'ok') return v;
  const sync = e.data;
  const tag = librarySyncState(sync, savedConfig(s).tokenSet);
  const anchor = sync.anchor_size || 0;
  const figure = anchor > 0 ? { value: count(anchor), unit: anchor === 1 ? 'track seen' : 'tracks seen' } : { value: 'Not anchored yet', word: true };
  const fraction = tag.label === 'Off' ? 0 : nextCheckFraction(sync, ctx.now);
  const viz = { kind: 'meter', fraction: fraction || 0, role: 'neutral', valuetext: fraction === null ? 'No check yet' : `${pct(fraction || 0)} of the way to the next check` };
  let note;
  let summary;
  if (tag.label === 'Off') {
    // The figure already says what was recorded.
    note = null;
    summary = { text: 'Off' };
  } else if (tag.label === 'Needs token') {
    note = { text: 'Needs the media user token', role: 'warning', link: { href: TOKEN_HREF, label: 'Set token' } };
    summary = { text: 'Needs token', role: 'warning' };
  } else if (tag.label === 'Error') {
    const text = keysToLabels(sync.last_error);
    note = { text, role: 'danger', title: sync.last_error };
    summary = { text: 'Error', role: 'danger' };
  } else {
    const due = nextCheckIn(sync, ctx.now);
    const albums = sync.total_submitted > 0 ? plural(sync.total_submitted, 'album', 'albums') + ' since start' : '';
    const when = due === null ? `First check within ${plural(sync.interval_minutes || 0, 'min', 'min')}` : due <= 0 ? 'Checking…' : `Next check in ${elapsedShort(due)}`;
    note = { text: [when, albums].filter(Boolean).join(' · ') };
    summary = { text: when };
  }
  return {
    ...v,
    role: roleOf(tag.role),
    tag,
    figure,
    viz,
    note,
    detail: tag.label === 'Error' ? keysToLabels(sync.last_error) : '',
    summary,
  };
}

/** "9 min", "2 h", "45 s": the wait before something, rounded up to its unit. */
function elapsedShort(ms) {
  const s = Math.ceil(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.ceil(s / 60);
  if (m < 60) return `${m} min`;
  return `${Math.round(m / 60)} h`;
}
