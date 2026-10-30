// The status store (lib/status-store.js) bound to the API and the page: one
// read per status endpoint for the whole tab, shared by Overview, System,
// Settings, New download and the shell. Keys and intervals:
//   health, system, wrapper, librarySync, hooks  30 s
//   stats (GET /stats?days=STATS_DAYS)           60 s (the server caches 30 s)
//   logs (errors of the last 24 h, by cursor)     60 s
//   config                                        no interval: first subscribe, invalidate, epoch
// An entry is {data, error, at, okAt}: `error` set when the last read failed,
// `data` from the last read that succeeded (at okAt); undefined before the
// first answer.

import { useEffect, useRef, useState } from './preact.js';
import { getHealth, getSystem, getWrapperStatus, getLibrarySync, getHooks, getStats, getConfig, listLogs } from '../api.js';
import { createStatusStore } from '../lib/status-store.js';
import { STATS_DAYS, logsQuery, mergeLogs } from '../lib/sysdata.js';

export { STATS_DAYS };

// The error log of the last 24 hours: one read of it, then only what is
// newer (the cursor). With none in that window, `last` is the latest error
// kept at all, read once.
let logState = null;
async function readLogs() {
  const now = Date.now();
  const page = await listLogs(logsQuery(logState, now));
  const last = logState ? logState.last : undefined;
  logState = { ...mergeLogs(logState, page, now), last };
  if (logState.entries.length === 0 && last === undefined) {
    const one = await listLogs({ level: 'error', limit: 1 });
    logState = { ...logState, last: (one && Array.isArray(one.logs) && one.logs[one.logs.length - 1]) || null };
  }
  return logState;
}

const store = createStatusStore({
  fetchers: {
    health: getHealth,
    system: getSystem,
    wrapper: getWrapperStatus,
    librarySync: getLibrarySync,
    hooks: getHooks,
    stats: () => getStats(STATS_DAYS),
    logs: readLogs,
    config: getConfig,
  },
  every: { health: 30000, system: 30000, wrapper: 30000, librarySync: 30000, hooks: 30000, stats: 60000, logs: 60000 },
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
/** The feed came back after an outage (app.epoch went up); the server may have restarted, so the log cursor too. */
export const statusEpoch = () => {
  logState = null;
  store.bumpEpoch();
};
