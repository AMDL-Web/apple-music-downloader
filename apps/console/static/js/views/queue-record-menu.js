// A record row's "…" menu on the Queue (items from lib/queue-rows.js
// recordMenu): Show details opens the record's drawer, Run next and Run
// last move its queued run, Pause, Resume and Retry act at once, Copy link
// copies its link, and Download again… and Remove from
// amdl… ask first. What it changes it tells the app at once (recordsChanged),
// ahead of the feed's resource_updated or resource_deleted.

import { html, useState } from '../ui/preact.js';
import { pauseRecord, resumeRecord, resetRecord, deleteRecord, cancelDownload } from '../api.js';
import { recordTitle, resetNote } from '../lib/records.js';
import { recordMenu, recordLink } from '../lib/queue-rows.js';
import { actionError } from '../lib/jobactions.js';
import { useApp, copyText } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { MenuButton } from '../ui/menu.js';

const TITLE_MAX = 48;
const RETRY_STATES = ['failed', 'given_up', 'missing'];

/**
 * Props: row (a record row: kind, record, live), href (its drawer),
 * onLeaving(key) as an action that may take the row out of the list starts,
 * place ({first, last}) and onMove('top'|'bottom') for its queued run.
 */
export function RecordMenu({ row, href, onLeaving, place, onMove }) {
  const app = useApp();
  const [ask, setAsk] = useState('');
  const [busy, setBusy] = useState(false);
  const { kind, record: r, live } = row;
  const name = recordTitle(r);
  const link = recordLink(kind, r) || (live ? live.input : '');
  const short = name.length > TITLE_MAX ? `${name.slice(0, TITLE_MAX - 1)}…` : name;
  const fail = (e) => app.toast({ message: `Couldn't do that: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
  const act = async (fn, done) => {
    try {
      const out = await fn();
      if (done) app.toast({ message: typeof done === 'function' ? done(out) : done, role: 'success' });
      if (app.recordsChanged) app.recordsChanged();
      return true;
    } catch (e) {
      fail(e);
      return false;
    }
  };
  const choose = (id) => {
    if (id === 'details') app.navigate(href);
    else if (id === 'top' || id === 'bottom') onMove(id);
    else if (id === 'copy') copyText(app, link);
    else if (id === 'again' || id === 'remove') setAsk(id);
    else if (id === 'pause') {
      onLeaving(row.id);
      // A run under way is cancelled, which pauses its record, as Activity always did.
      if (live) cancelDownload(live.id).then(() => app.recordsChanged && app.recordsChanged(), (e) => app.toast({ message: actionError('cancel', e), role: 'danger' }));
      else act(() => pauseRecord(kind, r.id), 'Paused');
    } else if (id === 'resume') act(() => resumeRecord(kind, r.id), 'Resumed');
    else if (id === 'retry') {
      onLeaving(row.id);
      act(() => resetRecord(kind, r.id, { what: 'audio', states: RETRY_STATES }), (out) => resetNote(out, r, `Downloading “${short}” again`));
    }
  };
  const confirm = async () => {
    setBusy(true);
    onLeaving(row.id);
    const ok =
      ask === 'remove'
        ? await act(() => deleteRecord(kind, r.id), `Removed “${short}” from amdl`)
        : await act(() => resetRecord(kind, r.id, { what: 'audio' }), (out) => resetNote(out, r, `Downloading “${short}” again`));
    setBusy(false);
    if (ok || ask === 'remove') setAsk('');
  };
  const done = r.tracks_done || 0;
  return [
    html`<${MenuButton}
      key="menu"
      className="job-menu-btn"
      label=${`Actions for ${name}`}
      title="More actions"
      heading=${name}
      items=${recordMenu(r, live, place, link)}
      onChoose=${choose}
    />`,
    ask === 'remove'
      ? html`<${ConfirmDialog}
          key="remove"
          title=${`Remove “${short}” from amdl?`}
          confirmLabel="Remove"
          busyLabel="Removing…"
          danger
          busy=${busy}
          onConfirm=${confirm}
          onCancel=${() => setAsk('')}
        >The files stay.</${ConfirmDialog}>`
      : null,
    ask === 'again'
      ? html`<${ConfirmDialog}
          key="again"
          title=${`Download “${short}” again?`}
          confirmLabel="Download again"
          busyLabel="Queueing…"
          busy=${busy}
          onConfirm=${confirm}
          onCancel=${() => setAsk('')}
        >${`Replaces the ${done} downloaded ${done === 1 ? 'file' : 'files'} once each new copy is complete.`}</${ConfirmDialog}>`
      : null,
  ];
}
