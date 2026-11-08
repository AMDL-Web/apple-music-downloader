// The last answer of each page read, kept for the tab's lifetime: a page opened
// again draws what it showed before while it reads again (stale while
// revalidate), instead of flashing its placeholders on every visit.

const entries = new Map();
export const CACHE_MAX = 64;

/** The value remembered under `key`, or undefined (also for an empty key). */
export function cached(key) {
  return key ? entries.get(key) : undefined;
}

/** Remembers `value` under `key`; the least recently stored entry goes past CACHE_MAX. */
export function remember(key, value) {
  if (!key) return;
  entries.delete(key);
  entries.set(key, value);
  if (entries.size > CACHE_MAX) entries.delete(entries.keys().next().value);
}

/** Forgets everything (tests, and a feed resync that makes old answers suspect). */
export function forgetAll() {
  entries.clear();
}
