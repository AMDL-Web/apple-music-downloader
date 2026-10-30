// The Settings page frame around the open section: the side list of
// sections (desktop), the phone's list of sections with its back link, and
// the sticky save bar.

import { html } from '../ui/preact.js';
import { SETTINGS_SECTIONS } from '../lib/settings-sections.js';
import { changesLabel } from '../lib/settings-draft.js';
import { settingsHash } from '../lib/route.js';
import { Icon, Button } from '../ui/components.js';

/** Words for a section's marks, read after its name: ", unsaved changes". */
const markWords = (id, dirty, invalid) => [dirty.has(id) ? 'unsaved changes' : '', invalid.has(id) ? 'needs attention' : ''].filter(Boolean);

/** The mark after a section's name: an error icon, else the unsaved dot. */
function Mark({ id, dirty, invalid }) {
  const words = markWords(id, dirty, invalid);
  return html`${invalid.has(id)
      ? html`<span class="settings-nav-mark" data-role="danger"><${Icon} name="circle-x" size="sm" /></span>`
      : dirty.has(id)
        ? html`<span class="settings-nav-mark settings-nav-dot" aria-hidden="true"></span>`
        : null}
    ${words.length ? html`<span class="sr-only">, ${words.join(', ')}</span>` : null}`;
}

/** The side list of sections (hidden under 640 px); `dirty` and `invalid` are Sets of section ids. */
export function SettingsNav({ section, dirty, invalid }) {
  const navItem = (s) => {
    return html`<li key=${s.id}>
      <a
        class="settings-nav-item"
        href=${settingsHash(s.id)}
        data-section=${s.id}
        data-dirty=${dirty.has(s.id) ? '' : undefined}
        data-invalid=${invalid.has(s.id) ? '' : undefined}
        aria-current=${s.id === section.id ? 'true' : undefined}
      >
        <${Icon} name=${s.icon} />
        <span class="settings-nav-label">${s.label}</span>
        <${Mark} id=${s.id} dirty=${dirty} invalid=${invalid} />
      </a>
    </li>`;
  };

  return html`<nav class="settings-nav" aria-label="Settings sections">
    <ul class="settings-nav-list">${SETTINGS_SECTIONS.map(navItem)}</ul>
  </nav>`;
}

/**
 * Under 640 px, #/settings without a section: the five sections as a grouped
 * list (icon, label, one-line summary, chevron), each opening as its own view.
 */
export function SettingsList({ dirty, invalid }) {
  return html`<nav class="card settings-list-card" aria-label="Settings sections">
    <ul class="settings-list">
      ${SETTINGS_SECTIONS.map(
        (s) => html`<li key=${s.id}>
          <a class="settings-list-row" href=${settingsHash(s.id)} data-section=${s.id}>
            <span class="settings-list-icon"><${Icon} name=${s.icon} /></span>
            <span class="settings-list-text">
              <span class="settings-list-label">${s.label}</span>
              <span class="settings-list-summary">${s.summary}</span>
            </span>
            <${Mark} id=${s.id} dirty=${dirty} invalid=${invalid} />
            <span class="settings-list-chevron" aria-hidden="true"><${Icon} name="chevron-right" size="sm" /></span>
          </a>
        </li>`,
      )}
    </ul>
  </nav>`;
}

/** A phone's way back from a section to the list. */
export function SettingsBack() {
  return html`<a class="settings-back" href=${settingsHash()}><${Icon} name="chevron-left" size="sm" /><span>Settings</span></a>`;
}

/** The sticky bar while anything differs from the server: the count, where, Discard and Save. */
export function SaveBar({ count, sections, saving, onDiscard, onSave }) {
  return html`<div class="save-bar" role="region" aria-label="Unsaved changes" data-state=${saving ? 'saving' : 'dirty'}>
    <p class="save-bar-text">
      <span class="save-bar-count">${changesLabel(count)}</span>
      <span class="save-bar-where">${SETTINGS_SECTIONS.filter((s) => sections.has(s.id)).map((s) => s.label).join(', ')}</span>
    </p>
    <div class="save-bar-actions">
      <${Button} variant="ghost" className="save-bar-discard" onClick=${onDiscard} disabled=${saving}>Discard</${Button}>
      <${Button}
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
