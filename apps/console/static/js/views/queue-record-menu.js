// A record row's "…" menu on the Queue (items from lib/queue-rows.js
// recordMenu): Show details opens the record's drawer, Run next and Run
// last move its queued run, Pause, Resume, Retry and Check files act at
// once, Copy link copies its link, and Download again… and Remove from
// amdl… ask first. What it changes it tells the app (recordsChanged), since
// a pause, resume or removal with no run sends no resource_updated.

import { html, useState, useEffect } from '../ui/preact.js';
import { pauseRecord, resumeRecord, resetRecord, deleteRecord, startCheck, cancelDownload } from '../api.js';
import { recordTitle, checkScope, checkBody, checkText, resetNote } from '../lib/records.js';
import { recordMenu, recordLink } from '../lib/queue-rows.js';
import { actionError } from '../lib/jobactions.js';
import { useApp, copyText } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { MenuButton } from '../ui/menu.js';

const TITLE_MAX = 48;
const RETRY_STATES = ['failed', 'given_up', 'missing'];

// The checks a menu started, by scope: their result is said once in a toast.
const started = new Set();

/** Says the result of each check a record menu started (mounted once by the Queue). */
export function useCheckToasts(app) {
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        for (const m of batch) {
          if (m.type !== 'check_updated' || !m.check || !m.check.finished_at || !started.has(m.check.scope)) continue;
          started.delete(m.check.scope);
          app.toast({ key: `check:${m.check.scope}`, message: checkText(m.check), role: m.check.aborted_reason ? 'danger' : 'success' });
        }
      }),
    [],
  );
}

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
  const check = async () => {
    const scope = checkScope(kind, r.id);
    try {
      const out = await startCheck(checkBody(scope));
      if (out && out.check && out.check.finished_at) app.toast({ key: `check:${scope}`, message: checkText(out.check), role: 'success' });
      else {
        started.add(scope);
        app.toast({ key: `check:${scope}`, message: `Checking “${short}”`, role: 'success' });
      }
    } catch (e) {
      fail(e);
    }
  };
  const choose = (id) => {
    if (id === 'details') app.navigate(href);
    else if (id === 'top' || id === 'bottom') onMove(id);
    else if (id === 'copy') copyText(app, link);
    else if (id === 'check') check();
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
        >The files stay. Adding it again picks them up.</${ConfirmDialog}>`
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
