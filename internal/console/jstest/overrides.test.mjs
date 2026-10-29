import test from 'node:test';
import assert from 'node:assert/strict';
import {
  OPTION_DEFAULTS,
  OPTION_KEYS,
  optionDefaults,
  buildOverrides,
  sanitizeOverrides,
  applyOverrides,
  moveItem,
  choices,
  describeOverrides,
  rateLabel,
  SAMPLE_RATES,
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

test('the form edits quality and overwrite only', () => {
  assert.deepEqual(OPTION_KEYS, ['quality_priority', 'codec_alternative', 'alac_max_sample_rate', 'alac_max_bit_depth', 'force_overwrite']);
});

test('optionDefaults reads the edited keys from the server and nothing else', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(Object.keys(d), OPTION_KEYS);
  assert.deepEqual(d, { ...OPTION_DEFAULTS, quality_priority: ['alac'] });
  for (const key of ['downloads_dir', 'memory_mode', 'max_attempts', 'embed_lyrics', 'cover_size']) assert.equal(key in d, false, key);
});

test('optionDefaults falls back per key', () => {
  const d = optionDefaults({ quality_priority: ['aac', 'bogus'], alac_max_sample_rate: 0, force_overwrite: 'yes' });
  assert.deepEqual(d.quality_priority, ['alac'], 'an unknown codec voids the list');
  assert.equal(d.alac_max_sample_rate, 192000);
  assert.equal(d.force_overwrite, false);
  assert.deepEqual(optionDefaults(null), { ...OPTION_DEFAULTS, quality_priority: ['alac'] });
});

test('the form at its defaults sends no overrides', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(buildOverrides(d, { ...d }), {});
});

test('only changed options are sent, with their exact values', () => {
  const d = optionDefaults(SERVER);
  const v = {
    ...d,
    quality_priority: ['ec3', 'alac'],
    codec_alternative: false,
    alac_max_sample_rate: 48000,
    force_overwrite: true,
  };
  assert.deepEqual(buildOverrides(d, v), {
    quality_priority: ['ec3', 'alac'],
    codec_alternative: false,
    alac_max_sample_rate: 48000,
    force_overwrite: true,
  });
});

test('quality order matters', () => {
  const d = optionDefaults({ ...SERVER, quality_priority: ['alac', 'aac'] });
  assert.deepEqual(buildOverrides(d, { ...d, quality_priority: ['aac', 'alac'] }), { quality_priority: ['aac', 'alac'] });
});

test('invalid form values are never sent', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(buildOverrides(d, { ...d, quality_priority: [], alac_max_bit_depth: -1, force_overwrite: 'yes' }), {});
});

test('sanitizeOverrides keeps only the edited keys with valid values', () => {
  const raw = { quality_priority: ['aac'], media_user_token: 'secret', bogus: 1, embed_cover: false, hooks: ['a'], force_overwrite: 'yes' };
  assert.deepEqual(sanitizeOverrides(raw), { quality_priority: ['aac'] });
  assert.deepEqual(sanitizeOverrides([1, 2]), {});
  assert.deepEqual(sanitizeOverrides(null), {});
});

test('applyOverrides lays overrides over the defaults', () => {
  const d = optionDefaults(SERVER);
  const form = applyOverrides(d, { quality_priority: ['aac'], force_overwrite: true, embed_cover: false });
  assert.deepEqual(form, { ...d, quality_priority: ['aac'], force_overwrite: true });
});

test('moveItem reorders and refuses moves past either end', () => {
  const list = ['alac', 'aac', 'ec3'];
  assert.deepEqual(moveItem(list, 2, -1), ['alac', 'ec3', 'aac']);
  assert.deepEqual(moveItem(list, 0, 1), ['aac', 'alac', 'ec3']);
  assert.equal(moveItem(list, 0, -1), list);
  assert.equal(moveItem(list, 2, 1), list);
});

test('choices adds a server value outside the usual list', () => {
  assert.deepEqual(choices(SAMPLE_RATES, 32000), [...SAMPLE_RATES, 32000]);
  assert.equal(choices(SAMPLE_RATES, SAMPLE_RATES[0]), SAMPLE_RATES);
  assert.equal(rateLabel(44100), '44.1 kHz');
  assert.equal(rateLabel(192000), '192 kHz');
});

test('describeOverrides names each change in display order', () => {
  assert.deepEqual(describeOverrides({}), []);
  assert.deepEqual(
    describeOverrides({
      force_overwrite: true,
      quality_priority: ['ec3', 'alac'],
      codec_alternative: false,
      alac_max_bit_depth: 16,
      embed_cover: false,
    }),
    ['Quality: Dolby Atmos (E-AC-3), ALAC (lossless)', 'Fallback to next codec off', 'ALAC up to 16-bit', 'Overwrite existing files'],
  );
  assert.deepEqual(describeOverrides({ force_overwrite: false }), ['Keep existing files']);
});

test('codec labels and ALAC choices match the Settings fields', async () => {
  const { fieldByKey } = await import('../static/js/lib/settings.js');
  const { CODECS, SAMPLE_RATES, BIT_DEPTHS } = await import('../static/js/lib/overrides.js');
  assert.deepEqual(CODECS.map((c) => [c.value, c.label]), fieldByKey('download.quality_priority').options.map((o) => [o.value, o.label]));
  assert.deepEqual(SAMPLE_RATES, fieldByKey('download.alac_max_sample_rate').options.map((o) => o.value));
  assert.deepEqual(BIT_DEPTHS, [16, 24]);
  assert.ok(CODECS.every((c) => c.hint), 'every codec has a description');
});
