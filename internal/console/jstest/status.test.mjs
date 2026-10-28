import test from 'node:test';
import assert from 'node:assert/strict';
import {
  jobCounts,
  isAlreadyInLibrary,
  jobStatusView,
  itemStatusView,
  hookStatusView,
  backendHealthView,
  wrapperView,
  summarizeError,
  librarySyncView,
  hooksView,
  logLevelView,
  connectionView,
  jobProgress,
  stageSegments,
  stageText,
  isTerminal,
} from '../static/js/lib/status.js';

const ROLES = new Set(['info', 'success', 'danger', 'warning', 'neutral', 'skipped']);

test('every job status has an icon, a role and a label', () => {
  for (const status of ['queued', 'running', 'completed', 'failed', 'cancelled', 'mystery']) {
    const v = jobStatusView({ status, total_items: 1 });
    assert.ok(v.icon, status);
    assert.ok(ROLES.has(v.role), status);
    assert.ok(v.label, status);
  }
});

test('job status views', () => {
  assert.deepEqual(jobStatusView({ status: 'queued' }), { icon: 'circle-dashed', role: 'neutral', label: 'Queued' });
  const running = jobStatusView({ status: 'running', total_items: 0 });
  assert.equal(running.role, 'info');
  assert.equal(running.pulse, true);
  assert.equal(running.detail, 'Resolving');
  const failed = jobStatusView({ status: 'failed', error: 'wrapper unavailable' });
  assert.equal(failed.role, 'danger');
  assert.equal(failed.detail, 'wrapper unavailable');
  assert.equal(jobStatusView({ status: 'cancelled' }).role, 'neutral');
});

test('a completed job whose tracks were all on disk is "Already in library"', () => {
  const job = { status: 'completed', total_items: 12, completed_items: 0, skipped_items: 12, failed_items: 0 };
  assert.equal(isAlreadyInLibrary(job), true);
  const v = jobStatusView(job);
  assert.equal(v.label, 'Already in library');
  assert.equal(v.role, 'skipped');

  const partial = { status: 'completed', total_items: 12, completed_items: 10, skipped_items: 2 };
  assert.equal(isAlreadyInLibrary(partial), false);
  assert.equal(jobStatusView(partial).label, 'Completed');
  assert.equal(jobStatusView(partial).detail, '2 already on disk');
  assert.equal(isAlreadyInLibrary({ status: 'completed', total_items: 0, skipped_items: 0 }), false);
});

test('jobCounts defaults missing counters to zero', () => {
  assert.deepEqual(jobCounts({ total_items: 3, completed_items: 1 }), { total: 3, completed: 1, skipped: 0, failed: 0, cancelled: 0, done: 1 });
});

test('item status views', () => {
  assert.deepEqual(itemStatusView({ status: 'waiting_download' }), { icon: 'clock', role: 'neutral', label: 'Waiting for download slot' });
  assert.equal(itemStatusView({ status: 'waiting_decrypt' }).label, 'Waiting for decrypt slot');
  assert.equal(itemStatusView({ status: 'decrypting' }).role, 'info');
  assert.equal(itemStatusView({ status: 'decrypting' }).label, 'Decrypting');
  assert.deepEqual(itemStatusView({ status: 'skipped_existing' }), { icon: 'archive', role: 'skipped', label: 'Already on disk' });
  assert.equal(itemStatusView({ status: 'failed', error: 'boom' }).detail, 'boom');
});

test('a retried item carries a retry overlay until it finishes', () => {
  assert.equal(itemStatusView({ status: 'downloading', attempt: 2, max_attempts: 3 }).retry, 'Retry 2/3');
  assert.equal(itemStatusView({ status: 'downloading', attempt: 1, max_attempts: 3 }).retry, undefined);
  assert.equal(itemStatusView({ status: 'completed', attempt: 2, max_attempts: 3 }).retry, undefined);
});

test('hook status views', () => {
  assert.equal(hookStatusView('running').role, 'info');
  assert.equal(hookStatusView('succeeded').role, 'success');
  assert.equal(hookStatusView('failed').role, 'danger');
  assert.equal(hookStatusView('interrupted').role, 'warning');
});

test('health rows', () => {
  assert.equal(backendHealthView({ data: { status: 'ok' } }).label, 'Healthy');
  const degraded = backendHealthView({ error: { status: 503, message: 'x', body: { status: 'degraded', database_error: 'disk I/O error' } } });
  assert.equal(degraded.label, 'Degraded');
  assert.equal(degraded.detail, 'disk I/O error');
  assert.equal(backendHealthView({ error: { status: 0, message: 'Cannot reach the server.' } }).label, 'Unreachable');

  assert.deepEqual(wrapperView({ data: { ready: true, status: true, regions: ['us', 'jp'], client_count: 2, accounts: [] } }), {
    icon: 'circle-check',
    role: 'success',
    label: 'Ready',
    detail: '2 clients · US, JP',
  });
  assert.equal(wrapperView({ data: { ready: false, status: true, regions: [], client_count: 0, accounts: [] } }).detail, 'No account logged in');
  assert.equal(wrapperView({ error: { status: 503, message: 'unreachable' } }).label, 'Unreachable');

  assert.equal(librarySyncView({ error: { status: 503 } }).label, 'Not configured');
  assert.equal(librarySyncView({ data: { enabled: false } }).label, 'Off');
  assert.equal(librarySyncView({ data: { enabled: true, last_error: 'no token' } }).role, 'warning');
  assert.equal(librarySyncView({ data: { enabled: true, interval_minutes: 30 } }).detail, 'Every 30 min');

  assert.equal(hooksView({ data: { enabled: false, entries: [] } }).label, 'Off');
  assert.equal(hooksView({ data: { enabled: true, entries: [] } }).label, 'None configured');
  assert.equal(hooksView({ data: { enabled: true, entries: [{ enabled: true }, { enabled: false }] } }).detail, '1 of 2 enabled');
});

test('log levels and connection states', () => {
  assert.deepEqual(logLevelView('warn'), { short: 'WRN', role: 'warning', label: 'Warn' });
  assert.equal(logLevelView('error').short, 'ERR');
  assert.equal(connectionView('live').label, 'Live');
  assert.equal(connectionView('reconnecting').role, 'warning');
  assert.equal(connectionView('offline').label, 'Disconnected');
});

test('jobProgress counts tracks and never reports a percentage', () => {
  const p = jobProgress({ status: 'running', total_items: 14, completed_items: 11, skipped_items: 1, failed_items: 1 });
  assert.equal(p.text, '12 / 14');
  assert.equal(p.aria, '12 of 14 tracks, 1 already on disk, 1 failed');
  assert.equal(p.indeterminate, false);
  assert.ok(!p.text.includes('%'));
  const resolving = jobProgress({ status: 'running', total_items: 0 });
  assert.equal(resolving.indeterminate, true);
  assert.equal(resolving.text, 'Resolving');
});

test('stage segments follow ItemProgress', () => {
  const states = (item) => stageSegments(item).map((s) => s.state);
  assert.deepEqual(states({ status: 'queued', progress: {} }), ['empty', 'empty', 'empty', 'empty', 'empty', 'empty', 'empty']);

  const unknownSize = stageSegments({ status: 'downloading', progress: { resolved: true, download: 0 } });
  assert.equal(unknownSize[1].state, 'indeterminate');

  const half = stageSegments({ status: 'downloading', progress: { resolved: true, download: 0.45 } });
  assert.equal(half[0].state, 'done');
  assert.equal(half[1].state, 'partial');
  assert.equal(half[1].fraction, 0.45);

  const unverified = stageSegments({
    status: 'completed',
    progress: { resolved: true, download: 1, decrypt: 1, remuxed: true, verified: false, tagged: true, saved: true },
  });
  assert.equal(unverified[4].state, 'unverified');
  assert.deepEqual(unverified.filter((s) => s.key !== 'verify').map((s) => s.state), ['done', 'done', 'done', 'done', 'done', 'done']);

  assert.deepEqual(states({ status: 'skipped_existing', progress: { resolved: true } }), ['done', 'empty', 'empty', 'empty', 'empty', 'empty', 'empty']);
  assert.match(stageText(half), /Download: 45%/);
  assert.match(stageText(unverified), /Verify: not verified/);
});

test('isTerminal', () => {
  assert.equal(isTerminal('completed'), true);
  assert.equal(isTerminal('failed'), true);
  assert.equal(isTerminal('cancelled'), true);
  assert.equal(isTerminal('running'), false);
});

test('summarizeError turns gRPC dial chains into one line and keeps the full text as title', () => {
  const raw = 'rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:8080: connect: connection refused"';
  assert.equal(summarizeError(raw), 'Connection refused (127.0.0.1:8080)');
  assert.equal(summarizeError('rpc error: code = DeadlineExceeded desc = context deadline exceeded'), 'Timed out');
  assert.equal(summarizeError('rpc error: code = Internal desc = boom'), 'boom');
  assert.equal(summarizeError('x'.repeat(100)).length, 80);
  assert.equal(summarizeError(undefined), undefined);
  const view = wrapperView({ error: { status: 503, message: raw } });
  assert.equal(view.detail, 'Connection refused (127.0.0.1:8080)');
  assert.equal(view.title, raw);
});
