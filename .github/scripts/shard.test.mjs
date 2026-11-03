import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { parseShard, pattern, slice } from './shard.mjs';

const names = Array.from({ length: 10 }, (_, i) => `Test${i + 1}`);

test('the slices together hold every test exactly once', () => {
  for (const n of [1, 2, 3, 4, 7]) {
    const all = Array.from({ length: n }, (_, i) => slice(names, `${i + 1}/${n}`)).flat();
    assert.deepEqual([...all].sort(), [...names].sort(), `n=${n}`);
  }
});

test('a slice takes every n-th name', () => {
  assert.deepEqual(slice(names, '2/3'), ['Test2', 'Test5', 'Test8']);
});

test('lines that are not test names are ignored', () => {
  assert.deepEqual(slice(['TestA', 'ok  \tamdl/x\t0.1s', '', 'ExampleB', 'FuzzC', 'helper'], '1/1'), ['TestA', 'ExampleB', 'FuzzC']);
});

test('the pattern is anchored, so Test1 does not select Test10', () => {
  const re = new RegExp(pattern(['Test1', 'Test2']));
  assert.ok(re.test('Test1') && !re.test('Test10') && !re.test('XTest1'));
});

test('bad shards and empty slices are refused', () => {
  for (const bad of ['', '0/3', '4/3', '1/0', 'a/b', '1/2/3', undefined]) assert.throws(() => parseShard(bad), /shard must be k\/n/, String(bad));
  assert.throws(() => pattern(slice(['TestA'], '2/2')), /no tests/);
});

test('the command prints the pattern and exits 2 on a bad argument', () => {
  const run = (arg, input) => spawnSync('node', [fileURLToPath(new URL('./shard.mjs', import.meta.url)), ...(arg ? [arg] : [])], { input, encoding: 'utf8' });
  assert.equal(run('1/2', 'TestA\nTestB\nTestC\nok  x 1s\n').stdout, '^(TestA|TestC)$\n');
  assert.equal(run('3/2', 'TestA\n').status, 2);
  assert.equal(run(undefined, 'TestA\n').status, 2);
  assert.equal(run('2/2', 'TestA\n').status, 2);
});
