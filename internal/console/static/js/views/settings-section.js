// The open Settings section's body: its fields, and in Account and
// Advanced the panels that are not config fields. Advanced is split into
// groups under sub-headings (lib/settings.js SETTINGS_GROUPS); a ?section=
// that names a group opens Advanced scrolled to it.

import { html } from '../ui/preact.js';
import { SETTINGS_GROUPS, sectionFields } from '../lib/settings.js';
import { Notice, ErrorNote, SkeletonRows, Unavailable } from '../ui/components.js';
import { SettingsField } from './settings-fields.js';
import { AccountPanel } from './settings-account.js';
import { LibrarySyncPanel, ConfigFilePanel } from './settings-panels.js';

/** The DOM id of an Advanced group's block. */
export const groupDomId = (id) => `settings-group-${id}`;

/**
 * `form` carries the page state every field needs: valueOf, dirtyKeys,
 * errors, warnings, setValue, revert, showKeys.
 */
function FieldRows({ fields, form }) {
  return html`<div class="field-list">
    ${fields.map(
      (f) => html`<${SettingsField}
        key=${f.key}
        field=${f}
        value=${form.valueOf(f.key)}
        changed=${form.dirtyKeys.includes(f.key)}
        error=${form.errors[f.key]}
        warning=${form.warnings[f.key]}
        showKeys=${form.showKeys}
        onChange=${(v) => form.setValue(f.key, v)}
        onRevert=${() => form.revert(f.key)}
      />`,
    )}
  </div>`;
}

/** The device preference that shows every field's config key and default. */
function KeysToggle({ on, onChange }) {
  return html`<div class="field settings-keys-toggle" data-type="switch">
    <div class="field-text">
      <div class="field-label-row"><label class="field-label" for="settings-show-keys">Show config keys and defaults</label></div>
      <p class="field-help" id="settings-show-keys-help">Each setting’s name in configs/config.yaml and its default, on every section. Kept on this device.</p>
    </div>
    <div class="field-control">
      <input
        type="checkbox"
        role="switch"
        class="field-switch"
        id="settings-show-keys"
        checked=${on}
        aria-describedby="settings-show-keys-help"
        onChange=${(e) => onChange(e.currentTarget.checked)}
      />
    </div>
  </div>`;
}

/**
 * The body for `section`. `cfg` is the page's load state ({loaded, error,
 * base, persisted, reloadError}); `onRetry` loads again, `onReloaded` takes
 * a ConfigResponse.
 */
export function SectionBody({ section, cfg, form, dirty, onRetry, onReloaded, setShowKeys }) {
  const fields = sectionFields(section.id);
  let rows;
  if (cfg.error && !cfg.loaded)
    rows =
      cfg.error.status === 404
        ? html`<${Unavailable} what="Runtime configuration (GET /api/v1/config)" />`
        : html`<${ErrorNote} title="Couldn't load the settings" error=${cfg.error} onRetry=${onRetry} />`;
  else if (!cfg.loaded) rows = html`<${SkeletonRows} n=${section.id === 'account' ? 1 : 6} kind="line" />`;

  if (section.id === 'account') {
    return html`<${AccountPanel} />
      <section class="account-block" aria-label="Media user token">${rows || html`<${FieldRows} fields=${fields} form=${form} />`}</section>`;
  }
  if (rows) return rows;

  if (section.id === 'advanced') {
    return SETTINGS_GROUPS.map((g) => {
      const titleId = `${groupDomId(g.id)}-title`;
      return html`<section key=${g.id} class="settings-group" id=${groupDomId(g.id)} data-group=${g.id} aria-labelledby=${titleId}>
        <h3 class="section-title settings-group-title" id=${titleId}>${g.label}</h3>
        ${g.intro ? html`<p class="settings-group-intro">${g.intro}</p>` : null}
        ${g.id === 'config_file' ? html`<div class="field-list"><${KeysToggle} on=${form.showKeys} onChange=${setShowKeys} /></div>` : null}
        ${fields.some((f) => f.group === g.id) ? html`<${FieldRows} fields=${fields.filter((f) => f.group === g.id)} form=${form} />` : null}
        ${g.id === 'library_sync'
          ? html`<${LibrarySyncPanel} enabled=${cfg.base['library_sync.enabled'] === true} interval=${cfg.base['library_sync.interval_minutes']} tokenSet=${Boolean(cfg.base['catalog.media_user_token'])} />`
          : null}
        ${g.id === 'config_file'
          ? html`<${ConfigFilePanel} persisted=${cfg.persisted} reloadError=${cfg.reloadError} dirty=${dirty} base=${cfg.base} onReloaded=${onReloaded} />`
          : null}
      </section>`;
    });
  }

  return html`
    ${section.id === 'files'
      ? html`<${Notice} role="warning" icon="triangle-alert">
          Changing a folder or a template changes where new downloads are saved. Tracks already saved under the old names are
          not recognized as downloaded, so they download again.
        </${Notice}>`
      : null}
    <${FieldRows} fields=${fields} form=${form} />`;
}
