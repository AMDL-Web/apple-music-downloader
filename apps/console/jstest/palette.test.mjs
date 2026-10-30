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
  assert.deepEqual(sectionOf('logging')[0], ['Settings: Logs', 'logging']);
  assert.ok(sectionOf('files').some(([l, s]) => l === 'Settings: Storage & files' && s === 'files'));
  assert.ok(sectionOf('quality').some(([l, s]) => l === 'Settings: Quality' && s === 'quality'));
  assert.ok(sectionOf('reload').some(([, s]) => s === 'config_file'), 'Reload from file lives in Configuration file');
  const group = paletteItems('reload').find((it) => it.id === 'set-config_file');
  assert.equal(group.sub, 'Server', 'a sub-group names its section');
  const attempts = paletteItems('download.max_attempts').find((it) => it.label === 'Tries per file');
  assert.ok(attempts, 'a field is found by its config key');
  assert.equal(parseHash(attempts.hash).params.section, 'jobs');
  assert.equal(attempts.sub, 'Jobs', 'the key is searchable but not shown');
  const simulate = paletteItems('simulate.enabled').find((it) => it.field === 'simulate.enabled');
  assert.equal(simulate.sub, 'Server › Simulate');
  assert.ok(paletteItems('cover').some((it) => it.label === 'Cover size'));
});

test('System is part of Settings: no System row, and its words find Settings', () => {
  assert.ok(!paletteItems('').some((it) => it.id === 'nav-system'));
  for (const q of ['system', 'health', 'status']) {
    assert.ok(paletteItems(q).some((it) => it.id === 'nav-settings'), `${q} finds Settings`);
  }
  const sections = paletteItems('settings').filter((it) => it.id.startsWith('set-') && !it.sub);
  assert.equal(sections.length, 9, 'one row per section');
});

test('the empty palette keeps its short list', () => {
  const items = paletteItems('');
  const settings = items.filter((it) => it.group === 'Settings').map((it) => it.label);
  assert.deepEqual(settings, ['Settings: Account', 'Settings: Library sync']);
  assert.ok(labels(items).includes('Use dark theme'));
});

test('job rows carry status and date, so two runs of one album differ', () => {
  const a = jobItem({ id: 'a', type: 'album', title: 'folklore', artist_name: 'Taylor Swift', status: 'completed', updated_at: '2026-10-03T10:00:00Z' });
  const b = jobItem({ id: 'b', type: 'album', title: 'folklore', artist_name: 'Taylor Swift', status: 'failed', updated_at: '2026-10-20T10:00:00Z' });
  assert.notEqual(a.sub, b.sub);
  assert.match(a.sub, /Completed/);
  assert.match(b.sub, /Failed/);
  assert.match(a.sub, /Oct 3 \d\d:\d\d/);
  assert.equal(parseHash(a.hash).jobId, 'a');
  // Two runs on one day differ by their time.
  const c = jobItem({ id: 'c', type: 'album', title: 'Blonde', status: 'completed', updated_at: '2026-10-03T10:01:00Z' });
  const d = jobItem({ id: 'd', type: 'album', title: 'Blonde', status: 'completed', updated_at: '2026-10-03T10:12:00Z' });
  assert.notEqual(c.sub, d.sub);
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
  assert.equal(route.params.section, 'jobs');
  assert.equal(settingsField(route.params), 'download.max_attempts');
  assert.equal(settingsField({ field: 'Bad Value' }), '');
  assert.equal(settingsField({}), '');
  const section = paletteItems('logging').find((it) => it.id === 'set-logging');
  assert.equal(parseHash(section.hash).params.field, undefined, 'a section result names no field');
});

test('parallel and concurrency find where worker pools live, and template finds the path templates', () => {
  for (const q of ['concurrency', 'parallel', 'threads', 'workers']) {
    assert.ok(paletteItems(q).some((it) => it.id === 'set-jobs'), `${q} finds Jobs, which shows the pools`);
  }
  const templates = paletteItems('template').filter((it) => it.field);
  assert.ok(templates.length >= 5, 'every path template field answers to "template"');
  assert.ok(templates.every((it) => it.field.endsWith('_path_format')));
  const retries = paletteItems('retries').find((it) => it.group === 'Settings');
  assert.equal(retries.field, 'download.max_attempts', 'the field is the first Settings result');
  assert.equal(retries.label, 'Tries per file');
  assert.equal(parseHash(retries.hash).params.section, 'jobs');
  assert.ok(paletteItems('folder').some((it) => it.field === 'download.downloads_dir'));
});

test('words offer a catalog search on New download; links do not', () => {
  const search = (q) => paletteItems(q).find((it) => it.id === 'act-search');
  const item = search('  Taylor   Swift ');
  assert.equal(item.label, 'Search Apple Music for “Taylor Swift”');
  assert.equal(item.group, 'Apple Music');
  assert.deepEqual(parseHash(item.hash), { page: 'new', jobId: null, params: { q: 'Taylor Swift' } });
  assert.equal(search('https://music.apple.com/us/album/igor/1463409338'), undefined);
  assert.equal(search('x'.repeat(201)), undefined, 'longer than the backend searches');
  assert.equal(search(''), undefined);
  // Matching pages and settings come first: Return still opens them.
  assert.equal(paletteItems('queue')[0].label, 'Queue');
});
