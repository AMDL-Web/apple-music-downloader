// The status store (lib/status-store.js) bound to the API and the page: one
// read per status endpoint for the whole tab, shared by Overview, System,
// Settings, New download and the shell. Keys and intervals:
//   health, system, wrapper, librarySync, hooks  30 s
//   stats (GET /stats?days=STATS_DAYS)           60 s (the server caches 30 s)
//   config                                        no interval: first subscribe, invalidate, epoch
// An entry is {data, error, at}: `error` set when the last read failed,
// `data` from the last read that succeeded; undefined before the first answer.

import { useEffect, useRef, useState } from './preact.js';
import { getHealth, getSystem, getWrapperStatus, getLibrarySync, getHooks, getStats, getConfig } from '../api.js';
import { createStatusStore } from '../lib/status-store.js';

export const STATS_DAYS = 14;

const store = createStatusStore({
  fetchers: {
    health: getHealth,
    system: getSystem,
    wrapper: getWrapperStatus,
    librarySync: getLibrarySync,
    hooks: getHooks,
    stats: () => getStats(STATS_DAYS),
    config: getConfig,
  },
  every: { health: 30000, system: 30000, wrapper: 30000, librarySync: 30000, hooks: 30000, stats: 60000 },
  visible: () => document.visibilityState === 'visible',
});

window.addEventListener('focus', () => store.focus());
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') store.focus();
});

/**
 * The entries of `keys`, re-rendering when one changes; the first frame
 * paints what the tab already read.
 */
export function useStatus(keys) {
  const id = keys.join(' ');
  const [, setTick] = useState(0);
  const out = {};
  for (const k of keys) out[k] = store.get(k);
  const shown = useRef(out);
  shown.current = out;
  useEffect(() => {
    const off = store.subscribe(keys, () => setTick((n) => n + 1));
    // An answer that landed between this render and the subscription.
    if (keys.some((k) => store.get(k) !== shown.current[k])) setTick((n) => n + 1);
    return off;
  }, [id]);
  return out;
}

/** Reads `key` again now (a Retry or Refresh button). */
export const refreshStatus = (key) => store.refresh(key);
/** Reads `keys` again: now where shown, else on their next subscribe. */
export const invalidateStatus = (keys) => store.invalidate(keys);
/** Stores an answer of the same GET read elsewhere. */
export const primeStatus = (key, data) => store.prime(key, data);
/** The feed came back after an outage (app.epoch went up). */
export const statusEpoch = () => store.bumpEpoch();
