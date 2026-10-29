// One job row for every list that shows jobs (Queue, Overview, New download's
// Submitted list), and the feed reducer those lists share.

import { html } from '../ui/preact.js';
import { jobTitle, jobSubtitle, typeLabel, artPalette, paletteStyle, jobFacts, isUnresolved, inputLabel, unresolvedHint, errorSummary } from '../lib/format.js';
import { jobStatusView } from '../lib/status.js';
import { upsertJob, removeJob } from '../lib/store.js';
import { Artwork, Badge, JobProgress, RelTime, Icon } from '../ui/components.js';

/**
 * One job in a list: artwork, title, type, progress, status, last update.
 * The row carries the job's status (data-status) and Apple palette
 * (--art-bg, --art-t1..t3) for the skin; .job-facts is hidden unless the skin shows it.
 * Selection is opt-in (the Queue's selection mode; the Overview passes
 * neither prop): `selected` sets data-selected, and with `onSelect` a plain
 * click selects instead of opening the drawer (Cmd/Ctrl-click still opens).
 */
export function JobRow({ job, href, selected, onSelect }) {
  // Before Apple names the job it shows its link (muted) and says so, rather
  // than a title guessed from the URL.
  const unresolved = isUnresolved(job);
  const title = unresolved ? inputLabel(job.input) || jobTitle(job) : jobTitle(job);
  const sub = unresolved ? unresolvedHint(job) : jobSubtitle(job);
  const reason = job.status === 'failed' ? errorSummary(job.error) : '';
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
    data-selected=${selected ? '' : undefined}
    style=${paletteStyle(palette)}
    onClick=${onClick}
  >
    <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${160} className="art-40" />
    <span class="job-main">
      <span class="job-facts">
        ${f.type ? html`<span class="job-fact" data-key="type">${f.type}</span>` : null}
        ${f.genre ? html`<span class="job-fact" data-key="genre" dir="auto">${f.genre}</span>` : null}
        ${f.year ? html`<span class="job-fact" data-key="year">${f.year}</span>` : null}
        ${f.started ? html`<span class="job-fact" data-key="started">Started ${f.started}</span>` : null}
      </span>
      <span class=${unresolved ? 'job-title is-unresolved' : 'job-title'} dir="auto" title=${unresolved ? job.input || title : title}>${title}</span>
      <span class="job-sub">
        ${sub ? html`<span class="job-sub-artist" dir="auto">${sub}</span>` : null}
        <span class="job-sub-type">${typeLabel(job.type)}</span>
        ${job.storefront ? html`<span class="job-sub-sf">${job.storefront.toUpperCase()}</span>` : null}
      </span>
      ${reason ? html`<span class="job-error" title=${job.error}>${reason}</span>` : null}
    </span>
    <span class="job-type">${typeLabel(job.type)}</span>
    <span class="job-progress"><${JobProgress} job=${job} /></span>
    <span class="job-status"><${Badge} view=${jobStatusView(job)} /></span>
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
