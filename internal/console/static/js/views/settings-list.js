// The ordered list editor of the Settings page (codec priority), also used by
// the New download Custom… sheet for the codec order.

import { html, useState, useRef, useLayoutEffect } from '../ui/preact.js';
import { Button } from '../ui/components.js';

// A label such as "Dolby Atmos (E-AC-3)" wraps at its spaces, never inside "E-AC-3".
const NB_HYPHEN = '\u2011';

/**
 * An ordered list of option values: move up, move down, remove (never below
 * `field.minItems`), and "+ <addLabel>…", a select whose choice is added at
 * once and lists only the options not yet listed, each with its hint. With
 * one item there is nothing to order, so rank and arrows hide. Changes are
 * announced in the editor's own polite region, and focus follows the row.
 * Props: field ({label, options, minItems, addLabel}), id (the group's
 * labelling prefix: `${id}-label`), value, described, onChange.
 */
export function ListEditor({ field, id, value, described, onChange }) {
  const items = Array.isArray(value) ? value : [];
  const [said, setSaid] = useState('');
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
  const ordered = items.length > 1;
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
    if (items.length <= min) return;
    const x = items[i];
    const next = items.filter((_, j) => j !== i);
    onChange(next);
    const neighbour = next[Math.min(i, next.length - 1)];
    refocus.current = [neighbour ? `[data-item="${neighbour}"] .list-remove` : '', '.list-add select'].filter(Boolean);
    setSaid(`${label(x)} removed.`);
  };
  const add = (v) => {
    if (!v) return;
    onChange([...items, v]);
    refocus.current = [`[data-item="${v}"] .list-up`, `[data-item="${v}"] .list-remove`];
    setSaid(`${label(v)} added at position ${items.length + 1}.`);
  };
  const addLabel = field.addLabel || 'Add';
  return html`<div class="list-editor" role="group" aria-labelledby=${`${id}-label`} aria-describedby=${described} data-ordered=${ordered ? '' : undefined} ref=${root}>
    <ol class="list-items">
      ${items.map((v, i) => {
        const last = items.length <= min;
        return html`<li key=${v} class="list-item" data-item=${v} title=${v}>
          ${ordered ? html`<span class="list-rank num" aria-hidden="true">${i + 1}</span>` : null}
          <span class="list-label">${label(v).replace(/-/g, NB_HYPHEN)}</span>
          <span class="list-actions">
            ${ordered
              ? html`<${Button} variant="ghost" size="sm" icon="chevron-up" className="list-up" aria-label=${`Move ${label(v)} up`} disabled=${i === 0} onClick=${() => move(i, -1)} />
                  <${Button} variant="ghost" size="sm" icon="chevron-down" className="list-down" aria-label=${`Move ${label(v)} down`} disabled=${i === items.length - 1} onClick=${() => move(i, 1)} />`
              : null}
            <${Button}
              variant="ghost"
              size="sm"
              icon="x"
              className="list-remove"
              aria-label=${`Remove ${label(v)}`}
              aria-disabled=${last ? 'true' : undefined}
              title=${last ? `Add another ${field.addNoun || 'item'} first` : undefined}
              onClick=${() => remove(i)}
            />
          </span>
        </li>`;
      })}
    </ol>
    ${remaining.length
      ? html`<div class="list-add">
          <select class="select" aria-label=${`${addLabel} to ${field.label}`} value="" onChange=${(e) => add(e.currentTarget.value)}>
            <option value="">+ ${addLabel}…</option>
            ${remaining.map((o) => html`<option key=${o.value} value=${o.value}>${o.hint ? `${o.label} — ${o.hint}` : o.label}</option>`)}
          </select>
        </div>`
      : null}
    <span class="sr-only" role="status" aria-live="polite">${said}</span>
  </div>`;
}
