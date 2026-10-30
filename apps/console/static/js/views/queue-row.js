// One job row for every list that shows jobs (Queue, Overview, New download's
// Submitted list), and the feed reducer those lists share.

import { html } from '../ui/preact.js';
import { artPalette, paletteStyle, jobFacts, artWidths } from '../lib/format.js';

// The row's cover is drawn 48 px wide (40 in the base, 48 in the skin).
const ROW_ART = artWidths(40, 48);
import { rowView } from '../lib/jobview.js';
import { upsertJob, removeJob } from '../lib/store.js';
import { useApp, Artwork, Badge, JobProgress, RelTime, Icon } from '../ui/components.js';

/**
 * One job in a list: artwork, title and a second line, progress while it
 * runs, status, last update. The row carries the job's status (data-status)
 * and Apple palette (--art-bg, --art-t1..t3) for the skin; .job-facts is
 * hidden unless the skin shows it. Only a queued or running job draws
 * progress (data-progress on|off; lib/jobview.js rowView says the rest): a
 * finished one counts its tracks in the second line, and a plain completed
 * job shows its status as an icon alone. A failed job shows what went wrong
 * on a line of its own. Selection is opt-in (the Queue's selection mode; the
 * Overview passes neither prop): `selected` sets data-selected, and with
 * `onSelect` a plain click selects instead of opening the drawer
 * (Cmd/Ctrl-click still opens).
 */
export function JobRow({ job, href, selected, onSelect }) {
  const app = useApp();
  const v = rowView(job, app && app.defaultStorefront);
  const palette = artPalette(job);
  const f = jobFacts(job);
  const onClick = onSelect
    ? (e) => {
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.altKey) return;
        e.preventDefault();
        onSelect(e);
      }
    : undefined;
  return html`<a
    class=${palette ? 'job-row has-palette' : 'job-row'}
    href=${href}
    data-row
    data-status=${job.status}
    data-progress=${v.progress ? 'on' : 'off'}
    data-selected=${selected ? '' : undefined}
    style=${paletteStyle(palette)}
    onClick=${onClick}
  >
    <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${100} widths=${ROW_ART} sizes="48px" className="art-40" />
    <span class="job-main">
      <span class="job-facts">
        ${f.type ? html`<span class="job-fact" data-key="type">${f.type}</span>` : null}
        ${f.genre ? html`<span class="job-fact" data-key="genre" dir="auto">${f.genre}</span>` : null}
        ${f.year ? html`<span class="job-fact" data-key="year">${f.year}</span>` : null}
        ${f.started ? html`<span class="job-fact" data-key="started">Started ${f.started}</span>` : null}
      </span>
      <span class=${v.mono ? 'job-title is-unresolved' : 'job-title'} dir="auto" title=${v.mono ? job.input || v.title : v.title}>${v.title}</span>
      ${v.sub ? html`<span class="job-sub" dir="auto">${v.sub}</span>` : null}
      ${v.reason ? html`<span class="job-error" title=${job.error}>${v.reason}</span>` : null}
    </span>
    <span class="job-progress">${v.progress ? html`<${JobProgress} job=${job} />` : null}</span>
    <span class="job-status">
      ${v.iconOnly
        ? html`<span class="status-text status-icon" data-role=${v.view.role} role="img" aria-label=${v.view.label} title=${v.view.label}><${Icon} name=${v.view.icon} size="sm" /></span>`
        : html`<${Badge} view=${v.view} />`}
    </span>
    <span class="job-updated"><${RelTime} value=${job.updated_at} /></span>
    <span class="job-chevron"><${Icon} name="chevron-right" /></span>
  </a>`;
}

/** Applies a batch of feed messages to a filtered job list. */
export function applyFeedToList(jobs, batch, filter, opts) {
  let next = jobs;
  for (const msg of batch) {
    if (msg.type === 'download_deleted') next = removeJob(next, msg.job_id);
    else if (msg.type === 'download_upserted' && msg.job) next = upsertJob(next, msg.job, filter, opts);
  }
  return next;
}
