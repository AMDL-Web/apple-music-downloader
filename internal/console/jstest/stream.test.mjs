import test from 'node:test';
import assert from 'node:assert/strict';
import {
  backoffDelay,
  withCursor,
  isNewerCursor,
  isFeatureUnavailable,
  endsStream,
  StreamClient,
  claimSecondary,
  releaseSecondary,
  currentSecondary,
  createCoalescer,
} from '../static/js/lib/stream.js';

const tick = () => new Promise((r) => setImmediate(r));
async function settle(n = 6) {
  for (let i = 0; i < n; i++) await tick();
}

/** Manual timers and clock. */
function fakeEnv() {
  let now = 1_000_000;
  let seq = 0;
  const timers = new Map();
  return {
    timers,
    now: () => now,
    advance(ms) {
      now += ms;
    },
    setTimeout(fn, ms) {
      const id = ++seq;
      timers.set(id, { fn, at: now + ms, ms });
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
    /** Runs the earliest timer, moving the clock to it. */
    runNext() {
      let best = null;
      for (const [id, t] of timers) if (!best || t.at < best[1].at) best = [id, t];
      if (!best) return false;
      timers.delete(best[0]);
      now = Math.max(now, best[1].at);
      best[1].fn();
      return true;
    },
    random: () => 0.5,
  };
}

/** A stream body we can write to; it errors when the request is aborted. */
function liveBody(signal) {
  let ctrl;
  const body = new ReadableStream({
    start(c) {
      ctrl = c;
    },
  });
  signal.addEventListener('abort', () => {
    try {
      ctrl.error(new DOMException('aborted', 'AbortError'));
    } catch {
      // already closed
    }
  });
  const enc = new TextEncoder();
  return {
    body,
    write: (s) => ctrl.enqueue(enc.encode(s)),
    close: () => ctrl.close(),
  };
}

test('backoff doubles from 1 s to a 30 s cap with ±20% jitter', () => {
  const mid = () => 0.5;
  assert.deepEqual([0, 1, 2, 3, 4, 5, 9].map((a) => backoffDelay(a, mid)), [1000, 2000, 4000, 8000, 16000, 30000, 30000]);
  assert.equal(backoffDelay(2, () => 0), 3200);
  assert.equal(backoffDelay(2, () => 1), 4800);
  assert.equal(backoffDelay(0, () => 0), 1000, 'never below 1 s');
  assert.equal(backoffDelay(5, () => 1), 30000, 'never above 30 s');
});

test('withCursor and isNewerCursor', () => {
  assert.equal(withCursor('http://h/api/v1/downloads/events', 42), 'http://h/api/v1/downloads/events?cursor=42');
  assert.equal(withCursor('http://h/api/v1/logs/stream?level=info', '7'), 'http://h/api/v1/logs/stream?level=info&cursor=7');
  assert.equal(withCursor('http://h/x', null), 'http://h/x');
  assert.equal(withCursor('http://h/x', 0), 'http://h/x');
  assert.equal(isNewerCursor(null, '1'), true);
  assert.equal(isNewerCursor('9', '10'), true, 'numeric, not lexical');
  assert.equal(isNewerCursor('10', '9'), false);
  assert.equal(isNewerCursor('10', '10'), false);
  assert.equal(isNewerCursor('10', 'abc'), false);
});

test('events advance the cursor and a closed stream resumes from it', async () => {
  const env = fakeEnv();
  const urls = [];
  const bodies = [];
  env.fetch = async (url, init) => {
    urls.push(url);
    const b = liveBody(init.signal);
    bodies.push(b);
    return new Response(b.body, { status: 200 });
  };
  const events = [];
  const states = [];
  let closes = 0;
  const client = new StreamClient({
    url: 'http://h/api/v1/downloads/events',
    cursor: 100,
    env,
    onEvent: (ev) => events.push(ev),
    onState: (s) => states.push(s.state),
    onClose: () => closes++,
  }).start();
  await settle();
  assert.equal(urls[0], 'http://h/api/v1/downloads/events?cursor=100');
  assert.equal(client.state, 'live');

  bodies[0].write('id: 101\nevent: download_upserted\ndata: {"event_id":101}\n\n: keepalive\n\nid: 105\nevent: download_deleted\ndata: {"event_id":105}\n\n');
  await settle();
  assert.deepEqual(events.map((e) => [e.type, e.id]), [
    ['download_upserted', '101'],
    ['download_deleted', '105'],
  ]);
  assert.equal(client.cursor, '105');

  bodies[0].close();
  await settle();
  assert.equal(closes, 1);
  assert.equal(client.state, 'reconnecting');
  assert.ok(env.runNext(), 'a reconnect is scheduled');
  await settle();
  assert.equal(urls[1], 'http://h/api/v1/downloads/events?cursor=105');
  assert.equal(client.state, 'live');
  client.stop();
  assert.equal(client.state, 'stopped');
  assert.deepEqual(states.slice(0, 2), ['connecting', 'live']);
});

test('204 ends the stream without a reconnect', async () => {
  const env = fakeEnv();
  env.fetch = async () => new Response(null, { status: 204 });
  let ended = null;
  const client = new StreamClient({ url: 'http://h/api/v1/downloads/job_1/events', cursor: 5, env, onEvent: () => {}, onEnd: (r) => (ended = r) }).start();
  await settle();
  assert.deepEqual(ended, { status: 204, body: null });
  assert.equal(client.state, 'ended');
  assert.equal(env.timers.size, 0);
});

test('404 and 400 end the stream and pass the error body on', async () => {
  const env = fakeEnv();
  env.fetch = async () =>
    new Response(JSON.stringify({ error: { code: 'invalid_request', message: 'bad cursor' } }), { status: 400, headers: { 'Content-Type': 'application/json' } });
  let ended = null;
  new StreamClient({ url: 'http://h/x', env, onEvent: () => {}, onEnd: (r) => (ended = r) }).start();
  await settle();
  assert.equal(ended.status, 400);
  assert.equal(ended.body.error.code, 'invalid_request');
  assert.equal(env.timers.size, 0);
});

test('503 feature_unavailable ends the stream; any other 503 is retried', async () => {
  const off = { error: { code: 'feature_unavailable', message: 'the log buffer is not available in this process' } };
  assert.equal(isFeatureUnavailable(503, off), true);
  assert.equal(isFeatureUnavailable(503, { error: { code: 'queue_full', message: 'full' } }), false);
  assert.equal(isFeatureUnavailable(503, null), false, 'a proxy page has no envelope');
  assert.equal(isFeatureUnavailable(500, off), false);
  assert.equal(endsStream(404, null), true);
  assert.equal(endsStream(502, null), false);

  const env = fakeEnv();
  env.fetch = async () => new Response(JSON.stringify(off), { status: 503, headers: { 'Content-Type': 'application/json' } });
  let ended = null;
  const client = new StreamClient({ url: 'http://h/api/v1/logs/stream', env, onEvent: () => {}, onEnd: (r) => (ended = r) }).start();
  await settle();
  assert.equal(client.state, 'ended');
  assert.equal(ended.status, 503);
  assert.equal(ended.body.error.code, 'feature_unavailable');
  assert.equal(env.timers.size, 0, 'no reconnect is scheduled');

  const retry = fakeEnv();
  let calls = 0;
  retry.fetch = async () => {
    calls++;
    return new Response('<html>Service Unavailable</html>', { status: 503, headers: { 'Content-Type': 'text/html' } });
  };
  let retryEnded = null;
  const proxied = new StreamClient({ url: 'http://h/api/v1/logs/stream', env: retry, onEvent: () => {}, onEnd: (r) => (retryEnded = r) }).start();
  await settle();
  assert.equal(proxied.state, 'reconnecting');
  assert.equal(retryEnded, null);
  assert.ok(retry.runNext(), 'a reconnect is scheduled');
  await settle();
  assert.equal(calls, 2);
  proxied.stop();
});

test('failures back off, turn offline after a minute and resync after five', async () => {
  const env = fakeEnv();
  let fail = true;
  const urls = [];
  env.fetch = async (url, init) => {
    urls.push(url);
    if (fail) throw new TypeError('network down');
    return new Response(liveBody(init.signal).body, { status: 200 });
  };
  let resyncs = 0;
  const client = new StreamClient({ url: 'http://h/x', cursor: 9, env, onEvent: () => {}, onResync: () => resyncs++ }).start();
  await settle();
  assert.equal(client.state, 'reconnecting');
  const delays = [];
  for (let i = 0; i < 4; i++) {
    const [t] = [...env.timers.values()];
    delays.push(t.ms);
    env.runNext();
    await settle();
  }
  assert.deepEqual(delays, [1000, 2000, 4000, 8000]);
  env.advance(60000);
  env.runNext();
  await settle();
  assert.equal(client.state, 'offline');
  env.advance(5 * 60000);
  fail = false;
  client.retryNow();
  await settle();
  assert.equal(client.state, 'live');
  assert.equal(resyncs, 1);
  assert.ok(urls.every((u) => u === 'http://h/x?cursor=9'), 'every retry resumes from the same cursor');
  client.stop();
});

test('a silent connection is dropped by the idle watchdog and retried', async () => {
  const env = fakeEnv();
  let calls = 0;
  env.fetch = async (url, init) => {
    calls++;
    return new Response(liveBody(init.signal).body, { status: 200 });
  };
  const client = new StreamClient({ url: 'http://h/x', env, onEvent: () => {}, idleTimeoutMs: 45000 }).start();
  await settle();
  const idle = [...env.timers.values()].find((t) => t.ms === 45000);
  assert.ok(idle, 'idle watchdog armed');
  env.runNext();
  await settle();
  assert.equal(client.state, 'reconnecting');
  env.runNext();
  await settle();
  assert.equal(calls, 2);
  client.stop();
});

test('the stream pauses 30 s after the tab hides and resumes from the cursor', async () => {
  const env = fakeEnv();
  const listeners = new Set();
  env.document = {
    hidden: false,
    addEventListener: (_, fn) => listeners.add(fn),
    removeEventListener: (_, fn) => listeners.delete(fn),
  };
  const urls = [];
  const bodies = [];
  env.fetch = async (url, init) => {
    urls.push(url);
    const b = liveBody(init.signal);
    bodies.push(b);
    return new Response(b.body, { status: 200 });
  };
  const client = new StreamClient({ url: 'http://h/x', cursor: 1, env, onEvent: () => {}, idleTimeoutMs: 10 ** 9 }).start();
  await settle();
  bodies[0].write('id: 3\nevent: log\ndata: {}\n\n');
  await settle();

  env.document.hidden = true;
  for (const fn of listeners) fn();
  const hidden = [...env.timers.values()].find((t) => t.ms === 30000);
  assert.ok(hidden, 'pause scheduled 30 s out');
  env.clearTimeout([...env.timers.entries()].find(([, t]) => t.ms === 10 ** 9)[0]);
  env.runNext();
  await settle();
  assert.equal(client.state, 'paused');

  env.document.hidden = false;
  for (const fn of listeners) fn();
  await settle();
  assert.equal(client.state, 'live');
  assert.equal(urls[1], 'http://h/x?cursor=3');
  client.stop();
  assert.equal(listeners.size, 0, 'stop removes the visibility listener');
});

test('a tab hidden for less than 30 s keeps its stream', async () => {
  const env = fakeEnv();
  const listeners = new Set();
  env.document = { hidden: false, addEventListener: (_, fn) => listeners.add(fn), removeEventListener: (_, fn) => listeners.delete(fn) };
  env.fetch = async (url, init) => new Response(liveBody(init.signal).body, { status: 200 });
  const client = new StreamClient({ url: 'http://h/x', env, onEvent: () => {} }).start();
  await settle();
  env.document.hidden = true;
  for (const fn of listeners) fn();
  env.document.hidden = false;
  for (const fn of listeners) fn();
  assert.ok(![...env.timers.values()].some((t) => t.ms === 30000), 'pause timer cancelled');
  assert.equal(client.state, 'live');
  client.stop();
});

test('only one secondary stream at a time', () => {
  const stopped = [];
  const a = { stop: () => stopped.push('a') };
  const b = { stop: () => stopped.push('b') };
  claimSecondary(a);
  claimSecondary(b);
  assert.deepEqual(stopped, ['a']);
  assert.equal(currentSecondary(), b);
  releaseSecondary(a);
  assert.equal(currentSecondary(), b, 'a stale release does not clear the slot');
  releaseSecondary(b);
  assert.equal(currentSecondary(), null);
});

test('the coalescer flushes at most once per interval', () => {
  const env = fakeEnv();
  const batches = [];
  const c = createCoalescer((b) => batches.push(b), 100, env);
  c.push(1);
  c.push(2);
  c.push(3);
  assert.equal(batches.length, 0, 'nothing renders synchronously');
  env.runNext();
  assert.deepEqual(batches, [[1, 2, 3]]);
  c.push(4);
  const [t] = [...env.timers.values()];
  assert.equal(t.ms, 100, 'the next flush waits out the interval');
  env.runNext();
  assert.deepEqual(batches, [[1, 2, 3], [4]]);
  c.push(5);
  c.cancel();
  assert.equal(env.timers.size, 0);
  c.push(6);
  c.flushNow();
  assert.deepEqual(batches[2], [6]);
});
