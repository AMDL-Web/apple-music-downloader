// Overview: the server status line, recent failures, the live queue, recently
// completed artwork, the job counts and throughput from /stats.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads, retryDownload, getHealth, getWrapperStatus, getLibrarySync, getHooks, getSystem, settle } from '../api.js';
import { count, bytes, jobTitle, jobSubtitle, artPalette, paletteStyle, absTime, isUnresolved, inputLabel, errorSummary, artWidths } from '../lib/format.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView, missingToolRows } from '../lib/status.js';
import { uniqueByTarget } from '../lib/albums.js';
import { diskRole, lowSpaceDisk } from '../lib/storage.js';
import { buildHash } from '../lib/route.js';
import { MotionCover } from '../ui/motion.js';
import { useApp, Card, Icon, Artwork, Badge, Empty, ErrorNote, SkeletonJobRows, Skeleton, LinkButton, Button, RelTime, StatusText, JobProgress } from '../ui/components.js';
import { usePoll, useColumns, useNow } from '../ui/hooks.js';
import { JobRow, applyFeedToList } from './queue.js';
import { ThroughputCard } from './throughput.js';

// Sections follow the reading order of the skin, so Tab order equals the
// visual order: the server status line, Needs attention (only with recent
// failures), Live queue, Recently completed, Jobs, Throughput. The Log tail
// is the Logs page; System health is the status line and the System page.
export function OverviewPage() {
  const app = useApp();
  const health = usePoll(loadHealth, 30000, [app.epoch]);
  return html`<div class="page page-overview">
    <${HealthStrip} poll=${health} />
    <${Attention} />
    <${LiveQueue} />
    <${RecentStrip} />
    <${KpiRow} />
    <${ThroughputCard} />
  </div>`;
}

const TILES = [
  { key: 'queued', label: 'Queued', icon: 'circle-dashed', role: 'neutral', href: buildHash('queue', {}) },
  { key: 'running', label: 'Running', icon: 'dot', role: 'info', href: buildHash('queue', {}) },
  { key: 'completed', label: 'Completed', icon: 'circle-check', role: 'success', href: buildHash('queue', { tab: 'all' }) },
  { key: 'failed', label: 'Failed', icon: 'circle-x', role: 'danger', href: buildHash('queue', { tab: 'failed' }) },
];
const TOTAL_TILE = { key: 'total', label: 'Total', icon: 'list', role: 'neutral', href: buildHash('queue', { tab: 'all' }) };

function KpiRow() {
  const { counts, conn } = useApp();
  const stale = conn.state === 'offline';
  const tile = (t, value, meta) => html`<a key=${t.key} class="kpi" data-key=${t.key} data-role=${t.role} data-zero=${value === 0 ? '' : undefined} href=${t.href}>
    <span class="kpi-label">${t.label}</span>
    <span class="kpi-icon"><${Icon} name=${t.icon} /></span>
    <span class="kpi-value">${counts ? (stale ? '—' : count(value)) : html`<${Skeleton} kind="kpi" />`}</span>
    <span class="kpi-meta">${counts ? meta : '\u00a0'}</span>
  </a>`;
  return html`<${Card} section="kpis" title="Jobs" labelId="kpis-title" note="All time">
    <div class="kpis">
      ${TILES.map((t) => tile(t, counts && counts[t.key], counts ? `of ${count(counts.total)} total` : ''))}
      ${tile(TOTAL_TILE, counts && counts.total, counts ? `${count(counts.cancelled)} cancelled` : '')}
    </div>
  </${Card}>`;
}

const LIVE_LIMIT = 8;
const LIVE_FILTER = { statuses: ['running', 'queued'] };

function LiveQueue() {
  const app = useApp();
  const [state, setState] = useState({ jobs: [], loading: true, error: null });
  const [stamp, setStamp] = useState(0);
  // A read that is not the latest one is dropped, so a slow older answer
  // cannot overwrite a newer list.
  const seq = useRef(0);
  const load = () => {
    const mine = ++seq.current;
    return listDownloads({ status: 'running,queued', sort: 'created_at', order: 'asc', limit: LIVE_LIMIT }).then(
      (body) => {
        if (mine === seq.current) setState({ jobs: body.downloads, loading: false, error: null });
      },
      (error) => {
        if (mine === seq.current) setState((s) => ({ ...s, loading: false, error }));
      },
    );
  };
  useEffect(() => {
    load();
  }, [app.epoch, stamp]);
  const jobsRef = useRef(state.jobs);
  jobsRef.current = state.jobs;
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        const held = new Set(jobsRef.current.map((j) => j.id));
        const leaving = batch.some(
          (m) =>
            (m.type === 'download_deleted' && held.has(m.job_id)) ||
            (m.job && held.has(m.job.id) && !LIVE_FILTER.statuses.includes(m.job.status)),
        );
        setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, LIVE_FILTER, { append: true, limit: LIVE_LIMIT }) }));
        // A job left the top 8; the next one in line is only in a fresh read.
        if (leaving) setStamp((n) => n + 1);
      }),
    [],
  );
  const c = app.counts;
  let body;
  // The seeded counts already say how many jobs are active: the placeholder
  // has their rows, or is the idle line itself, so the card keeps its height.
  // Before the counts, it is the idle card with its words held back (the
  // common case).
  const active = c ? Math.min(LIVE_LIMIT, c.running + c.queued) : 0;
  if (state.loading && !c) body = html`<${LiveIdle} pending />`;
  else if (state.loading && active > 0) body = html`<${SkeletonJobRows} n=${active} compact />`;
  else if (state.error) body = html`<${ErrorNote} title="Couldn't load the queue" error=${state.error} onRetry=${load} />`;
  else if (state.loading || state.jobs.length === 0) body = html`<${LiveIdle} />`;
  else
    body = html`<ul class="job-list job-list-compact" aria-label="Active jobs">
      ${state.jobs.map((job) => html`<li key=${job.id}><${JobRow} job=${job} href=${buildHash('queue', {}, job.id)} /></li>`)}
    </ul>`;
  return html`<${Card}
    className="card-flush"
    section="live"
    title="Live queue"
    labelId="live-queue-title"
    note=${c ? `${count(c.running)} running · ${count(c.queued)} queued` : '\u00a0'}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', {})}>Open queue</${LinkButton}>`}
  >${body}</${Card}>`;
}

/** A job's progress row as it first looks, held invisible so the idle card is as tall as a job's. */
const IDLE_PROGRESS_JOB = { status: 'running', total_items: 10 };
const IDLE_VIEW = { icon: 'circle-dashed', role: 'neutral', label: 'Idle' };

/**
 * The live queue with nothing in it: a card shaped like a job's (the same
 * cover square, the same four text lines and progress row), so the first
 * job that starts takes its place without moving the page below.
 */
function LiveIdle({ pending }) {
  return html`<div class="job-list job-list-compact" aria-hidden=${pending ? 'true' : undefined}>
    <div class="job-row live-idle-card" data-progress="on" data-pending=${pending ? '' : undefined}>
      <span class="art live-idle-art" aria-hidden="true"><${Icon} name="music" /></span>
      <span class="job-main">
        <span class="job-facts"><span class="job-fact">Albums, songs, playlists, artists</span></span>
        <span class="job-title">Nothing downloading</span>
        <span class="job-sub"><a class="link" href=${buildHash('new')}>Start a download</a></span>
      </span>
      <span class="job-status"><${Badge} view=${IDLE_VIEW} /></span>
      <span class="job-progress live-idle-progress" aria-hidden="true"><${JobProgress} job=${IDLE_PROGRESS_JOB} /></span>
    </div>
  </div>`;
}

const ATTENTION_LIMIT = 2;
const ATTENTION_WINDOW_MS = 24 * 60 * 60 * 1000;
const FAILED_FILTER = { statuses: ['failed'] };

/**
 * The "Needs attention" card above the live queue, only while there are
 * failures: the latest failed jobs of the last 24 hours, each with Retry. A
 * retried or deleted job leaves through the feed, and the next one in line
 * comes from a fresh read.
 */
function Attention() {
  const app = useApp();
  const now = useNow();
  const [jobs, setJobs] = useState([]);
  const [stamp, setStamp] = useState(0);
  const [busy, setBusy] = useState(null);
  const seq = useRef(0);
  useEffect(() => {
    const mine = ++seq.current;
    listDownloads({ status: 'failed', sort: 'updated_at', order: 'desc', limit: ATTENTION_LIMIT }).then(
      (body) => {
        if (mine === seq.current) setJobs(body.downloads);
      },
      () => {
        if (mine === seq.current) setJobs([]);
      },
    );
    return () => {
      seq.current++;
    };
  }, [app.epoch, stamp]);
  const jobsRef = useRef(jobs);
  jobsRef.current = jobs;
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        const held = new Set(jobsRef.current.map((j) => j.id));
        const leaving = batch.some(
          (m) => (m.type === 'download_deleted' && held.has(m.job_id)) || (m.job && held.has(m.job.id) && m.job.status !== 'failed'),
        );
        setJobs((list) => applyFeedToList(list, batch, FAILED_FILTER, { limit: ATTENTION_LIMIT }));
        if (leaving) setStamp((n) => n + 1);
      }),
    [],
  );
  // Retrying disables the button that had focus and the feed then removes
  // its row; until a moment after, focus that falls to the body moves to the
  // next Retry, or to the card's "Open queue" link.
  const rescueUntil = useRef(0);
  const retry = async (job) => {
    rescueUntil.current = Infinity;
    setBusy(job.id);
    try {
      await retryDownload(job.id);
      app.toast({ message: 'Job queued again', role: 'success' });
    } catch (e) {
      app.toast({ message: e.message || 'The request failed', role: 'danger', offline: e.status === 0 });
    } finally {
      setBusy(null);
      rescueUntil.current = Date.now() + 3000;
    }
  };
  useEffect(() => {
    if (Date.now() > rescueUntil.current) return;
    const active = document.activeElement;
    if (active && active !== document.body && !active.disabled) return;
    const target = document.querySelector('.attention .btn:not(:disabled)') || document.querySelector('[data-section="live"] .card-actions a');
    if (target) target.focus({ preventScroll: true });
  });
  const recent = jobs.filter((j) => now - Date.parse(j.finished_at || j.updated_at) < ATTENTION_WINDOW_MS);
  if (recent.length === 0) return null;
  return html`<${Card}
    section="attention"
    title="Needs attention"
    labelId="attention-title"
    note="Last 24 hours"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', { tab: 'failed' })}>All failed</${LinkButton}>`}
  ><ul class="attention" aria-label="Recent failures">
    ${recent.map(
      (job) => html`<li key=${job.id} class="attention-row" data-role="danger">
        <span class="attention-title">
          <span class="attention-label"><${Icon} name="circle-x" />Failed</span>
          ${isUnresolved(job)
            ? null
            : html`<a class="attention-link" href=${buildHash('queue', {}, job.id)} dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</a>`}
          ${isUnresolved(job)
            ? html`<a class="attention-link attention-input" href=${buildHash('queue', {}, job.id)} title=${job.input}>${inputLabel(job.input)}</a>`
            : html`<span class="attention-input" title=${job.input}>${job.input}</span>`}
          <span class="attention-time"><${RelTime} value=${job.finished_at || job.updated_at} /></span>
        </span>
        ${job.error
          ? html`<span class="attention-detail"><span class="attention-error" title=${job.error}>${errorSummary(job.error)}</span></span>`
          : null}
        <${Button} size="sm" icon="rotate" onClick=${() => retry(job)} disabled=${busy === job.id}>Retry</${Button}>
      </li>`,
    )}
  </ul></${Card}>`;
}

function loadHealth() {
  return Promise.all([settle(getHealth()), settle(getWrapperStatus()), settle(getLibrarySync()), settle(getHooks()), settle(getSystem())]);
}

/**
 * A Storage row when a disk is nearly full (System's red). System's orange
 * stays there: a large disk can sit at 85 % for months with room to spare.
 * A failed /system read stays on the System page too.
 */
function storageRows(res) {
  const disk = res && res.data ? lowSpaceDisk(res.data.storage) : null;
  if (!disk || diskRole(disk) !== 'danger') return [];
  return [['Storage', { role: 'danger', label: 'Low space', detail: `${bytes(disk.free)} free` }]];
}

/** The four health rows behind the status line. */
export function healthRows(results) {
  const [health, wrapper, sync, hooks] = results;
  return [
    ['Backend', backendHealthView(health)],
    ['Wrapper', wrapperView(wrapper)],
    ['Library sync', librarySyncView(sync)],
    ['Hooks', hooksView(hooks)],
  ];
}

/**
 * One line under the page head that answers "is the server healthy?": a quiet
 * status when everything is fine, a notice naming each service that is not
 * (the wrapper unreachable, library sync failing), a missing ffmpeg and a
 * nearly full disk, with a link to System.
 * Nothing renders until the first read settles.
 */
function HealthStrip({ poll }) {
  // Until the first check answers, an empty line holds the strip's place.
  if (!poll.data) return html`<p class="health-strip" data-state="loading" aria-hidden="true">\u00a0</p>`;
  const sys = poll.data[4] && poll.data[4].data;
  const rows = [...healthRows(poll.data), ...missingToolRows(sys), ...storageRows(poll.data[4])];
  const bad = rows.filter(([, v]) => v.role === 'danger' || v.role === 'warning');
  const details = html`<a class="health-strip-link" href=${buildHash('system')}>Details</a>`;
  if (bad.length === 0) {
    return html`<p class="health-strip" data-state="ok" data-role="success">
      <${StatusText} view=${{ icon: 'circle-check', role: 'success', label: 'All systems healthy' }} />${details}
    </p>`;
  }
  const worst = bad.some(([, v]) => v.role === 'danger') ? 'danger' : 'warning';
  return html`<div class="health-strip notice" data-state="problem" data-role=${worst} role="status">
    <${Icon} name=${worst === 'danger' ? 'circle-x' : 'triangle-alert'} />
    <div class="notice-body">
      ${bad.map(
        ([name, v]) => html`<span key=${name} class="health-strip-item"><strong>${name}</strong> ${v.label.toLowerCase()}${v.detail ? `: ${v.detail}` : ''}</span>`,
      )}
      ${details}
    </div>
  </div>`;
}

const RECENT_FILTER = { statuses: ['completed'] };
// Read more than are shown: repeat downloads of one album collapse to one tile.
const RECENT_FETCH = 32;
const RECENT_SHOWN = 16;
const TILE_MIN = 120;
// Tiles start at TILE_MIN and stretch to fill the row: about 120-130 px on a
// desktop, 140 on a phone.
const TILE_ART = artWidths(130, 140);
const TILE_GAP = 16;

function RecentStrip() {
  const app = useApp();
  const ref = useRef(null);
  const cols = useColumns(ref, TILE_MIN, TILE_GAP);
  const [state, setState] = useState({ jobs: [], loading: true, error: null });
  const seq = useRef(0);
  const load = () => {
    const mine = ++seq.current;
    return listDownloads({ status: 'completed', sort: 'updated_at', order: 'desc', limit: RECENT_FETCH }).then(
      (body) => {
        if (mine === seq.current) setState({ jobs: body.downloads, loading: false, error: null });
      },
      (error) => {
        if (mine === seq.current) setState((s) => ({ ...s, loading: false, error }));
      },
    );
  };
  useEffect(() => {
    load();
  }, [app.epoch]);
  useEffect(
    () => app.subscribeFeed((batch) => setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, RECENT_FILTER, { limit: RECENT_FETCH }) }))),
    [],
  );
  // Every tile renders; the ones past the whole-tile count carry is-overflow,
  // which the base hides and a skin may show as a scrolling shelf.
  return html`<${Card}
    section="recent"
    title="Recently completed"
    labelId="recent-title"
    note=${app.counts ? `${count(app.counts.completed)} completed` : '\u00a0'}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('showcase')}>Showcase</${LinkButton}>`}
  >
    <div class="strip" ref=${ref}>
      ${state.loading
        ? Array.from(
            { length: cols || 1 },
            (_, i) => html`<span key=${i} class="strip-tile is-skel" aria-hidden="true">
              <span class="art art-square skel"></span>
              <span class="strip-title"><span class="skel skel-lh"></span></span>
              <span class="strip-sub"><span class="skel skel-lh skel-short"></span></span>
            </span>`,
          )
        : state.error
          ? html`<${ErrorNote} title="Couldn't load completed jobs" error=${state.error} onRetry=${load} />`
          : state.jobs.length === 0
            ? html`<${Empty} icon="disc" title="Completed downloads will appear here" />`
            : uniqueByTarget(state.jobs).slice(0, RECENT_SHOWN).map((job, i) => {
                const p = artPalette(job);
                const cls = ['strip-tile', p ? 'has-palette' : '', cols > 0 && i >= cols ? 'is-overflow' : ''].filter(Boolean).join(' ');
                return html`<a key=${job.id} class=${cls} href=${buildHash('showcase', {}, job.id)} style=${paletteStyle(p)} title=${job.finished_at ? `Finished ${absTime(job.finished_at)}` : undefined}>
                  <${Artwork} url=${job.artwork_url} bg=${p && p.bg} px=${260} widths=${TILE_ART} sizes="(max-width: 639px) 140px, 130px" className="art-square">
                    ${job.motion_artwork_url ? html`<${MotionCover} job=${job} size=${140} />` : null}
                  </${Artwork}>
                  <span class="strip-title" dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</span>
                  <span class="strip-sub" dir="auto">${jobSubtitle(job)}</span>
                </a>`;
              })}
    </div>
  </${Card}>`;
}
