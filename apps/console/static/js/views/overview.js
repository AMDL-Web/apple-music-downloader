// Overview: the server status line, recent failures, the live queue, recently
// completed artwork, the job counts and throughput from /stats.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads, retryDownload, getHealth, getWrapperStatus, getLibrarySync, getHooks, getSystem, settle } from '../api.js';
import { count, bytes, jobTitle, isUnresolved, inputLabel, errorSummary } from '../lib/format.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView, missingToolRows } from '../lib/status.js';
import { diskRole, lowSpaceDisk } from '../lib/storage.js';
import { buildHash } from '../lib/route.js';
import { useApp, Card, Icon, Badge, ErrorNote, SkeletonJobRows, Skeleton, LinkButton, Button, RelTime, StatusText } from '../ui/components.js';
import { usePoll, useNow } from '../ui/hooks.js';
import { cached, remember } from '../lib/cache.js';
import { JobRow, applyFeedToList } from './queue.js';
import { RecentSection } from './overview-recent.js';
import { ThroughputCard } from './throughput.js';

// Sections follow the reading order of the skin, so Tab order equals the
// visual order: the server status line, Needs attention (only with recent
// failures), Live queue, Recently completed, Jobs, Throughput. The Log tail
// is the Logs page; System health is the status line and the System page.
export function OverviewPage() {
  const app = useApp();
  const health = usePoll(loadHealth, 30000, [app.epoch], 'overview-health');
  // Needs attention sits above everything and is known only after its read:
  // the sections below wait unseen for that answer (or HOLD_MS), so the card
  // never arrives late and pushes the page down.
  const [held, setHeld] = useState(() => cached(ATTENTION_KEY) === undefined);
  useEffect(() => {
    if (!held) return undefined;
    const t = setTimeout(() => setHeld(false), HOLD_MS);
    return () => clearTimeout(t);
  }, []);
  return html`<div class="page page-overview" data-held=${held ? '' : undefined}>
    <${HealthStrip} poll=${health} />
    <${Attention} onKnown=${() => setHeld(false)} />
    <${LiveQueue} />
    <${RecentSection} />
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
  // A feed that has not answered yet is loading; one that failed before its
  // first snapshot has nothing to show, and a placeholder would pulse forever.
  const down = stale || conn.state === 'reconnecting';
  const tile = (t, value) => html`<a key=${t.key} class="kpi" data-key=${t.key} data-role=${t.role} data-zero=${value === 0 ? '' : undefined} href=${t.href}>
    <span class="kpi-label">${t.label}</span>
    <span class="kpi-icon"><${Icon} name=${t.icon} /></span>
    <span class="kpi-value">${counts && !stale ? count(value) : counts || down ? '—' : html`<${Skeleton} kind="kpi" />`}</span>
  </a>`;
  return html`<${Card} section="kpis" title="Jobs" labelId="kpis-title" note="All time">
    <div class="kpis">
      ${TILES.map((t) => tile(t, counts && counts[t.key]))}
      ${tile(TOTAL_TILE, counts && counts.total)}
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
      ${state.jobs.map((job) => html`<li key=${job.id}><${JobRow} job=${job} href=${buildHash('overview', {}, job.id)} /></li>`)}
    </ul>`;
  return html`<${Card}
    className="card-flush"
    section="live"
    title="Live queue"
    labelId="live-queue-title"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', {})}>Open queue</${LinkButton}>`}
  >${body}</${Card}>`;
}

const IDLE_VIEW = { icon: 'circle-dashed', role: 'neutral', label: 'Idle' };

/**
 * The live queue with nothing in it: a card shaped like a job's (the same
 * cover square and four text lines), as tall as one with its progress row's
 * room split above and below (the skin's padding), so the first job that
 * starts takes its place without moving the page below.
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
const ATTENTION_KEY = 'overview-attention';
const HOLD_MS = 400;

function Attention({ onKnown }) {
  const app = useApp();
  const now = useNow();
  const [jobs, setJobs] = useState(() => cached(ATTENTION_KEY) || []);
  const [stamp, setStamp] = useState(0);
  const [busy, setBusy] = useState(null);
  const seq = useRef(0);
  useEffect(() => {
    const mine = ++seq.current;
    listDownloads({ status: 'failed', sort: 'updated_at', order: 'desc', limit: ATTENTION_LIMIT }).then(
      (body) => {
        if (mine !== seq.current) return;
        remember(ATTENTION_KEY, body.downloads);
        setJobs(body.downloads);
        onKnown();
      },
      () => {
        if (mine !== seq.current) return;
        setJobs([]);
        onKnown();
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
            : html`<a class="attention-link" href=${buildHash('overview', {}, job.id)} dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</a>`}
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

