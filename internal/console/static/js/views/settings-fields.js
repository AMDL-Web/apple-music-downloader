// One Settings field (label, help, the config key, a control by type, and
// its error and warning lines) and the controls themselves. The
// field list in lib/settings.js decides which control a key gets.

import { html, useState, useRef, useLayoutEffect } from '../ui/preact.js';
import { parseNumberInput } from '../lib/settings-draft.js';
import { Icon, Button } from '../ui/components.js';

/** A DOM id for a dotted config key. */
export const fieldId = (key) => `set-${key.replace(/[^a-z0-9]+/gi, '-')}`;

// Types whose control takes the row's full width under the text.
const WIDE = new Set(['list', 'path', 'template', 'secret']);
// Types labelled by a group (role=group + aria-labelledby) rather than <label for>.
const GROUPED = new Set(['list', 'set']);

/**
 * .field[data-key][data-type][data-wide][data-changed][data-invalid]
 * > .field-text (.field-label, .field-changed + revert, .field-help,
 * .field-key) + .field-control, then .field-error, .field-warning.
 */
export function SettingsField({ field, value, changed, error, warning, onChange, onRevert }) {
  const id = fieldId(field.key);
  const described = [`${id}-help`, field.defaultValue ? `${id}-default` : '', error ? `${id}-error` : '', warning && !error ? `${id}-warn` : ''].filter(Boolean).join(' ');
  const ctl = { field, id, value, invalid: Boolean(error), described, onChange };
  const grouped = GROUPED.has(field.type);
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
      </div>
      <p class="field-help" id=${`${id}-help`}>${field.help}</p>
      ${field.defaultValue
        ? html`<p class="field-default" id=${`${id}-default`}>Default: <code class="field-default-value">${field.defaultValue}</code>${field.defaultNote ? html`. ${field.defaultNote}` : null}</p>`
        : null}
      <code class="field-key">${field.key}</code>
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
    case 'secret':
      return html`<${SecretControl} ...${p} />`;
    case 'list':
      return html`<${ListEditor} ...${p} />`;
    case 'set':
      return html`<${SetControl} ...${p} />`;
    default:
      return html`<${TextControl} ...${p} />`;
  }
}

function SwitchControl({ id, value, disabled, described, onChange }) {
  const v = value === true;
  return html`<span class="field-switch-wrap">
    <input
      type="checkbox"
      role="switch"
      class="field-switch"
      id=${id}
      checked=${v}
      disabled=${disabled}
      aria-describedby=${described}
      onChange=${(e) => onChange(e.currentTarget.checked)}
    />
    <span class="field-switch-state" aria-hidden="true">${v ? 'On' : 'Off'}</span>
  </span>`;
}

function SelectControl({ field, id, value, disabled, invalid, described, onChange }) {
  const known = field.options.some((o) => o.value === value);
  const options = known || value === undefined ? field.options : [...field.options, { value, label: `${value} (current, not a listed value)` }];
  const picked = options.find((o) => o.value === value);
  return html`<div class="field-select">
    <select
      class="select"
      id=${id}
      disabled=${disabled}
      aria-invalid=${invalid ? 'true' : undefined}
      aria-describedby=${described}
      value=${String(value)}
      onChange=${(e) => {
        const o = options.find((x) => String(x.value) === e.currentTarget.value);
        onChange(o ? o.value : e.currentTarget.value);
      }}
    >
      ${options.map((o) => html`<option key=${String(o.value)} value=${String(o.value)}>${o.label}</option>`)}
    </select>
    ${picked && picked.hint ? html`<p class="field-option-hint">${picked.hint}</p>` : null}
  </div>`;
}

function NumberControl({ field, id, value, disabled, invalid, described, onChange }) {
  return html`<span class="field-number">
    <input
      type="text"
      inputmode="numeric"
      class="field-input num"
      id=${id}
      value=${value === undefined ? '' : String(value)}
      disabled=${disabled}
      autocomplete="off"
      spellcheck="false"
      aria-invalid=${invalid ? 'true' : undefined}
      aria-describedby=${described}
      onInput=${(e) => onChange(parseNumberInput(e.currentTarget.value))}
    />
    ${field.unit ? html`<span class="field-unit" aria-hidden="true">${field.unit}</span>` : null}
  </span>`;
}

function TextControl({ field, id, value, disabled, invalid, described, onChange }) {
  const mono = field.type === 'path' || field.type === 'template';
  return html`<input
    type="text"
    class=${mono ? 'field-input mono' : 'field-input'}
    id=${id}
    value=${value === undefined ? '' : String(value)}
    title=${disabled && value ? String(value) : undefined}
    disabled=${disabled}
    autocomplete="off"
    autocapitalize="off"
    spellcheck="false"
    aria-invalid=${invalid ? 'true' : undefined}
    aria-describedby=${described}
    onInput=${(e) => onChange(e.currentTarget.value)}
  />`;
}

function SecretControl({ id, value, disabled, invalid, described, onChange }) {
  const [shown, setShown] = useState(false);
  return html`<span class="field-secret" data-shown=${shown ? '' : undefined}>
    <input
      type=${shown ? 'text' : 'password'}
      class="field-input mono"
      id=${id}
      value=${value || ''}
      placeholder="Not set"
      disabled=${disabled}
      autocomplete="off"
      autocapitalize="off"
      spellcheck="false"
      aria-invalid=${invalid ? 'true' : undefined}
      aria-describedby=${described}
      onInput=${(e) => onChange(e.currentTarget.value)}
    />
    <${Button}
      variant="ghost"
      icon=${shown ? 'eye-off' : 'eye'}
      className="field-secret-toggle"
      aria-label=${shown ? 'Hide the token' : 'Show the token'}
      aria-pressed=${String(shown)}
      aria-controls=${id}
      onClick=${() => setShown((s) => !s)}
    />
  </span>`;
}

/**
 * An ordered list of option values: move up, move down, remove (never below
 * minItems), and add one of the options not yet listed. Moves are announced
 * in the editor's own polite region, and focus follows the moved row.
 */
function ListEditor({ field, id, value, disabled, described, onChange }) {
  const items = Array.isArray(value) ? value : [];
  const [said, setSaid] = useState('');
  const [adding, setAdding] = useState('');
  const refocus = useRef(null);
  const root = useRef(null);
  // A layout effect: a passive one from an earlier render could run first and
  // focus the row before it moved, and the move then drops focus on the body.
  useLayoutEffect(() => {
    const want = refocus.current;
    refocus.current = null;
    if (!want || !root.current) return;
    for (const sel of want) {
      const el = root.current.querySelector(sel);
      if (el && !el.disabled) {
        el.focus();
        return;
      }
    }
  });
  const label = (v) => (field.options.find((o) => o.value === v) || { label: v }).label;
  const remaining = field.options.filter((o) => !items.includes(o.value));
  const min = field.minItems || 0;
  const move = (i, d) => {
    const next = [...items];
    const [x] = next.splice(i, 1);
    next.splice(i + d, 0, x);
    onChange(next);
    // Keep focus on the moved row: the same arrow, or the other one at an end.
    refocus.current = [`[data-item="${x}"] .${d < 0 ? 'list-up' : 'list-down'}`, `[data-item="${x}"] .${d < 0 ? 'list-down' : 'list-up'}`];
    setSaid(`${label(x)} moved to position ${i + d + 1} of ${next.length}.`);
  };
  const remove = (i) => {
    const x = items[i];
    const next = items.filter((_, j) => j !== i);
    onChange(next);
    const neighbour = next[Math.min(i, next.length - 1)];
    refocus.current = [neighbour ? `[data-item="${neighbour}"] .list-remove` : '', '.list-add select', '.list-item .list-up'].filter(Boolean);
    setSaid(`${label(x)} removed.`);
  };
  const add = () => {
    if (!adding) return;
    onChange([...items, adding]);
    setSaid(`${label(adding)} added at position ${items.length + 1}.`);
    setAdding('');
  };
  return html`<div class="list-editor" role="group" aria-labelledby=${`${id}-label`} aria-describedby=${described} ref=${root}>
    <ol class="list-items">
      ${items.map(
        (v, i) => html`<li key=${v} class="list-item" data-item=${v}>
          <span class="list-rank num" aria-hidden="true">${i + 1}</span>
          <span class="list-label">${label(v)}</span>
          <code class="list-code">${v}</code>
          <span class="list-actions">
            <${Button} variant="ghost" size="sm" icon="chevron-up" className="list-up" aria-label=${`Move ${label(v)} up`} disabled=${disabled || i === 0} onClick=${() => move(i, -1)} />
            <${Button} variant="ghost" size="sm" icon="chevron-down" className="list-down" aria-label=${`Move ${label(v)} down`} disabled=${disabled || i === items.length - 1} onClick=${() => move(i, 1)} />
            <${Button} variant="ghost" size="sm" icon="x" className="list-remove" aria-label=${`Remove ${label(v)}`} disabled=${disabled || items.length <= min} aria-describedby=${items.length <= min && field.minItemsNote ? `${id}-min` : undefined} onClick=${() => remove(i)} />
          </span>
        </li>`,
      )}
    </ol>
    ${remaining.length && !disabled
      ? html`<div class="list-add">
          <select class="select" aria-label=${`Add to ${field.label}`} value=${adding} onChange=${(e) => setAdding(e.currentTarget.value)}>
            <option value="">Choose one to add…</option>
            ${remaining.map((o) => html`<option key=${o.value} value=${o.value}>${o.label}</option>`)}
          </select>
          <${Button} size="sm" icon="plus" disabled=${!adding} onClick=${add}>Add</${Button}>
        </div>`
      : null}
    ${field.minItemsNote && items.length <= min && !disabled ? html`<p class="list-min-note" id=${`${id}-min`}>${field.minItemsNote}</p>` : null}
    <span class="sr-only" role="status" aria-live="polite">${said}</span>
  </div>`;
}

/** An unordered subset of the options as checkboxes; keeps the current order. */
function SetControl({ field, id, value, disabled, described, onChange }) {
  const items = Array.isArray(value) ? value : [];
  return html`<div class="check-group" role="group" aria-labelledby=${`${id}-label`} aria-describedby=${described}>
    ${field.options.map(
      (o) => html`<label key=${o.value} class="check">
        <input
          type="checkbox"
          checked=${items.includes(o.value)}
          disabled=${disabled}
          onChange=${(e) => onChange(e.currentTarget.checked ? [...items.filter((v) => v !== o.value), o.value] : items.filter((v) => v !== o.value))}
        />
        <span>${o.label}</span>
      </label>`,
    )}
  </div>`;
}
