// Overview: KPI tiles, the live queue, system health, recently completed
// artwork, throughput from /stats and a log tail.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads, retryDownload, getHealth, getWrapperStatus, getLibrarySync, getHooks, getSystem, listLogs, logStreamUrl, settle, parseData } from '../api.js';
import { count, jobTitle, jobSubtitle, artPalette, paletteStyle, clockTime, jobFacts, systemFacts, attrPairs, isUnresolved, inputLabel, unresolvedHint, errorSummary } from '../lib/format.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView, logLevelView, isAlreadyInLibrary, LOG_RETENTION_OFF } from '../lib/status.js';
import { upsertJob, appendLogs, emptyLogs } from '../lib/store.js';
import { StreamClient, claimSecondary, releaseSecondary, createCoalescer, isFeatureUnavailable } from '../lib/stream.js';
import { buildHash } from '../lib/route.js';
import { useApp, Card, Icon, Artwork, Empty, ErrorNote, SkeletonRows, Skeleton, LinkButton, Button, RelTime, HealthList, Facts, StatusText } from '../ui/components.js';
import { usePoll, useColumns, useNow } from '../ui/hooks.js';
import { JobRow, applyFeedToList } from './queue.js';
import { ThroughputCard } from './throughput.js';

// Sections follow the reading order of both skins, so Tab order equals the
// visual order: the server status line, Live queue, Recently completed, Jobs,
// System health, Throughput, Log tail.
export function OverviewPage() {
  const app = useApp();
  const health = usePoll(loadHealth, 30000, [app.epoch]);
  return html`<div class="page page-overview">
    <${HealthStrip} poll=${health} />
    <${LiveQueue} />
    <${RecentStrip} />
    <${KpiRow} />
    <${HealthCard} poll=${health} />
    <${ThroughputCard} />
    <${LogTail} />
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
    ${counts ? html`<span class="kpi-value">${stale ? '—' : count(value)}</span>` : html`<${Skeleton} kind="kpi" />`}
    <span class="kpi-meta">${counts ? meta : ''}</span>
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
  if (state.loading) body = html`<${SkeletonRows} n=${4} kind="row" />`;
  else if (state.error) body = html`<${ErrorNote} title="Couldn't load the queue" error=${state.error} onRetry=${load} />`;
  else if (state.jobs.length === 0)
    body = html`<${Empty}
      icon="list"
      title="Nothing in the queue"
      action=${html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`}
    >Paste an Apple Music link anywhere in the console, or start one here.</${Empty}>`;
  else
    body = html`<ul class="job-list job-list-compact" aria-label="Active jobs">
      ${state.jobs.map((job) => html`<li key=${job.id}><${JobRow} job=${job} href=${buildHash('queue', {}, job.id)} /></li>`)}
    </ul>`;
  return html`<${Card}
    className="card-flush"
    section="live"
    title="Live queue"
    labelId="live-queue-title"
    note=${c ? `${count(c.running)} running · ${count(c.queued)} queued` : null}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', {})}>Open queue</${LinkButton}>`}
  >${body}<${Attention} /></${Card}>`;
}

const ATTENTION_LIMIT = 2;
const ATTENTION_WINDOW_MS = 24 * 60 * 60 * 1000;
const FAILED_FILTER = { statuses: ['failed'] };

/**
 * Opt-in hook (hidden by the base): the latest failed jobs of the last 24
 * hours under the live list, each with Retry. A retried or deleted job
 * leaves through the feed, and the next one in line comes from a fresh read.
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
      app.toast({ message: e.message || 'The request failed', role: 'danger', sticky: true });
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
  return html`<ul class="attention" aria-label="Recent failures">
    ${recent.map(
      (job) => html`<li key=${job.id} class="attention-row" data-role="danger">
        <span class="attention-icon"><${Icon} name="circle-x" /></span>
        <span class="attention-title">
          <span class="attention-label">Failed</span>
          ${isUnresolved(job)
            ? null
            : html`<a class="attention-link" href=${buildHash('queue', {}, job.id)} dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</a>`}
          ${isUnresolved(job)
            ? html`<a class="attention-link attention-input" href=${buildHash('queue', {}, job.id)} title=${job.input}>${inputLabel(job.input)}</a>`
            : html`<span class="attention-input" title=${job.input}>${job.input}</span>`}
        </span>
        <span class="attention-detail">
          ${job.error ? html`<span class="attention-error" title=${job.error}>${errorSummary(job.error)}</span>` : null}
          <${RelTime} value=${job.finished_at || job.updated_at} />
        </span>
        <${Button} size="sm" icon="rotate" onClick=${() => retry(job)} disabled=${busy === job.id}>Retry</${Button}>
      </li>`,
    )}
  </ul>`;
}

function loadHealth() {
  return Promise.all([settle(getHealth()), settle(getWrapperStatus()), settle(getLibrarySync()), settle(getHooks()), settle(getSystem())]);
}

/** The four health rows shared by the Overview card and the System page. */
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
 * (the wrapper unreachable, library sync failing), with a link to System.
 * Nothing renders until the first read settles.
 */
function HealthStrip({ poll }) {
  if (!poll.data) return null;
  const rows = healthRows(poll.data);
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

function HealthCard({ poll }) {
  const sys = poll.data && poll.data[4] && !poll.data[4].error ? poll.data[4].data : null;
  const facts = systemFacts(sys);
  return html`<${Card}
    section="health"
    title="System health"
    labelId="health-title"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('system')}>Details</${LinkButton}>`}
  >
    ${poll.data ? html`<${HealthList} rows=${healthRows(poll.data)} />` : html`<${SkeletonRows} n=${4} kind="line" />`}
    ${facts.length ? html`<${Facts} className="health-facts" label="Runtime" rows=${facts} />` : null}
  </${Card}>`;
}

const RECENT_FILTER = { statuses: ['completed'] };
const TILE_MIN = 120;
const TILE_GAP = 16;

function RecentStrip() {
  const app = useApp();
  const ref = useRef(null);
  const cols = useColumns(ref, TILE_MIN, TILE_GAP);
  const [state, setState] = useState({ jobs: [], loading: true, error: null });
  const seq = useRef(0);
  const load = () => {
    const mine = ++seq.current;
    return listDownloads({ status: 'completed', sort: 'updated_at', order: 'desc', limit: 16 }).then(
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
    () => app.subscribeFeed((batch) => setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, RECENT_FILTER, { limit: 16 }) }))),
    [],
  );
  // Every tile renders; the ones past the whole-tile count carry is-overflow,
  // which the base hides and a skin may show as a scrolling shelf.
  return html`<${Card}
    section="recent"
    title="Recently completed"
    labelId="recent-title"
    note=${app.counts ? `${count(app.counts.completed)} completed` : null}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('showcase')}>Showcase</${LinkButton}>`}
  >
    <div class="strip" ref=${ref}>
      ${state.loading
        ? Array.from({ length: cols || 1 }, (_, i) => html`<${Skeleton} key=${i} kind="tile" />`)
        : state.error
          ? html`<${ErrorNote} title="Couldn't load completed jobs" error=${state.error} onRetry=${load} />`
          : state.jobs.length === 0
            ? html`<${Empty} icon="disc" title="Completed downloads will appear here" />`
            : state.jobs.map((job, i) => {
                const p = artPalette(job);
                const f = jobFacts(job);
                const cls = ['strip-tile', p ? 'has-palette' : '', cols > 0 && i >= cols ? 'is-overflow' : ''].filter(Boolean).join(' ');
                return html`<a key=${job.id} class=${cls} href=${buildHash('showcase', {}, job.id)} style=${paletteStyle(p)}>
                  <${Artwork} url=${job.artwork_url} bg=${p && p.bg} px=${240} className="art-square" />
                  <span class="strip-title" dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</span>
                  <span class="strip-sub" dir="auto">${isAlreadyInLibrary(job) ? 'Already in library' : jobSubtitle(job)}</span>
                  <span class="strip-meta">${[f.tracks, f.finished, f.took].filter(Boolean).join(' · ')}</span>
                </a>`;
              })}
    </div>
  </${Card}>`;
}

const TAIL_LINES = 12;

function LogTail() {
  const app = useApp();
  const [logs, setLogs] = useState(emptyLogs());
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    let client = null;
    let cancelled = false;
    const coalescer = createCoalescer((batch) => setLogs((s) => appendLogs(s, batch, TAIL_LINES)), 250);
    const query = { level: 'info,warn,error', limit: TAIL_LINES };
    listLogs(query).then(
      (page) => {
        if (cancelled) return;
        setLoading(false);
        setLogs((s) => appendLogs(s, page.logs || [], TAIL_LINES));
        client = new StreamClient({
          url: logStreamUrl(query),
          cursor: page.next_cursor || null,
          onEvent: (ev) => {
            const entry = parseData(ev);
            if (entry) coalescer.push(entry);
          },
        });
        claimSecondary(client);
        client.start();
      },
      (err) => {
        if (cancelled) return;
        setLoading(false);
        setError(err);
      },
    );
    return () => {
      cancelled = true;
      coalescer.cancel();
      if (client) {
        client.stop();
        releaseSecondary(client);
      }
    };
  }, [app.epoch]);

  return html`<${Card}
    className="card-flush"
    section="logs"
    title="Log tail"
    labelId="logtail-title"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('logs')}>Open logs</${LinkButton}>`}
  >
    ${loading
      ? html`<${SkeletonRows} n=${6} kind="log" />`
      : error
        ? isFeatureUnavailable(error.status, error.body)
          ? html`<${Empty} icon="file-text" title=${LOG_RETENTION_OFF} />`
          : html`<${ErrorNote} title="Couldn't load logs" error=${error} />`
        : logs.entries.length === 0
          ? html`<${Empty} icon="file-text" title="No log records yet" />`
          : html`<ol class="log-tail" aria-label="Latest log records">
              ${logs.entries.map((e) => {
                const lv = logLevelView(e.level);
                const attrs = e.attributes || {};
                const pairs = attrPairs(attrs, ['component']);
                return html`<li key=${e.sequence} class="log-entry log-tail-row" data-level=${e.level}>
                  <span class="log-time">${clockTime(e.time, false)}</span>
                  <span class="log-level" data-role=${lv.role}><span class="log-dot"></span>${lv.short}</span>
                  <span class="log-comp">${typeof attrs.component === 'string' ? attrs.component : ''}</span>
                  <span class="log-msg">${e.message}${pairs.length ? html` <span class="log-attrs">${pairs.join('  ')}</span>` : null}</span>
                </li>`;
              })}
            </ol>`}
  </${Card}>`;
}
