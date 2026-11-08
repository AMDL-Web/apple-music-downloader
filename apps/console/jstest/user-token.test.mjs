import test from 'node:test';
import assert from 'node:assert/strict';
import { tokenState } from '../static/js/lib/user-token.js';

const ok = (data) => ({ data, at: 1, okAt: 1 });
const fail = (status) => ({ data: undefined, error: { status }, at: 1, okAt: 0 });
const shape = (s) => [s.known, s.present];

test('a token is present when the wrapper says an account is signed in', () => {
  assert.deepEqual(shape(tokenState(ok({ ready: true, logged_in: true }))), [true, true]);
  assert.deepEqual(shape(tokenState(ok({ ready: true, logged_in: false }))), [true, false]);
  assert.deepEqual(shape(tokenState(fail(503))), [true, false], 'a failed wrapper status has no account');
});

test('unknown until the wrapper status has answered', () => {
  assert.equal(tokenState(undefined).known, false);
  assert.equal(tokenState({ data: undefined, at: 0, okAt: 0 }).known, false);
});

test('the key changes with the sign-in state', () => {
  const keys = [tokenState(ok({ logged_in: true })).key, tokenState(ok({ logged_in: false })).key, tokenState(undefined).key];
  assert.equal(new Set(keys).size, 3);
});
