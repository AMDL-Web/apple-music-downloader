// One Settings field (label, help, on demand the config key and default, a
// control by type, and its error and warning lines) and the controls themselves. The
// field list in lib/settings.js decides which control a key gets.

import { html, useState } from '../ui/preact.js';
import { parseNumberInput, sameValue } from '../lib/settings-draft.js';
import { valueLabel } from '../lib/settings.js';
import { Icon } from '../ui/components.js';
import { ListEditor } from './settings-list.js';
import { TemplateControl } from './settings-template.js';
import { TokenControl } from './settings-token.js';

/** A DOM id for a dotted config key. */
export const fieldId = (key) => `set-${key.replace(/[^a-z0-9]+/gi, '-')}`;

// Types whose control takes the row's full width under the text.
const WIDE = new Set(['list', 'path', 'template', 'token']);
// Types labelled by a group (role=group + aria-labelledby) rather than <label for>.
const GROUPED = new Set(['list', 'set']);

/**
 * .field[data-key][data-type][data-wide][data-changed][data-invalid]
 * > .field-text (.field-label, .field-changed + Undo or .field-reset,
 * .field-help with a select's chosen .field-option-hint, then with `showKeys` .field-default and .field-key) +
 * .field-control, then .field-error, .field-warning. A saved value that
 * differs from the default offers "Reset to default", which is an edit like
 * any other (Undo takes it back).
 */
export function SettingsField({ field, value, changed, error, warning, showKeys, onChange, onRevert }) {
  const id = fieldId(field.key);
  const hasDefault = field.defaultValue !== undefined;
  const described = [`${id}-help`, showKeys && hasDefault ? `${id}-default` : '', error ? `${id}-error` : '', warning && !error ? `${id}-warn` : ''].filter(Boolean).join(' ');
  const ctl = { field, id, value, invalid: Boolean(error), described, onChange };
  const grouped = GROUPED.has(field.type);
  const resettable = !changed && hasDefault && value !== undefined && field.type !== 'token' && !sameValue(value, field.defaultValue);
  // The chosen option's note joins the help line, so the control column stays a bare control.
  const picked = field.type === 'select' ? field.options.find((o) => o.value === value) : null;
  return html`<div
    class="field"
    data-key=${field.key}
    data-type=${field.type}
    data-wide=${WIDE.has(field.type) ? '' : undefined}
    data-changed=${changed ? '' : undefined}
    data-invalid=${error ? '' : undefined}
  >
    <div class="field-text">
      <div class="field-label-row">
        ${grouped
          ? html`<span class="field-label" id=${`${id}-label`}>${field.label}</span>`
          : html`<label class="field-label" id=${`${id}-label`} for=${id}>${field.label}</label>`}
        ${changed
          ? html`<span class="field-changed">Unsaved</span>
              <button type="button" class="field-revert link" onClick=${onRevert} aria-label=${`Undo the change to ${field.label}`}>Undo</button>`
          : null}
        ${resettable
          ? html`<button
              type="button"
              class="field-reset link"
              title=${`Default: ${valueLabel(field, field.defaultValue)}`}
              aria-label=${`Reset ${field.label} to its default, ${valueLabel(field, field.defaultValue)}`}
              onClick=${() => onChange(Array.isArray(field.defaultValue) ? [...field.defaultValue] : field.defaultValue)}
            >Reset to default</button>`
          : null}
      </div>
      <p class="field-help" id=${`${id}-help`}>${field.help}${picked && picked.hint ? html` <span class="field-option-hint">${picked.hint}</span>` : null}</p>
      ${showKeys && hasDefault
        ? html`<p class="field-default" id=${`${id}-default`}>Default: <code class="field-default-value">${valueLabel(field, field.defaultValue)}</code>${field.defaultNote ? html`. ${field.defaultNote}` : null}</p>`
        : null}
      ${showKeys ? html`<code class="field-key">${field.key}</code>` : null}
    </div>
    <div class="field-control">${control(ctl)}</div>
    ${error ? html`<p class="field-error" id=${`${id}-error`} data-role="danger"><${Icon} name="circle-x" size="sm" /><span>${error}</span></p>` : null}
    ${warning && !error ? html`<p class="field-warning" id=${`${id}-warn`} data-role="warning"><${Icon} name="triangle-alert" size="sm" /><span>${warning}</span></p>` : null}
  </div>`;
}

function control(p) {
  switch (p.field.type) {
    case 'switch':
      return html`<${SwitchControl} ...${p} />`;
    case 'select':
      return html`<${SelectControl} ...${p} />`;
    case 'number':
      return html`<${NumberControl} ...${p} />`;
    case 'template':
      return html`<${TemplateControl} ...${p} />`;
    case 'token':
      return html`<${TokenControl} ...${p} />`;
    case 'list':
      return html`<${ListEditor} ...${p} />`;
    case 'set':
      return html`<${SetControl} ...${p} />`;
    default:
      return html`<${TextControl} ...${p} />`;
  }
}

function SwitchControl({ id, value, described, onChange }) {
  return html`<input
    type="checkbox"
    role="switch"
    class="field-switch"
    id=${id}
    checked=${value === true}
    aria-describedby=${described}
    onChange=${(e) => onChange(e.currentTarget.checked)}
  />`;
}

const CUSTOM = '__custom__';

/**
 * A select of the field's options. A `custom` field (cover size) adds
 * "Custom…", which reveals a text box for any other value; a saved value
 * that is not listed opens it.
 */
function SelectControl({ field, id, value, invalid, described, onChange }) {
  const known = field.options.some((o) => o.value === value);
  const [custom, setCustom] = useState(false);
  const typing = field.custom && (custom || (!known && value !== undefined));
  const options = known || value === undefined || field.custom ? field.options : [...field.options, { value, label: `${value} (current, not a listed value)` }];
  return html`<div class="field-select">
    <select
      class="select"
      id=${id}
      aria-invalid=${invalid && !typing ? 'true' : undefined}
      aria-describedby=${described}
      value=${typing ? CUSTOM : String(value)}
      onChange=${(e) => {
        const v = e.currentTarget.value;
        if (v === CUSTOM) {
          setCustom(true);
          return;
        }
        setCustom(false);
        const o = options.find((x) => String(x.value) === v);
        onChange(o ? o.value : v);
      }}
    >
      ${options.map((o) => html`<option key=${String(o.value)} value=${String(o.value)}>${o.label}</option>`)}
      ${field.custom ? html`<option value=${CUSTOM}>Custom…</option>` : null}
    </select>
    ${typing
      ? html`<input
          type="text"
          class="field-input field-custom"
          value=${value === undefined ? '' : String(value)}
          placeholder=${field.customPlaceholder || ''}
          aria-label=${`${field.label}, custom value`}
          aria-invalid=${invalid ? 'true' : undefined}
          aria-describedby=${described}
          autocomplete="off"
          spellcheck="false"
          onInput=${(e) => onChange(e.currentTarget.value)}
        />`
      : null}
  </div>`;
}

/** A whole number; the unit sits inside the box, so every control ends on one edge. */
function NumberControl({ field, id, value, invalid, described, onChange }) {
  return html`<span class="field-number" data-unit=${field.unit ? '' : undefined}>
    <input
      type="text"
      inputmode="numeric"
      class="field-input num"
      id=${id}
      value=${value === undefined ? '' : String(value)}
      autocomplete="off"
      spellcheck="false"
      aria-invalid=${invalid ? 'true' : undefined}
      aria-describedby=${described}
      onInput=${(e) => onChange(parseNumberInput(e.currentTarget.value))}
    />
    ${field.unit ? html`<span class="field-unit" aria-hidden="true">${field.unit}</span>` : null}
  </span>`;
}

function TextControl({ field, id, value, invalid, described, onChange }) {
  const mono = field.type === 'path' || field.type === 'template';
  return html`<input
    type="text"
    class=${mono ? 'field-input mono' : 'field-input'}
    id=${id}
    value=${value === undefined ? '' : String(value)}
    autocomplete="off"
    autocapitalize="off"
    spellcheck="false"
    aria-invalid=${invalid ? 'true' : undefined}
    aria-describedby=${described}
    onInput=${(e) => onChange(e.currentTarget.value)}
  />`;
}

/** An unordered subset of the options as checkboxes; keeps the current order. */
function SetControl({ field, id, value, described, onChange }) {
  const items = Array.isArray(value) ? value : [];
  return html`<div class="check-group" role="group" aria-labelledby=${`${id}-label`} aria-describedby=${described}>
    ${field.options.map(
      (o) => html`<label key=${o.value} class="check">
        <input
          type="checkbox"
          checked=${items.includes(o.value)}
          onChange=${(e) => onChange(e.currentTarget.checked ? [...items.filter((v) => v !== o.value), o.value] : items.filter((v) => v !== o.value))}
        />
        <span>${o.label}</span>
      </label>`,
    )}
  </div>`;
}
