// Server-Sent Events over fetch, with a resumable cursor, backoff, an idle
// watchdog and a pause while the tab is hidden. fetch instead of EventSource
// because the client must see the status code: 204 ends a per-job stream,
// 404 means the job is gone, 400 is a bad filter, a 503 feature_unavailable
// means the route is off in this backend (log retention disabled), and only
// network or other 5xx failures are worth a retry. Pure apart from the
// injected fetch, timers and document, so node:test drives it with fakes.

import { SSEParser } from './sse.js';

export const MIN_DELAY_MS = 1000;
export const MAX_DELAY_MS = 30000;

/**
 * An outage of at least this long, once the feed is live again, is a real one
 * (a restart, a lost network) after which pages refetch what failed. A shorter
 * one (a proxy closing an idle stream) reconnects without disturbing them.
 */
export const EPOCH_AFTER_MS = 5000;

/**
 * Reconnect delay for the given failed attempt (0-based): 1, 2, 4, 8, 16,
 * then 30 s, each with ±20% jitter, clamped to 1-30 s.
 */
export function backoffDelay(attempt, random = Math.random) {
  const base = Math.min(MAX_DELAY_MS, MIN_DELAY_MS * 2 ** Math.max(0, attempt));
  const jittered = base * (0.8 + 0.4 * random());
  return Math.round(Math.min(MAX_DELAY_MS, Math.max(MIN_DELAY_MS, jittered)));
}

/** Statuses after which reconnecting cannot succeed, so the stream ends. */
const ENDING_STATUSES = new Set([204, 400, 404, 405, 410, 422]);

/**
 * Whether a response is the backend saying the route is switched off in this
 * process: 503 with the error code feature_unavailable. Any other 503 (a proxy
 * during a restart, a full queue) is transient and retried.
 */
export function isFeatureUnavailable(status, body) {
  return status === 503 && Boolean(body && body.error && body.error.code === 'feature_unavailable');
}

/** Whether a non-200 response ends the stream instead of being retried. */
export function endsStream(status, body) {
  return ENDING_STATUSES.has(status) || isFeatureUnavailable(status, body);
}

/** Whether `next` is a cursor past `current`. Cursors are decimal event ids or log sequences. */
export function isNewerCursor(current, next) {
  if (next === null || next === undefined || next === '') return false;
  const n = Number(next);
  if (!Number.isFinite(n)) return false;
  if (current === null || current === undefined || current === '') return true;
  return n > Number(current);
}

/** Adds ?cursor= (when known) to a stream URL that may already carry a query. */
export function withCursor(url, cursor) {
  if (cursor === null || cursor === undefined || cursor === '' || Number(cursor) <= 0) return url;
  return `${url}${url.includes('?') ? '&' : '?'}cursor=${encodeURIComponent(String(cursor))}`;
}

const defaultEnv = () => ({
  fetch: (...a) => globalThis.fetch(...a),
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
  clearTimeout: (t) => globalThis.clearTimeout(t),
  now: () => Date.now(),
  random: Math.random,
  document: globalThis.document,
});

/**
 * One resumable SSE subscription.
 *
 * States: connecting, live, reconnecting, offline (failing for longer than
 * offlineAfterMs, still retrying), paused (tab hidden), ended (an ending
 * status, see endsStream; onEnd gets it), stopped.
 */
export class StreamClient {
  /**
   * @param {object} opts
   * @param {string} opts.url stream URL without the cursor
   * @param {string|number|null} [opts.cursor] resume point; never pass 0 to the feed
   * @param {(ev: {type: string, data: string, id: string|null}) => void} opts.onEvent
   * @param {(s: {state: string, retryAt: number|null, downSince: number|null}) => void} [opts.onState]
   * @param {(r: {status: number, body: any}) => void} [opts.onEnd]
   * @param {() => void} [opts.onClose] an established stream closed; it will reconnect
   * @param {() => void} [opts.onReconnect] live again after any outage (a
   *   failed connect or a closed stream); events in between are replayed from
   *   the cursor, but whatever else a page fetched meanwhile may have failed
   * @param {() => void} [opts.onResync] live again after an outage of at least
   *   resyncAfterMs, after onReconnect
   */
  constructor(opts) {
    this.url = opts.url;
    this.cursor = opts.cursor ?? null;
    this.onEvent = opts.onEvent;
    this.onState = opts.onState || (() => {});
    this.onEnd = opts.onEnd || (() => {});
    this.onClose = opts.onClose || (() => {});
    this.onReconnect = opts.onReconnect || (() => {});
    this.onResync = opts.onResync || (() => {});
    this.env = { ...defaultEnv(), ...(opts.env || {}) };
    this.hiddenPauseMs = opts.hiddenPauseMs ?? 30000;
    this.idleTimeoutMs = opts.idleTimeoutMs ?? 45000;
    this.offlineAfterMs = opts.offlineAfterMs ?? 60000;
    this.resyncAfterMs = opts.resyncAfterMs ?? 300000;
    this.state = 'idle';
    this.attempt = 0;
    this.downSince = null;
    this.retryAt = null;
    this.generation = 0;
    this.controller = null;
    this.retryTimer = null;
    this.idleTimer = null;
    this.hiddenTimer = null;
    this.onVisibility = this.onVisibility.bind(this);
  }

  start() {
    const doc = this.env.document;
    if (doc && doc.addEventListener) doc.addEventListener('visibilitychange', this.onVisibility);
    this.connect();
    return this;
  }

  /** Closes the stream for good. */
  stop() {
    this.generation++;
    this.abort();
    this.clearTimers();
    const doc = this.env.document;
    if (doc && doc.removeEventListener) doc.removeEventListener('visibilitychange', this.onVisibility);
    this.setState('stopped');
  }

  /** Skips the remaining backoff wait. */
  retryNow() {
    if (this.state === 'reconnecting' || this.state === 'offline' || this.state === 'paused') {
      this.connect();
    }
  }

  setState(state) {
    this.state = state;
    this.onState({ state, retryAt: this.retryAt, downSince: this.downSince });
  }

  abort() {
    if (this.controller) {
      this.controller.abort();
      this.controller = null;
    }
  }

  clearTimers() {
    const { clearTimeout } = this.env;
    for (const k of ['retryTimer', 'idleTimer', 'hiddenTimer']) {
      if (this[k] !== null) clearTimeout(this[k]);
      this[k] = null;
    }
  }

  async connect() {
    const gen = ++this.generation;
    this.abort();
    if (this.retryTimer !== null) {
      this.env.clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    this.retryAt = null;
    if (this.state !== 'reconnecting' && this.state !== 'offline') this.setState('connecting');
    const controller = new AbortController();
    this.controller = controller;
    let res;
    try {
      res = await this.env.fetch(withCursor(this.url, this.cursor), {
        headers: { Accept: 'text/event-stream' },
        cache: 'no-store',
        signal: controller.signal,
      });
    } catch {
      if (gen === this.generation) this.fail(false);
      return;
    }
    if (gen !== this.generation) return;
    if (res.status === 200 && res.body) {
      await this.read(res, gen);
      return;
    }
    if (ENDING_STATUSES.has(res.status) || res.status === 503) {
      let body = null;
      try {
        body = await res.json();
      } catch {
        body = null;
      }
      if (gen !== this.generation) return;
      if (endsStream(res.status, body)) {
        this.generation++;
        this.controller = null;
        this.setState('ended');
        this.onEnd({ status: res.status, body });
        return;
      }
    }
    this.fail(false);
  }

  async read(res, gen) {
    const { now } = this.env;
    const wasDownSince = this.downSince;
    this.attempt = 0;
    this.downSince = null;
    this.setState('live');
    if (wasDownSince !== null) {
      this.onReconnect();
      if (gen !== this.generation) return;
      if (now() - wasDownSince >= this.resyncAfterMs) this.onResync();
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    const parser = new SSEParser();
    this.armIdle(gen);
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (gen !== this.generation) return;
        if (done) break;
        this.armIdle(gen);
        const text = typeof value === 'string' ? value : decoder.decode(value, { stream: true });
        for (const ev of parser.push(text)) {
          if (ev.id !== null && isNewerCursor(this.cursor, ev.id)) this.cursor = ev.id;
          this.onEvent(ev);
          if (gen !== this.generation) return;
        }
      }
    } catch {
      if (gen !== this.generation) return;
    }
    if (this.idleTimer !== null) this.env.clearTimeout(this.idleTimer);
    this.idleTimer = null;
    this.controller = null;
    this.onClose();
    if (gen !== this.generation) return;
    this.fail(true);
  }

  armIdle(gen) {
    const { setTimeout, clearTimeout } = this.env;
    if (this.idleTimer !== null) clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(() => {
      this.idleTimer = null;
      // No byte, not even the 15 s keepalive comment, for idleTimeoutMs: the
      // connection is half-open. Aborting makes read() fall through to a retry.
      if (gen === this.generation) this.abort();
    }, this.idleTimeoutMs);
  }

  fail(afterLive) {
    const { now, setTimeout } = this.env;
    if (this.downSince === null) this.downSince = now();
    const delay = backoffDelay(afterLive ? 0 : this.attempt, this.env.random);
    this.attempt++;
    this.retryAt = now() + delay;
    const down = now() - this.downSince;
    this.setState(down >= this.offlineAfterMs ? 'offline' : 'reconnecting');
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      this.connect();
    }, delay);
  }

  onVisibility() {
    const doc = this.env.document;
    if (!doc) return;
    const { setTimeout, clearTimeout } = this.env;
    if (doc.hidden) {
      if (this.hiddenTimer === null && this.state !== 'ended' && this.state !== 'stopped') {
        this.hiddenTimer = setTimeout(() => {
          this.hiddenTimer = null;
          this.pause();
        }, this.hiddenPauseMs);
      }
      return;
    }
    if (this.hiddenTimer !== null) {
      clearTimeout(this.hiddenTimer);
      this.hiddenTimer = null;
    }
    if (this.state === 'paused') this.connect();
  }

  pause() {
    if (this.state === 'ended' || this.state === 'stopped') return;
    this.generation++;
    this.abort();
    const { clearTimeout } = this.env;
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    if (this.idleTimer !== null) clearTimeout(this.idleTimer);
    this.retryTimer = null;
    this.idleTimer = null;
    this.retryAt = null;
    this.setState('paused');
  }
}

let secondary = null;

/**
 * Claims the one secondary stream slot (a job or log stream next to the
 * feed), stopping whichever stream held it. Browsers allow six HTTP/1.1
 * connections per host, so a tab keeps at most two streams open.
 */
export function claimSecondary(client) {
  if (secondary && secondary !== client) secondary.stop();
  secondary = client;
  return client;
}

/** Releases the secondary slot if `client` holds it. */
export function releaseSecondary(client) {
  if (secondary === client) secondary = null;
}

/** The client holding the secondary slot, for tests. */
export function currentSecondary() {
  return secondary;
}

/**
 * Batches pushed items and hands them to `flush` at most once per
 * `intervalMs` (default 100 ms, so at most 10 renders a second).
 */
export function createCoalescer(flush, intervalMs = 100, env = {}) {
  const timers = { ...defaultEnv(), ...env };
  let queue = [];
  let timer = null;
  let last = -Infinity;
  const run = () => {
    timer = null;
    last = timers.now();
    const batch = queue;
    queue = [];
    if (batch.length > 0) flush(batch);
  };
  return {
    push(item) {
      queue.push(item);
      if (timer !== null) return;
      const wait = Math.max(0, last + intervalMs - timers.now());
      timer = timers.setTimeout(run, wait);
    },
    flushNow() {
      if (timer !== null) timers.clearTimeout(timer);
      run();
    },
    cancel() {
      if (timer !== null) timers.clearTimeout(timer);
      timer = null;
      queue = [];
    },
  };
}
