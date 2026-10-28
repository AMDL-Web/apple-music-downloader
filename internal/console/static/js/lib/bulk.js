// Pure helper for bulk requests: run one async call per item with a bound on
// how many are in flight, and collect every outcome instead of stopping at
// the first failure.

/**
 * Calls `fn(item, index)` for every item with at most `n` calls in flight and
 * resolves, once all have settled, to one outcome per item in input order:
 * {item, ok: true, value} or {item, ok: false, error}. A rejection (or a
 * synchronous throw) never stops the rest. `onSettled(done, total, outcome)`
 * runs after each call settles, for progress; an error it throws is ignored.
 * `n` below 1 (or not a number) runs one at a time.
 */
export async function runLimited(items, n, fn, onSettled) {
  const list = Array.from(items || []);
  const out = new Array(list.length);
  const limit = Math.max(1, Math.floor(Number(n)) || 1);
  let next = 0;
  let done = 0;
  const worker = async () => {
    while (next < list.length) {
      const i = next++;
      try {
        out[i] = { item: list[i], ok: true, value: await fn(list[i], i) };
      } catch (error) {
        out[i] = { item: list[i], ok: false, error };
      }
      done += 1;
      if (onSettled) {
        try {
          onSettled(done, list.length, out[i]);
        } catch {
          // Progress reporting must not change the outcome.
        }
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, list.length) }, worker));
  return out;
}
