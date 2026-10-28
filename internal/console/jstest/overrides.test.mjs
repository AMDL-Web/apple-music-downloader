import test from 'node:test';
import assert from 'node:assert/strict';
import {
  OPTION_DEFAULTS,
  OPTION_KEYS,
  optionDefaults,
  buildOverrides,
  sanitizeOverrides,
  parseStoredOptions,
  applyOverrides,
  moveItem,
  choices,
  describeOverrides,
  rateLabel,
  COVER_FORMATS,
} from '../static/js/lib/overrides.js';

// The download section GET /api/v1/config answers in simulate mode (paths trimmed).
const SERVER = {
  alac_max_bit_depth: 24,
  alac_max_sample_rate: 192000,
  check_integrity: true,
  codec_alternative: true,
  cover_format: 'jpg',
  cover_size: '5000x5000',
  downloads_dir: '/data/downloads',
  embed_cover: true,
  embed_lyrics: true,
  force_overwrite: false,
  lyrics_extras: [],
  lyrics_format: 'lrc',
  lyrics_type: 'lyrics',
  max_attempts: 3,
  memory_mode: 'low',
  quality_priority: ['alac'],
  save_album_cover: true,
  save_artist_cover: true,
  save_lyrics_file: false,
  save_playlist_cover: true,
};

test('optionDefaults reads the edited keys from the server and nothing else', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(Object.keys(d), OPTION_KEYS);
  assert.deepEqual(d, { ...OPTION_DEFAULTS, quality_priority: ['alac'], lyrics_extras: [] });
  assert.equal('downloads_dir' in d, false);
  assert.equal('memory_mode' in d, false);
});

test('optionDefaults falls back per key and clamps attempts like the backend', () => {
  const d = optionDefaults({ quality_priority: ['aac', 'bogus'], max_attempts: 40, cover_format: 'gif', lyrics_extras: ['pronunciation', 'translation'] });
  assert.deepEqual(d.quality_priority, ['alac'], 'an unknown codec voids the list');
  assert.equal(d.max_attempts, 10);
  assert.equal(d.cover_format, 'jpg');
  assert.deepEqual(d.lyrics_extras, ['translation', 'pronunciation'], 'extras are kept in canonical order');
  assert.equal(optionDefaults({ max_attempts: 0 }).max_attempts, 1);
  assert.deepEqual(optionDefaults(null), { ...OPTION_DEFAULTS, quality_priority: ['alac'], lyrics_extras: [] });
});

test('the form at its defaults sends no overrides', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(buildOverrides(d, { ...d }), {});
  assert.deepEqual(buildOverrides(d, { ...d }, { hooks: null, enabledHooks: ['a'] }), {}, 'untouched hooks are omitted');
  assert.deepEqual(buildOverrides(d, { ...d }, { hooks: ['a'], enabledHooks: ['a'] }), {}, 'every enabled hook equals omitting the field');
  assert.deepEqual(buildOverrides(d, { ...d }, { token: '   ' }), {}, 'a blank token is not sent');
});

test('only changed options are sent, with their exact values', () => {
  const d = optionDefaults(SERVER);
  const v = {
    ...d,
    quality_priority: ['ec3', 'alac'],
    alac_max_sample_rate: 48000,
    save_lyrics_file: true,
    lyrics_extras: ['pronunciation'],
    cover_size: '1400x1400',
    force_overwrite: true,
    max_attempts: 5,
  };
  assert.deepEqual(buildOverrides(d, v), {
    quality_priority: ['ec3', 'alac'],
    alac_max_sample_rate: 48000,
    save_lyrics_file: true,
    lyrics_extras: ['pronunciation'],
    cover_size: '1400x1400',
    force_overwrite: true,
    max_attempts: 5,
  });
});

test('quality order matters, lyrics extras order does not', () => {
  const d = optionDefaults({ ...SERVER, quality_priority: ['alac', 'aac'], lyrics_extras: ['translation', 'pronunciation'] });
  const v = { ...d, quality_priority: ['aac', 'alac'], lyrics_extras: ['pronunciation', 'translation'] };
  assert.deepEqual(buildOverrides(d, v), { quality_priority: ['aac', 'alac'] });
});

test('hooks: none checked sends [], a subset sends the names, the token is trimmed', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(buildOverrides(d, d, { hooks: [], enabledHooks: ['a', 'b'] }), { hooks: [] });
  assert.deepEqual(buildOverrides(d, d, { hooks: ['b', 'b'], enabledHooks: ['a', 'b'] }), { hooks: ['b'] });
  assert.deepEqual(buildOverrides(d, d, { token: ' tok ' }), { media_user_token: 'tok' });
});

test('invalid form values are never sent', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(buildOverrides(d, { ...d, max_attempts: 11, quality_priority: [], cover_size: 'big' }), {});
});

test('stored options are sanitized and never hold a token', () => {
  const raw = JSON.stringify({ quality_priority: ['aac'], media_user_token: 'secret', bogus: 1, embed_cover: 'yes', hooks: ['a'] });
  assert.deepEqual(parseStoredOptions(raw), { quality_priority: ['aac'], hooks: ['a'] });
  assert.equal(parseStoredOptions('{not json'), null);
  assert.equal(parseStoredOptions(''), null);
  assert.equal(parseStoredOptions(null), null);
  assert.equal(parseStoredOptions('{"media_user_token":"x"}'), null);
  assert.deepEqual(sanitizeOverrides([1, 2]), {});
});

test('remembered overrides re-apply on top of new defaults', () => {
  const d = optionDefaults(SERVER);
  const form = applyOverrides(d, { quality_priority: ['aac'], save_lyrics_file: true, junk: 1 });
  assert.deepEqual(form.quality_priority, ['aac']);
  assert.equal(form.save_lyrics_file, true);
  assert.equal(form.embed_cover, true);
  // The server default later matches the remembered value: nothing is sent for it.
  const later = optionDefaults({ ...SERVER, save_lyrics_file: true });
  assert.deepEqual(buildOverrides(later, form), { quality_priority: ['aac'] });
});

test('moveItem reorders and refuses moves past either end', () => {
  const list = ['alac', 'aac', 'ec3'];
  assert.deepEqual(moveItem(list, 2, -1), ['alac', 'ec3', 'aac']);
  assert.deepEqual(moveItem(list, 0, 1), ['aac', 'alac', 'ec3']);
  assert.equal(moveItem(list, 0, -1), list);
  assert.equal(moveItem(list, 2, 1), list);
});

test('choices adds a server value outside the usual list', () => {
  assert.deepEqual(choices(COVER_FORMATS, 'jpeg'), ['jpg', 'png', 'jpeg']);
  assert.equal(choices(COVER_FORMATS, 'png'), COVER_FORMATS);
  assert.equal(rateLabel(44100), '44.1 kHz');
  assert.equal(rateLabel(192000), '192 kHz');
});

test('describeOverrides names each change in display order', () => {
  assert.deepEqual(describeOverrides({}), []);
  assert.deepEqual(
    describeOverrides({
      hooks: [],
      quality_priority: ['ec3', 'alac'],
      save_lyrics_file: true,
      max_attempts: 1,
      lyrics_type: 'syllable-lyrics',
      cover_size: '1400x1400',
      media_user_token: 'secret',
    }),
    ['Quality: Dolby Atmos (E-AC-3), ALAC (lossless)', 'Lyrics file on', 'Word-timed lyrics', 'Cover 1400 × 1400', '1 attempt', 'Hooks: none'],
  );
  assert.deepEqual(describeOverrides({ hooks: ['notify'], force_overwrite: true }), ['Overwrite existing files', 'Hooks: notify']);
});

test('codec labels and ALAC choices match the Settings fields', async () => {
  const { fieldByKey } = await import('../static/js/lib/settings.js');
  const { CODECS, SAMPLE_RATES, BIT_DEPTHS } = await import('../static/js/lib/overrides.js');
  assert.deepEqual(CODECS.map((c) => [c.value, c.label]), fieldByKey('download.quality_priority').options.map((o) => [o.value, o.label]));
  assert.deepEqual(SAMPLE_RATES, fieldByKey('download.alac_max_sample_rate').options.map((o) => o.value));
  assert.deepEqual(BIT_DEPTHS, [16, 24]);
  assert.ok(CODECS.every((c) => c.hint), 'every codec has a description');
});
