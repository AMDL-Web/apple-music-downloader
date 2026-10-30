// Job drawer: one job's header with a summary line, the failure reason,
// actions (Cancel, Retry, Remove), the tracks, where the files are and a
// closed Details (times, id, Activity, hooks), kept live by the per-job event
// stream and, once that stream has ended, by the overview feed. Parts:
// drawer-detail.js (snapshot and stream), drawer-tracks.js (tracks),
// drawer-info.js (failure notice, files, Details).

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { cancelDownload, retryDownload, deleteDownload } from '../api.js';
import {
  jobTitle,
  jobSubtitle,
  typeLabel,
  artPalette,
  paletteStyle,
  artWidths,
  count,
  safeHttpUrl,
  isUnresolved,
  inputLabel,
} from '../lib/format.js';
import { drawerSummary, codecKinds, isNotFound, rowView } from '../lib/jobview.js';
import { jobStatusView, jobCounts, isTerminal } from '../lib/status.js';
import { jobAllows, actionDone, actionError } from '../lib/jobactions.js';
import { buildHash } from '../lib/route.js';
import { useDrawerDetail } from './drawer-detail.js';
import { TrackStrip, TrackLegend, TrackList } from './drawer-tracks.js';
import { FailureNotice, FilesSection, DetailsSection } from './drawer-info.js';
import {
  useApp,
  Artwork,
  Badge,
  JobProgress,
  Button,
  LinkButton,
  Empty,
  ErrorNote,
  SkeletonRows,
} from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { MotionCover } from '../ui/motion.js';

// The header cover: 112 px wide from 640 px up, 88 px on a phone.
const HEAD_ART = artWidths(88, 112);

const FOCUSABLE = 'a[href], button:not([disabled]), summary, input:not([disabled]), select:not([disabled]), textarea, [tabindex]:not([tabindex="-1"])';

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

  // Remove asks first (the dialog sits beside the panel, outside its Tab
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
      app.toast({ message: `Removed ${title}`, role: 'success' });
    } catch (e) {
      if (!actionDone('delete', e)) {
        if (jobIdRef.current === id) setDeleting(null);
        app.toast({ message: actionError('delete', e), role: 'danger' });
        return;
      }
      app.toast({ message: `${title} was already removed` });
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
        <${Button} variant="ghost" icon="x" className="drawer-close" aria-label="Close" onClick=${onClose} data-autofocus />
        <${Button} variant="ghost" icon="chevron-left" className="drawer-back" aria-label="Back" onClick=${onClose} data-autofocus>Back</${Button}>
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
          title="Remove this job?"
          confirmLabel="Remove job"
          busyLabel="Removing…"
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
  // A link that failed before Apple named anything reads as in the queue row
  // ("Link not found", the link under it) and has no type or region to state.
  const lost = unresolved && job.status === 'failed';
  const lostRow = lost ? rowView(job) : null;
  const title = lost ? lostRow.title : unresolved ? inputLabel(job.input) || jobTitle(job) : jobTitle(job);
  const sub = lost ? '' : jobSubtitle(job);
  const palette = artPalette(job);
  const meta = lost
    ? ''
    : [typeLabel(job.type), job.release_date ? job.release_date.slice(0, 4) : '', job.genre, job.storefront ? job.storefront.toUpperCase() : '']
        .filter(Boolean)
        .join(' · ');
  const summary = drawerSummary(items);
  const live = !isTerminal(job.status) && !streamEnded;
  // Once this drawer has shown the job live, finished tracks keep their stage
  // row, so the list does not shrink under the reader as each one completes.
  const seenLive = useRef({ id: '', on: false });
  if (seenLive.current.id !== job.id) seenLive.current = { id: job.id, on: false };
  if (live) seenLive.current.on = true;
  // A finished job with no tracks (a link that never resolved) has nothing to list.
  const tracks = items.length > 0 || !isTerminal(job.status);
  // The bar says where a finished job ended up; a clean completion needs none.
  const c = jobCounts(job);
  const showProgress = !isTerminal(job.status) || c.failed > 0 || c.cancelled > 0;

  return html`<div class="drawer-body">
    <header class="drawer-head" style=${paletteStyle(palette)}>
      <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${240} widths=${HEAD_ART} sizes="(max-width: 639px) 88px, 112px" className="art-64" eager>
        ${job.motion_artwork_url ? html`<${MotionCover} job=${job} size=${112} />` : null}
      </${Artwork}>
      <div class="drawer-head-text">
        <h2 id="drawer-title" class=${unresolved && !lost ? 'drawer-title is-unresolved' : 'drawer-title'} dir="auto">${title}</h2>
        ${lost && lostRow.sub ? html`<p class="drawer-meta drawer-input" dir="auto">${lostRow.sub}</p>` : null}
        ${sub
          ? safeHttpUrl(job.artist_url)
            ? html`<a class="drawer-artist" href=${safeHttpUrl(job.artist_url)} target="_blank" rel="noopener noreferrer" dir="auto">${sub}</a>`
            : html`<p class="drawer-artist" dir="auto">${sub}</p>`
          : null}
        ${meta ? html`<p class="drawer-meta">${meta}</p>` : null}
        ${summary ? html`<p class="drawer-meta drawer-summary">${summary}</p>` : null}
        <div class="drawer-badges">
          <${Badge} view=${jobStatusView(job)} />
          ${live ? html`<span class="live-tag"><span class="pulse-static" aria-hidden="true"></span>Live</span>` : null}
        </div>
      </div>
    </header>

    ${job.status === 'failed' && job.error ? html`<${FailureNotice} error=${job.error} />` : null}

    <${Actions} job=${job} app=${app} onChanged=${onChanged} onDelete=${onDelete} />

    ${tracks
      ? html`<section class="drawer-section" aria-labelledby="tracks-title">
          <div class="section-head">
            <h3 id="tracks-title" class="section-title">Tracks</h3>
            ${showProgress ? html`<${JobProgress} job=${job} />` : null}
          </div>
          ${items.length === 0
            ? html`<${Empty} icon="list" title="No tracks resolved yet" />`
            : [
                html`<${TrackStrip} key="strip" items=${items} />`,
                codecKinds(items).length > 1 ? html`<${TrackLegend} key="legend" items=${items} />` : null,
                html`<${TrackList} key="list" items=${items} albumArtist=${job.artist_name || ''} keepStatus=${seenLive.current.on} />`,
              ]}
        </section>`
      : null}

    ${job.status === 'completed' ? html`<${FilesSection} items=${items} />` : null}

    <${DetailsSection} job=${job} activity=${activity} hooks=${hooks} />
  </div>`;
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
  // Apple has nothing at this link: asking again fails the same way, so the
  // way out is the link in Apple Music, and Retry steps back.
  const notFound = job.status === 'failed' && isNotFound(job.error);
  const open = safeHttpUrl(job.input);
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
    ${notFound && open ? html`<${LinkButton} href=${open} variant="primary" icon="music" external>Open in Apple Music</${LinkButton}>` : null}
    ${canRetry
      ? html`<${Button} variant=${notFound ? 'secondary' : 'primary'} icon="rotate" onClick=${() => run('retry', retryDownload, 'Job queued again')} disabled=${busy}>Retry</${Button}>`
      : null}
    <${LinkButton} href=${buildHash('logs', { job_id: job.id })} icon="file-text" variant="ghost">View logs</${LinkButton}>
    ${open && !notFound ? html`<${LinkButton} href=${open} variant="ghost" icon="music" external>Open in Apple Music</${LinkButton}>` : null}
    ${jobAllows(job, 'delete') ? html`<${Button} variant="ghost" icon="trash" className="drawer-delete" onClick=${onDelete} disabled=${busy}>Remove</${Button}>` : null}
  </div>`;
}
