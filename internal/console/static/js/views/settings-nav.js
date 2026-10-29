// The Settings page frame around the open section: the section list (a
// select under 640 px) and the sticky save bar.

import { html } from '../ui/preact.js';
import { SETTINGS_SECTIONS } from '../lib/settings.js';
import { changesLabel } from '../lib/settings-draft.js';
import { settingsHash } from '../lib/route.js';
import { Icon, Button } from '../ui/components.js';

/** The section list; `dirty` and `invalid` are Sets of section ids. */
export function SettingsNav({ section, dirty, invalid, navigate }) {
  const navItem = (s) => {
    const marks = [dirty.has(s.id) ? 'unsaved changes' : '', invalid.has(s.id) ? 'needs attention' : ''].filter(Boolean);
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
        ${invalid.has(s.id)
          ? html`<span class="settings-nav-mark" data-role="danger"><${Icon} name="circle-x" size="sm" /></span>`
          : dirty.has(s.id)
            ? html`<span class="settings-nav-mark settings-nav-dot" aria-hidden="true"></span>`
            : null}
        ${marks.length ? html`<span class="sr-only">, ${marks.join(', ')}</span>` : null}
      </a>
    </li>`;
  };

  return html`<nav class="settings-nav" aria-label="Settings sections">
    <ul class="settings-nav-list">${SETTINGS_SECTIONS.map(navItem)}</ul>
    <label class="settings-jump">
      <span class="settings-jump-label">Section</span>
      <select class="select" value=${section.id} onChange=${(e) => navigate(settingsHash(e.currentTarget.value))}>
        ${SETTINGS_SECTIONS.map(
          (s) => html`<option key=${s.id} value=${s.id}>${s.label}${invalid.has(s.id) ? ' (needs attention)' : dirty.has(s.id) ? ' (unsaved)' : ''}</option>`,
        )}
      </select>
    </label>
  </nav>`;
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
