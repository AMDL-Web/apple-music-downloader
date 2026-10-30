import test from 'node:test';
import assert from 'node:assert/strict';
import {
  jobCounts,
  isAlreadyInLibrary,
  jobStatusView,
  itemStatusView,
  hookStatusView,
  logLevelView,
  connectionView,
  jobProgress,
  activityView,
  runningFirst,
  stageSegments,
  STAGE_WEIGHTS,
  stageText,
  isTerminal,
  progressCells,
  progressLegend,
  CELL_MAX,
} from '../static/js/lib/status.js';
import { backendHealthView, wrapperView, summarizeError, missingToolRows, librarySyncView, hooksView } from '../static/js/lib/health.js';

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

  assert.deepEqual(wrapperView({ data: { ready: true, status: true, regions: ['us', 'jp'], accounts: ['a@example.com', 'b@example.com'] } }), {
    icon: 'circle-check',
    role: 'success',
    label: 'Ready',
    detail: 'US, JP',
  });
  // Ready without regions still says something: the accounts, else that it is signed in.
  assert.equal(wrapperView({ data: { ready: true, status: true, regions: [], accounts: ['a@example.com'] } }).detail, '1 account signed in');
  assert.equal(wrapperView({ data: { ready: true, status: true, regions: [], accounts: ['a@example.com', 'b@example.com'] } }).detail, '2 accounts signed in');
  assert.equal(wrapperView({ data: { ready: true, status: true } }).detail, 'Signed in');
  assert.equal(wrapperView({ data: { ready: false, status: false, regions: [], accounts: [] } }).detail, 'No account logged in');
  assert.equal(wrapperView({ data: { ready: false, status: true, regions: [], accounts: [] } }).detail, 'Wrapper is not ready', 'a wrapper that names no accounts is judged by status');
  // Answering without a storefront means nobody is logged in: not Ready.
  assert.deepEqual(wrapperView({ data: { ready: true, status: false, regions: [], accounts: [], login_supported: false } }), {
    icon: 'triangle-alert',
    role: 'warning',
    label: 'No account',
    detail: 'Log in so downloads can be decrypted',
  });
  assert.equal(wrapperView({ error: { status: 503, message: 'unreachable' } }).label, 'Unreachable');

  assert.equal(librarySyncView({ error: { status: 503, message: 'Service Unavailable' } }).label, 'Unavailable');
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
  assert.equal(connectionView('live').label, 'Live updates on');
  assert.equal(connectionView('reconnecting').role, 'warning');
  assert.equal(connectionView('offline').label, 'Offline');
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

test('a running job with tracks and nothing finished is working, not empty', () => {
  const song = jobProgress({ status: 'running', total_items: 1 });
  assert.equal(song.working, true);
  assert.equal(song.aria, '0 of 1 tracks, working');
  assert.equal(song.text, 'Working', 'a lone track would read 0 / 1 for the whole run');
  assert.equal(jobProgress({ status: 'running', total_items: 12 }).text, '0 / 12');
  assert.equal(jobProgress({ status: 'completed', total_items: 1, completed_items: 1 }).text, '1 / 1');
  assert.equal(jobProgress({ status: 'failed', total_items: 1 }).text, '0 / 1');
  assert.equal(jobProgress({ status: 'running', total_items: 5, completed_items: 1 }).working, false);
  assert.equal(jobProgress({ status: 'running', total_items: 0 }).working, false);
  assert.equal(jobProgress({ status: 'queued', total_items: 3 }).working, false);
  assert.equal(jobProgress({ status: 'failed', total_items: 3 }).working, false);
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

  const skipped = stageSegments({ status: 'skipped_existing', progress: { resolved: true } });
  assert.deepEqual(skipped.map((s) => s.state), Array(7).fill('skipped'), 'a track already on disk is finished, not stuck after resolve');
  assert.ok(skipped.every((s) => s.fraction === 1));
  assert.deepEqual(skipped.map((s) => s.weight), stageSegments({ status: 'queued' }).map((s) => s.weight));
  assert.equal(stageText(skipped), 'Already on disk, nothing downloaded');
  assert.match(stageText(half), /Download: 45%/);
  assert.match(stageText(unverified), /Verify: not verified/);
});

test('stage segments are weighted by their share of a track run', () => {
  const segs = stageSegments({ status: 'decrypting', progress: { resolved: true, download: 1, decrypt: 0.3 } });
  assert.deepEqual(
    segs.map((s) => [s.key, s.weight]),
    [['resolve', 0.04], ['download', 0.56], ['decrypt', 0.26], ['remux', 0.06], ['verify', 0.03], ['tag', 0.03], ['save', 0.02]],
  );
  const total = Object.values(STAGE_WEIGHTS).reduce((a, w) => a + w, 0);
  assert.ok(Math.abs(total - 1) < 1e-9, `weights add up to ${total}`);
  assert.deepEqual(Object.keys(STAGE_WEIGHTS), segs.map((s) => s.key), 'one weight per stage, in pipeline order');
  const [download, decrypt] = [STAGE_WEIGHTS.download, STAGE_WEIGHTS.decrypt];
  for (const [k, w] of Object.entries(STAGE_WEIGHTS)) {
    if (k !== 'download') assert.ok(download > w, `download outweighs ${k}`);
    if (k !== 'download' && k !== 'decrypt') assert.ok(decrypt > w, `decrypt outweighs ${k}`);
  }
  assert.ok(Object.isFrozen(STAGE_WEIGHTS));
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

test('missingToolRows flags a missing ffmpeg outside simulate mode', () => {
  const err = 'exec: "ffmpeg": executable file not found in $PATH';
  const sys = { simulate: false, tools: [{ name: 'ffmpeg', available: false, error: err }] };
  const rows = missingToolRows(sys);
  assert.deepEqual(rows.map(([name, v]) => [name, v.role, v.label]), [['ffmpeg', 'danger', 'Missing']]);
  assert.equal(rows[0][1].detail, 'needed for every download');
  assert.equal(rows[0][1].title, err);
  assert.deepEqual(missingToolRows({ ...sys, simulate: true }), [], 'simulate mode needs no ffmpeg');
  assert.deepEqual(missingToolRows({ tools: [{ name: 'ffmpeg', available: true, path: '/usr/bin/ffmpeg' }] }), []);
  assert.deepEqual(missingToolRows(undefined), []);
});

test('progressCells gives one cell per track in ordered runs', () => {
  const cells = progressCells({ total_items: 6, completed_items: 2, skipped_items: 1, failed_items: 1, cancelled_items: 0 });
  assert.deepEqual(
    cells.map((c) => c.state),
    ['completed', 'completed', 'skipped', 'failed', 'remaining', 'remaining'],
  );
  assert.deepEqual(
    cells.map((c) => c.role),
    ['success', 'success', 'skipped', 'danger', '', ''],
  );
  assert.equal(progressCells({ total_items: CELL_MAX, completed_items: 1 }).length, CELL_MAX);
  assert.equal(progressCells({ total_items: CELL_MAX + 1 }), null);
  assert.equal(progressCells({ total_items: 0, status: 'running' }), null);
  // Counters that overrun the total never add cells.
  assert.equal(progressCells({ total_items: 2, completed_items: 5 }).length, 2);
});

test('progressLegend counts every cell state in cell order, zeros included', () => {
  const legend = progressLegend({ total_items: 6, completed_items: 2, skipped_items: 1, failed_items: 1 });
  assert.deepEqual(
    legend.map((k) => [k.state, k.n]),
    [['completed', 2], ['skipped', 1], ['failed', 1], ['cancelled', 0], ['remaining', 2]],
  );
  assert.equal(legend.find((k) => k.state === 'failed').role, 'danger');
  assert.equal(progressLegend({ total_items: 0, status: 'running' }), null);
  // Counters that overrun the total are clamped, like the cells.
  assert.deepEqual(
    progressLegend({ total_items: 2, completed_items: 5, failed_items: 1 }).map((k) => k.n),
    [2, 0, 0, 0, 0],
  );
});

test('activityView names the event and keeps one useful line', () => {
  assert.deepEqual(activityView({ type: 'job_queued', message: 'job queued' }), { label: 'Queued', role: 'neutral', text: '' });
  assert.deepEqual(activityView({ type: 'resolved_input', message: 'album' }), { label: 'Resolved', role: 'neutral', text: 'album' });
  const failed = activityView({ type: 'job_failed', message: 'catalog request failed: 404 Not Found: {"errors":[{"id":"Q"}]}' });
  assert.equal(failed.role, 'danger');
  assert.equal(failed.text, 'Apple Music has nothing at this link (404)');
  assert.equal(activityView({ type: 'operation_exhausted', phase: 'resolve_tracks' }).text, 'resolve tracks');
  assert.equal(activityView({ type: 'some_new_event' }).label, 'Some new event');
});

test('runningFirst lifts running jobs and keeps each group in order', () => {
  const jobs = [{ id: 'a', status: 'queued' }, { id: 'b', status: 'running' }, { id: 'c', status: 'queued' }, { id: 'd', status: 'running' }];
  assert.deepEqual(runningFirst(jobs).map((j) => j.id), ['b', 'd', 'a', 'c']);
  const queuedOnly = jobs.filter((j) => j.status === 'queued');
  assert.equal(runningFirst(queuedOnly), queuedOnly, 'nothing to move returns the same list');
});
