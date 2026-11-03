// The Queue's list: one row per record (albums and catalog playlists) and
// per station run, each a link to its drawer with Retry where it applies
// and a "…" menu after it. A record with a run under way draws that run
// (its progress); a queued one in run order carries the drag grip. A
// record whose run the live lists no longer hold reads as that run ended,
// until the record is read again.

import { html } from '../ui/preact.js';
import { buildHash, recordHash } from '../lib/route.js';
import { isNotFound } from '../lib/jobview.js';
import { recordTitle, resetNote } from '../lib/records.js';
import { Button } from '../ui/components.js';
import { JobRow } from './queue-row.js';
import { RecordRow } from './record-row.js';
import { JobMenu, runOne, jobName } from './queue-menu.js';
import { RecordMenu } from './queue-record-menu.js';
import { useRegions } from './region-tag.js';
import { regionTag } from '../lib/accounts.js';
import { JobGrip } from './queue-order.js';

const RETRY_STATES = ['failed', 'given_up', 'missing'];

/** Where a row opens: a record's drawer, or a station run's. */
export const rowHref = (row, params) => (row.kind === 'station' ? buildHash('queue', params, row.job.id) : recordHash(row.kind, row.record.id, params));

/** A row's name for labels. */
const nameOf = (row) => (row.kind === 'station' ? jobName(row.job) : recordTitle(row.record));

/**
 * Props: rows (in the order drawn), params (the route's, kept by the
 * drawers), selecting, selected (Set of row keys), onToggle(key, shift),
 * order (views/queue-order.js useQueueDrag), grouped (rows slide when the
 * feed moves them), busy (a bulk run), onLeaving(key), onRetry(row).
 * While the accounts serve two regions or more, each row's second line
 * leads with its region's tag.
 */
export function QueueRows({ rows, params, selecting, selected, onToggle, order, grouped, busy, onLeaving, onRetry }) {
  const regions = useRegions();
  return rows.map((row) => {
    const job = row.live || row.job || null;
    const href = rowHref(row, params);
    const on = selecting && selected.has(row.id);
    const r = row.record;
    const retry =
      !selecting &&
      (row.kind === 'station'
        ? job.status === 'failed' && !isNotFound(job.error)
        : !row.live && !r.active_job_id && Boolean(r.tracks_failed || r.tracks_given_up || r.tracks_missing));
    const sortable = Boolean(row.live) && order.draggable && row.live.status === 'queued';
    const drag = sortable
      ? { ...order.sort.row(row.live.id), 'data-sortable': '', 'aria-keyshortcuts': 'Alt+ArrowUp Alt+ArrowDown Alt+Home Alt+End', onKeyDown: (e) => order.keyDown(e, row.live) }
      : grouped
        ? { 'data-sort-flip': row.id }
        : {};
    const name = nameOf(row);
    // A record's region is where its runs read it (pinned); a station run's, its own.
    const region = regionTag(row.kind === 'station' ? job.storefront : row.record.pinned_storefront || (row.live && row.live.storefront) || '', regions);
    const select = selecting ? (e) => onToggle(row.id, e.shiftKey) : undefined;
    return html`<li key=${row.id} class="job-item" data-row-key=${row.id} data-job-id=${job ? job.id : undefined} data-kind=${row.kind} data-selected=${on ? '' : undefined} data-action=${selecting ? undefined : retry ? 'retry' : 'menu'} ...${drag}>
      ${selecting
        ? html`<label class="job-check">
            <input type="checkbox" class="job-check-input" checked=${on} aria-label=${`Select ${name}`} onClick=${(e) => onToggle(row.id, e.shiftKey)} />
          </label>`
        : null}
      ${job
        ? html`<${JobRow} job=${job} href=${href} region=${region} selected=${selecting ? on : undefined} onSelect=${select} />`
        : html`<${RecordRow} record=${r} region=${region} runStatus=${r.active_job_id ? 'completed' : ''} href=${href} selected=${selecting ? on : undefined} onSelect=${select} />`}
      ${retry
        ? html`<${Button}
            size="sm"
            icon="rotate"
            className="job-retry"
            aria-label=${`Retry ${name}`}
            disabled=${busy}
            onClick=${() => {
              onLeaving(row.id);
              onRetry(row);
            }}
          >Retry</${Button}>`
        : null}
      ${selecting
        ? null
        : row.kind === 'station'
          ? html`<${JobMenu} job=${job} href=${href} onLeaving=${() => onLeaving(row.id)} place=${order.place(job.id)} onMove=${(where) => order.moveTo(job, where)} />`
          : html`<${RecordMenu}
              row=${row}
              href=${href}
              onLeaving=${onLeaving}
              place=${row.live ? order.place(row.live.id) : {}}
              onMove=${(where) => row.live && order.moveTo(row.live, where)}
            />`}
      ${sortable ? html`<${JobGrip} />` : null}
    </li>`;
  });
}

/** Retries one row: a station run again (POST .../retry), a record's failed, given-up and missing tracks (a reset). */
export async function retryRow(app, row, reset) {
  if (row.kind === 'station') return runOne(app, 'retry', row.job);
  try {
    const out = await reset(row.kind, row.record.id, { what: 'audio', states: RETRY_STATES });
    app.toast({ message: resetNote(out, row.record, `Downloading “${recordTitle(row.record)}” again`), role: 'success' });
    if (app.recordsChanged) app.recordsChanged();
    return true;
  } catch (e) {
    app.toast({ message: `Couldn't retry it: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    return false;
  }
}
