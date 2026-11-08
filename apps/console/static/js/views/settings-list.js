// The ordered list editor of the Settings page (codec priority), also used by
// the New download Custom… sheet for the codec order.

import { html, useState, useRef, useLayoutEffect } from '../ui/preact.js';
import { Button, Icon } from '../ui/components.js';
import { useSortable } from '../ui/sortable.js';
import { splitUse, locate, placeItem, canonicalIndex, dropTarget, keyTarget, moveMessage } from '../lib/sortable.js';

// A label such as "Dolby Atmos (E-AC-3)" wraps at its spaces, never inside "E-AC-3".
const NB_HYPHEN = '‑';

/**
 * Two lists of option values: In use, ordered (the value), and Not used,
 * every other option in the options' order. A row is dragged within In use
 * to reorder it, between the lists to use it or not (it lands at its own
 * place in Not used), with ui/sortable.js; Alt+Up and Alt+Down move a
 * focused row one place, across the lists at the ends; and each row has a
 * button to the other list (Remove, Add). In use never drops below
 * `field.minItems`. Changes are announced in the editor's own polite
 * region, and focus stays on the moved row. Props: field ({label, options,
 * minItems, addNoun}), id (the group's labelling prefix: `${id}-label`),
 * value, described, onChange.
 */
export function ListEditor({ field, id, value, described, onChange }) {
  const [said, setSaid] = useState('');
  const refocus = useRef(null);
  const root = useRef(null);
  const committed = splitUse(value, field.options);
  const order = field.options.map((o) => o.value);
  const min = field.minItems || 0;
  const option = (v) => field.options.find((o) => o.value === v) || { label: v };
  const label = (v) => option(v).label;
  const locked = () => Boolean(root.current && root.current.closest('fieldset[disabled]'));
  // The one codec in use when it must stay: nowhere to drag it.
  const fixed = (v) => min > 0 && committed.used.length === 1 && committed.used[0] === v;

  const commit = (v, to, lists) => {
    const from = locate(committed, v);
    const next = lists || placeItem(committed, v, to.list, to.index);
    onChange(next.used);
    setSaid(moveMessage(label(v), from, to, next));
  };

  const sort = useSortable({
    container: root,
    lists: committed,
    canDrag: (v) => !locked() && !fixed(v),
    resolve: ({ id: v, list, index, current }) => dropTarget({ lists: committed, order, min, id: v, list, index, current }),
    onDrop: ({ id: v, to, lists, focused }) => {
      if (focused) refocus.current = v;
      commit(v, to, lists);
    },
  });

  // A layout effect: a passive one from an earlier render could run first and
  // focus the row before it moved, and the move then drops focus on the body.
  useLayoutEffect(() => {
    const want = refocus.current;
    refocus.current = null;
    if (!want || !root.current) return;
    const el = root.current.querySelector(`[data-sort-id="${CSS.escape(want)}"]`);
    if (el) el.focus({ preventScroll: true });
  });

  const go = (v, to) => {
    if (!to || locked()) return;
    sort.animate();
    refocus.current = v;
    commit(v, to);
  };
  const remove = (v) => {
    if (committed.used.length <= min) return;
    go(v, { list: 'unused', index: canonicalIndex(order, committed.unused, v) });
  };
  const add = (v) => go(v, { list: 'used', index: committed.used.length });
  const onKey = (e, v) => {
    if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    e.preventDefault();
    e.stopPropagation();
    if (sort.drag) return;
    go(v, keyTarget({ lists: committed, order, min, id: v, dir: e.key === 'ArrowUp' ? -1 : 1 }));
  };

  const { used, unused } = sort.lists;
  const noun = field.addNoun || 'item';
  const row = (v, i, list) => {
    const inUse = list === 'used';
    const name = label(v);
    const last = inUse && committed.used.length <= min && committed.used.includes(v);
    const where = inUse ? `${name}, ${i + 1} of ${used.length}` : `${name}, not used`;
    return html`<li
      key=${v}
      class="list-item"
      data-item=${v}
      title=${option(v).hint || v}
      data-fixed=${fixed(v) ? '' : undefined}
      tabindex="0"
      aria-label=${where}
      aria-roledescription="sortable item"
      aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
      onKeyDown=${(e) => onKey(e, v)}
      ...${sort.row(v)}
    >
      <span class="list-rank num" aria-hidden="true">${inUse ? i + 1 : ''}</span>
      <span class="list-label" aria-hidden="true">${name.replace(/-/g, NB_HYPHEN)}</span>
      ${inUse
        ? html`<${Button}
            variant="ghost"
            size="sm"
            icon="x"
            className="list-remove"
            aria-label=${`Stop using ${name}`}
            aria-disabled=${last ? 'true' : undefined}
            title=${last ? `Add another ${noun} first` : undefined}
            onClick=${() => remove(v)}
          />`
        : html`<${Button} variant="ghost" size="sm" icon="plus" className="list-add" aria-label=${`Use ${name}`} onClick=${() => add(v)} />`}
      <span class="list-grip" aria-hidden="true"><${Icon} name="grip" /></span>
    </li>`;
  };
  // While a row is held the empty area keeps the height it had when the drag
  // began, so the editor does not grow under the pointer.
  const empty = unused.length === 0 && committed.unused.length === 0;
  return html`<div class="list-editor" role="group" aria-labelledby=${`${id}-label`} aria-describedby=${described} data-sorting=${sort.drag ? '' : undefined} ref=${root}>
    <div class="list-block" data-sort-list="used">
      <span class="list-block-title" id=${`${id}-used`}>In use</span>
      <ol class="list-items" data-sort-box="used" aria-labelledby=${`${id}-used`}>
        ${used.map((v, i) => row(v, i, 'used'))}
      </ol>
    </div>
    <div class="list-block" data-sort-list="unused">
      <span class="list-block-title" id=${`${id}-unused`} data-sort-flip="unused-title">Not used</span>
      <ol class="list-items" data-sort-box="unused" aria-labelledby=${`${id}-unused`} data-empty=${empty ? '' : undefined}>
        ${unused.map((v, i) => row(v, i, 'unused'))}
        ${empty ? html`<li key="empty" class="list-empty" aria-hidden="true"></li>` : null}
      </ol>
    </div>
    <span class="sr-only" role="status" aria-live="polite">${said}</span>
  </div>`;
}
