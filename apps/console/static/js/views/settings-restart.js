// The restart that applies saved keys waiting for one (pending_restart of the
// config answers): POST /api/v1/system/restart, then GET /health until the
// server answers again. The save bar (views/settings-nav.js SaveBar) offers
// it while nothing is unsaved; its button keeps "Restarting…" until the
// configuration read afterwards empties pending_restart and the bar leaves.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { restartSystem, getHealth } from '../api.js';

// How long the restart waits for the server to answer again, and how often it asks.
const BACK_WITHIN_MS = 90000;
const POLL_MS = 1000;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/** Resolves once GET /health answers ok, false after BACK_WITHIN_MS. */
async function waitForHealth(alive) {
  const deadline = Date.now() + BACK_WITHIN_MS;
  // The server answers the restart request first and only then stops; a
  // health check right away would still reach the old process.
  await sleep(POLL_MS);
  while (alive() && Date.now() < deadline) {
    try {
      const h = await getHealth();
      if (h && h.status === 'ok') return true;
    } catch {
      // still restarting
    }
    await sleep(POLL_MS);
  }
  return false;
}

/**
 * `pending` is the config answer's pending_restart; `onRestarted` runs once
 * the server answers again, to read the configuration and status anew.
 * Returns {busy, restart}. A restart that worked stays busy until the next
 * config answer (a new `pending`), so the button never reads "Restart"
 * again before the bar leaves.
 */
export function useRestart({ pending, onRestarted, toast }) {
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);
  useEffect(
    () => () => {
      alive.current = false;
    },
    [],
  );
  useEffect(() => setBusy(false), [pending]);
  const restart = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await restartSystem();
      const back = await waitForHealth(() => alive.current);
      if (!alive.current) return;
      if (back) {
        onRestarted();
        return;
      }
      toast({ message: 'The server has not come back yet', role: 'danger' });
    } catch (err) {
      if (!alive.current) return;
      toast({ message: `Not restarted: ${err.message || err}`, role: 'danger' });
    }
    setBusy(false);
  };
  return { busy, restart };
}
