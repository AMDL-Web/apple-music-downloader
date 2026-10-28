// Job drawer: one job's header, palette, per-track strip and stage meters,
// hooks and actions (Cancel, Retry, Delete), kept live by the per-job event
// stream and, once that stream has ended, by the overview feed.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { getDownload, cancelDownload, retryDownload, deleteDownload, jobStreamUrl, parseData } from '../api.js';
import {
  jobTitle,
  jobSubtitle,
  typeLabel,
  artPalette,
  paletteStyle,
  trackLength,
  bytes,
  codecLabel,
  isHiRes,
  absTime,
  elapsed,
  shortId,
  count,
  safeHttpUrl,
} from '../lib/format.js';
import { jobStatusView, itemStatusView, hookStatusView, isTerminal } from '../lib/status.js';
import { jobAllows, actionDone, actionError } from '../lib/jobactions.js';
import { detailFromSnapshot, applyJobEvents, mergeSnapshot } from '../lib/store.js';
import { StreamClient, claimSecondary, releaseSecondary, createCoalescer } from '../lib/stream.js';
import { buildHash } from '../lib/route.js';
import {
  useApp,
  Artwork,
  Badge,
  StatusText,
  JobProgress,
  StageTrack,
  Icon,
  Button,
  LinkButton,
  Chip,
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
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState(null);
  const [streamKey, setStreamKey] = useState(0);
  const [streamEnded, setStreamEnded] = useState(false);
  const panel = useRef(null);
  const detailRef = useRef(null);
  detailRef.current = detail;
  const jobIdRef = useRef(jobId);
  jobIdRef.current = jobId;
  const streamEndedRef = useRef(streamEnded);
  streamEndedRef.current = streamEnded;

  const refetch = () =>
    getDownload(jobId).then(
      (body) => {
        if (jobIdRef.current !== jobId) return null;
        setError(null);
        setDetail((d) => (d ? mergeSnapshot(d, body) : detailFromSnapshot(body)));
        return body;
      },
      (err) => {
        if (jobIdRef.current === jobId) setError(err);
        return null;
      },
    );

  useEffect(() => {
    setDetail(null);
    setError(null);
    setStreamEnded(false);
    refetch();
  }, [jobId]);

  // Start the per-job stream once the snapshot (and its cursor) is known.
  const loaded = Boolean(detail && detail.job && detail.job.id === jobId);
  useEffect(() => {
    if (!loaded) return undefined;
    setStreamEnded(false);
    const coalescer = createCoalescer((batch) => setDetail((d) => (d ? applyJobEvents(d, batch) : d)), 100);
    const client = new StreamClient({
      url: jobStreamUrl(jobId),
      cursor: detailRef.current.cursor,
      onEvent: (ev) => {
        const data = parseData(ev);
        if (data) coalescer.push(data);
      },
      onEnd: ({ status }) => {
        coalescer.flushNow();
        setStreamEnded(true);
        if (status === 404) setError({ status: 404, message: 'This job was deleted.' });
        else refetch();
      },
      onClose: () => {
        coalescer.flushNow();
        refetch();
      },
    });
    claimSecondary(client);
    client.start();
    return () => {
      coalescer.cancel();
      client.stop();
      releaseSecondary(client);
    };
  }, [loaded, jobId, streamKey]);

  // A finished job's stream ends (204), so later changes made elsewhere reach
  // the drawer only through the overview feed: a delete, or a retry that starts
  // a new run, which reopens the per-job stream from the snapshot's cursor.
  // One reopen at a time: the feed can deliver several upserts (and the
  // drawer's own Retry its own) before the reopened stream renders.
  const reopening = useRef(false);
  const reopen = (always) => {
    if (reopening.current) return;
    reopening.current = true;
    refetch().then((body) => {
      reopening.current = false;
      if (always || (body && body.job && !isTerminal(body.job.status))) setStreamKey((k) => k + 1);
    });
  };
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        for (const msg of batch) {
          if (msg.type === 'download_deleted' && msg.job_id === jobIdRef.current) {
            setError({ status: 404, message: 'This job was deleted.' });
            return;
          }
          if (msg.type === 'download_upserted' && msg.job && msg.job.id === jobIdRef.current && streamEndedRef.current) {
            reopen(false);
            return;
          }
        }
      }),
    [jobId],
  );
  // A feed resync (new epoch) may have skipped messages; catch up the same way.
  useEffect(() => {
    if (app.epoch && streamEndedRef.current) reopen(false);
  }, [app.epoch]);

  // Events that change the item set (resolved_input, retry, recovery) or
  // that the stream does not carry (motion artwork) need a fresh snapshot.
  useEffect(() => {
    if (!detail || !detail.needsRefetch) return undefined;
    const t = setTimeout(() => {
      setDetail((d) => (d ? { ...d, needsRefetch: false } : d));
      refetch();
    }, 300);
    return () => clearTimeout(t);
  }, [detail && detail.needsRefetch]);

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
  const deleted = (detail && detail.deleted) || (error && error.status === 404);
  // Deleted elsewhere while the confirm asked: the question is gone, and
  // focus, which was in it, goes to the drawer's Close.
  useEffect(() => {
    if (!deleted) return;
    setDeleting((d) => (d === 'ask' ? null : d));
    const active = document.activeElement;
    const close = panel.current && panel.current.querySelector('.drawer-close');
    if (close && (!active || active === document.body)) close.focus({ preventScroll: true });
  }, [deleted]);

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
        <span class="drawer-bar-id mono" title=${jobId}>${shortId(jobId)}</span>
        <${CopyButton} text=${jobId} label="Copy job ID" />
        <${Button} variant="ghost" icon="x" className="drawer-close" aria-label="Close" onClick=${onClose} data-autofocus />
      </div>
      <div class="drawer-scroll">
        ${deleted
          ? html`<${Empty} icon="circle-slash" title="This job was deleted" />`
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
    ${deleting && job && !deleted
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
  const title = jobTitle(job);
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
        <h2 id="drawer-title" class="drawer-title" dir="auto">${title}</h2>
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

    ${job.status === 'failed' && job.error
      ? html`<div class="notice" data-role="danger"><${Icon} name="circle-x" /><div class="notice-body">${job.error}</div></div>`
      : null}

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

    ${activity.length > 0
      ? html`<details class="drawer-section activity">
          <summary class="section-title">Activity <span class="muted">(${count(activity.length)} events this session)</span></summary>
          <ol class="activity-list">
            ${activity
              .slice()
              .reverse()
              .map(
                (a) => html`<li key=${a.id}>
                  <span class="mono muted">${a.type}${a.phase ? ` · ${a.phase}` : ''}</span>
                  ${a.message ? html`<span class="activity-msg">${a.message}</span>` : null}
                </li>`,
              )}
          </ol>
        </details>`
      : null}
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
      app.toast({ message: actionError(action, e), role: 'danger', sticky: true });
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
          <span>Cancel this job? Finished tracks stay on disk.</span>
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

function Swatch({ hex, label }) {
  if (!hex || !/^[0-9a-f]{6}$/i.test(hex)) return null;
  return html`<li class="swatch" title=${`${label} ${hex}`}>
    <span class="swatch-dot" style=${{ '--swatch': `#${hex}` }}></span>
    <span class="swatch-label mono">${hex}</span>
  </li>`;
}

function Palette({ job }) {
  if (!job.artwork_bg_color) return null;
  const still = ['bg_color', 'text_color1', 'text_color2', 'text_color3', 'text_color4'];
  const names = ['Background', 'Text 1', 'Text 2', 'Text 3', 'Text 4'];
  return html`<section class="drawer-section" aria-labelledby="palette-title">
    <h3 id="palette-title" class="section-title">Artwork palette</h3>
    <ul class="swatches" aria-label="Cover colors">
      ${still.map((k, i) => html`<${Swatch} key=${k} hex=${job[`artwork_${k}`]} label=${names[i]} />`)}
    </ul>
    ${job.motion_artwork_bg_color
      ? html`<p class="small muted">Motion cover</p>
          <ul class="swatches" aria-label="Motion cover colors">
            ${still.map((k, i) => html`<${Swatch} key=${k} hex=${job[`motion_artwork_${k}`]} label=${names[i]} />`)}
          </ul>`
      : null}
  </section>`;
}

function TrackStrip({ items }) {
  return html`<div class="track-strip" aria-hidden="true">
    ${items.map((it) => {
      const v = itemStatusView(it);
      return html`<span key=${it.id} class="track-block" data-role=${v.role} title=${`${it.index}. ${it.title || ''}: ${v.label}`}></span>`;
    })}
  </div>`;
}

function TrackList({ items }) {
  return html`<ol class="tracks">
    ${items.map((it) => {
      const v = itemStatusView(it);
      const codec = codecLabel(it);
      return html`<li key=${it.id} class="track" data-row tabindex="-1">
        <span class="track-index num">${it.index}</span>
        <span class="track-main">
          <span class="track-title" dir="auto" title=${it.title}>${it.title || html`<span class="muted">Untitled</span>`}</span>
          ${it.artist ? html`<span class="track-artist" dir="auto">${it.artist}</span>` : null}
        </span>
        <span class="track-len num">${trackLength(it.duration_ms)}</span>
        <span class="track-codec">${codec ? html`<${Chip} title=${isHiRes(it) ? 'High resolution' : undefined}>${codec}</${Chip}>` : null}</span>
        <span class="track-stages"><${StageTrack} item=${it} /></span>
        <span class="track-status">
          <${StatusText} view=${v} />
          ${v.retry ? html`<span class="badge" data-role="warning"><${Icon} name="rotate" size="sm" /><span class="badge-label">${v.retry}</span></span>` : null}
        </span>
        <span class="track-size num">${it.file_size > 0 ? bytes(it.file_size) : ''}</span>
        ${it.status === 'failed' && it.error
          ? html`<p class="track-error">${it.error}</p>`
          : it.status_message && !['completed', 'skipped_existing'].includes(it.status)
            ? html`<p class="track-note">${it.status_message}</p>`
            : null}
      </li>`;
    })}
  </ol>`;
}
