import test from 'node:test';
import assert from 'node:assert/strict';
import { paletteItems, jobItem, resultCount } from '../static/js/lib/palette.js';
import { newDownloadUrls, parseHash, settingsField } from '../static/js/lib/route.js';

const labels = (items) => items.map((it) => it.label);

test('an Apple Music link offers to download it, first', () => {
  const url = 'https://music.apple.com/us/album/reputation/1340651075';
  const items = paletteItems(url);
  assert.equal(items[0].label, 'Download this link');
  const route = parseHash(items[0].hash);
  assert.equal(route.page, 'new');
  assert.deepEqual(newDownloadUrls(route.params), [url]);
  const two = paletteItems(`${url} https://music.apple.com/us/album/blonde/1146195596`);
  assert.equal(two[0].label, 'Download these 2 links');
  assert.equal(newDownloadUrls(parseHash(two[0].hash).params).length, 2);
  assert.ok(!labels(paletteItems('reputation')).includes('Download this link'), 'plain words are not a link');
});

test('Settings sections and fields are findable by label, id, key and what their panels do', () => {
  const sectionOf = (q) => paletteItems(q).filter((it) => it.group === 'Settings').map((it) => [it.label, parseHash(it.hash).params.section]);
  assert.deepEqual(sectionOf('logging')[0], ['Settings: Logging', 'logging']);
  assert.ok(sectionOf('files').some(([l, s]) => l === 'Settings: Files and paths' && s === 'files'));
  assert.ok(sectionOf('reload').some(([, s]) => s === 'config_file'), 'Reload from file lives in Configuration file');
  const attempts = paletteItems('max_attempts').find((it) => it.label === 'Attempts per step');
  assert.ok(attempts, 'a field is found by its config key');
  assert.equal(parseHash(attempts.hash).params.section, 'downloads');
  assert.match(attempts.sub, /download\.max_attempts/);
  assert.ok(paletteItems('cover').some((it) => it.label === 'Cover size'));
});

test('the empty palette keeps its short list', () => {
  const items = paletteItems('');
  const settings = items.filter((it) => it.group === 'Settings').map((it) => it.label);
  assert.deepEqual(settings, ['Settings: Apple Music account', 'Settings: Library sync']);
  assert.ok(labels(items).includes('Use dark theme'));
});

test('job rows carry status and date, so two runs of one album differ', () => {
  const a = jobItem({ id: 'a', type: 'album', title: 'folklore', artist_name: 'Taylor Swift', status: 'completed', updated_at: '2026-10-03T10:00:00Z' });
  const b = jobItem({ id: 'b', type: 'album', title: 'folklore', artist_name: 'Taylor Swift', status: 'failed', updated_at: '2026-10-20T10:00:00Z' });
  assert.notEqual(a.sub, b.sub);
  assert.match(a.sub, /Completed/);
  assert.match(b.sub, /Failed/);
  assert.match(a.sub, /Oct 3/);
  assert.equal(parseHash(a.hash).jobId, 'a');
});

test('result counts read as a sentence', () => {
  assert.equal(resultCount(0), 'No results');
  assert.equal(resultCount(1), '1 result');
  assert.equal(resultCount(7), '7 results');
});

test('a Settings field result names its field, so the page can focus it', () => {
  const attempts = paletteItems('attempts').find((it) => it.id === 'field-download.max_attempts');
  assert.equal(attempts.field, 'download.max_attempts');
  const route = parseHash(attempts.hash);
  assert.equal(route.params.section, 'downloads');
  assert.equal(settingsField(route.params), 'download.max_attempts');
  assert.equal(settingsField({ field: 'Bad Value' }), '');
  assert.equal(settingsField({}), '');
  const section = paletteItems('logging').find((it) => it.id === 'set-logging');
  assert.equal(parseHash(section.hash).params.field, undefined, 'a section result names no field');
});

test('parallel and concurrency find where worker pools live, and template finds the path templates', () => {
  for (const q of ['concurrency', 'parallel', 'threads', 'workers']) {
    const hit = paletteItems(q).find((it) => it.id === 'set-config_file');
    assert.ok(hit, `${q} finds Configuration file`);
    assert.match(hit.sub, /Worker pools/);
  }
  const templates = paletteItems('template').filter((it) => it.field);
  assert.ok(templates.length >= 5, 'every path template field answers to "template"');
  assert.ok(templates.every((it) => it.field.endsWith('_path_format')));
  assert.ok(paletteItems('retries').some((it) => it.field === 'download.max_attempts'));
  assert.ok(paletteItems('folder').some((it) => it.field === 'download.downloads_dir'));
});
