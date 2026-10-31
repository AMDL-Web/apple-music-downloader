// The index math of drag-to-reorder (ui/sortable.js): where a pointer drops
// an item among the rows of one or more lists, and the rules of the codec
// editor's two lists, In use and Not used (views/settings-list.js). Lists are
// plain objects of id arrays, `{ used: ['alac', 'ec3'], unused: ['aac'] }`;
// every function returns new arrays and leaves its input alone.

/**
 * The insertion index of a dragged row among the other rows of a list: how
 * many of their midpoints lie above `y`. The midpoints are layout positions
 * with the dragged row's slot where it is drawn, so after a swap the pointer
 * has crossed exactly one midpoint and the answer does not flip back.
 */
export function insertionIndex(mids, y) {
  let n = 0;
  for (const m of mids) if (m < y) n++;
  return n;
}

/**
 * The list `y` is over: blocks [{ id, top, bottom }] in layout order; the
 * one that holds `y`, else the one with the nearest edge. Null with none.
 */
export function listAt(blocks, y) {
  let best = null;
  let gap = Infinity;
  for (const b of blocks) {
    if (y >= b.top && y <= b.bottom) return b.id;
    const d = y < b.top ? b.top - y : y - b.bottom;
    if (d < gap) {
      gap = d;
      best = b.id;
    }
  }
  return best;
}

/** Where `id` sits in `lists`: { list, index }, or null. */
export function locate(lists, id) {
  for (const [list, ids] of Object.entries(lists)) {
    const index = ids.indexOf(id);
    if (index >= 0) return { list, index };
  }
  return null;
}

/** `lists` with `id` taken out of its list and put in `list` at `index` (clamped). */
export function placeItem(lists, id, list, index) {
  const out = {};
  for (const [name, ids] of Object.entries(lists)) out[name] = ids.filter((x) => x !== id);
  const into = out[list] || [];
  const at = Math.max(0, Math.min(index, into.length));
  out[list] = [...into.slice(0, at), id, ...into.slice(at)];
  return out;
}

/** Whether two places ({ list, index }) are the same. */
export const samePlace = (a, b) => Boolean(a && b && a.list === b.list && a.index === b.index);

/**
 * The codec editor's two lists from a field value and the field's options:
 * `used` is the value as ordered, `unused` every other option in the
 * options' order. Values the options do not name stay in `used`.
 */
export function splitUse(value, options) {
  const used = Array.isArray(value) ? value.slice() : [];
  const unused = options.map((o) => o.value).filter((v) => !used.includes(v));
  return { used, unused };
}

/**
 * Where `id` belongs in Not used: the number of other unused ids that come
 * before it in `order` (the options' order), whatever the pointer says.
 */
export function canonicalIndex(order, unused, id) {
  const rank = (v) => {
    const i = order.indexOf(v);
    return i < 0 ? order.length : i;
  };
  const mine = rank(id);
  return unused.filter((v) => v !== id && rank(v) < mine).length;
}

/**
 * The place a drag over `list` at `index` resolves to under the codec
 * editor's rules: In use takes any position; Not used takes the item at its
 * canonical place; and the last `min` items of In use cannot leave, so a
 * drag of one of them over Not used keeps the place it has (`current`).
 * `lists` is the committed { used, unused }; `order` the options' values.
 */
export function dropTarget({ lists, order, min = 0, id, list, index, current }) {
  const from = locate(lists, id);
  if (list === 'unused') {
    if (from && from.list === 'used' && lists.used.length <= min) return current || from;
    return { list: 'unused', index: canonicalIndex(order, lists.unused, id) };
  }
  const others = lists.used.filter((x) => x !== id).length;
  return { list: 'used', index: Math.max(0, Math.min(index, others)) };
}

/**
 * The place an Alt+Arrow press moves `id` to, or null when it cannot move:
 * up or down one place in In use; down from the last In-use place to Not
 * used (not below `min`); up from Not used to the end of In use. A Not-used
 * item has no order of its own, so Alt+Down there does nothing.
 */
export function keyTarget({ lists, order, min = 0, id, dir }) {
  const from = locate(lists, id);
  if (!from) return null;
  if (from.list === 'unused') return dir < 0 ? { list: 'used', index: lists.used.length } : null;
  const to = from.index + (dir < 0 ? -1 : 1);
  if (to < 0) return null;
  if (to < lists.used.length) return { list: 'used', index: to };
  if (lists.used.length <= min) return null;
  return { list: 'unused', index: canonicalIndex(order, lists.unused, id) };
}

/**
 * What the editor's live region says after `id` went from `from` to `to`
 * ({ list, index }), given the lists after the change and the item's label.
 */
export function moveMessage(label, from, to, after) {
  if (to.list === 'unused') return from && from.list === 'unused' ? '' : `${label} no longer used.`;
  if (!from || from.list === 'unused') return `${label} added at position ${to.index + 1}.`;
  if (from.index === to.index) return '';
  return `${label} moved to position ${to.index + 1} of ${after.used.length}.`;
}
