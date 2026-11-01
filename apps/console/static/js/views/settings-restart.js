// The restart notice on Settings: shown while saved keys wait for a restart
// (pending_restart of the config answers), with the action that restarts the
// server in place (POST /api/v1/system/restart). The button keeps its width
// while the restart runs; the page reads the configuration again once
// /health answers.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { restartSystem, getHealth } from '../api.js';
import { Notice, Button } from '../ui/components.js';
import { labelOf } from '../lib/settings-draft.js';

// How long the notice waits for the server to answer again, and how often it asks.
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
 * `pending` lists the dotted keys waiting for a restart; `onRestarted` runs
 * once the server answers again, to read the configuration and status anew.
 */
export function RestartNotice({ pending, offline, onRestarted, toast }) {
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);
  useEffect(
    () => () => {
      alive.current = false;
    },
    [],
  );
  if (!Array.isArray(pending) || pending.length === 0) return null;
  const restart = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await restartSystem();
      const back = await waitForHealth(() => alive.current);
      if (!alive.current) return;
      if (back) onRestarted();
      else toast({ message: 'The server has not come back yet', role: 'danger' });
    } catch (err) {
      if (alive.current) toast({ message: `Not restarted: ${err.message || err}`, role: 'danger' });
    } finally {
      if (alive.current) setBusy(false);
    }
  };
  const action = html`<${Button}
    variant="secondary"
    size="sm"
    className="settings-restart-button"
    aria-busy=${busy ? 'true' : undefined}
    aria-disabled=${busy || offline ? 'true' : undefined}
    data-busy=${busy ? '' : undefined}
    onClick=${busy || offline ? undefined : restart}
  >
    <span class="settings-restart-label" data-shown=${busy ? undefined : ''} aria-hidden=${busy ? 'true' : undefined}>Restart</span>
    <span class="settings-restart-label" data-shown=${busy ? '' : undefined} aria-hidden=${busy ? undefined : 'true'}>Restarting…</span>
  </${Button}>`;
  return html`<div class="settings-restart">
    <${Notice} role="info" icon="rotate" action=${action}>Restart to apply ${pending.map(labelOf).join(', ')}.</${Notice}>
  </div>`;
}
