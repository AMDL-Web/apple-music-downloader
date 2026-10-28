import test from 'node:test';
import assert from 'node:assert/strict';
import {
  SETTINGS_SECTIONS,
  FIELDS,
  FIELD_KEYS,
  NOT_EDITED,
  sectionById,
  flattenConfig,
  sameValue,
  changedKeys,
  buildPatch,
  pinnedMap,
  parseLockedKey,
  keysInMessage,
  fieldErrors,
  validateDraft,
  parseNumberInput,
  changesLabel,
} from '../static/js/lib/settings.js';
import { templateUnknownVars } from '../static/js/lib/settings-template.js';
import { wrapperErrorText } from '../static/js/lib/settings-account.js';
import { ApiError } from '../static/js/api.js';

// A trimmed GET /api/v1/config "config" as the preview server returned it.
const CONFIG = {
  catalog: { album_track_url_mode: 'song', media_user_token: '', motion_artwork_enabled: true, signed_mode_hls_source: 'wrapper' },
  download: { max_attempts: 3, quality_priority: ['alac'], lyrics_extras: [], cover_size: '5000x5000', downloads_dir: '/data/downloads' },
  library_sync: { enabled: false, interval_minutes: 15 },
  logging: { access_log: false, level: 'info' },
  simulate: { enabled: true, max_speed_kbps: 4096, min_speed_kbps: 512 },
};

test('sections and fields: every field names a section, keys are unique dotted literals', () => {
  const ids = new Set(SETTINGS_SECTIONS.map((s) => s.id));
  assert.ok(ids.has('account') && ids.has('library_sync'), 'the palette links to account and library_sync');
  for (const id of ids) assert.match(id, /^[a-z][a-z0-9_]*$/, 'settingsSection accepts the id');
  assert.equal(new Set(FIELD_KEYS).size, FIELD_KEYS.length);
  for (const f of FIELDS) {
    assert.ok(ids.has(f.section), `${f.key} names an unknown section`);
    assert.match(f.key, /^[a-z_]+\.[a-z0-9_]+$/);
    assert.ok(f.label && f.help, `${f.key} has a label and help`);
    if (f.type === 'select' || f.type === 'list' || f.type === 'set') assert.ok(f.options.length > 0, `${f.key} has options`);
  }
  assert.deepEqual(NOT_EDITED, {});
  assert.equal(sectionById('nope').id, 'downloads');
  assert.equal(sectionById('account').id, 'account');
});

test('flattenConfig and changedKeys', () => {
  const flat = flattenConfig(CONFIG);
  assert.equal(flat['download.max_attempts'], 3);
  assert.deepEqual(flat['download.quality_priority'], ['alac']);
  assert.equal(flat['logging.level'], 'info');
  assert.deepEqual(flattenConfig(null), {});
  assert.ok(sameValue(['a', 'b'], ['a', 'b']));
  assert.ok(!sameValue(['a', 'b'], ['b', 'a']), 'order matters');
  assert.ok(!sameValue(['a'], 'a'));
  const draft = { 'download.max_attempts': 3, 'logging.level': 'debug', 'download.quality_priority': ['alac'] };
  assert.deepEqual(changedKeys(flat, draft), ['logging.level'], 'a draft equal to the base is no change');
});

test('buildPatch sends only changed leaves, lists whole, and never a pinned key', () => {
  const base = flattenConfig(CONFIG);
  const draft = {
    'download.max_attempts': 4,
    'download.quality_priority': ['ec3', 'alac'],
    'download.lyrics_extras': [],
    'logging.level': 'info',
    'simulate.max_speed_kbps': 8000,
    'download.downloads_dir': '/elsewhere',
  };
  const body = buildPatch(base, draft, { 'download.downloads_dir': 'AMDL_DOWNLOAD_DOWNLOADS_DIR' });
  assert.deepEqual(body, {
    download: { max_attempts: 4, quality_priority: ['ec3', 'alac'] },
    simulate: { max_speed_kbps: 8000 },
  });
  assert.notEqual(body.download.quality_priority, draft['download.quality_priority'], 'lists are copied');
  // A shorter list replaces the old one on the server, so it is sent as is.
  assert.deepEqual(buildPatch({ 'download.quality_priority': ['ec3', 'alac'] }, { 'download.quality_priority': ['aac'] }), {
    download: { quality_priority: ['aac'] },
  });
  assert.deepEqual(buildPatch(base, {}), {});
});

test('pinnedMap and parseLockedKey', () => {
  assert.deepEqual(
    pinnedMap([
      { key: 'download.downloads_dir', env: 'AMDL_DOWNLOAD_DOWNLOADS_DIR' },
      { key: 'simulate.enabled', env: 'AMDL_SIMULATE_ENABLED' },
      { key: 1 },
    ]),
    { 'download.downloads_dir': 'AMDL_DOWNLOAD_DOWNLOADS_DIR', 'simulate.enabled': 'AMDL_SIMULATE_ENABLED' },
  );
  assert.deepEqual(pinnedMap(undefined), {});
  assert.deepEqual(parseLockedKey('download.temp_dir (AMDL_DOWNLOAD_TEMP_DIR)'), { key: 'download.temp_dir', env: 'AMDL_DOWNLOAD_TEMP_DIR' });
  assert.deepEqual(parseLockedKey('download.max_running_jobs'), { key: 'download.max_running_jobs', env: '' });
  assert.equal(parseLockedKey('not a key'), null);
});

// The messages below are what the preview server answered to these PUTs.
test('fieldErrors maps 422 and 400 answers onto fields', () => {
  const pinned = new ApiError(422, 'config_key_pinned', 'fields are pinned by environment variables; unset the variable and restart to change them: download.downloads_dir (AMDL_DOWNLOAD_DOWNLOADS_DIR), download.temp_dir (AMDL_DOWNLOAD_TEMP_DIR)', {
    keys: ['download.downloads_dir (AMDL_DOWNLOAD_DOWNLOADS_DIR)', 'download.temp_dir (AMDL_DOWNLOAD_TEMP_DIR)'],
  });
  const p = fieldErrors(pinned);
  assert.deepEqual(Object.keys(p.fields), ['download.downloads_dir', 'download.temp_dir']);
  assert.match(p.fields['download.temp_dir'], /AMDL_DOWNLOAD_TEMP_DIR/);
  assert.equal(p.message, pinned.message);

  const locked = fieldErrors(new ApiError(422, 'config_key_not_mutable', 'fields can only be changed in the config file and require a restart: download.max_running_jobs', { keys: ['download.max_running_jobs'] }));
  assert.deepEqual(Object.keys(locked.fields), ['download.max_running_jobs']);

  const range = fieldErrors(new ApiError(422, 'invalid_config', 'download.max_attempts must be at most 10'));
  assert.deepEqual(range.fields, { 'download.max_attempts': 'download.max_attempts must be at most 10' });

  const codec = fieldErrors(new ApiError(422, 'invalid_config', 'unsupported codec "flac" in download.quality_priority'));
  assert.deepEqual(Object.keys(codec.fields), ['download.quality_priority'], 'the key may come last');

  const speeds = fieldErrors(new ApiError(422, 'invalid_config', 'simulate.max_speed_kbps must be >= simulate.min_speed_kbps'));
  assert.deepEqual(Object.keys(speeds.fields), ['simulate.min_speed_kbps', 'simulate.max_speed_kbps'], 'both keys named get it');

  const type = fieldErrors(new ApiError(400, 'invalid_request', 'json: cannot unmarshal string into Go struct field DownloadConfig.download.max_attempts of type int'));
  assert.deepEqual(Object.keys(type.fields), ['download.max_attempts']);

  const unknown = fieldErrors(new ApiError(400, 'invalid_request', 'json: unknown field "bogus"'));
  assert.deepEqual(unknown.fields, {});
  assert.equal(unknown.message, 'json: unknown field "bogus"');

  const network = fieldErrors(new ApiError(0, 'network_error', 'Cannot reach the server.'));
  assert.deepEqual(network.fields, {});
  assert.equal(network.message, 'Cannot reach the server.');
});

test('keysInMessage matches whole keys only', () => {
  assert.deepEqual(keysInMessage('download.save_lyrics_file_x is odd'), []);
  assert.deepEqual(keysInMessage('logging.level must be debug, info, warn, or error'), ['logging.level']);
  assert.deepEqual(keysInMessage('library_sync.interval_minutes must be between 1 and 1440'), ['library_sync.interval_minutes']);
});

test('validateDraft blocks unsendable numbers and warns on odd values', () => {
  const { errors, warnings } = validateDraft({
    'download.max_attempts': '',
    'simulate.min_speed_kbps': 'fast',
    'library_sync.interval_minutes': 2000,
    'download.cover_size': 'big',
    'download.album_path_format': 'albums/{AlbumName}/{Track}. {SongName}',
    'download.song_path_format': 'songs/{TrackNumber:02d}. {SongName}',
    'download.quality_priority': [],
  });
  assert.deepEqual(Object.keys(errors).sort(), ['download.max_attempts', 'download.quality_priority', 'simulate.min_speed_kbps']);
  assert.ok(!('library_sync.interval_minutes' in errors), 'bounds stay with the server');
  assert.match(warnings['download.cover_size'], /3000x3000/);
  assert.match(warnings['download.album_path_format'], /\{Track\}/);
  assert.ok(!('download.song_path_format' in warnings));
});

test('parseNumberInput and changesLabel', () => {
  assert.equal(parseNumberInput('12'), 12);
  assert.equal(parseNumberInput(' -3 '), -3);
  assert.equal(parseNumberInput(''), '');
  assert.equal(parseNumberInput('1.5'), '1.5');
  assert.equal(changesLabel(1), '1 unsaved change');
  assert.equal(changesLabel(3), '3 unsaved changes');
});

test('templateUnknownVars', () => {
  assert.deepEqual(templateUnknownVars('albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}'), []);
  assert.deepEqual(templateUnknownVars('{Foo}/{SongName:02d}/{TrackNumber:2}/{Foo}'), ['{Foo}', '{SongName:02d}', '{TrackNumber:2}']);
  assert.deepEqual(templateUnknownVars(''), []);
});

test('wrapperErrorText per status and code', () => {
  const e = (status, code, message = 'raw') => new ApiError(status, code, message);
  assert.match(wrapperErrorText(e(401, 'authentication_failed'), 'login').message, /Apple ID and password/);
  assert.match(wrapperErrorText(e(401, 'authentication_failed'), 'code').message, /code/);
  assert.match(wrapperErrorText(e(404, 'login_not_found'), 'code').message, /Start again/);
  assert.match(wrapperErrorText(e(404, 'account_not_found'), 'logout').message, /no session/);
  assert.match(wrapperErrorText(e(409, 'already_logged_in')).message, /already logged in/);
  assert.match(wrapperErrorText(e(409, 'login_pending')).message, /in progress/);
  const bad = wrapperErrorText(e(502, 'upstream_error', 'dial tcp 127.0.0.1:8080: connect: connection refused'));
  assert.match(bad.message, /could not complete/);
  assert.match(bad.detail, /connection refused/);
  assert.match(wrapperErrorText(e(503, 'wrapper_unavailable')).message, /not reachable/);
  assert.match(wrapperErrorText(e(503, 'feature_unavailable')).message, /no wrapper connection/);
  assert.match(wrapperErrorText(e(504, 'upstream_timeout')).message, /in time/);
  assert.equal(wrapperErrorText(e(400, 'invalid_request', 'username and password are required')).message, 'username and password are required');
  assert.match(wrapperErrorText(e(0, 'timeout')).message, /in time/);
  assert.match(wrapperErrorText(e(0, 'network_error')).message, /Cannot reach/);
  assert.match(wrapperErrorText(e(401, '')).message, /Apple ID/, 'a bare 401 reads like authentication_failed');
});
