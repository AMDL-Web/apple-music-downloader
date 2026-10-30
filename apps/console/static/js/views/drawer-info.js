// The job drawer's secondary sections: the failure notice, where the files
// are, and the closed Details (times, job id, Activity, hooks).

import { html, useState, useEffect } from '../ui/preact.js';
import { getConfig } from '../api.js';
import { absTime, dateTime, elapsed, count, errorSummary, clockTime } from '../lib/format.js';
import { hookStatusView } from '../lib/status.js';
import { activityRows } from '../lib/jobview.js';
import { outputFolders } from '../lib/files.js';
import { settingsHash } from '../lib/route.js';
import { Icon, Badge, CopyButton } from '../ui/components.js';
import { useClock } from '../ui/hooks.js';

/** The failure reason as one line, with the full upstream text a click away when it says more. */
export function FailureNotice({ error }) {
  const summary = errorSummary(error);
  const more = error.trim() !== summary;
  return html`<div class="notice" data-role="danger">
    <${Icon} name="circle-x" />
    <div class="notice-body">
      ${summary || error}
      ${more ? html`<details class="notice-more"><summary>Full error</summary><pre class="notice-raw">${error}</pre></details>` : null}
    </div>
  </div>`;
}

let downloadsDir; // the configured downloads folder, read once per page load; null when it could not be read

/**
 * Where a finished job's files are, with a Copy button: the one folder its
 * tracks were written to (from the items' `output_path`; an album or a song),
 * or, when they went to several folders (a playlist, an artist), the downloads
 * folder from GET /config with a count of the folders under it. A link to
 * Settings > Files & naming, where the name templates are set, follows.
 */
export function FilesSection({ items }) {
  const folders = outputFolders(items);
  const only = folders.length === 1 ? folders[0] : null;
  const [root, setRoot] = useState(downloadsDir);
  const dir = only || root;
  useEffect(() => {
    if (only || downloadsDir !== undefined) return undefined;
    let live = true;
    getConfig().then(
      (body) => {
        downloadsDir = (body && body.config && body.config.download && body.config.download.downloads_dir) || null;
        if (live) setRoot(downloadsDir);
      },
      () => {
        if (live) setRoot(null);
      },
    );
    return () => {
      live = false;
    };
  }, [only]);
  return html`<section class="drawer-section" aria-labelledby="files-title">
    <h3 id="files-title" class="section-title">Files</h3>
    <div class="drawer-files">
      ${dir
        ? html`<code class="drawer-files-dir mono" title=${dir}>${pathBreaks(dir)}</code><${CopyButton} text=${dir} label="Copy folder path" />`
        : html`<span class="muted">The downloads folder</span>`}
    </div>
    ${folders.length > 1 ? html`<p class="drawer-files-note muted small">Spread over ${count(folders.length)} folders.</p>` : null}
    <a class="drawer-files-link" href=${settingsHash('files')}>Files & naming</a>
  </section>`;
}

// A folder path wraps after a slash (each one a <wbr>, the folder names kept
// whole by .drawer-files-dir), so an artist's name is not cut in two.
const pathBreaks = (dir) => dir.split('/').flatMap((part, i, all) => (i < all.length - 1 ? [`${part}/`, html`<wbr key=${i} />`] : [part]));

const when = (value) =>
  value ? html`<time datetime=${value} title=${absTime(value)}>${dateTime(value)}</time>` : html`<span class="muted">Not yet</span>`;

/**
 * What happened to the job, newest first: the persisted history (the stream
 * replays it when the drawer opens) plus live events. The list scrolls
 * instead of growing.
 */
function Activity({ activity }) {
  const rows = activityRows(activity);
  if (rows.length === 0) return null;
  return html`<section class="activity" aria-labelledby="activity-title">
    <h4 id="activity-title" class="subsection-title">Activity <span class="muted">(${count(rows.length)} ${rows.length === 1 ? 'event' : 'events'})</span></h4>
    <ol class="activity-list" aria-label="Job events, newest first">
      ${rows
        .slice()
        .reverse()
        .map(({ entry: a, view: v }) => {
          return html`<li key=${a.id} class="activity-row" data-role=${v.role}>
            <time class="activity-time mono muted" datetime=${a.created_at} title=${a.created_at}>${clockTime(a.created_at, false)}</time>
            <span class="activity-label">${v.label}</span>
            ${v.text ? html`<span class="activity-msg muted">${v.text}</span>` : null}
          </li>`;
        })}
    </ol>
  </section>`;
}

/** The closed Details: when it ran, the job id, Activity and hooks. */
export function DetailsSection({ job, activity, hooks }) {
  const now = useClock(job.status === 'running' ? 1000 : 0);
  const started = job.started_at ? Date.parse(job.started_at) : null;
  const finished = job.finished_at ? Date.parse(job.finished_at) : null;
  const duration = started ? (finished || (job.status === 'running' ? now : null)) - started : null;
  return html`<details class="drawer-section drawer-details">
    <summary class="section-title">Details</summary>
    <div class="drawer-details-body">
      <dl class="drawer-times">
        <div><dt>Created</dt><dd>${when(job.created_at)}</dd></div>
        <div><dt>Started</dt><dd>${when(job.started_at)}</dd></div>
        <div><dt>Finished</dt><dd>${job.finished_at ? when(job.finished_at) : html`<span class="muted">—</span>`}</dd></div>
        <div><dt>Duration</dt><dd class="num">${duration !== null && duration >= 0 ? elapsed(duration) : html`<span class="muted">—</span>`}</dd></div>
      </dl>
      <div class="drawer-id">
        <span class="muted">Job ID</span>
        <code class="mono" title=${job.id}>${job.id}</code>
        <${CopyButton} text=${job.id} label="Copy job ID" />
      </div>
      <${Activity} activity=${activity} />
      <section aria-labelledby="hooks-title">
        <h4 id="hooks-title" class="subsection-title">Hooks</h4>
        ${hooks.length === 0
          ? html`<p class="muted small">No hook has run for this job.</p>`
          : html`<ul class="hook-list">
              ${hooks.map(
                (h) => html`<li key=${h.name} class="hook-row">
                  <span class="mono">${h.name}</span>
                  <${Badge} view=${hookStatusView(h.status)} />
                  ${h.error ? html`<span class="hook-error">${h.error}</span>` : null}
                </li>`,
              )}
            </ul>`}
      </section>
    </div>
  </details>`;
}
