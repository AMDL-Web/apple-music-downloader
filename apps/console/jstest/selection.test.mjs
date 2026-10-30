import test from 'node:test';
import assert from 'node:assert/strict';
import { toggleId, setIds, pruneSelection, rangeIds, selectedJobs, selectionState } from '../static/js/lib/selection.js';

const jobs = ['a', 'b', 'c', 'd', 'e'].map((id) => ({ id }));

test('toggleId flips or forces one id and returns the same Set when nothing changes', () => {
  const empty = new Set();
  const one = toggleId(empty, 'a');
  assert.deepEqual([...one], ['a']);
  assert.equal(empty.size, 0, 'input untouched');
  assert.deepEqual([...toggleId(one, 'a')], []);
  assert.equal(toggleId(one, 'a', true), one);
  assert.equal(toggleId(empty, 'a', false), empty);
});

test('setIds adds or removes many ids', () => {
  const s = setIds(new Set(['a']), ['b', 'c'], true);
  assert.deepEqual([...s].sort(), ['a', 'b', 'c']);
  assert.deepEqual([...setIds(s, ['a', 'c'], false)], ['b']);
  assert.equal(setIds(s, ['a'], true), s);
});

test('pruneSelection drops ids the list no longer holds', () => {
  const s = new Set(['a', 'c', 'x']);
  const pruned = pruneSelection(s, jobs);
  assert.deepEqual([...pruned].sort(), ['a', 'c']);
  assert.equal(pruneSelection(pruned, jobs), pruned, 'unchanged keeps identity');
  const empty = new Set();
  assert.equal(pruneSelection(empty, []), empty);
});

test('rangeIds spans list order in either direction', () => {
  assert.deepEqual(rangeIds(jobs, 'b', 'd'), ['b', 'c', 'd']);
  assert.deepEqual(rangeIds(jobs, 'd', 'b'), ['b', 'c', 'd']);
  assert.deepEqual(rangeIds(jobs, 'gone', 'c'), ['c']);
  assert.deepEqual(rangeIds(jobs, 'a', 'gone'), []);
});

test('selectedJobs and selectionState follow the current list', () => {
  assert.deepEqual(selectedJobs(new Set(['d', 'a', 'x']), jobs).map((j) => j.id), ['a', 'd']);
  assert.equal(selectionState(new Set(), jobs), 'none');
  assert.equal(selectionState(new Set(['a', 'x']), jobs), 'some');
  assert.equal(selectionState(new Set(jobs.map((j) => j.id)), jobs), 'all');
  assert.equal(selectionState(new Set(['a']), []), 'none');
});
