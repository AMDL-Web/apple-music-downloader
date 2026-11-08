import test from 'node:test';
import assert from 'node:assert/strict';
import { FIXED_ROWS, factRows, chipOf, refreshKeys } from '../static/js/lib/facts.js';

const full = {
  version: 'v2.0.0',
  simulate: false,
  default_storefront: 'us',
  jobs: { workers: 2, active: 1, queue_depth: 0, queue_capacity: 1000, recovery_backlog: 0 },
  pools: { downloads: 4, decrypts: 8, wrapper_requests: 24 },
  tools: [{ name: 'ffmpeg', available: true, path: '/usr/bin/ffmpeg' }],
  startup: {
    wrapper_address: 'wrapper:20020',
    listen: ':8080',
    database_path: 'data/amdl.db',
    log_buffer_size: 5000,
    catalog_language: 'en-US',
    max_parallel_requests: 4,
  },
};
const zero = {
  simulate: false,
  default_storefront: 'us',
  jobs: { workers: 0, active: 0, queue_depth: 0, queue_capacity: 0, recovery_backlog: 0 },
  pools: { downloads: 0, decrypts: 0, wrapper_requests: 0 },
  tools: [{ name: 'ffmpeg', available: false }],
  startup: { wrapper_address: 'w', listen: 'l', database_path: 'd', log_buffer_size: 0, catalog_language: 'en', max_parallel_requests: 1 },
};
const byId = (id) => FIXED_ROWS.find((r) => r.id === id);

test('no row has an own property named key', () => {
  for (const row of FIXED_ROWS) assert.equal(Object.hasOwn(row, 'key'), false, row.id);
});

test('every read is null without a system; only hooks_file answers an empty one', () => {
  for (const row of FIXED_ROWS) {
    assert.equal(row.read(undefined), null, row.id);
    assert.equal(row.read(null), null, row.id);
    if (row.id !== 'hooks_file') assert.equal(row.read({}), null, row.id);
  }
});

test('no row yields an empty or dash text', () => {
  for (const system of [full, zero]) {
    for (const row of FIXED_ROWS) {
      const v = row.read(system);
      if (v) assert.ok(v.text && v.text !== '—', `${row.id}: ${v && v.text}`);
    }
  }
  assert.equal(byId('decrypts').read(zero).text, '0');
});

// Keys the API can change are fields, not facts: only the built-in pools and
// the three file-only keys are read-only rows (the wrapper row only while amdl
// runs the wrapper).
test('fact rows name only keys the API cannot change', () => {
  const keys = FIXED_ROWS.map((r) => r.configKey).filter(Boolean);
  assert.deepEqual(keys, ['wrapper.address', 'server.listen', 'database.path', 'tools.ffmpeg']);
});

test('values from a full system', () => {
  assert.equal(byId('wrapper_address').read(full), null);
  assert.deepEqual(byId('ffmpeg').read(full), { text: '/usr/bin/ffmpeg', mono: true });
  assert.deepEqual(byId('decrypts').read(full), { text: '8' });
  assert.equal(byId('decrypts').title, 'One per CPU');
});

test('factRows keeps table order and drops null rows', () => {
  assert.deepEqual(factRows('jobs', full).map((r) => r.id), ['decrypts', 'wrapper_requests']);
  assert.deepEqual(factRows('server', full).map((r) => r.id), ['listen', 'database_path', 'ffmpeg']);
  assert.deepEqual(factRows('jobs', { pools: { decrypts: 3 } }).map((r) => r.id), ['decrypts']);
  assert.deepEqual(factRows('jobs', undefined), []);
  assert.deepEqual(factRows('hooks', full)[0].value, { text: 'hooks.yaml', mono: true });
  assert.equal(factRows('jobs', full)[0].value.text, '8');
});

test('factRows while pending: every row of the section, each with an empty value', () => {
  assert.deepEqual(
    factRows('jobs', undefined, true).map((r) => [r.id, r.value.text]),
    ['decrypts', 'wrapper_requests'].map((id) => [id, '']),
  );
  assert.deepEqual(factRows('quality', undefined, true), []);
});

test('ffmpeg missing is danger, neutral in simulate, plain Available without a path', () => {
  assert.deepEqual(byId('ffmpeg').read(zero), { text: 'Missing', role: 'danger' });
  assert.deepEqual(byId('ffmpeg').read({ ...zero, simulate: true }), { text: 'Missing', role: 'neutral' });
  assert.deepEqual(byId('ffmpeg').read({ tools: [{ name: 'ffmpeg', available: true }] }), { text: 'Available' });
  assert.equal(byId('ffmpeg').read({ tools: [] }), null);
});

test('chipOf per kind', () => {
  assert.deepEqual(chipOf(byId('listen')), { label: 'config.yaml', icon: 'lock', title: 'Set in configs/config.yaml (server.listen); restart to change' });
  assert.deepEqual(chipOf(byId('decrypts')), { label: 'Built in', icon: '', title: 'One per CPU' });
  assert.equal(chipOf(byId('wrapper_requests')).title, 'Fixed by the server');
  assert.deepEqual(chipOf(byId('hooks_file')), { label: 'At startup', icon: 'lock', title: 'Read at startup; restart to change' });
});

test('refreshKeys: de-duplicated in first-seen order', () => {
  assert.deepEqual(refreshKeys(['simulate.enabled', 'download.temp_dir', 'library_sync.enabled', 'download.cover_size']), ['system', 'librarySync']);
  assert.deepEqual(refreshKeys(['library_sync.enabled', 'simulate.enabled']), ['librarySync', 'system']);
  assert.deepEqual(refreshKeys([]), []);
  assert.deepEqual(refreshKeys(['download.cover_size']), []);
});

test('a wrapper amdl runs is not a config.yaml address', () => {
  const managed = { logged_in: true, managed: { mode: 'serving', accel: 'tcg' } };
  const [row] = factRows('account', full, false, managed).filter((r) => r.id === 'wrapper_address');
  assert.deepEqual(row.value, { text: 'This machine, QEMU (tcg)' });
  assert.equal(row.title, 'wrapper:20020');
  assert.equal(chipOf(row).label, 'Run by amdl');
  // A manager run natively has no QEMU to name.
  const native = { logged_in: true, managed: { mode: 'serving', accel: 'native', qemu: '' } };
  const [nrow] = factRows('account', full, false, native).filter((r) => r.id === 'wrapper_address');
  assert.deepEqual(nrow.value, { text: 'This machine, native' });
  // Otherwise the address is the wrapper.address field, and no row holds its place.
  assert.equal(factRows('account', full, false, { logged_in: true }).find((r) => r.id === 'wrapper_address'), undefined);
  assert.equal(factRows('account', full, true, managed).find((r) => r.id === 'wrapper_address'), undefined);
});
