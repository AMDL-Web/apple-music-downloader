import test from 'node:test';
import assert from 'node:assert/strict';
import { runLimited } from '../static/js/lib/bulk.js';

const tick = (ms) => new Promise((r) => setTimeout(r, ms));

test('runLimited never has more than n calls in flight', async () => {
  let live = 0;
  let peak = 0;
  const items = Array.from({ length: 12 }, (_, i) => i);
  const out = await runLimited(items, 4, async (i) => {
    live += 1;
    peak = Math.max(peak, live);
    await tick(i % 3);
    live -= 1;
    return i * 2;
  });
  assert.equal(peak, 4);
  assert.deepEqual(
    out.map((o) => o.value),
    items.map((i) => i * 2),
  );
  assert.ok(out.every((o) => o.ok));
});

test('runLimited keeps input order and goes on after a failure', async () => {
  const boom = new Error('boom');
  const out = await runLimited(['a', 'b', 'c', 'd'], 2, async (x) => {
    await tick(x === 'a' ? 5 : 0);
    if (x === 'b') throw boom;
    if (x === 'c') return Promise.reject(new Error('rejected'));
    return x.toUpperCase();
  });
  assert.deepEqual(
    out.map((o) => [o.item, o.ok]),
    [
      ['a', true],
      ['b', false],
      ['c', false],
      ['d', true],
    ],
  );
  assert.equal(out[0].value, 'A');
  assert.equal(out[1].error, boom);
  assert.equal(out[2].error.message, 'rejected');
});

test('runLimited catches a synchronous throw as that item’s outcome', async () => {
  const out = await runLimited([1, 2], 4, (x) => {
    if (x === 1) throw new Error('sync');
    return x;
  });
  assert.equal(out[0].ok, false);
  assert.equal(out[0].error.message, 'sync');
  assert.deepEqual(out[1], { item: 2, ok: true, value: 2 });
});

test('runLimited reports progress once per item and ignores a throwing reporter', async () => {
  const seen = [];
  const out = await runLimited([1, 2, 3], 2, async (x) => x, (done, total, outcome) => {
    seen.push([done, total, outcome.item]);
    throw new Error('reporter bug');
  });
  assert.deepEqual(
    seen.map(([d, t]) => [d, t]),
    [
      [1, 3],
      [2, 3],
      [3, 3],
    ],
  );
  assert.ok(out.every((o) => o.ok));
});

test('runLimited handles an empty list and a bad limit', async () => {
  assert.deepEqual(await runLimited([], 4, async () => 1), []);
  let live = 0;
  let peak = 0;
  await runLimited([1, 2, 3], 0, async () => {
    live += 1;
    peak = Math.max(peak, live);
    await tick(1);
    live -= 1;
  });
  assert.equal(peak, 1);
  const fewer = await runLimited([1, 2], 10, async (x) => x);
  assert.equal(fewer.length, 2);
});
