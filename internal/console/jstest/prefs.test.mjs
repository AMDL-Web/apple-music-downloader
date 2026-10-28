import test from 'node:test';
import assert from 'node:assert/strict';
import {
  SKINS,
  DEFAULT_SKIN,
  DEFAULT_THEME,
  SKIN_KEY,
  THEME_KEY,
  normalizeSkin,
  normalizeTheme,
  nextSkin,
  readPref,
  writePref,
} from '../static/js/lib/prefs.js';

const memory = (init = {}) => {
  const data = { ...init };
  return { getItem: (k) => (k in data ? data[k] : null), setItem: (k, v) => (data[k] = String(v)), data };
};
const broken = { getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); } };

test('skins: music is the default, unknown values fall back to it', () => {
  assert.deepEqual(SKINS.map((s) => s.value), ['music', 'editorial']);
  assert.equal(DEFAULT_SKIN, 'music');
  assert.equal(normalizeSkin('editorial'), 'editorial');
  assert.equal(normalizeSkin('terminal'), 'music');
  assert.equal(normalizeSkin(null), 'music');
  assert.equal(nextSkin('music'), 'editorial');
  assert.equal(nextSkin('editorial'), 'music');
  assert.equal(nextSkin('bogus'), 'editorial');
});

test('themes: system is the default', () => {
  assert.equal(DEFAULT_THEME, 'system');
  assert.equal(normalizeTheme('dark'), 'dark');
  assert.equal(normalizeTheme('sepia'), 'system');
});

test('readPref and writePref survive missing or throwing storage', () => {
  const store = memory({ [SKIN_KEY]: 'editorial', [THEME_KEY]: 'nope' });
  assert.equal(readPref(store, SKIN_KEY, normalizeSkin), 'editorial');
  assert.equal(readPref(store, THEME_KEY, normalizeTheme), 'system');
  assert.equal(readPref(broken, SKIN_KEY, normalizeSkin), 'music');
  assert.equal(readPref(null, SKIN_KEY, normalizeSkin), 'music');
  assert.equal(writePref(store, SKIN_KEY, 'music'), true);
  assert.equal(store.data[SKIN_KEY], 'music');
  assert.equal(writePref(broken, SKIN_KEY, 'music'), false);
  assert.equal(writePref(null, SKIN_KEY, 'music'), false);
});
