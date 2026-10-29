import test from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_THEME, THEME_KEY, normalizeTheme, readPref, writePref } from '../static/js/lib/prefs.js';

const memory = (init = {}) => {
  const data = { ...init };
  return { getItem: (k) => (k in data ? data[k] : null), setItem: (k, v) => (data[k] = String(v)), data };
};
const broken = { getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); } };

test('themes: system is the default', () => {
  assert.equal(DEFAULT_THEME, 'system');
  assert.equal(normalizeTheme('dark'), 'dark');
  assert.equal(normalizeTheme('sepia'), 'system');
});

test('readPref and writePref survive missing or throwing storage', () => {
  const store = memory({ [THEME_KEY]: 'nope' });
  assert.equal(readPref(store, THEME_KEY, normalizeTheme), 'system');
  assert.equal(readPref(memory({ [THEME_KEY]: 'dark' }), THEME_KEY, normalizeTheme), 'dark');
  assert.equal(readPref(broken, THEME_KEY, normalizeTheme), 'system');
  assert.equal(readPref(null, THEME_KEY, normalizeTheme), 'system');
  assert.equal(writePref(store, THEME_KEY, 'light'), true);
  assert.equal(store.data[THEME_KEY], 'light');
  assert.equal(writePref(broken, THEME_KEY, 'light'), false);
  assert.equal(writePref(null, THEME_KEY, 'light'), false);
});
