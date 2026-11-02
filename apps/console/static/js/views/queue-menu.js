// An Activity row's "…" menu (items from lib/jobactions.js rowMenu): Show
// details opens the drawer, Open in Library its record, Move to top and
// Move to bottom reorder a queued job (views/queue-order.js), Cancel, Pause
// and Retry act at once, Copy link copies the job's link, and Download
// again… and Delete… ask first. One job's action runs alone, so the
// selection bar (a bulk run) stays out of it.

import { html, useState } from '../ui/preact.js';
import { cancelDownload, retryDownload, deleteDownload, createDownload, resetRecord } from '../api.js';
import { recordHash } from '../lib/route.js';
import { jobTitle, isUnresolved, inputLabel } from '../lib/format.js';
import { rowMenu, actionError, bulkSummary } from '../lib/jobactions.js';
import { useApp, copyText } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { MenuButton } from '../ui/menu.js';

const CALLS = { retry: retryDownload, cancel: cancelDownload, pause: cancelDownload, delete: deleteDownload };

// The delete dialog's title names the job, cut like the desktop app's.
const TITLE_MAX = 48;

export const jobName = (job) => (isUnresolved(job) ? inputLabel(job.input) : jobTitle(job));

/**
 * Runs `action` on one job (pause: the cancel of a record's run). A
 * failure toasts why (lib/jobactions.js actionError); a retry that went
 * through says so, since its new job may land on another tab. Resolves to
 * whether it went through.
 */
export async function runOne(app, action, job) {
  let res;
  try {
    res = await CALLS[action](job.id);
  } catch (error) {
    app.toast({ message: actionError(action === 'pause' ? 'cancel' : action, error), role: 'danger' });
    return false;
  }
  if (action === 'retry') {
    if (res && res.status === 'already_downloaded') app.toast({ message: `“${jobTitle(job)}” is already downloaded`, role: 'success' });
    else app.toast(bulkSummary(action, [{ ok: true, item: job }], jobTitle));
  }
  return true;
}

/**
 * The "…" button of a Queue row and its menu; `href` opens the job's drawer.
 * onLeaving(id) runs as an action that may take the row out of the list
 * starts (a confirmed delete, a cancel, a retry), while the row is still
 * there: the Queue finds the row focus moves to if it leaves. A queued job
 * adds Move to top and Move to bottom, which call onMove('top'|'bottom');
 * `place` ({first, last}, when known) leaves out the one that would not move it.
 */
export function JobMenu({ job, href, onLeaving, place, onMove }) {
  const app = useApp();
  const [asking, setAsking] = useState(false);
  const [again, setAgain] = useState(false);
  const [busy, setBusy] = useState(false);
  const name = jobName(job);
  const choose = (id) => {
    if (id === 'details') app.navigate(href);
    else if (id === 'record') app.navigate(recordHash(job.resource.kind, job.resource.id));
    else if (id === 'copy') copyText(app, job.input);
    else if (id === 'delete') setAsking(true);
    else if (id === 'again') setAgain(true);
    else if (id === 'top' || id === 'bottom') onMove(id);
    else {
      onLeaving(job.id);
      runOne(app, id, job);
    }
  };
  const remove = async () => {
    setBusy(true);
    onLeaving(job.id);
    await runOne(app, 'delete', job);
    setBusy(false);
    setAsking(false);
  };
  // A record's run: its record's tracks again, replacing the files at their
  // stored paths. A station's: the link queued again with the job's overrides.
  const downloadAgain = async () => {
    setBusy(true);
    try {
      if (job.resource) await resetRecord(job.resource.kind, job.resource.id, { what: 'audio' });
      else await createDownload({ urls: [job.input], overrides: { ...(job.overrides || {}) } });
      app.toast({ message: `Downloading “${jobTitle(job)}” again`, role: 'success' });
    } catch (error) {
      app.toast({ message: actionError('again', error), role: 'danger' });
    }
    setBusy(false);
    setAgain(false);
  };
  const short = name.length > TITLE_MAX ? `${name.slice(0, TITLE_MAX - 1)}…` : name;
  return [
    html`<${MenuButton}
      key="menu"
      className="job-menu-btn"
      label=${`Actions for ${name}`}
      title="More actions"
      heading=${name}
      items=${rowMenu(job, place)}
      onChoose=${choose}
    />`,
    asking
      ? html`<${ConfirmDialog}
          key="delete"
          title=${`Delete “${short}”?`}
          confirmLabel="Delete"
          busyLabel="Deleting…"
          danger
          busy=${busy}
          onConfirm=${remove}
          onCancel=${() => setAsking(false)}
        >Its history leaves Activity. ${job.resource ? 'Its library record and files stay.' : 'Files already saved stay on disk.'}</${ConfirmDialog}>`
      : null,
    again
      ? html`<${ConfirmDialog}
          key="again"
          title=${`Download “${short}” again?`}
          confirmLabel="Download again"
          busyLabel="Queueing…"
          busy=${busy}
          onConfirm=${downloadAgain}
          onCancel=${() => setAgain(false)}
        >${job.resource ? 'Replaces its downloaded files.' : 'Queues its link again.'}</${ConfirmDialog}>`
      : null,
  ];
}
