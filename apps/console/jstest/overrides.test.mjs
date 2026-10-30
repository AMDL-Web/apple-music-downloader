import test from 'node:test';
import assert from 'node:assert/strict';
import {
  OPTION_DEFAULTS,
  OPTION_KEYS,
  optionDefaults,
  buildOverrides,
  sanitizeOverrides,
  applyOverrides,
  choices,
  PRESETS,
  presetValues,
  presetOverrides,
  qualityText,
  customLabel,
  probeWarning,
  probedQuality,
  dockQuality,
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
  lyrics_files: [],
  lyrics_timing: 'line',
  max_attempts: 3,
  memory_mode: 'low',
  quality_priority: ['alac'],
  save_album_cover: true,
  save_artist_cover: true,
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

test('choices adds a server value outside the usual list', () => {
  assert.deepEqual(choices(SAMPLE_RATES, 32000), [...SAMPLE_RATES, 32000]);
  assert.equal(choices(SAMPLE_RATES, SAMPLE_RATES[0]), SAMPLE_RATES);
  assert.equal(rateLabel(44100), '44.1 kHz');
  assert.equal(rateLabel(192000), '192 kHz');
});

test('each preset maps to its overrides; following Settings with Overwrite untouched sends none', () => {
  const d = optionDefaults(SERVER);
  assert.deepEqual(PRESETS.map((p) => p.id), ['follow', 'lossless', 'atmos', 'aac']);
  assert.deepEqual(presetOverrides(d, 'follow'), {});
  assert.deepEqual(presetOverrides(d, 'follow', null, false), {}, 'Overwrite at the Settings value is not sent');
  assert.deepEqual(presetOverrides(d, 'follow', null, true), { force_overwrite: true });
  assert.deepEqual(presetOverrides(d, 'lossless'), {}, 'Settings already downloads ALAC with the fallback on');
  assert.deepEqual(presetOverrides(d, 'atmos'), { quality_priority: ['ec3', 'alac'] });
  assert.deepEqual(presetOverrides(d, 'aac'), { quality_priority: ['aac'] });
  const strict = optionDefaults({ ...SERVER, quality_priority: ['ec3'], codec_alternative: false });
  assert.deepEqual(presetOverrides(strict, 'lossless'), { quality_priority: ['alac'], codec_alternative: true });
  assert.deepEqual(presetOverrides(strict, 'follow'), {});
});

test('the custom preset sends its edited quality keys, never other keys', () => {
  const d = optionDefaults(SERVER);
  const custom = { quality_priority: ['ec3', 'aac'], codec_alternative: false, alac_max_sample_rate: 48000, alac_max_bit_depth: 16, force_overwrite: true, embed_cover: false };
  assert.deepEqual(presetOverrides(d, 'custom', custom), { quality_priority: ['ec3', 'aac'], codec_alternative: false, alac_max_sample_rate: 48000, alac_max_bit_depth: 16 });
  assert.deepEqual(presetOverrides(d, 'custom', { quality_priority: [] }), {}, 'an empty codec list is never sent');
  assert.deepEqual(presetOverrides(d, 'bogus'), {});
  assert.equal(presetValues(d, 'atmos', null, true).force_overwrite, true);
});

test('qualityText, customLabel and probeWarning say what a download gets', () => {
  const d = optionDefaults(SERVER);
  assert.equal(qualityText(d), 'Lossless (ALAC) up to 24-bit / 192 kHz');
  assert.equal(qualityText(presetValues(d, 'atmos')), 'Dolby Atmos, then Lossless (ALAC) up to 24-bit / 192 kHz');
  assert.equal(qualityText({ ...d, quality_priority: ['aac', 'alac'], codec_alternative: false }), 'AAC only');
  assert.equal(customLabel({ quality_priority: ['ec3', 'alac'] }), 'Custom: Dolby Atmos, ALAC');
  const slots = [
    { id: 'alac', tracks: 12, total: 12 },
    { id: 'ec3', tracks: 0, total: 12 },
    { id: 'aac-binaural', tracks: 3, total: 12 },
  ];
  assert.equal(probeWarning(presetValues(d, 'atmos'), slots), 'Dolby Atmos not offered');
  assert.equal(probeWarning(presetValues(d, 'lossless'), slots), '');
  assert.equal(probeWarning({ quality_priority: ['aac-binaural'] }, slots), 'AAC binaural on 3 of 12 tracks');
  assert.equal(probeWarning(d, []), '');
  const probed = [
    { id: 'alac', tracks: 12, total: 12, best: '24-bit / 44.1\u00a0kHz · 1,580\u00a0kbps · 2\u00a0ch' },
    { id: 'ec3', tracks: 0, total: 12, best: '' },
  ];
  assert.deepEqual(probedQuality(presetValues(d, 'atmos'), probed), { text: 'Lossless (ALAC), 24-bit / 44.1\u00a0kHz', warning: 'Dolby Atmos not offered' });
  assert.equal(probedQuality({ ...presetValues(d, 'atmos'), codec_alternative: false }, probed).text, 'AAC (the fallback every track has)');
});

test('the dock says what the links will download once read, else what the choice asks for', () => {
  const d = optionDefaults(SERVER);
  const cd = { text: 'Lossless (ALAC), 16-bit / 44.1\u00a0kHz', warning: '' };
  const hires = { text: 'Lossless (ALAC), 24-bit / 96\u00a0kHz', warning: '' };
  assert.equal(dockQuality(d, [cd]), 'Will download: Lossless (ALAC), 16-bit / 44.1\u00a0kHz', 'as the row says it');
  assert.equal(dockQuality(d, [cd, cd]), 'Will download: Lossless (ALAC), 16-bit / 44.1\u00a0kHz');
  assert.equal(dockQuality(d, [cd, null]), 'Asks for Lossless (ALAC) up to 24-bit / 192 kHz', 'a link not read yet: the ceiling, said as one');
  assert.equal(dockQuality(d, [cd, hires]), 'Asks for Lossless (ALAC) up to 24-bit / 192 kHz', 'links that differ');
  assert.equal(dockQuality(d, []), 'Asks for Lossless (ALAC) up to 24-bit / 192 kHz');
  assert.equal(dockQuality(null, [cd]), '');
});

test('codec labels and ALAC choices match the Settings fields', async () => {
  const { fieldByKey } = await import('../static/js/lib/settings.js');
  const { CODECS, SAMPLE_RATES, BIT_DEPTHS } = await import('../static/js/lib/overrides.js');
  assert.deepEqual(CODECS.map((c) => [c.value, c.label]), fieldByKey('download.quality_priority').options.map((o) => [o.value, o.label]));
  assert.deepEqual(SAMPLE_RATES, fieldByKey('download.alac_max_sample_rate').options.map((o) => o.value));
  assert.deepEqual(BIT_DEPTHS, [16, 24]);
  assert.ok(CODECS.every((c) => c.hint), 'every codec has a description');
});
