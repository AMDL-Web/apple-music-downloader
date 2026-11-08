// The Queue's Done tab heads with Download N missing while records have
// files a disk check found missing (POST /library/reset over the missing
// flag).

import { html, useState } from '../ui/preact.js';
import { resetRecords } from '../api.js';
import { count } from '../lib/format.js';
import { useApp, Icon, Button } from '../ui/components.js';

/** Props: summary (GET /library/summary), onChanged() after the reset. */
export function MissingBar({ summary, onChanged }) {
  const app = useApp();
  const [busy, setBusy] = useState(false);
  const missing = summary ? (summary.albums.flags.missing || 0) + (summary.playlists.flags.missing || 0) : 0;
  if (!missing) return null;
  const downloadMissing = async () => {
    setBusy(true);
    try {
      const r = await resetRecords({ what: 'audio', filter: { flag: ['missing'] } });
      app.toast({ message: `Reset the missing files of ${count(r.reset)} ${r.reset === 1 ? 'record' : 'records'}`, role: 'success' });
      if (app.recordsChanged) app.recordsChanged();
    } catch (e) {
      app.toast({ message: `Couldn't download them: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy(false);
    onChanged();
  };
  return html`<div class="card library-check" data-section="missing">
    <div class="card-body library-check-body">
      <span class="library-check-text">
        <${Icon} name="hard-drive" />
        <span>Files missing on disk</span>
      </span>
      <span class="library-check-actions">
        <${Button} size="sm" icon="download" disabled=${busy} onClick=${downloadMissing}>Download ${count(missing)} missing</${Button}>
      </span>
    </div>
  </div>`;
}
