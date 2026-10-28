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
  draftAfterSave,
  pinnedMap,
  parseLockedKey,
  keysInMessage,
  fieldErrors,
  validateDraft,
  parseNumberInput,
  changesLabel,
  fieldWarning,
  savedWarnings,
  diffConfig,
  labelList,
  reloadSummary,
  lossSummary,
  unsavedAnnouncement,
  librarySyncState,
  anchorResetState,
} from '../static/js/lib/settings.js';
import { templateUnknownVars } from '../static/js/lib/settings-template.js';
import { wrapperErrorText, wrapperUnreachable } from '../static/js/lib/settings-account.js';
import { ApiError } from '../static/js/api.js';
import { readFileSync } from 'node:fs';

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
  for (const [v, warned] of [['0x0', true], ['05000x5000', true], ['5000x0', true], ['5000x5000', false], ['1400x1400', false]]) {
    assert.equal('download.cover_size' in validateDraft({ 'download.cover_size': v }).warnings, warned, v);
  }
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

test('draftAfterSave keeps only what changed while the save ran', () => {
  const list = ['alac', 'aac'];
  const sent = { 'download.max_attempts': 4, 'download.quality_priority': list, 'logging.level': 'warn' };
  const now = { ...sent, 'logging.level': 'error', 'download.progress_event_interval_ms': 750 };
  assert.deepEqual(draftAfterSave(now, sent), { 'logging.level': 'error', 'download.progress_event_interval_ms': 750 });
  assert.deepEqual(draftAfterSave({ 'download.quality_priority': [...list] }, sent), { 'download.quality_priority': ['alac', 'aac'] }, 'a list edited again is a new value');
  assert.deepEqual(draftAfterSave(sent, sent), {});
});

// The upgrade-safe way to state defaults: the page shows them, and they must
// be the ones configs/config.example.yaml sets (a key missing from the file
// takes those).
const EXAMPLE = readFileSync(new URL('../../../configs/config.example.yaml', import.meta.url), 'utf8');
const exampleValue = (name) => new RegExp(`^  ${name}: "(.*)"$`, 'm').exec(EXAMPLE)?.[1];

test('every field states its default, and the path defaults match config.example.yaml', () => {
  // The token's help says "Empty means none".
  const exempt = new Set(['catalog.media_user_token']);
  for (const f of FIELDS) {
    if (exempt.has(f.key)) continue;
    assert.ok(/Default: /.test(f.help) || f.defaultValue, `${f.key} states no default`);
  }
  const templates = FIELDS.filter((f) => f.type === 'template' || f.type === 'path');
  assert.equal(templates.length, 7);
  for (const f of templates) {
    assert.ok(f.defaultValue, `${f.key} has no defaultValue`);
    assert.equal(f.defaultValue, exampleValue(f.key.split('.')[1]), `${f.key} drifted from config.example.yaml`);
  }
});

test('fieldWarning and savedWarnings keep an unknown variable flagged after the save', () => {
  const song = FIELDS.find((f) => f.key === 'download.song_path_format');
  assert.equal(fieldWarning(song, 'songs/{SongName}'), '');
  assert.match(fieldWarning(song, 'songs/{Foo}/{SongName}'), /\{Foo\}/);
  const base = { 'download.song_path_format': 'songs/{Foo}', 'download.album_path_format': 'a/{SongName}', 'download.cover_size': '5000x5000' };
  assert.deepEqual(Object.keys(savedWarnings(base)), ['download.song_path_format']);
  assert.deepEqual(savedWarnings(base, { 'download.song_path_format': 'songs/{SongName}' }), {}, 'a field under edit has its own warning');
});

test('diffConfig, reloadSummary and labelList', () => {
  const a = { 'logging.level': 'info', 'download.max_attempts': 3, 'download.quality_priority': ['alac'] };
  assert.deepEqual(diffConfig(a, { ...a }), []);
  assert.deepEqual(diffConfig(a, { ...a, 'logging.level': 'warn', 'download.max_attempts': 7 }), ['download.max_attempts', 'logging.level'], 'field order');
  assert.deepEqual(diffConfig(a, { ...a, 'download.quality_priority': ['alac', 'aac'] }), ['download.quality_priority']);
  assert.equal(labelList(['download.max_attempts']), 'Attempts per step');
  assert.equal(labelList(['download.max_attempts', 'logging.level']), 'Attempts per step and Log level');
  assert.match(labelList(['download.max_attempts', 'logging.level', 'simulate.enabled', 'download.cover_size', 'library_sync.enabled'], 3), /and 2 more$/);
  assert.match(reloadSummary([], 0).long, /no changes\.$/);
  assert.equal(reloadSummary([], 0).short, 'Reloaded from the file: no changes');
  const two = reloadSummary(['logging.level', 'download.max_attempts'], 3);
  assert.match(two.long, /2 settings changed \(Log level and Attempts per step\)/);
  assert.match(two.long, /3 pinned keys keep their environment values/);
  assert.match(reloadSummary(['logging.level'], 1).short, /1 setting changed/);
});

test('lossSummary and unsavedAnnouncement', () => {
  const t = lossSummary(['download.max_attempts', 'download.song_path_format', 'logging.level']);
  assert.equal(t.count, 3);
  assert.equal(t.sections, 'Downloads, Files and paths and Logging');
  assert.equal(t.fields, 'Attempts per step, Songs and Log level');
  assert.equal(unsavedAnnouncement(1), '1 unsaved change');
  assert.equal(unsavedAnnouncement(4), '4 unsaved changes');
  assert.equal(unsavedAnnouncement(0), 'No unsaved changes');
});

test('fieldErrors marks only the keys of the PUT when the message names others', () => {
  const err = new ApiError(422, 'invalid_config', 'download.song_path_format must stay inside download.downloads_dir');
  assert.deepEqual(Object.keys(fieldErrors(err).fields), ['download.downloads_dir', 'download.song_path_format'], 'without the PUT keys, every named key');
  assert.deepEqual(Object.keys(fieldErrors(err, ['download.song_path_format']).fields), ['download.song_path_format']);
  assert.deepEqual(Object.keys(fieldErrors(err, ['logging.level']).fields), ['download.downloads_dir', 'download.song_path_format'], 'none of the PUT keys named: keep all');
});

test('librarySyncState: on but idle is not Off', () => {
  const base = { enabled: true, interval_minutes: 15, anchor_size: 0 };
  assert.equal(librarySyncState({ ...base, enabled: false }, true).label, 'Off');
  const idle = librarySyncState({ ...base, last_error: 'media user token is empty' }, false);
  assert.equal(idle.label, 'On, idle');
  assert.equal(idle.role, 'warning');
  assert.equal(librarySyncState({ ...base, last_error: 'boom' }, true).label, 'Error');
  assert.match(librarySyncState(base, true).detail, /first poll pending/);
  assert.equal(librarySyncState({ ...base, last_poll_at: '2026-01-01T00:00:00Z' }, true).detail, 'Every 15 min');
});

test('anchorResetState disables Reset anchor without an anchor', () => {
  assert.equal(anchorResetState(undefined).allowed, false);
  const none = anchorResetState({ anchor_size: 0 });
  assert.equal(none.allowed, false);
  assert.match(none.reason, /no anchor/);
  assert.deepEqual(anchorResetState({ anchor_size: 120 }), { allowed: true, reason: '' });
});

test('wrapperUnreachable reads the address out of a gRPC dial error', () => {
  const raw = 'rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:8080: connect: connection refused"';
  const status = wrapperUnreachable(new ApiError(503, 'wrapper_unavailable', raw));
  assert.equal(status.address, '127.0.0.1:8080');
  const msg = wrapperErrorText(new ApiError(503, 'wrapper_unavailable', raw), 'status');
  assert.equal(msg.message, 'The Apple Music session service (wrapper-manager) at 127.0.0.1:8080 is not reachable.');
  assert.equal(msg.detail, raw, 'the raw text stays in the details');
  const login = wrapperErrorText(new ApiError(502, 'upstream_error', raw), 'login');
  assert.match(login.message, /session service/, 'a gRPC Unavailable behind a 502 reads the same');
  assert.equal(wrapperUnreachable(new ApiError(502, 'upstream_error', 'some other failure')), null);
  assert.equal(wrapperUnreachable(new ApiError(503, 'feature_unavailable', 'no wrapper')), null, 'no wrapper connection is a different state');
  assert.equal(wrapperUnreachable(new ApiError(0, 'network_error', 'Cannot reach the server.')), null, 'the console server itself being down is not the wrapper');
  assert.equal(wrapperErrorText(new ApiError(503, 'wrapper_unavailable', 'dns lookup failed'), 'status').message, 'The Apple Music session service (wrapper-manager) is not reachable.');
});
