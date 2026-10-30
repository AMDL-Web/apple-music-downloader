// Read-only rows of a Settings section: values fixed at startup
// (lib/facts.js FIXED_ROWS, read from GET /system) and a few more facts the
// section shows, laid out on the field grid so values end where controls
// end. A chip says what fixes the value; facts never enter the draft.

import { html } from '../ui/preact.js';
import { factRows, chipOf } from '../lib/facts.js';
import { Icon } from '../ui/components.js';

function Chip({ chip }) {
  return html`<span class="fact-chip" title=${chip.title}>${chip.icon ? html`<${Icon} name=${chip.icon} size="sm" />` : null}<span>${chip.label}</span></span>`;
}

/**
 * The fact rows of `section` from `system` (the /system body), then `extra`
 * rows ({id, label, value: {text, mono?, role?}, title?, action?}). Rows
 * whose value is unknown are left out; with none, nothing renders. While
 * `pending`, the rows are drawn with empty values.
 */
export function FactRows({ section, system, pending, showKeys, extra = [] }) {
  const rows = [...factRows(section, system, pending).map((r) => ({ ...r, chipView: chipOf(r) })), ...extra];
  if (rows.length === 0) return null;
  return html`<dl class="fact-list" data-domain=${section}>
    ${rows.map(
      (r) => html`<div key=${r.id} class="fact-row" data-fact=${r.id} data-role=${r.value.role || undefined}>
        <dt class="fact-row-text">
          <span class="fact-row-label">${r.label}</span>
          ${showKeys && r.configKey ? html`<span class="field-key mono">${r.configKey}</span>` : null}
        </dt>
        <dd class="fact-row-value">
          ${r.value.role === 'danger' || r.value.role === 'warning' ? html`<${Icon} name=${r.value.role === 'danger' ? 'circle-x' : 'triangle-alert'} size="sm" />` : null}
          ${r.value.text !== '' ? html`<span class=${r.value.mono ? 'fact-row-text-value mono wrap-any' : 'fact-row-text-value'} title=${r.title || undefined}>${r.value.text}</span>` : null}
          ${r.action || null}
          ${r.chipView ? html`<${Chip} chip=${r.chipView} />` : null}
        </dd>
      </div>`,
    )}
  </dl>`;
}
