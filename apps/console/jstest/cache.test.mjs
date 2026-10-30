import test from 'node:test';
import assert from 'node:assert/strict';
import { cached, remember, forgetAll, CACHE_MAX } from '../static/js/lib/cache.js';

test('a remembered answer comes back under its key, and an empty key holds nothing', () => {
  forgetAll();
  remember('queue|failed||', { jobs: [1], total: 1 });
  assert.deepEqual(cached('queue|failed||'), { jobs: [1], total: 1 });
  assert.equal(cached('queue|all||'), undefined);
  remember('', 'x');
  assert.equal(cached(''), undefined);
});

test('the oldest entry goes once the cache is full, a re-stored one counts as new', () => {
  forgetAll();
  for (let i = 0; i < CACHE_MAX; i++) remember(`k${i}`, i);
  remember('k0', 'again');
  remember('extra', 1);
  assert.equal(cached('k0'), 'again');
  assert.equal(cached('k1'), undefined);
  assert.equal(cached('extra'), 1);
});
