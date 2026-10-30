import test from 'node:test';
import assert from 'node:assert/strict';
import { domainView, domainLabel, attentionRows, librarySyncState } from '../static/js/lib/domains.js';
import { SETTINGS_SECTIONS, BOARD_SECTIONS } from '../static/js/lib/settings-sections.js';

const NOW = Date.parse('2026-10-30T12:00:00Z');
const ok = (data) => ({ data, error: undefined, at: NOW, okAt: NOW });
const fail = (status, extra = {}) => ({ data: undefined, error: { status, message: 'boom', ...extra }, at: NOW, okAt: 0 });
const GB = 1e9;
const disk = (free, total = 500 * GB, name = 'downloads_dir') => ({ name, path: `/data/${name}`, free_bytes: free, total_bytes: total });
const ctx = (over = {}) => ({ now: NOW, counts: { running: 2, queued: 5, completed: 0, failed: 0, cancelled: 0, total: 7 }, conn: { state: 'live' }, simulate: false, ...over });
const system = (over = {}) => ({
  version: 'v2.0.0',
  go_version: 'go1.26.10',
  os: 'linux',
  arch: 'amd64',
  started_at: '2026-10-27T08:00:00Z',
  simulate: false,
  default_storefront: 'us',
  jobs: { workers: 3, active: 2, queue_depth: 0, queue_capacity: 256, recovery_backlog: 0 },
  pools: { downloads: 16, decrypts: 8, wrapper_requests: 24 },
  tools: [{ name: 'ffmpeg', available: true, path: '/usr/bin/ffmpeg' }],
  storage: [disk(200 * GB), disk(200 * GB, 500 * GB, 'temp_dir')],
  ...over,
});
const config = (over = {}) => ok({ config: { catalog: { media_user_token: 'tok' }, download: { quality_priority: ['alac', 'ec3', 'aac'] }, ...over }, persisted: true });

test('every domain renders a view; loading before an answer, unavailable without data, missing on 404', () => {
  for (const s of BOARD_SECTIONS) {
    assert.equal(domainView(s.id, {}, ctx({ counts: null })).state, 'loading', s.id);
  }
  assert.equal(domainView('lyrics_artwork', {}, ctx()), null, 'no status for Lyrics & artwork');
  const failed = domainView('files', { system: fail(500) }, ctx());
  assert.equal(failed.state, 'failed');
  assert.equal(failed.tag.label, 'Unavailable');
  assert.equal(failed.role, null, 'unknown is not a fault');
  assert.equal(failed.figure.value, "Couldn't read status");
  const missing = domainView('hooks', { hooks: fail(404) }, ctx());
  assert.equal(missing.state, 'missing');
  assert.equal(missing.tag.label, 'Not on this server');
});

test('a failed read after data keeps the values and says when they are from', () => {
  const stale = { ...ok(system()), error: { status: 0, message: 'offline' }, at: NOW, okAt: NOW - 60000 };
  const v = domainView('files', { system: stale }, ctx());
  assert.equal(v.state, 'ok');
  assert.equal(v.figure.value, '200 GB');
  assert.equal(v.asOf, NOW - 60000);
  assert.equal(domainView('files', { system: ok(system()) }, ctx({ conn: { state: 'offline' } })).asOf, NOW, 'an offline feed makes every head stale');
});

test('disk thresholds: warning at 85 %, danger at 95 %, the fullest disk wins', () => {
  const role = (usedPct) => domainView('files', { system: ok(system({ storage: [disk((1 - usedPct / 100) * 500 * GB)] })) }, ctx()).role;
  assert.equal(role(84), null);
  assert.equal(role(85), 'warning');
  assert.equal(role(94), 'warning');
  assert.equal(role(95), 'danger');
  const two = domainView('files', { system: ok(system({ storage: [disk(400 * GB), disk(10 * GB, 100 * GB, 'temp_dir')] })) }, ctx());
  assert.equal(two.figure.value, '10.0 GB', 'the head shows the fullest disk');
  assert.match(two.note.text, /^Downloads folder: 400 GB free on another disk$/, 'the other disk changes the note, not the rows');
  const unreadable = domainView('files', { system: ok(system({ storage: [{ name: 'downloads_dir', path: '/x', error: 'permission denied' }] })) }, ctx());
  assert.equal(unreadable.role, 'danger');
  assert.equal(unreadable.tag.label, "Can't read folder");
});

test('queue thresholds set the Jobs role and note; backlog wins the note', () => {
  const jobs = (depth, backlog = 0) => domainView('jobs', { system: ok(system({ jobs: { workers: 3, queue_depth: depth, queue_capacity: 100, recovery_backlog: backlog } })) }, ctx());
  assert.equal(jobs(59).role, null);
  assert.equal(jobs(60).role, 'warning');
  assert.equal(jobs(60).note.text, 'Queue 60 of 100 slots');
  assert.equal(jobs(89).role, 'warning');
  assert.equal(jobs(90).role, 'danger');
  assert.equal(jobs(90, 4).note.text, 'Recovering 4 jobs from the last run');
  assert.equal(jobs(90, 4).role, 'danger', 'the backlog changes the note only');
  const v = jobs(0);
  assert.equal(v.figure.value, '2/3');
  assert.equal(v.figure.second, '5 queued');
  assert.deepEqual(v.viz, { kind: 'cells', mode: 'cells', total: 3, filled: 2 });
  assert.equal(domainView('jobs', { system: ok(system()) }, ctx({ counts: { running: 0, queued: 0 } })).figure.second, undefined, 'no "0 queued"');
  assert.equal(domainView('jobs', { system: ok(system()) }, ctx({ conn: { state: 'offline', downSince: NOW - 90000 } })).tag.label, 'Paused');
});

test('hooks: a failure in the last 24 h warns, never danger; 23 h 59 m vs 24 h', () => {
  const hooks = ok({ enabled: true, entries: [{ name: 'notify', enabled: true, type: 'webhook', events: ['job_finished'], job_types: [] }] });
  const stats = (ago) => ok({ totals: {}, daily: [], codecs: [], hooks: [{ name: 'notify', succeeded: 19, failed: 1, last_failed_at: new Date(NOW - ago).toISOString() }] });
  const at = (ago) => domainView('hooks', { hooks, stats: stats(ago) }, ctx());
  assert.equal(at(24 * 3600e3 - 60e3).role, 'warning');
  assert.equal(at(24 * 3600e3 - 60e3).tag.label, 'Failed recently');
  assert.equal(at(24 * 3600e3).role, null);
  assert.equal(at(24 * 3600e3).tag.label, '1 on');
  assert.equal(at(3600e3).figure.value, '95%');
  assert.match(at(3600e3).note.text, /^Last failure: notify · 1h ago$/);
  assert.equal(domainView('hooks', { hooks: ok({ enabled: false, entries: [] }) }, ctx()).tag.label, 'Off in hooks.yaml');
  assert.equal(domainView('hooks', { hooks: ok({ enabled: true, entries: [] }) }, ctx()).figure.value, 'None');
});

test('library sync: Off, Needs token (its error not repeated), Error, Watching', () => {
  const sync = (over = {}) => ok({ enabled: true, interval_minutes: 15, anchor_size: 412, last_submitted: 0, total_submitted: 37, last_poll_at: new Date(NOW - 6 * 60e3).toISOString(), ...over });
  const view = (s, cfg = config()) => domainView('library_sync', { librarySync: s, config: cfg }, ctx());
  assert.equal(view(sync({ enabled: false })).tag.label, 'Off');
  assert.equal(view(sync({ enabled: false })).note.text, '412 tracks recorded');
  const needs = view(sync({ last_error: 'catalog.media_user_token is empty' }), config({ catalog: { media_user_token: '' } }));
  assert.equal(needs.tag.label, 'Needs token');
  assert.equal(needs.role, 'warning');
  assert.equal(needs.note.text, 'Needs the media user token', 'the raw error is suppressed');
  assert.equal(needs.note.link.label, 'Set token');
  const err = view(sync({ last_error: 'boom' }));
  assert.equal(err.tag.label, 'Error');
  assert.equal(err.role, 'danger');
  const watching = view(sync());
  assert.equal(watching.tag.label, 'Watching');
  assert.equal(watching.note.text, 'Next check in 9 min · 37 albums since start');
  assert.equal(watching.viz.fraction, 0.4);
  assert.equal(view(sync({ last_poll_at: undefined })).note.text, 'First check within 15 min · 37 albums since start');
  assert.match(view(sync({ last_poll_at: new Date(NOW - 20 * 60e3).toISOString() })).note.text, /^Checking…/);
  assert.equal(view(sync({ anchor_size: 0 })).figure.value, 'Not anchored yet');
  // Overview shares the state function; an unread config counts as a set token.
  assert.equal(librarySyncState({ enabled: true }, undefined).label, 'Watching');
});

test('account: Ready with storefronts, a warning when the default is not served, Unreachable, simulate neutral', () => {
  const wrapper = (over = {}) => ok({ ready: true, logged_in: true, regions: ['cn', 'us'], accounts: ['a@example.com'], ...over });
  const view = (w, c = ctx(), sys = system()) => domainView('account', { wrapper: w, system: ok(sys) }, c);
  const ready = view(wrapper());
  assert.equal(ready.tag.label, 'Ready');
  assert.deepEqual([ready.figure.value, ready.figure.unit], ['2', 'storefronts']);
  assert.equal(ready.note, null, 'the default storefront is a fact row, said once');
  assert.deepEqual(ready.viz.items, [{ label: 'CN' }, { label: 'US' }]);
  const unserved = view(wrapper(), ctx(), system({ default_storefront: 'jp' }));
  assert.equal(unserved.role, 'warning');
  assert.equal(unserved.note.text, "Default storefront JP isn't served by this account");
  assert.equal(view(wrapper({ ready: false, logged_in: false, regions: [] })).tag.label, 'Not logged in');
  const raw = 'rpc error: code = Unavailable desc = dial tcp 127.0.0.1:1: connect: connection refused';
  const down = view({ data: undefined, error: { status: 503, code: 'wrapper_unavailable', message: raw }, at: NOW, okAt: 0 });
  assert.equal(down.tag.label, 'Unreachable');
  assert.equal(down.role, 'danger');
  assert.equal(down.note.text, 'Connection refused (127.0.0.1:1)');
  assert.equal(down.note.title, raw);
  const sim = view({ data: undefined, error: { status: 503, code: 'wrapper_unavailable', message: raw }, at: NOW, okAt: 0 }, ctx({ simulate: true }));
  assert.equal(sim.role, null, 'simulate jobs never call the session service');
  assert.equal(sim.tag.label, 'Not used in simulate');
  assert.equal(sim.note, null);
});

test('account: a wrapper amdl runs says what it does instead of serving, never "No account"', () => {
  const managed = (mode, over = {}) =>
    ok({ ready: false, logged_in: false, regions: [], accounts: [], login_supported: true, managed: { mode, accel: 'tcg', accel_fallback: false, qemu: 'qemu-system-x86_64', guest: 'abc', login_residue: false, ...over } });
  const view = (w) => domainView('account', { wrapper: w, system: ok(system()) }, ctx());
  const starting = view(managed('starting'));
  assert.equal(starting.tag.label, 'Starting');
  assert.equal(starting.role, null, 'a boot is nothing to act on');
  assert.equal(view(managed('login', { login_residue: true })).tag.label, 'Logging in');
  const stopped = view(managed('stopped'));
  assert.equal(stopped.role, 'warning');
  assert.equal(stopped.note.text, 'Start it with amdl wrapper start');
  const raw = 'qemu-system-x86_64: failed to initialize kvm: Permission denied';
  const failed = view(managed('failed', { last_error: raw }));
  assert.equal(failed.role, 'danger');
  assert.equal(failed.note.title, raw);
  assert.equal(view(managed('failed')).note.text, 'See amdl wrapper logs');
  assert.equal(view(managed('serving')).tag.label, 'Not logged in', 'only a serving guest without an account has none');
  assert.equal(view(managed('rebooting')).tag.label, 'Not logged in', 'an unknown mode reads as before');
  const rows = (w) => attentionRows({ wrapper: w, system: ok(system()), health: ok({ status: 'ok' }) }, ctx(), SETTINGS_SECTIONS).map((r) => r.text);
  assert.deepEqual(rows(managed('starting')), []);
  assert.deepEqual(rows(managed('stopped')), ['stopped: start it with amdl wrapper start']);
  assert.deepEqual(rows(managed('failed', { last_error: raw })), [`failed: ${raw}`]);
});

test('quality: the first-choice share leaves aac-lc out; no tracks is words', () => {
  const stats = (codecs) => ok({ totals: { tracks_completed: 10 }, daily: [], hooks: [], codecs });
  const v = domainView('quality', { stats: stats([{ codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: 9 }, { codec: 'aac-lc', bit_depth: 0, sample_rate: 0, tracks: 1 }]), config: config() }, ctx());
  assert.equal(v.figure.value, '90%');
  assert.equal(v.figure.unit, 'first choice');
  assert.equal(v.role, null, 'Quality has no role');
  const empty = domainView('quality', { stats: stats([]), config: config() }, ctx());
  assert.equal(empty.figure.value, 'No tracks yet');
  assert.equal(empty.figure.word, true);
});

test('logs: retention off warns; no errors is words', () => {
  const off = domainView('logging', { logs: { data: undefined, error: { status: 503, code: 'feature_unavailable', message: 'off' }, at: NOW, okAt: 0 } }, ctx());
  assert.equal(off.tag.label, 'Retention off');
  assert.equal(off.role, 'warning');
  const none = domainView('logging', { logs: ok({ entries: [], cursor: 3, full: false, last: null }) }, ctx());
  assert.equal(none.figure.value, 'No errors');
  assert.equal(none.note.text, 'No errors kept');
  const one = { sequence: 4, time: new Date(NOW - 2 * 3600e3).toISOString(), level: 'error', message: 'wrapper: refused' };
  const some = domainView('logging', { logs: ok({ entries: [one], cursor: 4, full: false }) }, ctx());
  assert.equal(some.figure.value, '1');
  assert.equal(some.figure.unit, 'error · 24 h');
  assert.equal(some.note.text, 'Last error 2h ago: wrapper: refused');
});

test('server: degraded beats offline beats ffmpeg missing beats simulate', () => {
  const degraded = { data: undefined, error: { status: 503, message: 'x', body: { status: 'degraded', database_error: 'disk I/O error' } }, at: NOW, okAt: 0 };
  const noFfmpeg = system({ tools: [{ name: 'ffmpeg', available: false, error: 'not found' }] });
  const tag = (s, c = ctx()) => domainView('server', s, c).tag.label;
  assert.equal(tag({ system: ok(noFfmpeg), health: degraded }, ctx({ conn: { state: 'offline' } })), 'Degraded');
  assert.equal(tag({ system: ok(noFfmpeg), health: ok({ status: 'ok' }) }, ctx({ conn: { state: 'offline' } })), 'Offline');
  assert.equal(tag({ system: ok(noFfmpeg), health: ok({ status: 'ok' }) }), 'ffmpeg missing');
  assert.equal(tag({ system: ok(noFfmpeg), health: ok({ status: 'ok' }) }, ctx({ simulate: true })), 'Simulate');
  assert.equal(tag({ system: ok(system()), health: ok({ status: 'ok' }) }), 'Healthy');
  const v = domainView('server', { system: ok(system()), health: ok({ status: 'ok' }), config: config() }, ctx());
  assert.equal(v.figure.value, '3d 4h');
  assert.equal(v.note.text, 'v2.0.0 · go1.26.10 · linux/amd64');
  assert.deepEqual(v.viz.items.map((c) => [c.label, c.ok]), [['Database', true], ['ffmpeg', true], ['Config file', true]]);
  const unsaved = domainView('server', { system: ok(system()), health: ok({ status: 'ok' }), config: { ...config(), data: { config: {}, persisted: false } } }, ctx());
  assert.equal(unsaved.role, 'warning', 'a config that is not persisted warns');
});

test('no note, figure or summary says "0", "—" or nothing for an all-zero server', () => {
  const zero = {
    system: ok(system({ jobs: { workers: 3, queue_depth: 0, queue_capacity: 256, recovery_backlog: 0 } })),
    health: ok({ status: 'ok' }),
    wrapper: ok({ ready: true, logged_in: true, regions: [], accounts: [] }),
    librarySync: ok({ enabled: true, interval_minutes: 15, anchor_size: 0, last_submitted: 0, total_submitted: 0 }),
    hooks: ok({ enabled: true, entries: [] }),
    stats: ok({ totals: { tracks_completed: 0, bytes_written: 0, duration_ms: 0 }, daily: [], codecs: [], hooks: [] }),
    logs: ok({ entries: [], cursor: 0, full: false, last: null }),
    config: config(),
  };
  for (const s of BOARD_SECTIONS) {
    const v = domainView(s.id, zero, ctx({ counts: { running: 0, queued: 0 } }));
    const texts = [v.figure && v.figure.value, v.figure && v.figure.unit, v.figure && v.figure.second, v.note && v.note.text, v.summary && v.summary.text].filter((t) => t !== undefined && t !== null);
    for (const t of texts) {
      assert.notEqual(t, '', `${s.id}: an empty part`);
      assert.doesNotMatch(String(t), /—|(^|\s)0(\s|$)| 0 /, `${s.id}: "${t}"`);
    }
  }
  assert.equal(domainView('files', zero, ctx()).note.text, 'Nothing downloaded yet');
  assert.equal(domainView('jobs', zero, ctx({ counts: { running: 0, queued: 0 } })).summary.text, 'Idle');
  assert.equal(domainView('jobs', zero, ctx({ counts: { running: 0, queued: 0 } })).note.text, 'No downloads in 14 days');
});

test('accessible names join label, tag and figure; Overview lists only what needs action', () => {
  const v = domainView('files', { system: ok(system({ storage: [disk(25 * GB)] })) }, ctx());
  assert.equal(domainLabel(v, 'Storage & files'), 'Storage & files, Low space, 25.0 GB free of 500 GB');
  const raw = 'dial tcp 127.0.0.1:1: connect: connection refused';
  const s = {
    system: ok(system({ storage: [disk(50 * GB)] })),
    health: ok({ status: 'ok' }),
    wrapper: { data: undefined, error: { status: 503, code: 'wrapper_unavailable', message: raw }, at: NOW, okAt: 0 },
    librarySync: ok({ enabled: true, interval_minutes: 15, anchor_size: 1 }),
    config: config({ catalog: { media_user_token: '' } }),
  };
  const rows = attentionRows(s, ctx(), SETTINGS_SECTIONS);
  assert.deepEqual(rows.map((r) => [r.id, r.role, r.href]), [
    ['account', 'danger', '#/settings?section=account'],
    ['library_sync', 'warning', '#/settings?section=library_sync'],
  ], 'a disk at 90 % used is a Settings warning, not on Overview');
  assert.equal(rows[0].text, 'unreachable: Connection refused (127.0.0.1:1)');
  const full = attentionRows({ ...s, system: ok(system({ storage: [disk(10 * GB)] })) }, ctx(), SETTINGS_SECTIONS);
  assert.ok(full.some((r) => r.id === 'files' && r.role === 'danger'));
});
