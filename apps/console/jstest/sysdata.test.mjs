import test from 'node:test';
import assert from 'node:assert/strict';
import {
  LOG_FIRST_LIMIT,
  logsQuery,
  mergeLogs,
  errorCountLabel,
  hourBuckets,
  peakHour,
  nextCheckFraction,
  nextCheckIn,
  firstChoiceShare,
  workerCells,
} from '../static/js/lib/sysdata.js';

const H = 3600000;
// A local-hour boundary plus 20 minutes, so the tests hold in any time zone.
const hourStartOf = (ms) => {
  const d = new Date(ms);
  d.setMinutes(0, 0, 0);
  return d.getTime();
};
const NOW = hourStartOf(Date.UTC(2026, 9, 30, 6, 0, 0)) + 20 * 60000;
const START = hourStartOf(NOW);
const at = (ms, sequence) => ({ sequence, time: new Date(ms).toISOString(), level: 'error', message: 'x' });

test('logsQuery: the first read asks for the last 24 hours', () => {
  const now = Date.UTC(2026, 9, 30, 8, 0, 0, 456);
  assert.deepEqual(logsQuery(undefined, now), { level: 'error', since: '2026-10-29T08:00:00Z', limit: LOG_FIRST_LIMIT });
  assert.deepEqual(logsQuery({ entries: [], cursor: null, full: false }, now).since, '2026-10-29T08:00:00Z');
});

test('logsQuery: later reads carry the cursor', () => {
  assert.deepEqual(logsQuery({ entries: [], cursor: 41, full: false }, NOW), { level: 'error', cursor: 41 });
});

test('mergeLogs: appends, de-duplicates by sequence, sorts and drops old or bad times', () => {
  const s1 = mergeLogs(undefined, { logs: [at(NOW - H, 5), at(NOW - 2 * H, 3)], next_cursor: 5 }, NOW);
  assert.deepEqual(s1.entries.map((e) => e.sequence), [3, 5]);
  assert.equal(s1.cursor, 5);
  const page = {
    logs: [at(NOW - H, 5), at(NOW, 6), at(NOW - 25 * H, 4), { sequence: 7, time: 'nope', level: 'error', message: 'x' }],
    next_cursor: 7,
  };
  const s2 = mergeLogs(s1, page, NOW);
  assert.deepEqual(s2.entries.map((e) => e.sequence), [3, 5, 6]);
  assert.equal(s2.cursor, 7);
});

test('mergeLogs: a page without a numeric cursor keeps the old one', () => {
  const s1 = mergeLogs(undefined, { logs: [], next_cursor: 9 }, NOW);
  assert.equal(mergeLogs(s1, { logs: [], next_cursor: null }, NOW).cursor, 9);
});

test('mergeLogs: does not mutate its inputs', () => {
  const state = Object.freeze({ entries: Object.freeze([at(NOW, 1)]), cursor: 1, full: false });
  const page = Object.freeze({ logs: Object.freeze([at(NOW, 2)]), next_cursor: 2 });
  const out = mergeLogs(state, page, NOW);
  assert.equal(state.entries.length, 1);
  assert.equal(out.entries.length, 2);
});

test('errorCountLabel: 200+ after a full first read, and later reads keep it', () => {
  const logs = Array.from({ length: LOG_FIRST_LIMIT }, (_, i) => at(NOW - 1000, i + 1));
  const s1 = mergeLogs(undefined, { logs, next_cursor: LOG_FIRST_LIMIT }, NOW);
  assert.equal(s1.full, true);
  assert.equal(errorCountLabel(s1), '200+');
  const s2 = mergeLogs(s1, { logs: [], next_cursor: LOG_FIRST_LIMIT }, NOW);
  assert.equal(s2.full, true);
  assert.equal(errorCountLabel(s2), '200+');
});

test('errorCountLabel: counts, zero and no state', () => {
  const s = mergeLogs(undefined, { logs: [at(NOW, 1), at(NOW, 2)], next_cursor: 2 }, NOW);
  assert.equal(s.full, false);
  assert.equal(errorCountLabel(s), '2');
  assert.equal(errorCountLabel(mergeLogs(undefined, { logs: [], next_cursor: 0 }, NOW)), '0');
  assert.equal(errorCountLabel(undefined), '');
});

test('hourBuckets: 24 buckets ending at the current hour', () => {
  const b = hourBuckets([at(START, 1), at(NOW, 2), at(START - 1, 3), at(START - 23 * H, 4), at(START - 24 * H, 5), at(NOW - 30 * H, 6), { time: 'bad' }], NOW);
  assert.equal(b.length, 24);
  assert.equal(b[23], 2);
  assert.equal(b[22], 1);
  assert.equal(b[0], 1);
  assert.equal(b.reduce((n, x) => n + x, 0), 4);
});

test('peakHour: the busiest hour, the latest on a tie, null when empty', () => {
  assert.equal(peakHour(new Array(24).fill(0), NOW), null);
  const b = new Array(24).fill(0);
  b[10] = 2;
  b[20] = 2;
  b[5] = 1;
  assert.deepEqual(peakHour(b, NOW), { count: 2, at: START - 3 * H });
  assert.deepEqual(peakHour(Object.assign(new Array(24).fill(0), { 23: 1 }), NOW), { count: 1, at: START });
});

test('nextCheckFraction and nextCheckIn', () => {
  const min = 60000;
  const sync = (over) => ({ enabled: true, interval_minutes: 10, last_poll_at: new Date(NOW - 5 * min).toISOString(), ...over });
  assert.equal(nextCheckFraction(sync(), NOW), 0.5);
  assert.equal(nextCheckIn(sync(), NOW), 5 * min);
  const late = sync({ last_poll_at: new Date(NOW - 30 * min).toISOString() });
  assert.equal(nextCheckFraction(late, NOW), 1);
  assert.equal(nextCheckIn(late, NOW), -20 * min);
  assert.equal(nextCheckFraction(sync({ last_poll_at: undefined }), NOW), null);
  assert.equal(nextCheckIn(sync({ last_poll_at: undefined }), NOW), null);
  assert.equal(nextCheckFraction(sync({ enabled: false }), NOW), null);
  assert.equal(nextCheckFraction(sync({ interval_minutes: 0 }), NOW), null);
  assert.equal(nextCheckFraction(undefined, NOW), null);
  assert.equal(nextCheckFraction(sync({ last_poll_at: new Date(NOW + min).toISOString() }), NOW), 0);
});

test('firstChoiceShare: the first-choice codec, with aac-lc only in the total', () => {
  const codecs = [
    { codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: 3 },
    { codec: 'alac', bit_depth: 16, sample_rate: 44100, tracks: 1 },
    { codec: 'aac-lc', tracks: 4 },
    { codec: 'aac', tracks: 2 },
  ];
  assert.deepEqual(firstChoiceShare(codecs, ['alac', 'aac']), { kind: 'first', codec: 'alac', share: 0.4 });
  assert.deepEqual(firstChoiceShare(codecs, ['aac-lc']), { kind: 'first', codec: 'aac-lc', share: 0 });
});

test('firstChoiceShare: unknown codec, missing priority or no tracks', () => {
  const unknown = [{ codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: 3 }, { codec: 'flac', tracks: 1 }];
  assert.deepEqual(firstChoiceShare(unknown, ['alac']), { kind: 'common', label: 'ALAC 24/96', share: 0.75 });
  const known = [{ codec: 'aac', tracks: 1 }, { codec: 'ec3', tracks: 3 }];
  assert.deepEqual(firstChoiceShare(known, []), { kind: 'common', label: 'Dolby Atmos', share: 0.75 });
  assert.equal(firstChoiceShare(known, undefined).kind, 'common');
  assert.equal(firstChoiceShare(known, ['flac']).kind, 'common');
  assert.equal(firstChoiceShare([{ codec: 'alac', tracks: 0 }], ['alac']), null);
  assert.equal(firstChoiceShare([], ['alac']), null);
  assert.equal(firstChoiceShare(undefined, ['alac']), null);
});

test('workerCells: cells up to 24, a meter beyond, clamped', () => {
  assert.deepEqual(workerCells(4, 2), { mode: 'cells', total: 4, filled: 2 });
  assert.deepEqual(workerCells(4, 9), { mode: 'cells', total: 4, filled: 4 });
  assert.deepEqual(workerCells(4, 'x'), { mode: 'cells', total: 4, filled: 0 });
  assert.deepEqual(workerCells(24, 1), { mode: 'cells', total: 24, filled: 1 });
  assert.deepEqual(workerCells(25, 5), { mode: 'meter', fraction: 0.2 });
  assert.deepEqual(workerCells(25, 99), { mode: 'meter', fraction: 1 });
  assert.equal(workerCells(0, 0), null);
  assert.equal(workerCells(undefined, 0), null);
  assert.equal(workerCells(2.5, 0), null);
});
