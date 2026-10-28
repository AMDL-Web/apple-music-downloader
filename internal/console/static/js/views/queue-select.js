// Queue selection mode and bulk actions: the bar over the job list (select
// all, the count, the actions the selection allows, the progress of a run),
// the confirmation before a bulk cancel or delete, and the runner that sends
// at most BULK_LIMIT requests at a time and ends with one summary toast.
// Rows leave the list through the downloads feed, never by hand.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { cancelDownload, retryDownload, deleteDownload } from '../api.js';
import { jobTitle } from '../lib/format.js';
import { runLimited } from '../lib/bulk.js';
import { selectionState } from '../lib/selection.js';
import { actionDone, bulkLabel, bulkProgressText, bulkSummary } from '../lib/jobactions.js';
import { Button } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';

export const BULK_LIMIT = 4;

const CALLS = { retry: retryDownload, cancel: cancelDownload, delete: deleteDownload };

/**
 * The bulk runner. start(action, jobs) sends the action's request for every
 * job, BULK_LIMIT at a time, keeps {action, done, total} in `run` meanwhile,
 * toasts bulkSummary when all have settled, and resolves to the ids the
 * action reached (deleting a job that is already gone counts). One run at a
 * time: a second start while one runs resolves to [] at once.
 */
export function useBulkRun(app) {
  const [run, setRun] = useState(null);
  const busy = useRef(false);
  const start = async (action, jobs) => {
    const call = CALLS[action];
    if (!call || busy.current || jobs.length === 0) return [];
    busy.current = true;
    setRun({ action, done: 0, total: jobs.length });
    const outcomes = await runLimited(
      jobs,
      BULK_LIMIT,
      (job) => call(job.id),
      (done, total) => setRun({ action, done, total }),
    );
    busy.current = false;
    setRun(null);
    app.toast(bulkSummary(action, outcomes, jobTitle));
    return outcomes.filter((o) => o.ok || actionDone(action, o.error)).map((o) => o.item.id);
  };
  return { run, start };
}

const ACTION_VIEW = {
  retry: { icon: 'rotate', variant: 'secondary', label: 'Retry' },
  cancel: { icon: 'circle-slash', variant: 'secondary', label: 'Cancel' },
  delete: { icon: 'trash', variant: 'danger', label: 'Delete' },
};

/** Progress of a bulk run: "Deleting 3 of 12" and a meter. */
function BulkProgress({ run }) {
  const text = bulkProgressText(run.action, run.done, run.total);
  return html`<div class="bulk-progress">
    <span class="bulk-progress-text">${text}</span>
    <span
      class="bulk-meter"
      role="progressbar"
      aria-label=${bulkLabel(run.action, run.total)}
      aria-valuemin="0"
      aria-valuemax=${run.total}
      aria-valuenow=${run.done}
      aria-valuetext=${text}
    >
      <span class="bulk-meter-fill" style=${{ '--f': String(run.total > 0 ? run.done / run.total : 0) }}></span>
    </span>
  </div>`;
}

/**
 * The bar over the job list: .bulk-bar[data-state=select|run]. While
 * selecting it holds the select-all checkbox (.bulk-all), the count
 * (.bulk-count) and one button per action the selection allows
 * (.bulk-action[data-action], count in .bulk-n, hidden at zero); during a run
 * the progress replaces the buttons. `plan` is actionPlan of the selection.
 */
export function SelectionBar({ jobs, selected, plan, selecting, run, onSelectAll, onAction }) {
  const state = selectionState(selected, jobs);
  const n = state === 'none' ? 0 : jobs.filter((j) => selected.has(j.id)).length;
  const all = useRef(null);
  useEffect(() => {
    if (all.current) all.current.indeterminate = state === 'some';
  }, [state, selecting]);
  const actions = ['retry', 'cancel', 'delete']
    .filter((a) => plan[a].length > 0)
    .map((a) => {
      const v = ACTION_VIEW[a];
      const k = plan[a].length;
      return html`<${Button}
        key=${a}
        variant=${v.variant}
        icon=${v.icon}
        className="bulk-action"
        data-action=${a}
        aria-label=${bulkLabel(a, k)}
        disabled=${Boolean(run)}
        onClick=${() => onAction(a)}
      >${v.label} <span class="bulk-n">${k}</span></${Button}>`;
    });
  let hint = null;
  if (selecting && !run && actions.length === 0) {
    hint = html`<span class="bulk-hint">${n === 0 ? 'Select jobs to retry, cancel or delete them.' : 'No action applies to these jobs.'}</span>`;
  }
  return html`<div class="bulk-bar" data-state=${run ? 'run' : 'select'} role="group" aria-label="Selection">
    ${selecting
      ? html`<label class="bulk-all">
            <input
              type="checkbox"
              class="bulk-check"
              ref=${all}
              checked=${state === 'all'}
              disabled=${jobs.length === 0}
              aria-label=${`Select all ${jobs.length} loaded jobs`}
              onClick=${onSelectAll}
            />
          </label>
          <span class="bulk-count" aria-live="polite">${n === 0 ? 'None selected' : `${n} selected`}</span>`
      : null}
    ${run ? html`<${BulkProgress} run=${run} />` : html`<div class="bulk-actions">${actions}${hint}</div>`}
  </div>`;
}

const plural = (n, one, many) => (n === 1 ? one : many);

/**
 * Asks before a bulk cancel or delete of `jobs` (the selection's targets for
 * `action`); `skipped` selected jobs the action does not apply to are named
 * as left alone. While `run` is set the confirm button shows its progress.
 */
export function BulkConfirm({ action, jobs, skipped, run, onConfirm, onCancel }) {
  const n = jobs.length;
  const left = skipped > 0 ? ` ${skipped} other selected ${plural(skipped, 'job is', 'jobs are')} left alone.` : '';
  const body =
    action === 'delete'
      ? `${plural(n, 'It leaves', 'They leave')} the console with ${plural(n, 'its', 'their')} track records and event history. Downloaded files stay on disk.${left}`
      : `Tracks that already finished stay on disk. A cancelled job can't be retried; submit its link again to download it.${left}`;
  return html`<${ConfirmDialog}
    title=${`${bulkLabel(action, n)}?`}
    confirmLabel=${bulkLabel(action, n)}
    cancelLabel=${action === 'cancel' ? 'Keep running' : 'Cancel'}
    busyLabel=${run ? bulkProgressText(run.action, run.done, run.total) : 'Working…'}
    danger
    busy=${Boolean(run)}
    onConfirm=${onConfirm}
    onCancel=${onCancel}
  >${body}</${ConfirmDialog}>`;
}
