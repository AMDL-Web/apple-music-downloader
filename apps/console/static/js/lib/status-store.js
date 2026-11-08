// One read per status endpoint for the whole tab. A page that shows server
// state subscribes to its keys instead of polling on its own: a key with an
// interval is read while anything subscribes to it, at most once per
// interval, and its last answer is what a page paints first. Pure: the
// fetchers, the clock, the timers and the visibility test come in, so
// node --test drives it (ui/system-status.js binds it to the API).

/**
 * @typedef {{data?: any, error?: any, at: number, okAt: number}} StatusEntry
 * The last answer of a key: `data` from the last success (a failed read
 * keeps it), `error` from the last read if it failed, `at` when it answered
 * and `okAt` when the last success did (0 before one).
 */

/**
 * `fetchers` maps each key to a function returning a promise of its data;
 * `every` gives a key's interval in ms (absent or 0: read on the first
 * subscribe, after `invalidate` and on `bumpEpoch` only). While `visible()`
 * is false interval reads are skipped; `focus()` reads every polled key not
 * read for `focusGapMs`.
 */
export function createStatusStore({ fetchers, every = {}, now = Date.now, setTimer = setTimeout, clearTimer = clearTimeout, visible = () => true, focusGapMs = 5000 }) {
  const slots = new Map();
  const listeners = new Set();

  const slot = (key) => {
    if (!Object.hasOwn(fetchers, key)) throw new Error(`unknown status key: ${key}`);
    let s = slots.get(key);
    if (!s) {
      s = { entry: undefined, subs: 0, gen: 0, inflight: null, ranAt: 0, stale: false, timer: null };
      slots.set(key, s);
    }
    return s;
  };
  const interval = (key) => (every[key] > 0 ? every[key] : 0);

  const publish = (key, entry) => {
    slot(key).entry = entry;
    for (const l of listeners) if (l.keys.has(key)) l.onChange();
  };

  // A read that a newer read, a prime or an epoch has overtaken is dropped.
  function run(key, fresh) {
    const s = slot(key);
    if (s.inflight && !fresh) return s.inflight;
    const gen = ++s.gen;
    s.ranAt = now();
    s.stale = false;
    const done = (entry) => {
      if (gen !== s.gen) return;
      s.inflight = null;
      publish(key, entry);
    };
    let read;
    try {
      read = Promise.resolve(fetchers[key]());
    } catch (error) {
      read = Promise.reject(error);
    }
    s.inflight = read.then(
        (data) => done({ data, error: undefined, at: now(), okAt: now() }),
        (error) => done({ data: s.entry ? s.entry.data : undefined, error, at: now(), okAt: s.entry ? s.entry.okAt : 0 }),
      );
    return s.inflight;
  }

  // The next interval read: `delay` after a skipped tick (hidden tab, or a
  // read still in flight), else one interval after the last read began.
  function arm(key, delay) {
    const s = slot(key);
    if (s.timer !== null) clearTimer(s.timer);
    s.timer = null;
    const ms = interval(key);
    if (!ms || s.subs === 0) return;
    s.timer = setTimer(
      () => {
        s.timer = null;
        if (s.subs === 0) return;
        if (!visible() || s.inflight) return arm(key, ms);
        run(key, false);
        arm(key);
      },
      delay !== undefined ? delay : Math.max(0, s.ranAt + ms - now()),
    );
  }

  function refresh(key) {
    const p = run(key, true);
    arm(key);
    return p;
  }

  function invalidate(keys) {
    for (const key of keys) {
      const s = slot(key);
      if (s.subs > 0) refresh(key);
      else {
        s.gen++;
        s.inflight = null;
        s.stale = true;
      }
    }
  }

  const isFresh = (s, ms) => s.entry !== undefined && !s.stale && (!ms || now() - s.ranAt < ms);

  return {
    /** The last answer of `key`, or undefined before its first. */
    get: (key) => slot(key).entry,

    /**
     * Subscribes `onChange` to `keys`; each key with an interval polls while
     * it has a subscriber. A key read within its interval is not read again.
     * Returns the unsubscribe function.
     */
    subscribe(keys, onChange) {
      const l = { keys: new Set(keys), onChange };
      listeners.add(l);
      for (const key of l.keys) {
        const s = slot(key);
        s.subs++;
        if (s.subs > 1) continue;
        if (!isFresh(s, interval(key))) run(key, false);
        arm(key);
      }
      return () => {
        if (!listeners.delete(l)) return;
        for (const key of l.keys) {
          const s = slot(key);
          s.subs--;
          if (s.subs === 0 && s.timer !== null) {
            clearTimer(s.timer);
            s.timer = null;
          }
        }
      };
    },

    /** Reads `key` now, overtaking a read in flight; the interval restarts. */
    refresh,

    /** Stores an answer read elsewhere (the same GET), so subscribers need no read of their own. */
    prime(key, data) {
      const s = slot(key);
      s.gen++;
      s.inflight = null;
      s.ranAt = now();
      s.stale = false;
      publish(key, { data, error: undefined, at: now(), okAt: now() });
      arm(key);
    },

    /** Reads the subscribed keys of `keys` again now; the others on their next subscribe. */
    invalidate,

    /** After an outage every answer is suspect: invalidates every key. */
    bumpEpoch: () => invalidate(Object.keys(fetchers)),

    /** The tab came back: reads each polled key not read for focusGapMs. */
    focus() {
      for (const [key, s] of slots) {
        if (s.subs === 0 || !interval(key) || now() - s.ranAt < focusGapMs) continue;
        run(key, false);
        arm(key);
      }
    },
  };
}
