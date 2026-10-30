// Shared Preact hooks for timers, polling and layout measurement.

import { useEffect, useLayoutEffect, useRef, useState, useCallback } from './preact.js';

const nowListeners = new Set();
let nowTimer = null;

/** The current time, re-rendering every 15 s; one shared interval for all relative timestamps. */
export function useNow(intervalMs = 15000) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    nowListeners.add(setNow);
    if (nowTimer === null) {
      nowTimer = setInterval(() => {
        const t = Date.now();
        for (const fn of nowListeners) fn(t);
      }, intervalMs);
    }
    return () => {
      nowListeners.delete(setNow);
      if (nowListeners.size === 0 && nowTimer !== null) {
        clearInterval(nowTimer);
        nowTimer = null;
      }
    };
  }, []);
  return now;
}

/** The current time, re-rendering every `intervalMs`; 0 stops ticking. */
export function useClock(intervalMs) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!(intervalMs > 0)) return undefined;
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

/** A value that follows `value` after it has been stable for `ms`. */
export function useDebounced(value, ms) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

const FOCUS_REFRESH_MS = 5000;

/**
 * Runs `load` now and every `intervalMs` while the tab is visible, and again
 * when the tab regains focus. Returns {data, error, loading, reload}.
 * `load` must return a promise; deps restart the cycle. Status endpoints
 * shared between pages read through ui/system-status.js instead.
 */
export function usePoll(load, intervalMs, deps = []) {
  const [state, setState] = useState({ data: undefined, error: undefined, loading: true });
  const loadRef = useRef(load);
  loadRef.current = load;
  const seq = useRef(0);
  const lastRun = useRef(0);
  const run = useCallback(() => {
    lastRun.current = Date.now();
    const mine = ++seq.current;
    return loadRef.current().then(
      (data) => {
        if (mine !== seq.current) return;
        setState({ data, error: undefined, loading: false });
      },
      (error) => {
        if (mine === seq.current) setState((s) => ({ data: s.data, error, loading: false }));
      },
    );
  }, []);
  useEffect(() => {
    setState((s) => ({ ...s, loading: true }));
    run();
    let timer = null;
    const schedule = () => {
      if (timer !== null) clearInterval(timer);
      timer = intervalMs > 0 ? setInterval(() => {
        if (document.visibilityState === 'visible') run();
      }, intervalMs) : null;
    };
    schedule();
    // Focus and visibility can fire in bursts (window switches, tab
    // previews); refresh on them at most once per FOCUS_REFRESH_MS.
    const onFocus = () => {
      if (Date.now() - lastRun.current >= FOCUS_REFRESH_MS) run();
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') onFocus();
    };
    if (intervalMs > 0) {
      window.addEventListener('focus', onFocus);
      document.addEventListener('visibilitychange', onVisible);
    }
    return () => {
      seq.current++;
      if (timer !== null) clearInterval(timer);
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, deps);
  return { ...state, reload: run };
}

/**
 * How many columns of at least `min` px (plus `gap`) fit in the element's
 * width. Measured before the first paint, so a grid sized by it is drawn at
 * its final size.
 */
export function useColumns(ref, min, gap) {
  const [cols, setCols] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return undefined;
    const measure = () => {
      const w = el.clientWidth;
      setCols(Math.max(1, Math.floor((w + gap) / (min + gap))));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [min, gap]);
  return cols;
}

/** Whether the user asked for reduced motion; follows changes. */
export function useReducedMotion() {
  const query = '(prefers-reduced-motion: reduce)';
  const [reduced, setReduced] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const on = () => setReduced(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return reduced;
}

/** Whether a media query matches; follows changes. */
export function useMedia(query) {
  const [match, setMatch] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const on = () => setMatch(mq.matches);
    mq.addEventListener('change', on);
    on();
    return () => mq.removeEventListener('change', on);
  }, [query]);
  return match;
}
