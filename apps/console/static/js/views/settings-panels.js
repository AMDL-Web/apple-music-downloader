// Settings action blocks that are not config fields: library sync's "Start
// over from now" and the configuration file (saved to a file, Reload).

import { html, useState } from '../ui/preact.js';
import { flattenConfig, diffConfig, reloadSummary, splitReloadError } from '../lib/settings-draft.js';
import { resetLibrarySync, reloadConfig } from '../api.js';
import { count } from '../lib/format.js';
import { useApp, Button, Icon, Notice } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { invalidateStatus } from '../ui/system-status.js';

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * "Start over from now" (POST /library-sync/reset, which clears the anchor),
 * offered only once a check has recorded the library (`sync` is the
 * GET /library-sync answer).
 */
export function SyncResetAction({ sync, disabled }) {
  const app = useApp();
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  if (!sync || !(sync.anchor_size > 0)) return null;
  const reset = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await resetLibrarySync();
      app.toast({ message: `Library sync starts over: forgot ${plural(res.cleared, 'track', 'tracks')}`, role: 'success' });
      setAsking(false);
      invalidateStatus(['librarySync']);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  return html`<div class="settings-actions" data-action="sync-reset">
    <span class="settings-actions-label">Start over from now</span>
    <${Button} variant="secondary" className="sync-reset" onClick=${disabled ? undefined : () => setAsking(true)} aria-disabled=${disabled ? 'true' : undefined} title=${disabled ? 'Offline' : undefined}>Start Over…</${Button}>
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
          <p>Albums you added before now won't be downloaded.</p>
          ${error ? html`<${Notice} role="danger" icon="circle-x">${error.message}</${Notice}>` : null}
        </${ConfirmDialog}>`
      : null}
  </div>`;
}

/**
 * Where the settings live: whether PUT writes them to the file, the last
 * reload error, and "Reload from file" (POST /config/reload), then the one
 * line naming the startup settings no row shows. onReloaded gets the
 * ConfigResponse.
 */
export function ConfigFilePanel({ persisted, reloadError, dirty, base, onReloaded, disabled }) {
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
    <dl class="fact-list">
      <div class="fact-row" data-fact="persisted" data-role=${persisted ? undefined : 'warning'}>
        <dt class="fact-row-text"><span class="fact-row-label">Saved to a file</span></dt>
        <dd class="fact-row-value">
          ${persisted ? null : html`<${Icon} name="triangle-alert" size="sm" />`}
          <span class="fact-row-text-value">${persisted ? 'Yes' : 'No: saved changes last until a restart'}</span>
        </dd>
      </div>
    </dl>
    <div class="settings-actions" data-action="config-reload">
      <span class="settings-actions-label">${dirty ? 'Your unsaved changes stay pending' : 'configs/config.yaml'}</span>
      <${Button}
        variant="secondary"
        icon="rotate"
        className="config-reload"
        onClick=${disabled ? undefined : reload}
        aria-disabled=${busy || disabled ? 'true' : undefined}
        title=${disabled ? 'Offline' : undefined}
      >${busy ? 'Reloading…' : 'Reload from file'}</${Button}>
    </div>
    ${outcome
      ? html`<div class="config-outcome" role=${outcome.role === 'success' ? undefined : 'alert'}>
          <${Notice} role=${outcome.role} icon=${outcome.role === 'danger' ? 'circle-x' : 'circle-check'}>${outcome.text}</${Notice}>
        </div>`
      : reloadError
        ? html`<${ReloadError} key=${tries} message=${reloadError} announce=${tries > 0} />`
        : null}
    <p class="settings-footnote">Also read at startup from configs/config.yaml: log outputs, session service timeouts, token cache.</p>
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
