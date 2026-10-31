import test from 'node:test';
import assert from 'node:assert/strict';
import {
  slotIndex,
  collapse,
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

test('slotIndex picks the place whose slot centre is nearest', () => {
  const rows = [
    { top: 0, height: 44 },
    { top: 44, height: 44 },
    { top: 88, height: 44 },
  ];
  // Slot centres for a 44 px row: 22, 66, 110, 154; the borders lie half-way.
  assert.equal(slotIndex([], 44, 10), 0);
  assert.equal(slotIndex(rows, 44, 0), 0);
  assert.equal(slotIndex(rows, 44, 43), 0);
  assert.equal(slotIndex(rows, 44, 45), 1);
  assert.equal(slotIndex(rows, 44, 131), 2);
  assert.equal(slotIndex(rows, 44, 133), 3);
  assert.equal(slotIndex(rows, 44, 999), 3);
});

test('collapse lays the other rows out without the slot', () => {
  const drawn = [
    { top: 0, height: 44 },
    { top: 88, height: 44 },
  ];
  assert.deepEqual(collapse(drawn, { top: 44, height: 44 }), [
    { top: 0, height: 44 },
    { top: 44, height: 44 },
  ]);
  assert.equal(collapse(drawn, null), drawn);
});

// The other rows as drawn with the held row's slot at `slot`: rows of
// varying heights, the slot as tall as the held row. Returns them and the slot.
function drawn(heights, held, slot) {
  const others = heights.filter((_, i) => i !== held);
  const rows = [];
  let y = 0;
  let at = null;
  others.forEach((h, i) => {
    if (i === slot) {
      at = { top: y, height: heights[held] };
      y += heights[held];
    }
    rows.push({ top: y, height: h });
    y += h;
  });
  if (at === null) at = { top: y, height: heights[held] };
  return { rows, slot: at };
}

test('slotIndex does not depend on where the slot is drawn, whatever the row heights', () => {
  const heights = [44, 96, 44, 140, 60, 44];
  const total = heights.reduce((a, b) => a + b, 0);
  for (let held = 0; held < heights.length; held++) {
    for (let y = -20; y <= total + 20; y += 3) {
      const answers = new Set();
      for (let slot = 0; slot < heights.length; slot++) {
        const d = drawn(heights, held, slot);
        answers.add(slotIndex(collapse(d.rows, d.slot), heights[held], y));
      }
      assert.equal(answers.size, 1, `held ${held}, y ${y}: ${[...answers]}`);
    }
  }
});

test('slotIndex moves a held row on once it is nearer the next place', () => {
  // Five 68 px rows; the first is held and moved down: its centre at 34 + d.
  const heights = [68, 68, 68, 68, 68];
  const at = (centre) => {
    const d = drawn(heights, 0, 0);
    return slotIndex(collapse(d.rows, d.slot), 68, centre);
  };
  assert.equal(at(34), 0);
  assert.equal(at(34 + 33), 0);
  assert.equal(at(34 + 35), 1);
  assert.equal(at(34 + 3 * 68 - 30), 3);
  assert.equal(at(34 + 4 * 68), 4);
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
