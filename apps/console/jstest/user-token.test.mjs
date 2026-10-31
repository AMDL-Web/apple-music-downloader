import test from 'node:test';
import assert from 'node:assert/strict';
import { tokenState } from '../static/js/lib/user-token.js';
import { linkIssue } from '../static/js/lib/submit-check.js';
import { scanLinks } from '../static/js/lib/links.js';

const ok = (data) => ({ data, error: undefined });
const down = { data: undefined, error: { status: 503, message: 'wrapper_unavailable' } };
const pending = undefined;
const wrapperWith = (token) => ok({ ready: true, logged_in: true, regions: ['us'], accounts: [], media_user_token: token });

test('the wrapper token wins over the saved one', () => {
  const s = tokenState('...wxyz', wrapperWith('...a1b2'));
  assert.deepEqual([s.known, s.present, s.source, s.wrapperToken], [true, true, 'wrapper', '...a1b2']);
});

test('the wrapper token is enough before the config loads', () => {
  const s = tokenState(undefined, wrapperWith('...a1b2'));
  assert.deepEqual([s.known, s.present, s.source], [true, true, 'wrapper']);
});

test('the saved token applies when the wrapper has none, is down, or has not answered', () => {
  for (const wrapper of [wrapperWith(''), down, pending, ok({ ready: true })]) {
    const s = tokenState('...wxyz', wrapper);
    assert.deepEqual([s.known, s.present, s.source, s.wrapperToken], [true, true, 'config', '']);
  }
});

test('no token is known only once both answers are in', () => {
  assert.deepEqual([tokenState('', wrapperWith('')).known, tokenState('', wrapperWith('')).present], [true, false]);
  assert.deepEqual([tokenState('', down).known, tokenState('', down).present], [true, false], 'a failed wrapper status has no token');
  assert.equal(tokenState('', pending).known, false, 'the wrapper status has not answered');
  assert.equal(tokenState(undefined, wrapperWith('')).known, false, 'the config has not loaded');
});

test('the key changes with the token in use', () => {
  const keys = [
    tokenState('', wrapperWith('')).key,
    tokenState('...wxyz', wrapperWith('')).key,
    tokenState('...abcd', wrapperWith('')).key,
    tokenState('...wxyz', wrapperWith('...a1b2')).key,
  ];
  assert.equal(new Set(keys).size, keys.length);
  assert.equal(tokenState('...wxyz', wrapperWith('...a1b2')).key, tokenState('', wrapperWith('...a1b2')).key, 'a saved token the wrapper overrides does not change it');
});

test('a station warns about the token only when neither source has one', () => {
  const [station] = scanLinks('https://music.apple.com/us/station/x/ra.123', 'us');
  const issue = (saved, wrapper) => {
    const t = tokenState(saved, wrapper);
    return linkIssue(station, null, { hasToken: !t.known || t.present });
  };
  assert.equal(issue('', wrapperWith('...a1b2')), null, 'the wrapper has one');
  assert.equal(issue('...wxyz', wrapperWith('')), null, 'Settings has one');
  assert.equal(issue('', pending), null, 'not flagged before the wrapper status answers');
  assert.equal(issue('', wrapperWith('')).code, 'needs_token');
});
