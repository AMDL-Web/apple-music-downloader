import test from 'node:test';
import assert from 'node:assert/strict';
import { followRoute, settle, pageReady } from '../static/js/lib/page-swap.js';

const r = (page, params = {}) => ({ page, params });

test('a page change keeps the page drawn until the next is ready', () => {
  const overview = r('overview');
  let s = followRoute(null, overview);
  assert.deepEqual(s, { shown: overview, pending: null });
  const queue = r('queue', { tab: 'all' });
  s = followRoute(s, queue);
  assert.equal(s.shown, overview, 'the old page stays drawn');
  assert.equal(s.pending, queue);
  assert.equal(followRoute(s, queue), s, 'the same route again changes nothing');
  s = settle(s);
  assert.deepEqual(s, { shown: queue, pending: null });
  const filtered = r('queue', { tab: 'done' });
  assert.deepEqual(followRoute(s, filtered), { shown: filtered, pending: null }, 'a route on the page shown is drawn at once');
});

test('the hold keeps the last page drawn, whatever pages pass meanwhile', () => {
  const a = r('overview');
  const b = r('queue');
  const c = r('logs');
  let s = followRoute(followRoute(null, a), b);
  s = followRoute(s, c);
  assert.equal(s.shown, a);
  assert.equal(s.pending, c, 'the page passed through is dropped');
  s = followRoute(s, a);
  assert.deepEqual(s, { shown: a, pending: null }, 'back on the page shown, nothing waits');
  assert.equal(settle(s), s, 'nothing to swap');
});

test('a page is ready once its root holds nothing back', () => {
  const root = (...attrs) => ({ hasAttribute: (n) => attrs.includes(n) });
  assert.ok(pageReady(null), 'a page without a root (the missing page) shows at once');
  assert.ok(pageReady(root()));
  assert.ok(!pageReady(root('data-held')));
  assert.ok(!pageReady(root('data-pending')));
});
