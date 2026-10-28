// Settings panels that are not config fields: the library sync status with
// "Reset anchor", the configuration file (persisted, pinned keys, Reload),
// and the path template reference.

import { html, useState } from '../ui/preact.js';
import { getLibrarySync, resetLibrarySync, reloadConfig } from '../api.js';
import { TEMPLATE_VARIABLES, PADDED_VARIABLES } from '../lib/settings-template.js';
import { librarySyncView } from '../lib/status.js';
import { count } from '../lib/format.js';
import { settingsHash } from '../lib/route.js';
import { useApp, Badge, Button, KV, Notice, RelTime, SkeletonRows, ErrorNote } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { usePoll } from '../ui/hooks.js';

const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;

/**
 * Live state from GET /library-sync (polled every 30 s) and Reset anchor.
 * `enabled` and `tokenSet` are the saved config values, for the
 * missing-token warning.
 */
export function LibrarySyncPanel({ enabled, tokenSet }) {
  const app = useApp();
  const poll = usePoll(getLibrarySync, 30000, [app.epoch]);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const s = poll.data;

  const reset = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await resetLibrarySync();
      app.toast({ message: `Anchor cleared (${plural(res.cleared, 'track', 'tracks')}); the next poll re-anchors without submitting`, role: 'success' });
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
    body =
      poll.error.status === 503
        ? html`<p class="muted small">This process has no library watcher.</p>`
        : html`<${ErrorNote} title="Couldn't load the library sync status" error=${poll.error} onRetry=${poll.reload} />`;
  } else if (!s) body = html`<${SkeletonRows} n=${4} kind="line" />`;
  else
    body = html`<${KV}
      rows=${[
        ['State', html`<${Badge} view=${librarySyncView({ data: s })} />`],
        ['Last poll', s.last_poll_at ? html`<${RelTime} value=${s.last_poll_at} />` : 'Never'],
        ['Last error', s.last_error ? html`<span class="text-warning wrap-any">${s.last_error}</span>` : null],
        ['Anchor', s.anchor_size > 0 ? plural(s.anchor_size, 'track', 'tracks') : 'Not set; the next poll sets it'],
        ['Albums submitted', `${count(s.last_submitted)} last poll · ${count(s.total_submitted)} since the backend started`],
      ]}
    />`;

  return html`<section class="settings-panel" data-panel="library-sync" aria-labelledby="sync-status-title">
    <div class="settings-panel-head">
      <h3 class="section-title" id="sync-status-title">Status</h3>
      <${Button} variant="ghost" size="sm" icon="rotate" onClick=${poll.reload}>Refresh</${Button}>
    </div>
    ${enabled && !tokenSet
      ? html`<${Notice} role="warning" icon="triangle-alert">
          Library sync is on but the media user token is empty, so the monitor idles.
          <a class="link" href=${settingsHash('catalog')}>Set the token in Catalog</a>.
        </${Notice}>`
      : null}
    ${body}
    <div class="settings-panel-actions">
      <${Button} variant="secondary" icon="rotate" className="sync-reset" onClick=${() => setAsking(true)} disabled=${!s}>Reset anchor…</${Button}>
      <p class="muted small">Forgets which songs the monitor has seen. Nothing is downloaded by the reset.</p>
    </div>
    ${asking
      ? html`<${ConfirmDialog}
          title="Reset the library anchor?"
          confirmLabel=${error ? 'Try again' : 'Reset anchor'}
          busyLabel="Resetting…"
          danger
          busy=${busy}
          onConfirm=${reset}
          onCancel=${() => {
            setAsking(false);
            setError(null);
          }}
        >
          <p>The monitor forgets its history. Its next poll records your library as it is then and submits no albums, so songs added before that moment are never downloaded by library sync.</p>
          <p>To download one album again, submit its link on <a class="link" href="#/new">New download</a> instead.</p>
          ${error ? html`<${Notice} role="danger" icon="circle-x">${error.message}</${Notice}>` : null}
        </${ConfirmDialog}>`
      : null}
  </section>`;
}

/**
 * Where the settings live: whether PUT writes them to the file, the keys an
 * environment variable pins, the last reload error, and "Reload from file"
 * (POST /config/reload). onReloaded gets the ConfigResponse.
 */
export function ConfigFilePanel({ persisted, pinned, reloadError, dirty, onReloaded }) {
  const app = useApp();
  const [busy, setBusy] = useState(false);
  const reload = async () => {
    setBusy(true);
    try {
      const res = await reloadConfig();
      onReloaded(res);
      app.toast(
        res.reload_error
          ? { message: 'The config file could not be used; the previous settings stay active', role: 'warning', sticky: true }
          : { message: 'Settings reloaded from the config file', role: 'success' },
      );
    } catch (e) {
      app.toast({ message: e.message, role: 'danger' });
    } finally {
      setBusy(false);
    }
  };
  const list = Array.isArray(pinned) ? pinned : [];
  return html`<div class="settings-panel" data-panel="config-file">
    <${KV}
      rows=${[
        ['Saved to a file', persisted ? 'Yes: saved changes are written to configs/config.yaml and survive a restart' : 'No: saved changes last until the backend restarts'],
        ['Pinned keys', list.length ? plural(list.length, 'key', 'keys') : 'None'],
      ]}
    />
    ${reloadError
      ? html`<${Notice} role="warning" icon="triangle-alert">
          The config file could not be used, so the previous settings stay active.
          <span class="mono small wrap-any settings-reload-error">${reloadError}</span>
        </${Notice}>`
      : null}
    ${list.length
      ? html`<div class="table-wrap">
          <table class="table pinned-table">
            <caption class="sr-only">Keys pinned by environment variables</caption>
            <thead><tr><th scope="col">Key</th><th scope="col">Set by</th></tr></thead>
            <tbody>
              ${list.map((p) => html`<tr key=${p.key}><td class="mono" data-label="Key">${p.key}</td><td class="mono" data-label="Set by">${p.env}</td></tr>`)}
            </tbody>
          </table>
        </div>
        <p class="muted small">A pinned key is shown but cannot be changed here: the variable wins on every reload. Unset it and restart the backend to manage the key on this page.</p>`
      : null}
    <div class="settings-panel-actions">
      <${Button} variant="secondary" icon="rotate" className="config-reload" onClick=${reload} aria-disabled=${busy ? 'true' : undefined}>${busy ? 'Reloading…' : 'Reload from file'}</${Button}>
      <p class="muted small">Reads configs/config.yaml again, for example after a hand edit.${dirty ? ' Your unsaved changes stay pending.' : ''}</p>
    </div>
    <p class="muted small">
      Startup settings (listen address, database, log outputs, wrapper address, tools, catalog signing and request limits, worker pools) are not on this page: edit configs/config.yaml and restart. configs/config.example.yaml documents every key.
    </p>
  </div>`;
}

/** The path template variables, collapsed by default. */
export function TemplateReference() {
  return html`<details class="template-ref">
    <summary class="template-ref-summary">Template variables</summary>
    <div class="template-ref-body">
      <p class="small">
        "/" separates folders and the last part is the file name; ".m4a" is added. Each part is cleaned on its own, so a value
        cannot create folders. Unknown variables stay as written. Numbers marked "pads" take a width, as in <code>{TrackNumber:02d}</code>.
      </p>
      <dl class="template-vars">
        ${TEMPLATE_VARIABLES.map(
          ([name, text]) => html`<div key=${name} class="template-var">
            <dt><code>{${name}}</code></dt>
            <dd>${text}${PADDED_VARIABLES.includes(name) ? html` <span class="chip">pads</span>` : null}</dd>
          </div>`,
        )}
      </dl>
    </div>
  </details>`;
}
