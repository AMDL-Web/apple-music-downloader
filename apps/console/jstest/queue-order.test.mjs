import test from 'node:test';
import assert from 'node:assert/strict';
import { orderable, upNext, arrange, placeQueued, moveBody, stepIndex, queuePlace } from '../static/js/lib/queue-order.js';

const at = (m) => `2026-11-01T10:${String(m).padStart(2, '0')}:00Z`;
const q = (id, order, m = 0) => ({ id, status: 'queued', queue_order: order, updated_at: at(m) });
const r = (id, started, m = 0) => ({ id, status: 'running', started_at: at(started), updated_at: at(m) });
const done = (id, status = 'completed') => ({ id, status, updated_at: at(0) });
const ids = (jobs) => jobs.map((j) => j.id);

test('orderable: Active with no search and no type', () => {
  assert.ok(!orderable({ tab: 'all', q: '', type: '' }), 'All keeps the newest-first order');
  assert.ok(orderable({ tab: 'active', q: '', type: '' }));
  assert.ok(!orderable({ tab: 'completed', q: '', type: '' }));
  assert.ok(!orderable({ tab: 'active', q: 'blonde', type: '' }));
  assert.ok(!orderable({ tab: 'active', q: '', type: 'album' }));
  assert.ok(!orderable(null));
});

test('upNext sorts queued jobs by queue_order, missing values last, ties kept', () => {
  const jobs = [q('c', 3000), done('x'), q('a', 1000), { id: 'n', status: 'queued' }, q('b', 1000)];
  assert.deepEqual(ids(upNext(jobs)), ['a', 'b', 'c', 'n']);
});

test('arrange: running by start, then the queue in run order, then the rest as listed', () => {
  const main = [q('q2', 2000), r('r2', 5), done('d1'), r('r1', 1), q('q1', 1000), done('d2', 'failed')];
  assert.deepEqual(ids(arrange(main)), ['r1', 'r2', 'q1', 'q2', 'd1', 'd2']);
});

test('arrange: the queue read on its own fills in jobs past the loaded pages, each once', () => {
  const main = [r('r1', 1), q('q2', 2000), done('d1')];
  const queue = [q('q1', 1000), q('q2', 2000), q('q3', 3000)];
  assert.deepEqual(ids(arrange(main, queue)), ['r1', 'q1', 'q2', 'q3', 'd1']);
});

test('arrange: the newer version of a job wins, so a start leaves the queue', () => {
  const main = [r('q1', 9, 9), done('d1')];
  const queue = [q('q1', 1000, 1), q('q2', 2000)];
  assert.deepEqual(ids(arrange(main, queue)), ['q1', 'q2', 'd1']);
  assert.equal(arrange(main, queue)[0].status, 'running');
});

test('arrange: a queued job that starts lands at the bottom of running, where the queue head was', () => {
  const before = arrange([r('r1', 1), q('q1', 1000), q('q2', 2000)]);
  const after = arrange([r('r1', 1), r('q1', 2, 5), q('q2', 2000)]);
  assert.deepEqual(ids(before), ids(after));
});

test('placeQueued gives the moved job a queue_order between its new neighbours', () => {
  const queue = [q('a', 1000), q('b', 2000), q('c', 3000)];
  assert.deepEqual(ids(upNext(placeQueued(queue, 'c', 0))), ['c', 'a', 'b']);
  assert.deepEqual(ids(upNext(placeQueued(queue, 'a', 1))), ['b', 'a', 'c']);
  assert.deepEqual(ids(upNext(placeQueued(queue, 'a', 9))), ['b', 'c', 'a']);
  assert.equal(placeQueued(queue, 'a', 1).find((j) => j.id === 'a').queue_order, 2500);
  assert.equal(placeQueued(queue, 'nope', 0), queue);
  assert.equal(queue[0].queue_order, 1000, 'input untouched');
});

test('moveBody: to first at the top, else after the job above', () => {
  assert.deepEqual(moveBody(['c', 'a', 'b'], 'c'), { to: 'first' });
  assert.deepEqual(moveBody(['a', 'c', 'b'], 'c'), { after: 'a' });
  assert.deepEqual(moveBody(['a', 'b', 'c'], 'c'), { after: 'b' });
  assert.equal(moveBody(['a'], 'x'), null);
});

test('stepIndex and queuePlace', () => {
  assert.equal(stepIndex(['a', 'b', 'c'], 'a', -1), null);
  assert.equal(stepIndex(['a', 'b', 'c'], 'a', 1), 1);
  assert.equal(stepIndex(['a', 'b', 'c'], 'c', 1), null);
  assert.equal(stepIndex(['a', 'b', 'c'], 'x', 1), null);
  assert.deepEqual(queuePlace(['a', 'b'], 'a'), { first: true, last: false });
  assert.deepEqual(queuePlace(['a'], 'a'), { first: true, last: true });
  assert.deepEqual(queuePlace(['a'], 'x'), {});
});
