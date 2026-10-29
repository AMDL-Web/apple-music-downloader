// Settings panels that are not config fields: the library sync status with
// "Start over from now" and the configuration file (persisted, Reload).

import { html, useState } from '../ui/preact.js';
import { flattenConfig, diffConfig, reloadSummary, splitReloadError, librarySyncState, anchorResetState } from '../lib/settings-draft.js';
import { getLibrarySync, resetLibrarySync, reloadConfig } from '../api.js';
import { count } from '../lib/format.js';
import { settingsHash } from '../lib/route.js';
import { useApp, Badge, Button, KV, Notice, RelTime, SkeletonRows, ErrorNote } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { usePoll } from '../ui/hooks.js';

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * Live state from GET /library-sync (polled every 30 s, and again when a
 * save changes the switch or the interval) and "Start over from now" (POST
 * /library-sync/reset, which clears the anchor), offered only once a check
 * has recorded the library. `enabled`,
 * `interval` and `tokenSet` are the saved config values.
 */
export function LibrarySyncPanel({ enabled, interval, tokenSet }) {
  const app = useApp();
  const poll = usePoll(getLibrarySync, 30000, [app.epoch, enabled, interval]);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const s = poll.data;
  const resetState = anchorResetState(s);

  const reset = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await resetLibrarySync();
      app.toast({ message: `Library sync starts over: forgot ${plural(res.cleared, 'track', 'tracks')}; the next check downloads nothing`, role: 'success' });
      setAsking(false);
      poll.reload();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  let body;
  if (poll.error && !s) {
    body = html`<${ErrorNote} title="Couldn't load the library sync status" error=${poll.error} onRetry=${poll.reload} />`;
  } else if (!s) body = html`<${SkeletonRows} n=${4} kind="line" />`;
  else
    body = html`<${KV}
      rows=${[
        ['State', html`<${SyncState} view=${librarySyncState(s, tokenSet)} />`],
        ['Last check', s.last_poll_at ? html`<${RelTime} value=${s.last_poll_at} />` : 'Never'],
        ['Last error', s.last_error ? html`<span class="text-warning wrap-any">${s.last_error}</span>` : null],
        ['Last seen in your library', s.anchor_size > 0 ? plural(s.anchor_size, 'track', 'tracks') : 'Nothing yet; the next check records it'],
        ['Albums submitted', `${count(s.last_submitted)} last check · ${count(s.total_submitted)} since the backend started`],
      ]}
    />`;

  return html`<section class="settings-panel" data-panel="library-sync" aria-labelledby="sync-status-title">
    <div class="settings-panel-head">
      <h3 class="section-title" id="sync-status-title">Status</h3>
      <${Button} variant="ghost" size="sm" icon="rotate" onClick=${poll.reload}>Refresh</${Button}>
    </div>
    ${enabled && !tokenSet
      ? html`<${Notice} role="warning" icon="triangle-alert">
          Library sync is on but the media user token is empty, so it waits.${' '}
          <a class="link" href=${settingsHash('account', 'catalog.media_user_token')}>Set the token in Account</a>.
        </${Notice}>`
      : null}
    ${body}
    ${resetState.allowed
      ? html`<div class="settings-panel-actions">
          <${Button} variant="secondary" icon="rotate" className="sync-reset" onClick=${() => setAsking(true)} aria-describedby="sync-reset-note">Start over from now…</${Button}>
          <p class="muted small" id="sync-reset-note">Forgets which songs library sync has seen. Nothing is downloaded by this.</p>
        </div>`
      : null}
    ${asking
      ? html`<${ConfirmDialog}
          title="Start library sync over from now?"
          confirmLabel=${error ? 'Try again' : 'Start Over'}
          busyLabel="Starting over…"
          danger
          busy=${busy}
          onConfirm=${reset}
          onCancel=${() => {
            setAsking(false);
            setError(null);
          }}
        >
          <p>Library sync forgets what it has seen. Its next check records your library as it is then and downloads nothing, so songs added before that moment are never downloaded by library sync.</p>
          <p>To download one album again, submit its link on <a class="link" href="#/new">New download</a> instead.</p>
          ${error ? html`<${Notice} role="danger" icon="circle-x">${error.message}</${Notice}>` : null}
        </${ConfirmDialog}>`
      : null}
  </section>`;
}

/** The State badge with its detail (why it is idle, how often it polls) beside it. */
function SyncState({ view }) {
  return html`<span class="sync-state"><${Badge} view=${view} />${view.detail ? html`<span class="muted small wrap-any">${view.detail}</span>` : null}</span>`;
}

/**
 * Where the settings live: whether PUT writes them to the file, the last
 * reload error, and "Reload from file"
 * (POST /config/reload). onReloaded gets the ConfigResponse.
 */
export function ConfigFilePanel({ persisted, reloadError, dirty, base, onReloaded }) {
  const app = useApp();
  const [busy, setBusy] = useState(false);
  // What the last Reload did, kept on the page: {role, text} or null.
  const [outcome, setOutcome] = useState(null);
  // Counts the reloads made here, so an unchanged failure is announced again
  // (and the warning is not, when the panel is shown again later).
  const [tries, setTries] = useState(0);
  const reload = async () => {
    if (busy) return;
    setBusy(true);
    setOutcome(null);
    try {
      const res = await reloadConfig();
      onReloaded(res);
      setTries((n) => n + 1);
      // A file that could not be used is the warning under the button
      // (reloadError, which also survives leaving the page): said once.
      if (!res.reload_error) {
        const sum = reloadSummary(diffConfig(base, flattenConfig(res.config)));
        setOutcome({ role: 'success', text: sum.long });
        app.toast({ message: sum.short, role: 'success' });
      }
    } catch (e) {
      setOutcome({ role: 'danger', text: `Reload failed: ${e.message}` });
    } finally {
      setBusy(false);
    }
  };
  return html`<div class="settings-panel" data-panel="config-file">
    <${KV}
      rows=${[
        ['Saved to a file', persisted ? 'Yes: saved changes are written to configs/config.yaml and survive a restart' : 'No: saved changes last until the backend restarts'],
      ]}
    />
    <div class="settings-panel-actions">
      <${Button} variant="secondary" icon="rotate" className="config-reload" onClick=${reload} aria-disabled=${busy ? 'true' : undefined}>${busy ? 'Reloading…' : 'Reload from file'}</${Button}>
      <p class="muted small">Reads configs/config.yaml again, for example after a hand edit.${dirty ? ' Your unsaved changes stay pending.' : ''}</p>
    </div>
    ${outcome
      ? html`<div class="config-outcome" role=${outcome.role === 'success' ? undefined : 'alert'}>
          <${Notice} role=${outcome.role} icon=${outcome.role === 'danger' ? 'circle-x' : 'circle-check'}>${outcome.text}</${Notice}>
        </div>`
      : reloadError
        ? html`<${ReloadError} key=${tries} message=${reloadError} announce=${tries > 0} />`
        : null}
    <p class="muted small">
      Startup settings (listen address, database, log outputs, wrapper address, tools, catalog signing and request limits, worker pools) are not on this page: edit configs/config.yaml and restart. configs/config.example.yaml documents every key.
    </p>
  </div>`;
}

/**
 * The config file could not be used: the parser's reason, and the server's path
 * behind a details line. Only a reload made on this panel (`announce`) is an
 * alert; the warning shown again after leaving and returning is plain content.
 */
function ReloadError({ message, announce }) {
  const { reason, file } = splitReloadError(message);
  return html`<div class="config-outcome" role=${announce ? 'alert' : undefined}>
    <${Notice} role="warning" icon="triangle-alert">
      The config file could not be used, so the previous settings stay active.
      ${reason ? html`<span class="mono small wrap-any settings-reload-error">${reason}</span>` : null}
      ${file ? html`<details class="account-error-details"><summary>Which file</summary><span class="mono small wrap-any account-error-detail">${file}</span></details>` : null}
    </${Notice}>
  </div>`;
}
