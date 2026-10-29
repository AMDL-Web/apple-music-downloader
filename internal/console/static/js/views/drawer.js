// Job drawer: one job's header, palette, per-track strip and stage meters,
// hooks and actions (Cancel, Retry, Delete), kept live by the per-job event
// stream and, once that stream has ended, by the overview feed. Parts:
// drawer-detail.js (snapshot and stream), drawer-tracks.js (palette, tracks).

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { getConfig, cancelDownload, retryDownload, deleteDownload } from '../api.js';
import {
  jobTitle,
  jobSubtitle,
  typeLabel,
  artPalette,
  paletteStyle,
  absTime,
  elapsed,
  displayId,
  count,
  safeHttpUrl,
  isUnresolved,
  inputLabel,
  errorSummary,
  clockTime,
} from '../lib/format.js';
import { jobStatusView, hookStatusView, activityView, isTerminal } from '../lib/status.js';
import { jobAllows, actionDone, actionError } from '../lib/jobactions.js';
import { buildHash, settingsHash } from '../lib/route.js';
import { useDrawerDetail } from './drawer-detail.js';
import { Palette, TrackStrip, TrackList } from './drawer-tracks.js';
import {
  useApp,
  Artwork,
  Badge,
  JobProgress,
  Icon,
  Button,
  LinkButton,
  Empty,
  ErrorNote,
  SkeletonRows,
  CopyButton,
  RelTime,
} from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { useClock } from '../ui/hooks.js';

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea, [tabindex]:not([tabindex="-1"])';

export function JobDrawer({ jobId, onClose }) {
  const app = useApp();
  const { detail, error, streamEnded, refetch, reopen, detailRef, jobIdRef } = useDrawerDetail({ jobId, app });
  const panel = useRef(null);

  // Focus moves into the dialog, Tab stays inside it.
  useEffect(() => {
    const el = panel.current;
    if (!el) return undefined;
    const first = Array.from(el.querySelectorAll('[data-autofocus]')).find((n) => n.offsetParent !== null);
    if (first) first.focus();
    const onKey = (e) => {
      if (e.key !== 'Tab') return;
      const nodes = Array.from(el.querySelectorAll(FOCUSABLE)).filter((n) => n.offsetParent !== null);
      if (nodes.length === 0) return;
      const firstNode = nodes[0];
      const last = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === firstNode) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        firstNode.focus();
      }
    };
    el.addEventListener('keydown', onKey);
    return () => el.removeEventListener('keydown', onKey);
  }, [jobId]);

  // Delete asks first (the dialog sits beside the panel, outside its Tab
  // trap); on success the drawer closes and the feed's download_deleted
  // takes the row out of every list.
  const [deleting, setDeleting] = useState(null);
  useEffect(() => setDeleting(null), [jobId]);
  const confirmDelete = async () => {
    const id = jobId;
    const title = jobTitle(detailRef.current && detailRef.current.job);
    setDeleting('busy');
    try {
      await deleteDownload(id);
      app.toast({ message: `Deleted ${title}`, role: 'success' });
    } catch (e) {
      if (!actionDone('delete', e)) {
        if (jobIdRef.current === id) setDeleting(null);
        app.toast({ message: actionError('delete', e), role: 'danger' });
        return;
      }
      app.toast({ message: `${title} was already deleted` });
    }
    if (jobIdRef.current === id) {
      setDeleting(null);
      onClose();
    }
  };

  const job = detail && detail.job;
  // A 404 after the job was on screen means it was deleted; a 404 for a job
  // never loaded (a typo in the link, a job gone before the page opened)
  // says only that no download has this id.
  const gone = Boolean((detail && detail.deleted) || (error && error.status === 404));
  const deleted = gone && Boolean(job);
  const missing = gone && !job;
  // Deleted elsewhere while the confirm asked: the question is gone, and
  // focus, which was in it, goes to the drawer's Close.
  useEffect(() => {
    if (!gone) return;
    setDeleting((d) => (d === 'ask' ? null : d));
    const active = document.activeElement;
    const close = panel.current && panel.current.querySelector('.drawer-close');
    if (close && (!active || active === document.body)) close.focus({ preventScroll: true });
  }, [gone]);

  return html`<div class="drawer-host">
    <div class="scrim" onClick=${onClose}></div>
    <aside
      class="drawer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="drawer-title"
      ref=${panel}
    >
      <div class="drawer-bar">
        <${Button} variant="ghost" icon="chevron-left" className="drawer-back" aria-label="Back" onClick=${onClose} data-autofocus>Back</${Button}>
        <span class="drawer-bar-id mono" title=${jobId}>${displayId(jobId)}</span>
        <${CopyButton} text=${jobId} label="Copy job ID" />
        <${Button} variant="ghost" icon="x" className="drawer-close" aria-label="Close" onClick=${onClose} data-autofocus />
      </div>
      <div class="drawer-scroll">
        ${missing
          ? html`<h2 id="drawer-title" class="sr-only">Download not found</h2>
              <${Empty} icon="circle-slash" title="No download with this id">It may have been deleted, or the link may have a typo.</${Empty}>`
          : deleted
          ? html`<h2 id="drawer-title" class="sr-only">Job deleted</h2><${Empty} icon="circle-slash" title="This job was deleted" />`
          : !job && error
            ? html`<${ErrorNote} title="Couldn't load this job" error=${error} onRetry=${refetch} />`
            : !job
              ? html`<h2 id="drawer-title" class="sr-only">Loading job</h2><${SkeletonRows} n=${8} kind="track" />`
              : html`<${DrawerBody}
                  detail=${detail}
                  streamEnded=${streamEnded}
                  onChanged=${() => reopen(true)}
                  onDelete=${() => setDeleting('ask')}
                  app=${app}
                />`}
      </div>
    </aside>
    ${deleting && job && !gone
      ? html`<${ConfirmDialog}
          title="Delete this job?"
          confirmLabel="Delete job"
          busyLabel="Deleting…"
          danger
          busy=${deleting === 'busy'}
          onConfirm=${confirmDelete}
          onCancel=${() => setDeleting(null)}
        >
          <strong dir="auto">${jobTitle(job)}</strong> leaves the console with its ${count(detail.items.length)}
          ${detail.items.length === 1 ? ' track record' : ' track records'} and event history. Downloaded files stay on disk.
        </${ConfirmDialog}>`
      : null}
  </div>`;
}

function DrawerBody({ detail, streamEnded, onChanged, onDelete, app }) {
  const { job, items, hooks, activity } = detail;
  const unresolved = isUnresolved(job);
  const title = unresolved ? inputLabel(job.input) || jobTitle(job) : jobTitle(job);
  const sub = jobSubtitle(job);
  const palette = artPalette(job);
  const now = useClock(job.status === 'running' ? 1000 : 0);
  const started = job.started_at ? Date.parse(job.started_at) : null;
  const finished = job.finished_at ? Date.parse(job.finished_at) : null;
  const duration = started ? (finished || (job.status === 'running' ? now : null)) - started : null;
  const meta = [typeLabel(job.type), job.release_date ? job.release_date.slice(0, 4) : '', job.genre, job.storefront ? job.storefront.toUpperCase() : '']
    .filter(Boolean)
    .join(' · ');
  const live = !isTerminal(job.status) && !streamEnded;

  return html`<div class="drawer-body">
    <header class="drawer-head" style=${paletteStyle(palette)}>
      <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${128} className="art-64" eager />
      <div class="drawer-head-text">
        <h2 id="drawer-title" class=${unresolved ? 'drawer-title is-unresolved' : 'drawer-title'} dir="auto">${title}</h2>
        ${sub
          ? safeHttpUrl(job.artist_url)
            ? html`<a class="drawer-artist" href=${safeHttpUrl(job.artist_url)} target="_blank" rel="noopener noreferrer" dir="auto">${sub}</a>`
            : html`<p class="drawer-artist" dir="auto">${sub}</p>`
          : null}
        <p class="drawer-meta">${meta}</p>
        <div class="drawer-badges">
          <${Badge} view=${jobStatusView(job)} />
          ${live ? html`<span class="live-tag"><span class="pulse-static" aria-hidden="true"></span>Live</span>` : null}
        </div>
      </div>
    </header>

    <dl class="drawer-times">
      <div><dt>Created</dt><dd><${RelTime} value=${job.created_at} /></dd></div>
      <div><dt>Started</dt><dd>${job.started_at ? html`<time datetime=${job.started_at} title=${absTime(job.started_at)}>${absTime(job.started_at)}</time>` : html`<span class="muted">Not yet</span>`}</dd></div>
      <div><dt>Finished</dt><dd>${job.finished_at ? html`<time datetime=${job.finished_at}>${absTime(job.finished_at)}</time>` : html`<span class="muted">—</span>`}</dd></div>
      <div><dt>Duration</dt><dd class="num">${duration !== null && duration >= 0 ? elapsed(duration) : html`<span class="muted">—</span>`}</dd></div>
    </dl>

    <${Actions} job=${job} app=${app} onChanged=${onChanged} onDelete=${onDelete} />

    ${job.status === 'failed' && job.error ? html`<${FailureNotice} error=${job.error} />` : null}

    ${job.status === 'completed' ? html`<${SavedTo} />` : null}

    <${Activity} activity=${activity} />

    <${Palette} job=${job} />

    <section class="drawer-section" aria-labelledby="tracks-title">
      <div class="section-head">
        <h3 id="tracks-title" class="section-title">Tracks</h3>
        <${JobProgress} job=${job} />
      </div>
      ${items.length === 0
        ? html`<${Empty} icon="list" title=${job.status === 'running' || job.status === 'queued' ? 'No tracks resolved yet' : 'No tracks'} />`
        : [html`<${TrackStrip} key="strip" items=${items} />`, html`<${TrackList} key="list" items=${items} />`]}
    </section>

    <section class="drawer-section" aria-labelledby="hooks-title">
      <h3 id="hooks-title" class="section-title">Hooks</h3>
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
  </div>`;
}

/** The failure reason as one line, with the full upstream text a click away when it says more. */
function FailureNotice({ error }) {
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
 * Where a finished job's files are. The job carries no path, so the drawer
 * names the downloads folder from GET /config and links to Settings > Files
 * and paths, where the name templates under it are set.
 */
function SavedTo() {
  const [dir, setDir] = useState(downloadsDir);
  useEffect(() => {
    if (downloadsDir !== undefined) return undefined;
    let live = true;
    getConfig().then(
      (body) => {
        downloadsDir = (body && body.config && body.config.download && body.config.download.downloads_dir) || null;
        if (live) setDir(downloadsDir);
      },
      () => {
        if (live) setDir(null);
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return html`<p class="drawer-files">
    <span class="drawer-files-label">Saved to</span>
    ${dir ? html`<code class="drawer-files-dir mono" title=${dir}>${dir}</code>` : html`<span>the downloads folder</span>`}
    <a class="drawer-files-link" href=${settingsHash('files')}>Files and paths</a>
  </p>`;
}

/**
 * What happened to the job, newest first: the persisted history (the stream
 * replays it when the drawer opens) plus live events. Open, so a failure's
 * story is on screen; the list scrolls instead of pushing the tracks away.
 */
function Activity({ activity }) {
  if (activity.length === 0) return null;
  return html`<details class="drawer-section activity" open>
    <summary class="section-title">Activity <span class="muted">(${count(activity.length)} ${activity.length === 1 ? 'event' : 'events'})</span></summary>
    <ol class="activity-list" aria-label="Job events, newest first">
      ${activity
        .slice()
        .reverse()
        .map((a) => {
          const v = activityView(a);
          return html`<li key=${a.id} class="activity-row" data-role=${v.role}>
            <time class="activity-time mono muted" datetime=${a.created_at} title=${a.created_at}>${clockTime(a.created_at, false)}</time>
            <span class="activity-label">${v.label}</span>
            ${v.text ? html`<span class="activity-msg muted">${v.text}</span>` : null}
          </li>`;
        })}
    </ol>
  </details>`;
}

function Actions({ job, app, onChanged, onDelete }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => setConfirming(false), [job.id, job.status]);
  const run = async (action, fn, okMessage) => {
    setBusy(true);
    try {
      await fn(job.id);
      app.toast({ message: okMessage, role: 'success' });
      onChanged();
    } catch (e) {
      app.toast({ message: actionError(action, e), role: 'danger', offline: e.status === 0 });
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  };
  const canCancel = jobAllows(job, 'cancel');
  const canRetry = jobAllows(job, 'retry');
  return html`<div class="drawer-actions">
    ${canCancel && !confirming
      ? html`<${Button} variant="secondary" icon="circle-slash" onClick=${() => setConfirming(true)} disabled=${busy}>Cancel job</${Button}>`
      : null}
    ${canCancel && confirming
      ? html`<div class="confirm" role="group" aria-label="Confirm cancel">
          <span>Cancel this job? Finished tracks stay on disk, and Retry queues the rest again.</span>
          <${Button} variant="danger" onClick=${() => run('cancel', cancelDownload, 'Job cancelled')} disabled=${busy}>Cancel job</${Button}>
          <${Button} variant="ghost" onClick=${() => setConfirming(false)}>Keep running</${Button}>
        </div>`
      : null}
    ${canRetry ? html`<${Button} variant="primary" icon="rotate" onClick=${() => run('retry', retryDownload, 'Job queued again')} disabled=${busy}>Retry</${Button}>` : null}
    <${LinkButton} href=${buildHash('logs', { job_id: job.id })} icon="file-text" variant="ghost">View logs</${LinkButton}>
    ${safeHttpUrl(job.input) ? html`<${LinkButton} href=${safeHttpUrl(job.input)} variant="ghost" icon="music" external>Open in Apple Music</${LinkButton}>` : null}
    ${jobAllows(job, 'delete') ? html`<${Button} variant="ghost" icon="trash" className="drawer-delete" onClick=${onDelete} disabled=${busy}>Delete</${Button}>` : null}
  </div>`;
}
