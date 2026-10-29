import test from 'node:test';
import assert from 'node:assert/strict';
import { endsWatch, addEnded, finishedToast } from '../static/js/lib/watch.js';

const job = (id, status, extra = {}) => ({ id, status, type: 'album', title: `Album ${id}`, ...extra });

test('only a terminal status ends a watch', () => {
  assert.equal(endsWatch(job('a', 'running')), false);
  assert.equal(endsWatch(job('a', 'queued')), false);
  for (const s of ['completed', 'failed', 'cancelled']) assert.equal(endsWatch(job('a', s)), true, s);
  assert.equal(endsWatch(null), false);
});

test('one ended job names itself and opens its drawer over a page that has one', () => {
  const t = finishedToast([job('j1', 'completed', { title: 'folklore' })], { page: 'new', params: { url: 'https://music.apple.com/us/album/x/1' } });
  assert.equal(t.message, 'Album folklore finished');
  assert.equal(t.role, 'success');
  assert.equal(t.action.label, 'Open');
  assert.match(t.action.hash, /^#\/new\/j1\?url=/, 'the drawer opens over #/new and keeps its links');
  const elsewhere = finishedToast([job('j1', 'failed')], { page: 'logs', params: { level: 'error' } });
  assert.equal(elsewhere.role, 'danger');
  assert.equal(elsewhere.action.hash, '#/queue/j1', 'a page without a drawer opens the Queue drawer');
  assert.match(elsewhere.message, / failed$/);
});

test('a burst becomes one toast with counts and a Queue link', () => {
  const all = finishedToast([job('a', 'completed'), job('b', 'completed'), job('c', 'completed')]);
  assert.equal(all.message, '3 downloads finished');
  assert.equal(all.role, 'success');
  assert.deepEqual(all.action, { label: 'Queue', hash: '#/queue?tab=all' });
  const mixed = finishedToast([job('a', 'completed'), job('b', 'failed'), job('c', 'cancelled')]);
  assert.equal(mixed.message, '3 downloads ended: 1 completed, 1 failed, 1 cancelled');
  assert.equal(mixed.role, 'warning');
  const failed = finishedToast([job('a', 'failed'), job('b', 'failed')]);
  assert.equal(failed.role, 'danger');
  assert.equal(failed.action.hash, '#/queue?tab=failed');
});

test('a job that ends twice in one burst counts once', () => {
  let burst = addEnded([], job('a', 'failed'));
  burst = addEnded(burst, job('b', 'completed'));
  burst = addEnded(burst, job('a', 'completed'));
  assert.deepEqual(burst.map((j) => `${j.id}:${j.status}`), ['a:completed', 'b:completed']);
  assert.equal(finishedToast(burst).message, '2 downloads finished');
  assert.equal(addEnded([], job('a', 'failed')).length, 1);
});
