import test from 'node:test';
import assert from 'node:assert/strict';
import { createStatusStore } from '../static/js/lib/status-store.js';

// A fake clock with timers, and fetchers that count their calls and answer
// when the test resolves them.
function rig({ every = { a: 30000, b: 30000 }, keys = ['a', 'b', 'cfg'] } = {}) {
  let t = 1000;
  let hidden = false;
  const timers = new Map();
  let nextId = 1;
  const calls = Object.fromEntries(keys.map((k) => [k, 0]));
  const pending = Object.fromEntries(keys.map((k) => [k, []]));
  const fetchers = Object.fromEntries(
    keys.map((k) => [
      k,
      () => {
        calls[k]++;
        return new Promise((resolve, reject) => pending[k].push({ resolve, reject }));
      },
    ]),
  );
  const store = createStatusStore({
    fetchers,
    every,
    now: () => t,
    setTimer: (fn, ms) => {
      const id = nextId++;
      timers.set(id, { fn, at: t + ms });
      return id;
    },
    clearTimer: (id) => timers.delete(id),
    visible: () => !hidden,
  });
  const flush = () => new Promise((r) => setImmediate(r));
  return {
    store,
    calls,
    async answer(key, value, i = 0) {
      pending[key].splice(i, 1)[0].resolve(value);
      await flush();
    },
    async fail(key, error) {
      pending[key].shift().reject(error);
      await flush();
    },
    pending: (key) => pending[key].length,
    async advance(ms) {
      const end = t + ms;
      for (;;) {
        const due = [...timers].filter(([, v]) => v.at <= end).sort((x, y) => x[1].at - y[1].at)[0];
        if (!due) break;
        t = due[1].at;
        timers.delete(due[0]);
        due[1].fn();
        await flush();
      }
      t = end;
    },
    hide: (v) => (hidden = v),
    timers: () => timers.size,
  };
}

test('a key polls while it has a subscriber and stops with the last one', async () => {
  const r = rig();
  const offA = r.store.subscribe(['a'], () => {});
  const offB = r.store.subscribe(['a'], () => {});
  assert.equal(r.calls.a, 1, 'two subscribers share one read');
  await r.answer('a', { n: 1 });
  await r.advance(30000);
  assert.equal(r.calls.a, 2);
  await r.answer('a', { n: 2 });
  offA();
  await r.advance(30000);
  assert.equal(r.calls.a, 3, 'one subscriber left keeps it polling');
  await r.answer('a', { n: 3 });
  offB();
  assert.equal(r.timers(), 0);
  await r.advance(120000);
  assert.equal(r.calls.a, 3, 'no subscriber, no reads');
  assert.equal(r.calls.b, 0, 'a key nobody subscribes to is never read');
});

test('a key is never read twice inside its interval', async () => {
  const r = rig();
  const off = r.store.subscribe(['a'], () => {});
  await r.answer('a', 1);
  await r.advance(10000);
  off();
  r.store.subscribe(['a'], () => {});
  assert.equal(r.calls.a, 1, 'a revisit within the interval paints the cache and reads nothing');
  assert.deepEqual(r.store.get('a'), { data: 1, error: undefined, at: 1000 });
  await r.advance(19999);
  assert.equal(r.calls.a, 1);
  await r.advance(1);
  assert.equal(r.calls.a, 2, 'the next read is one interval after the last began');
});

test('subscribers hear only their keys, and a failed read keeps the last data', async () => {
  const r = rig();
  let heardA = 0;
  let heardB = 0;
  r.store.subscribe(['a'], () => heardA++);
  r.store.subscribe(['b'], () => heardB++);
  await r.answer('a', 'one');
  assert.equal(heardA, 1);
  assert.equal(heardB, 0);
  await r.advance(30000);
  const err = Object.assign(new Error('down'), { status: 0 });
  await r.fail('a', err);
  assert.equal(r.store.get('a').data, 'one');
  assert.equal(r.store.get('a').error, err);
  assert.equal(r.store.get('b'), undefined, 'no answer yet');
});

test('prime stores an answer so a subscriber reads nothing, and overtakes a read in flight', async () => {
  const r = rig();
  r.store.prime('cfg', { v: 1 });
  r.store.subscribe(['cfg'], () => {});
  assert.equal(r.calls.cfg, 0);
  assert.deepEqual(r.store.get('cfg').data, { v: 1 });
  r.store.subscribe(['a'], () => {});
  r.store.prime('a', 'primed');
  await r.answer('a', 'older');
  assert.equal(r.store.get('a').data, 'primed', 'the older read is dropped');
});

test('a key without an interval reads once, then only after invalidate or an epoch', async () => {
  const r = rig();
  const off = r.store.subscribe(['cfg'], () => {});
  assert.equal(r.calls.cfg, 1);
  await r.answer('cfg', 1);
  await r.advance(600000);
  off();
  r.store.subscribe(['cfg'], () => {});
  assert.equal(r.calls.cfg, 1);
  r.store.invalidate(['cfg']);
  assert.equal(r.calls.cfg, 2);
  await r.answer('cfg', 2);
  r.store.bumpEpoch();
  assert.equal(r.calls.cfg, 3);
});

test('invalidate reads only the named keys, and an unsubscribed key on its next subscribe', async () => {
  const r = rig();
  r.store.subscribe(['a', 'b'], () => {});
  await r.answer('a', 1);
  await r.answer('b', 1);
  r.store.invalidate(['a']);
  assert.equal(r.calls.a, 2);
  assert.equal(r.calls.b, 1);
  r.store.invalidate(['cfg']);
  assert.equal(r.calls.cfg, 0, 'nobody shows cfg, so nothing reads it yet');
  r.store.subscribe(['cfg'], () => {});
  assert.equal(r.calls.cfg, 1);
});

test('an epoch rereads every subscribed key and drops the answers it overtook', async () => {
  const r = rig();
  r.store.subscribe(['a', 'b'], () => {});
  r.store.bumpEpoch();
  assert.equal(r.calls.a, 2);
  assert.equal(r.calls.b, 2);
  await r.answer('a', 'before the outage', 0);
  assert.equal(r.store.get('a'), undefined);
  await r.answer('a', 'after');
  assert.equal(r.store.get('a').data, 'after');
});

test('hidden tabs skip interval reads and focus catches up at most once per gap', async () => {
  const r = rig();
  r.store.subscribe(['a', 'cfg'], () => {});
  await r.answer('a', 1);
  await r.answer('cfg', 1);
  r.hide(true);
  await r.advance(90000);
  assert.equal(r.calls.a, 1, 'no reads while hidden');
  r.hide(false);
  r.store.focus();
  assert.equal(r.calls.a, 2);
  assert.equal(r.calls.cfg, 1, 'focus leaves keys without an interval alone');
  await r.answer('a', 2);
  await r.advance(1000);
  r.store.focus();
  assert.equal(r.calls.a, 2, 'a second focus within 5 s reads nothing');
});

test('an unknown key fails loudly', () => {
  const r = rig();
  assert.throws(() => r.store.subscribe(['nope'], () => {}), /unknown status key: nope/);
});

test('a read still in flight at its tick waits a full interval instead of spinning', async () => {
  const r = rig();
  r.store.subscribe(['a'], () => {});
  await r.advance(90000);
  assert.equal(r.calls.a, 1, 'no second read while the first hangs');
  await r.answer('a', 1);
  await r.advance(30000);
  assert.equal(r.calls.a, 2);
});
