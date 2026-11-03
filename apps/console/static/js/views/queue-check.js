// The Queue's Done tab heads with the disk check: the last check's line,
// Download N missing while a check found some, Mark them anyway after a
// check that stopped on too many missing, and Check files (POST
// /library/check over the whole library).

import { html, useState } from '../ui/preact.js';
import { startCheck, resetRecords } from '../api.js';
import { checkText, checkRunning, checkBody } from '../lib/records.js';
import { count } from '../lib/format.js';
import { useApp, Icon, Button, RelTime } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';

/** Props: summary (GET /library/summary, its check kept live by the feed), onChanged() after an action. */
export function CheckBar({ summary, onChanged }) {
  const app = useApp();
  const [busy, setBusy] = useState('');
  const [asking, setAsking] = useState(false);
  const check = summary && summary.check;
  const checking = checkRunning(check);
  const fail = (what, e) => app.toast({ message: `Couldn't ${what}: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
  const runCheck = async (force = false) => {
    setBusy('check');
    try {
      // Mark them anyway checks again what the stopped check covered.
      await startCheck(force ? checkBody(check && check.scope, true) : {});
      onChanged();
    } catch (e) {
      fail('check the files', e);
    }
    setBusy('');
    setAsking(false);
  };
  const missing = summary ? (summary.albums.flags.missing || 0) + (summary.playlists.flags.missing || 0) : 0;
  const downloadMissing = async () => {
    setBusy('missing');
    try {
      const r = await resetRecords({ what: 'audio', filter: { flag: ['missing'] } });
      app.toast({ message: `Reset the missing files of ${count(r.reset)} ${r.reset === 1 ? 'record' : 'records'}`, role: 'success' });
      if (app.recordsChanged) app.recordsChanged();
    } catch (e) {
      fail('download them', e);
    }
    setBusy('');
    onChanged();
  };
  return [
    html`<div key="bar" class="card library-check" data-section="check">
      <div class="card-body library-check-body">
        <span class="library-check-text">
          <${Icon} name="hard-drive" />
          <span>${check ? [checkText(check), check.finished_at ? html` · <${RelTime} key="t" value=${check.finished_at} />` : null] : summary ? 'Not checked yet' : ' '}</span>
        </span>
        <span class="library-check-actions">
          ${missing ? html`<${Button} size="sm" icon="download" disabled=${Boolean(busy)} onClick=${downloadMissing}>Download ${count(missing)} missing</${Button}>` : null}
          ${check && check.aborted_reason === 'too_many_missing'
            ? html`<${Button} size="sm" variant="ghost" disabled=${Boolean(busy) || checking} onClick=${() => setAsking(true)}>Mark them anyway</${Button}>`
            : null}
          <${Button} size="sm" icon="hard-drive" className="library-check-btn" disabled=${Boolean(busy) || checking} aria-busy=${checking ? 'true' : undefined} onClick=${() => runCheck(false)}>${checking ? 'Checking…' : 'Check files'}</${Button}>
        </span>
      </div>
    </div>`,
    asking
      ? html`<${ConfirmDialog}
          key="force"
          title="Mark the missing files?"
          confirmLabel="Check and mark"
          busyLabel="Starting…"
          danger
          busy=${busy === 'check'}
          onConfirm=${() => runCheck(true)}
          onCancel=${() => setAsking(false)}
        >More than half the files were not found; if the library folder is not mounted, every one of them is marked missing.</${ConfirmDialog}>`
      : null,
  ];
}
