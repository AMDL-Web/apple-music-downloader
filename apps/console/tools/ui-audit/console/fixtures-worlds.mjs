// The audit's worlds: the deterministic states the mock server starts from
// (empty, idle, active, many), their seeded logs, and the stable job keys
// the drawer scenarios open.

import { makeJob, emptyWorld, album, hexId, ERRORS, HOUR, MIN } from './fixtures.mjs';
import { seedHooks } from './fixtures-hooks.mjs';

const LOG_LINES = [
  ['info', 'amdl backend listening', { addr: '[::]:18080', component: 'main' }],
  ['info', 'job started', { component: 'jobs', job_id: '%J' }],
  ['debug', 'codec selected', { component: 'media', codec: 'alac', bit_depth: 24, sample_rate: 48000, job_id: '%J' }],
  ['info', 'track saved', { component: 'jobs', job_id: '%J', path: '/app/data/downloads/albums/Yorushika/second person/01. Early morning, mailbox.m4a' }],
  ['warn', 'lyrics attempt 1/3 failed; retrying', { component: 'lyrics', job_id: '%J', error: 'wrapper /lyrics: context deadline exceeded' }],
  ['error', 'decrypt failed', { component: 'fairplay', job_id: '%J', error: 'wrapper closed the connection while a sample was in flight; the decrypt session is gone' }],
  ['info', 'GET /api/v1/downloads', { component: 'http', status: 200, duration_ms: 3, request_id: 'req_7f3a' }],
  ['debug', 'feed client connected', { component: 'http', remote: '192.168.3.20' }],
];

function seedLogs(world, n) {
  const ids = [...world.jobs.keys()];
  for (let i = 0; i < n; i++) {
    const [level, message, attrs] = LOG_LINES[i % LOG_LINES.length];
    const attributes = {};
    for (const [k, v] of Object.entries(attrs)) attributes[k] = v === '%J' ? ids[i % Math.max(1, ids.length)] || 'job_none' : v;
    world.addLog({ level, message, attributes, at: world.now - (n - i) * 7000 });
  }
}

/** The world presets the scenarios name. */
export const WORLDS = {
  /** No jobs at all: first run. */
  empty(now) {
    const w = emptyWorld(now);
    w.statsMode = 'none';
    seedLogs(w, 3);
    return w;
  },
  /** A few finished albums, nothing running. */
  idle(now) {
    const w = emptyWorld(now);
    for (let i = 0; i < 9; i++) makeJob(w, { key: `idle${i}`, album: i * 7 + 3, status: i === 5 ? 'failed' : 'completed', createdAgo: (i + 1) * 5 * HOUR, error: i === 5 ? ERRORS.decrypt : undefined, stages: i === 5 ? (k) => (k < 3 ? 'completed' : 'failed') : undefined, motion: i === 0 });
    w.statsMode = 'rich';
    seedLogs(w, 40);
    return w;
  },
  /** The everyday busy state: running, queued, finished, one recent failure. */
  active(now) {
    const w = emptyWorld(now);
    buildActive(w);
    seedLogs(w, 120);
    return w;
  },
  /** 160 jobs over two weeks: pagination, long lists, many failures. */
  many(now) {
    const w = emptyWorld(now);
    buildActive(w);
    const statuses = ['completed', 'completed', 'completed', 'failed', 'completed', 'cancelled', 'completed'];
    for (let i = 0; i < 150; i++) {
      const st = statuses[i % statuses.length];
      makeJob(w, {
        key: `many${i}`,
        album: i,
        status: st,
        type: i % 11 === 0 ? 'song' : i % 13 === 0 ? 'playlist' : 'album',
        tracks: i % 11 === 0 ? 1 : undefined,
        createdAgo: 3 * HOUR + i * 2 * HOUR,
        error: st === 'failed' ? [ERRORS.decrypt, ERRORS.notFound, ERRORS.storefront, ERRORS.json][i % 4] : undefined,
        unresolved: st === 'failed' && i % 4 === 1,
        stages: st === 'failed' ? (k) => (k % 3 === 0 ? 'failed' : 'completed') : st === 'cancelled' ? (k) => (k < 2 ? 'completed' : 'cancelled') : undefined,
      });
    }
    seedLogs(w, 400);
    return w;
  },
};

/** Stable ids for the drawer scenarios. */
export const JOB_KEYS = {
  running22: 'running22',
  resolving: 'resolving',
  queued: 'queued1',
  done1: 'done1',
  done5: 'done5',
  done22: 'done22',
  done150: 'done150',
  failed: 'failed1',
  failed404: 'failed404',
  cancelled: 'cancelled1',
  skipped: 'skipped1',
  mixed: 'mixed1',
  long: 'long1',
};

export const jobId = (key) => hexId('job', key);

function buildActive(w) {
  makeJob(w, {
    key: JOB_KEYS.running22,
    album: 12,
    status: 'running',
    tracks: 22,
    createdAgo: 4 * MIN,
    stages: (i) => (i < 8 ? 'completed' : i === 8 ? 'downloading' : i === 9 ? 'decrypting' : i === 10 ? 'waiting_download' : i === 11 ? 'resolving' : 'queued'),
    fracs: (i) => (i === 8 ? 0.42 : 0.6),
    retrying: (i) => i === 8,
  });
  makeJob(w, { key: JOB_KEYS.resolving, album: 30, status: 'running', unresolved: true, createdAgo: 20000, input: 'https://music.apple.com/cn/album/1580904295' });
  makeJob(w, { key: JOB_KEYS.queued, album: 44, status: 'queued', tracks: 12, createdAgo: 10000 });
  makeJob(w, { key: 'queued2', album: 52, status: 'queued', tracks: 9, createdAgo: 9000 });
  makeJob(w, { key: JOB_KEYS.done1, album: 3, type: 'song', status: 'completed', tracks: 1, createdAgo: 30 * MIN });
  makeJob(w, { key: JOB_KEYS.done5, album: 9, status: 'completed', tracks: 5, createdAgo: 50 * MIN, motion: true });
  makeJob(w, { key: JOB_KEYS.done22, album: 15, status: 'completed', tracks: 22, createdAgo: 2 * HOUR });
  makeJob(w, { key: JOB_KEYS.done150, album: 21, type: 'playlist', status: 'completed', tracks: 150, createdAgo: 5 * HOUR, durationMs: 40 * MIN, trackArtist: (i) => album(i * 3).artist_name });
  makeJob(w, {
    key: JOB_KEYS.failed,
    album: 27,
    status: 'failed',
    tracks: 14,
    createdAgo: 3 * HOUR,
    error: ERRORS.decrypt,
    stages: (i) => (i < 9 ? 'completed' : i < 12 ? 'failed' : 'cancelled'),
    itemError: (i) => (i === 9 ? ERRORS.decrypt : 'download: unexpected EOF after 3 attempts'),
  });
  makeJob(w, { key: JOB_KEYS.failed404, album: 33, status: 'failed', unresolved: true, createdAgo: 6 * HOUR, error: ERRORS.notFound, input: 'https://music.apple.com/cn/album/1234567890' });
  makeJob(w, { key: JOB_KEYS.cancelled, album: 38, status: 'cancelled', tracks: 10, createdAgo: 8 * HOUR, stages: (i) => (i < 4 ? 'completed' : 'cancelled') });
  makeJob(w, { key: JOB_KEYS.skipped, album: 41, status: 'completed', tracks: 7, createdAgo: 10 * HOUR, stages: () => 'skipped_existing' });
  makeJob(w, {
    key: JOB_KEYS.mixed,
    album: 47,
    status: 'completed',
    tracks: 12,
    createdAgo: 20 * HOUR,
    codecs: (i) => (i === 0 ? 'alac-24-96' : i === 5 ? 'aac' : i === 7 ? 'alac-16-44' : 'alac-24-48'),
    stages: (i) => (i === 3 ? 'skipped_existing' : 'completed'),
  });
  makeJob(w, { key: JOB_KEYS.long, album: { ...album(60), name: 'Beethoven: Symphonies Nos. 1–9 · Overtures · Missa solemnis (Remastered 2026, Deluxe Edition with Rehearsal Excerpts)' }, status: 'completed', tracks: 9, createdAgo: 30 * HOUR, longTitles: true });
  seedHooks(w);
}
