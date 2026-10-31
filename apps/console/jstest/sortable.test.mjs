import test from 'node:test';
import assert from 'node:assert/strict';
import {
  insertionIndex,
  listAt,
  locate,
  placeItem,
  samePlace,
  splitUse,
  canonicalIndex,
  dropTarget,
  keyTarget,
  moveMessage,
} from '../static/js/lib/sortable.js';

const ORDER = ['alac', 'ec3', 'ac3', 'aac', 'aac-binaural', 'aac-downmix'];
const OPTIONS = ORDER.map((value) => ({ value, label: value.toUpperCase() }));

test('insertionIndex counts the midpoints above the pointer', () => {
  assert.equal(insertionIndex([], 10), 0);
  assert.equal(insertionIndex([22, 66, 110], 0), 0);
  assert.equal(insertionIndex([22, 66, 110], 50), 1);
  assert.equal(insertionIndex([22, 66, 110], 500), 3);
});

// The midpoints of the other rows as drawn with the dragged row's slot at
// `slot`: rows of varying heights, the slot as tall as the dragged row.
function drawnMids(heights, dragged, slot) {
  const others = heights.filter((_, i) => i !== dragged);
  const mids = [];
  let y = 0;
  others.forEach((h, i) => {
    if (i === slot) y += heights[dragged];
    mids.push(y + h / 2);
    y += h;
  });
  return mids;
}

test('insertionIndex does not flip back after a swap, whatever the row heights', () => {
  const heights = [44, 96, 44, 140, 60, 44];
  for (let dragged = 0; dragged < heights.length; dragged++) {
    const total = heights.reduce((a, b) => a + b, 0);
    for (let slot = 0; slot < heights.length; slot++) {
      for (let y = 0; y <= total; y += 3) {
        const next = insertionIndex(drawnMids(heights, dragged, slot), y);
        // Drawn again with the slot where the pointer sent it, the same pointer keeps it there.
        const again = insertionIndex(drawnMids(heights, dragged, next), y);
        assert.equal(again, next, `dragged ${dragged}, slot ${slot}, y ${y}`);
      }
    }
  }
});

test('listAt picks the block under the pointer, else the nearest edge', () => {
  const blocks = [
    { id: 'used', top: 0, bottom: 150 },
    { id: 'unused', top: 170, bottom: 300 },
  ];
  assert.equal(listAt(blocks, 10), 'used');
  assert.equal(listAt(blocks, 155), 'used');
  assert.equal(listAt(blocks, 165), 'unused');
  assert.equal(listAt(blocks, 400), 'unused');
  assert.equal(listAt(blocks, -40), 'used');
  assert.equal(listAt([], 10), null);
});

test('locate and placeItem move an id between lists without touching the input', () => {
  const lists = { used: ['alac', 'ec3'], unused: ['ac3', 'aac'] };
  assert.deepEqual(locate(lists, 'aac'), { list: 'unused', index: 1 });
  assert.equal(locate(lists, 'nope'), null);
  assert.deepEqual(placeItem(lists, 'aac', 'used', 1), { used: ['alac', 'aac', 'ec3'], unused: ['ac3'] });
  assert.deepEqual(placeItem(lists, 'alac', 'used', 1), { used: ['ec3', 'alac'], unused: ['ac3', 'aac'] });
  assert.deepEqual(placeItem(lists, 'alac', 'unused', 99), { used: ['ec3'], unused: ['ac3', 'aac', 'alac'] });
  assert.deepEqual(placeItem(lists, 'ec3', 'used', -3), { used: ['ec3', 'alac'], unused: ['ac3', 'aac'] });
  assert.deepEqual(lists, { used: ['alac', 'ec3'], unused: ['ac3', 'aac'] });
  assert.ok(samePlace({ list: 'used', index: 1 }, { list: 'used', index: 1 }));
  assert.ok(!samePlace({ list: 'used', index: 1 }, { list: 'unused', index: 1 }));
  assert.ok(!samePlace(null, { list: 'used', index: 1 }));
});

test('splitUse keeps the value order and lists the rest in the options order', () => {
  assert.deepEqual(splitUse(['aac', 'alac'], OPTIONS), { used: ['aac', 'alac'], unused: ['ec3', 'ac3', 'aac-binaural', 'aac-downmix'] });
  assert.deepEqual(splitUse(undefined, OPTIONS).unused, ORDER);
  assert.deepEqual(splitUse(ORDER, OPTIONS).unused, []);
});

test('canonicalIndex places an id among the unused by the options order', () => {
  assert.equal(canonicalIndex(ORDER, ['ac3', 'aac-downmix'], 'alac'), 0);
  assert.equal(canonicalIndex(ORDER, ['ac3', 'aac-downmix'], 'aac'), 1);
  assert.equal(canonicalIndex(ORDER, ['ac3', 'aac-downmix'], 'aac-downmix'), 1, 'its own entry does not count');
  assert.equal(canonicalIndex(ORDER, ['ac3'], 'unknown'), 1, 'an id the order lacks goes last');
});

test('dropTarget: In use takes any place, clamped to its length', () => {
  const lists = { used: ['alac', 'ec3'], unused: ['ac3', 'aac'] };
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'ec3', list: 'used', index: 0 }), { list: 'used', index: 0 });
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'ec3', list: 'used', index: 9 }), { list: 'used', index: 1 });
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'aac', list: 'used', index: 9 }), { list: 'used', index: 2 });
});

test('dropTarget: Not used puts the item at its canonical place, whatever the index', () => {
  const lists = { used: ['alac', 'aac'], unused: ['ec3', 'ac3', 'aac-downmix'] };
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'aac', list: 'unused', index: 0 }), { list: 'unused', index: 2 });
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'alac', list: 'unused', index: 3 }), { list: 'unused', index: 0 });
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'ac3', list: 'unused', index: 0 }), { list: 'unused', index: 1 }, 'a Not-used item keeps its place');
});

test('dropTarget: the last In-use item stays where it is drawn', () => {
  const lists = { used: ['alac'], unused: ['ec3', 'ac3'] };
  assert.deepEqual(dropTarget({ lists, order: ORDER, min: 1, id: 'alac', list: 'unused', index: 0 }), { list: 'used', index: 0 });
  const current = { list: 'used', index: 0 };
  assert.equal(dropTarget({ lists, order: ORDER, min: 1, id: 'alac', list: 'unused', index: 0, current }), current);
  const two = { used: ['alac', 'ec3'], unused: ['ac3'] };
  assert.deepEqual(dropTarget({ lists: two, order: ORDER, min: 1, id: 'alac', list: 'unused', index: 0 }), { list: 'unused', index: 0 });
  assert.deepEqual(dropTarget({ lists: two, order: ORDER, min: 2, id: 'alac', list: 'unused', index: 0 }), { list: 'used', index: 0 }, 'min is the field’s');
});

test('keyTarget moves one place, across the lists at the ends', () => {
  const lists = { used: ['alac', 'ec3', 'aac'], unused: ['ac3', 'aac-downmix'] };
  const k = (id, dir, l = lists, min = 1) => keyTarget({ lists: l, order: ORDER, min, id, dir });
  assert.equal(k('alac', -1), null, 'first In-use item up');
  assert.deepEqual(k('alac', 1), { list: 'used', index: 1 });
  assert.deepEqual(k('aac', -1), { list: 'used', index: 1 });
  assert.deepEqual(k('aac', 1), { list: 'unused', index: 1 }, 'last In-use item down: no longer used, at its canonical place');
  assert.deepEqual(k('ac3', -1), { list: 'used', index: 3 }, 'Not-used item up: end of In use');
  assert.equal(k('ac3', 1), null, 'Not used has no order of its own');
  assert.equal(k('alac', 1, { used: ['alac'], unused: ['ec3'] }), null, 'the last one in use stays');
  assert.equal(k('nope', 1), null);
});

test('moveMessage says what happened in the editor’s words', () => {
  const after = { used: ['ec3', 'alac', 'aac'], unused: [] };
  assert.equal(moveMessage('ALAC', { list: 'used', index: 0 }, { list: 'used', index: 1 }, after), 'ALAC moved to position 2 of 3.');
  assert.equal(moveMessage('AAC', { list: 'unused', index: 0 }, { list: 'used', index: 2 }, after), 'AAC added at position 3.');
  assert.equal(moveMessage('AAC', { list: 'used', index: 2 }, { list: 'unused', index: 0 }, after), 'AAC no longer used.');
  assert.equal(moveMessage('AAC', { list: 'used', index: 2 }, { list: 'used', index: 2 }, after), '');
  assert.equal(moveMessage('AC3', { list: 'unused', index: 0 }, { list: 'unused', index: 0 }, after), '');
});
