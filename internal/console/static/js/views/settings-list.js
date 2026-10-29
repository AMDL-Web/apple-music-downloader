// The ordered list editor of the Settings page (codec priority), also used by
// the New download Custom… sheet.

import { html, useState, useRef, useLayoutEffect } from '../ui/preact.js';
import { Button } from '../ui/components.js';

/**
 * An ordered list of option values: move up, move down, remove (never below
 * minItems), and add one of the options not yet listed. Moves are announced
 * in the editor's own polite region, and focus follows the moved row. The
 * New download Custom… sheet reuses it for the codec order.
 */
export function ListEditor({ field, id, value, disabled, described, onChange }) {
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
