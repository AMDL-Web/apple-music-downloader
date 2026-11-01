// The Settings page frame: the side list of sections (desktop), the phone's
// list of sections with live summaries and its back link, and the save bar
// (which also offers the restart that applies saved keys).

import { html, useRef } from '../ui/preact.js';
import { SETTINGS_SECTIONS } from '../lib/settings-sections.js';
import { changesLabel, labelOf } from '../lib/settings-draft.js';
import { settingsHash, buildHash } from '../lib/route.js';
import { Icon, Button } from '../ui/components.js';

/**
 * The mark after a section's name, in a slot that is always there: an
 * invalid value, else unsaved changes, else the domain's danger or warning.
 * Each says itself in words for screen readers.
 */
function Mark({ id, dirty, invalid, view }) {
  let icon = '';
  let role = '';
  let word = '';
  if (invalid.has(id)) [icon, role, word] = ['circle-x', 'danger', 'has an invalid value'];
  else if (dirty.has(id)) [icon, role, word] = ['dot', 'accent', 'unsaved'];
  else if (view && view.role) [icon, role, word] = [view.role === 'danger' ? 'circle-x' : 'triangle-alert', view.role, view.tag ? view.tag.label : view.role];
  return html`<span class="settings-nav-mark" data-role=${role || undefined}>
    ${icon === 'dot' ? html`<span class="settings-nav-dot" aria-hidden="true"></span>` : icon ? html`<${Icon} name=${icon} size="sm" />` : null}
    ${word ? html`<span class="sr-only">, ${word}</span>` : null}
  </span>`;
}

/**
 * The side list (hidden under 640 px): Status (the board), then the
 * sections, a hairline before the second group. `section` is the open one
 * (null on the board); `dirty` and `invalid` are Sets of section ids;
 * `views` the domain views by section id.
 */
export function SettingsNav({ section, dirty, invalid, views }) {
  const item = (s) => html`<li key=${s.id} class=${s.split ? 'settings-nav-split' : undefined}>
    <a
      class="settings-nav-item"
      href=${settingsHash(s.id)}
      data-domain=${s.id}
      aria-current=${section && s.id === section.id ? 'page' : undefined}
    >
      <${Icon} name=${s.icon} />
      <span class="settings-nav-label">${s.label}</span>
      <${Mark} id=${s.id} dirty=${dirty} invalid=${invalid} view=${views[s.id]} />
    </a>
  </li>`;
  return html`<nav class="settings-nav" aria-label="Settings sections">
    <ul class="settings-nav-list">
      <li>
        <a class="settings-nav-item" href=${settingsHash()} data-domain="status" aria-current=${section ? undefined : 'page'}>
          <${Icon} name="activity" />
          <span class="settings-nav-label">Status</span>
          <span class="settings-nav-mark"></span>
        </a>
      </li>
      ${SETTINGS_SECTIONS.map(item)}
    </ul>
  </nav>`;
}

/**
 * Under 640 px, #/settings without a section: every section as a row of a
 * grouped list (icon, label, its live one-line summary, mark, chevron),
 * each opening as its own view. Logs, whose summary counts errors, opens the
 * Logs page instead (its errors when there are some), where the Logs tab opens
 * it unfiltered; the Logs page's head links back to its settings. `summaries` holds each row's {text, role?}; a row without
 * one yet keeps the line's room.
 */
export function SettingsList({ dirty, invalid, views, summaries }) {
  const row = (s) => {
    const v = views[s.id];
    const sum = summaries[s.id];
    const role = v && v.role ? v.role : undefined;
    const errors = s.id === 'logging' && v && v.viz && v.viz.kind === 'bars' && v.viz.buckets.some((n) => n > 0);
    const href = s.id === 'logging' ? buildHash('logs', errors ? { level: 'error' } : {}) : settingsHash(s.id);
    return html`<li key=${s.id}>
      <a class="settings-list-row" href=${href} data-domain=${s.id}>
        <span class="settings-list-icon" data-role=${role}><${Icon} name=${s.icon} /></span>
        <span class="settings-list-text">
          <span class="settings-list-label">${s.label}</span>
          <span class="settings-list-summary" data-role=${sum && sum.role ? sum.role : undefined}>${sum && sum.text ? sum.text : ' '}</span>
        </span>
        <${Mark} id=${s.id} dirty=${dirty} invalid=${invalid} view=${v} />
        <span class="settings-list-chevron" aria-hidden="true"><${Icon} name="chevron-right" size="sm" /></span>
      </a>
    </li>`;
  };
  const split = SETTINGS_SECTIONS.findIndex((s) => s.split);
  return html`<nav class="settings-list-card" aria-label="Settings sections">
    <div class="card settings-list-group"><ul class="settings-list">${SETTINGS_SECTIONS.slice(0, split).map(row)}</ul></div>
    <div class="card settings-list-group"><ul class="settings-list">${SETTINGS_SECTIONS.slice(split).map(row)}</ul></div>
  </nav>`;
}

/** A phone's way back from a section to the list. */
export function SettingsBack() {
  return html`<a class="settings-back" href=${settingsHash()}><${Icon} name="chevron-left" size="sm" /><span>Settings</span></a>`;
}

/**
 * The bar while anything differs from the server: the count and the
 * sections it is in (each a link), Discard and Save. With nothing unsaved
 * and saved keys waiting for a restart (`restart`: {pending, busy, offline,
 * onRestart}), the same bar offers the restart instead. Fixed and out of
 * flow, one row at every width: the page keeps its room, so it moves
 * nothing when it comes, changes or goes. A bar that is there when the
 * page first draws (a restart still pending) does not rise in.
 * `place` is a style object with its --save-x and --save-w.
 */
export function SaveBar({ count, sections, saving, onDiscard, onSave, place, restart }) {
  const quiet = useRef(Boolean(restart)).current;
  if (restart) {
    const names = restart.pending.map(labelOf).join(', ');
    const off = restart.busy || restart.offline;
    return html`<div class="save-bar" role="region" aria-label="Restart to apply" data-state="restart" data-quiet=${quiet ? '' : undefined} style=${place}>
      <p class="save-bar-text" title=${names}>
        <span class="save-bar-count">Restart to apply</span>
        <span class="save-bar-where">${names}</span>
      </p>
      <div class="save-bar-actions">
        <${Button}
          key="restart"
          variant="primary"
          className="settings-restart-button"
          aria-busy=${restart.busy ? 'true' : undefined}
          aria-disabled=${off ? 'true' : undefined}
          onClick=${off ? undefined : restart.onRestart}
        >
          <span class="settings-restart-label" data-shown=${restart.busy ? undefined : ''} aria-hidden=${restart.busy ? 'true' : undefined}>Restart</span>
          <span class="settings-restart-label" data-shown=${restart.busy ? '' : undefined} aria-hidden=${restart.busy ? undefined : 'true'}>Restarting…</span>
        </${Button}>
      </div>
    </div>`;
  }
  const where = SETTINGS_SECTIONS.filter((s) => sections.has(s.id));
  // "unsaved" is left out on a phone, where the count shares one row with two buttons.
  const [head, tail] = changesLabel(count).split('unsaved ');
  return html`<div class="save-bar" role="region" aria-label="Unsaved changes" data-state=${saving ? 'saving' : 'dirty'} data-quiet=${quiet ? '' : undefined} style=${place}>
    <p class="save-bar-text">
      <span class="save-bar-count">${head}<span class="save-bar-unsaved">unsaved </span>${tail}</span>
      <span class="save-bar-where">
        ${where.map((s, i) => html`${i ? ', ' : ''}<a key=${s.id} class="link" href=${settingsHash(s.id)}>${s.label}</a>`)}
      </span>
    </p>
    <div class="save-bar-actions">
      <${Button} key="discard" variant="ghost" className="save-bar-discard" onClick=${onDiscard} disabled=${saving}>Discard</${Button}>
      <${Button}
        key="save"
        variant="primary"
        icon="save"
        className="save-bar-save"
        aria-keyshortcuts="Meta+S Control+S"
        aria-disabled=${saving ? 'true' : undefined}
        onClick=${onSave}
      >${saving ? 'Saving…' : 'Save'}</${Button}>
    </div>
  </div>`;
}
