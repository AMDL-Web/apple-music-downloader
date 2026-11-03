// Queue selection mode and bulk actions: the bar over the list (select all,
// the count, the actions the selection allows, the progress of a run), the
// confirmation before a bulk Download again or Remove, and the runner that
// sends at most BULK_LIMIT requests at a time and ends with one summary
// toast. A record row acts on its record, a station row on its run; rows
// leave the list through the feed and the read again after, never by hand.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { cancelDownload, retryDownload, deleteDownload, resetRecord, pauseRecord, deleteRecord } from '../api.js';
import { jobTitle } from '../lib/format.js';
import { recordTitle } from '../lib/records.js';
import { runLimited } from '../lib/bulk.js';
import { selectionState } from '../lib/selection.js';
import { rowsProgressText, rowsSummary } from '../lib/queue-rows.js';
import { actionReason } from '../lib/jobactions.js';
import { Button } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';

export const BULK_LIMIT = 4;

const RETRY_STATES = ['failed', 'given_up', 'missing'];

/** One row's request for `action`. */
function call(action, row) {
  if (row.kind === 'station') {
    const id = row.job.id;
    return { retry: retryDownload, pause: cancelDownload, remove: deleteDownload }[action](id);
  }
  const { kind, record: r, live } = row;
  if (action === 'retry') return resetRecord(kind, r.id, { what: 'audio', states: RETRY_STATES });
  if (action === 'again') return resetRecord(kind, r.id, { what: 'audio' });
  if (action === 'pause') return live ? cancelDownload(live.id) : pauseRecord(kind, r.id);
  return deleteRecord(kind, r.id);
}

/** A row's name in a summary. */
export const rowName = (row) => (row.kind === 'station' ? jobTitle(row.job) : recordTitle(row.record));

const REASON = { retry: 'retry', again: 'again', pause: 'cancel', remove: 'delete' };

/**
 * The bulk runner. start(action, rows) sends the action's request for every
 * row, BULK_LIMIT at a time, keeps {action, done, total} in `run` meanwhile,
 * toasts the summary when all have settled and tells the app records
 * changed. One run at a time.
 */
export function useBulkRun(app) {
  const [run, setRun] = useState(null);
  const busy = useRef(false);
  const start = async (action, rows) => {
    if (busy.current || rows.length === 0) return;
    busy.current = true;
    setRun({ action, done: 0, total: rows.length });
    const outcomes = await runLimited(rows, BULK_LIMIT, (row) => call(action, row), (done, total) => setRun({ action, done, total }));
    busy.current = false;
    setRun(null);
    app.toast(rowsSummary(action, outcomes, rowName, (e) => actionReason(REASON[action], e)));
    if (app.recordsChanged) app.recordsChanged();
  };
  return { run, start };
}

const ACTION_VIEW = {
  retry: { icon: 'rotate', variant: 'secondary', label: 'Retry' },
  again: { icon: 'rotate', variant: 'secondary', label: 'Download again' },
  pause: { icon: 'pause', variant: 'secondary', label: 'Pause' },
  remove: { icon: 'trash', variant: 'danger', label: 'Remove' },
};

/** Progress of a bulk run: "Removing 3 of 12" and a meter. */
function BulkProgress({ run }) {
  const text = rowsProgressText(run.action, run.done, run.total);
  return html`<div class="bulk-progress">
    <span class="bulk-progress-text">${text}</span>
    <span class="bulk-meter" role="progressbar" aria-label=${text} aria-valuemin="0" aria-valuemax=${run.total} aria-valuenow=${run.done} aria-valuetext=${text}>
      <span class="bulk-meter-fill" style=${{ '--f': String(run.total > 0 ? run.done / run.total : 0) }}></span>
    </span>
  </div>`;
}

/**
 * The bar over the list: .bulk-bar[data-state=select|run]. While selecting
 * it holds the select-all checkbox (.bulk-all), the count (.bulk-count) and
 * one button per action the selection allows (.bulk-action[data-action],
 * hidden at zero); during a run the progress replaces the count and the
 * buttons. `plan` is lib/queue-rows.js rowPlan of the selection.
 */
export function SelectionBar({ rows, selected, count, shown, plan, selecting, run, onSelectAll, onAction }) {
  const state = selectionState(selected, rows);
  const all = useRef(null);
  useEffect(() => {
    if (all.current) all.current.indeterminate = state === 'some';
  }, [state, selecting]);
  const actions = ['retry', 'again', 'pause', 'remove']
    .filter((a) => plan[a].length > 0)
    .map((a) => {
      const v = ACTION_VIEW[a];
      return html`<${Button} key=${a} variant=${v.variant} icon=${v.icon} className="bulk-action" data-action=${a} disabled=${Boolean(run)} onClick=${() => onAction(a)}>${`${v.label} ${plan[a].length}`}</${Button}>`;
    });
  return html`<div class="bulk-bar" data-state=${run ? 'run' : 'select'} role="group" aria-label="Selection">
    ${selecting
      ? html`<label class="bulk-all">
            <input
              type="checkbox"
              class="bulk-check"
              ref=${all}
              checked=${state === 'all'}
              disabled=${rows.length === 0}
              aria-label=${`Select all ${rows.length} shown`}
              onClick=${onSelectAll}
            />
          </label>
          ${run ? null : html`<span class="bulk-count" aria-live="polite">${count === 0 ? 'None selected' : shown < count ? `${count} selected · ${shown} shown` : `${count} selected`}</span>`}`
      : null}
    ${run ? html`<${BulkProgress} run=${run} />` : html`<div class="bulk-actions">${actions}</div>`}
  </div>`;
}

const plural = (n, one, many) => (n === 1 ? one : many);

/**
 * Asks before a bulk Download again or Remove of `rows` (the selection's
 * targets); `skipped` selected rows the action does not apply to are named
 * as left alone. While `run` is set the confirm button shows its progress.
 */
export function BulkConfirm({ action, rows, skipped, run, onConfirm, onCancel }) {
  const n = rows.length;
  const left = skipped > 0 ? ` ${skipped} other selected ${plural(skipped, 'row is', 'rows are')} left alone.` : '';
  const body = action === 'remove' ? `Records leave amdl and runs their history. The files stay.${left}` : `Replaces their downloaded files once each new copy is complete.${left}`;
  const label = action === 'remove' ? `Remove ${n}` : `Download ${n} again`;
  return html`<${ConfirmDialog}
    title=${`${label}?`}
    confirmLabel=${label}
    busyLabel=${run ? rowsProgressText(run.action, run.done, run.total) : 'Working…'}
    danger=${action === 'remove'}
    busy=${Boolean(run)}
    onConfirm=${onConfirm}
    onCancel=${onCancel}
  >${body}</${ConfirmDialog}>`;
}
