// Overview: KPI tiles, the live queue, system health, recently completed
// artwork, throughput from /stats and a log tail.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads, getHealth, getWrapperStatus, getLibrarySync, getHooks, getStats, listLogs, logStreamUrl, settle, parseData } from '../api.js';
import { count, bytes, dayLabel, jobTitle, jobSubtitle, artPalette, clockTime } from '../lib/format.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView, logLevelView, isAlreadyInLibrary, LOG_RETENTION_OFF } from '../lib/status.js';
import { upsertJob, appendLogs, emptyLogs } from '../lib/store.js';
import { StreamClient, claimSecondary, releaseSecondary, createCoalescer, isFeatureUnavailable } from '../lib/stream.js';
import { buildHash } from '../lib/route.js';
import { useApp, Card, Icon, Badge, Artwork, Empty, ErrorNote, SkeletonRows, Skeleton, Unavailable, LinkButton } from '../ui/components.js';
import { usePoll, useColumns } from '../ui/hooks.js';
import { JobRow, applyFeedToList } from './queue.js';

export function OverviewPage() {
  return html`<div class="page page-overview">
    <${KpiRow} />
    <div class="grid-2">
      <${LiveQueue} />
      <${HealthCard} />
    </div>
    <${RecentStrip} />
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

function KpiRow() {
  const { counts, conn } = useApp();
  const stale = conn.state === 'offline';
  return html`<section class="kpis" aria-label="Job counts">
    ${TILES.map(
      (t) => html`<a key=${t.key} class="kpi" href=${t.href}>
        <span class="kpi-label">${t.label}</span>
        <span class="kpi-icon" data-role=${t.role}><${Icon} name=${t.icon} /></span>
        ${counts ? html`<span class="kpi-value">${stale ? '—' : count(counts[t.key])}</span>` : html`<${Skeleton} kind="kpi" />`}
        <span class="kpi-meta">${counts ? `of ${count(counts.total)} total` : ''}</span>
      </a>`,
    )}
    <a class="kpi kpi-total" href=${buildHash('queue', { tab: 'all' })}>
      <span class="kpi-label">Total</span>
      <span class="kpi-icon" data-role="neutral"><${Icon} name="list" /></span>
      ${counts ? html`<span class="kpi-value">${stale ? '—' : count(counts.total)}</span>` : html`<${Skeleton} kind="kpi" />`}
      <span class="kpi-meta">${counts ? `${count(counts.cancelled)} cancelled` : ''}</span>
    </a>
  </section>`;
}

const LIVE_LIMIT = 8;
const LIVE_FILTER = { statuses: ['running', 'queued'] };

function LiveQueue() {
  const app = useApp();
  const [state, setState] = useState({ jobs: [], loading: true, error: null });
  const [stamp, setStamp] = useState(0);
  const load = () =>
    listDownloads({ status: 'running,queued', sort: 'created_at', order: 'asc', limit: LIVE_LIMIT }).then(
      (body) => setState({ jobs: body.downloads, loading: false, error: null }),
      (error) => setState((s) => ({ ...s, loading: false, error })),
    );
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
  const active = app.counts ? app.counts.running + app.counts.queued : null;
  let body;
  if (state.loading) body = html`<${SkeletonRows} n=${4} kind="row" />`;
  else if (state.error) body = html`<${ErrorNote} title="Couldn't load the queue" error=${state.error} onRetry=${load} />`;
  else if (state.jobs.length === 0) body = html`<${Empty} icon="list" title="Nothing in the queue">Submit links from the iOS app, amdl-web or <code>POST /api/v1/downloads</code>.</${Empty}>`;
  else
    body = html`<ul class="job-list job-list-compact" aria-label="Active jobs">
      ${state.jobs.map((job) => html`<li key=${job.id}><${JobRow} job=${job} href=${buildHash('queue', {}, job.id)} /></li>`)}
    </ul>`;
  return html`<${Card}
    className="card-flush span-8"
    title="Live queue"
    labelId="live-queue-title"
    note=${active !== null ? `${count(active)} active` : null}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', {})}>Open queue</${LinkButton}>`}
  >${body}</${Card}>`;
}

function loadHealth() {
  return Promise.all([settle(getHealth()), settle(getWrapperStatus()), settle(getLibrarySync()), settle(getHooks())]);
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

function HealthCard() {
  const poll = usePoll(loadHealth, 30000);
  return html`<${Card}
    className="span-4"
    title="System health"
    labelId="health-title"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('system')}>Details</${LinkButton}>`}
  >
    ${poll.data
      ? html`<ul class="health-list">
          ${healthRows(poll.data).map(
            ([name, view]) => html`<li key=${name} class="health-row">
              <span class="health-name">${name}</span>
              <${Badge} view=${view} />
              ${view.detail ? html`<span class="health-detail" title=${view.detail}>${view.detail}</span>` : null}
            </li>`,
          )}
        </ul>`
      : html`<${SkeletonRows} n=${4} kind="line" />`}
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
  const load = () =>
    listDownloads({ status: 'completed', sort: 'updated_at', order: 'desc', limit: 16 }).then(
      (body) => setState({ jobs: body.downloads, loading: false, error: null }),
      (error) => setState((s) => ({ ...s, loading: false, error })),
    );
  useEffect(() => {
    load();
  }, [app.epoch]);
  useEffect(
    () => app.subscribeFeed((batch) => setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, RECENT_FILTER, { limit: 16 }) }))),
    [],
  );
  const shown = cols > 0 ? state.jobs.slice(0, cols) : [];
  return html`<${Card}
    title="Recently completed"
    labelId="recent-title"
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('showcase')}>Showcase</${LinkButton}>`}
  >
    <div class="strip" ref=${ref}>
      ${state.loading
        ? Array.from({ length: cols || 1 }, (_, i) => html`<${Skeleton} key=${i} kind="tile" />`)
        : state.error
          ? html`<${ErrorNote} title="Couldn't load completed jobs" error=${state.error} onRetry=${load} />`
          : state.jobs.length === 0
            ? html`<${Empty} icon="disc" title="Completed downloads will appear here" />`
            : shown.map((job) => {
                const p = artPalette(job);
                return html`<a key=${job.id} class="strip-tile" href=${buildHash('showcase', {}, job.id)}>
                  <${Artwork} url=${job.artwork_url} bg=${p && p.bg} px=${240} className="art-square" />
                  <span class="strip-title" dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</span>
                  <span class="strip-sub" dir="auto">${isAlreadyInLibrary(job) ? 'Already in library' : jobSubtitle(job)}</span>
                </a>`;
              })}
    </div>
  </${Card}>`;
}

const STATS_DAYS = 14;

function ThroughputCard() {
  const app = useApp();
  const poll = usePoll(() => getStats(STATS_DAYS), 60000, [app.epoch]);
  if (poll.error && poll.error.status === 404) {
    return html`<${Card} title="Throughput" labelId="throughput-title"><${Unavailable} what="Throughput history (GET /api/v1/stats)" /></${Card}>`;
  }
  const stats = poll.data;
  const totals = stats && stats.totals;
  return html`<${Card}
    title=${`Throughput · last ${STATS_DAYS} days`}
    labelId="throughput-title"
    note=${totals ? `${count(totals.tracks_completed)} tracks · ${bytes(totals.bytes_written)} written` : null}
  >
    ${stats
      ? html`<${Throughput} daily=${stats.daily || []} />`
      : poll.error
        ? html`<${ErrorNote} title="Couldn't load stats" error=${poll.error} onRetry=${poll.reload} />`
        : html`<${Skeleton} kind="chart" />`}
  </${Card}>`;
}

/** Compact tick value: 1284 -> "1.3K". */
function compact(n) {
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(n >= 1e4 ? 0 : 1)}K`;
  return String(n);
}

/**
 * Two small multiples sharing the date axis: tracks completed and bytes
 * written per day. Two measures, two charts, never a second y-axis.
 */
export function Throughput({ daily }) {
  const [hover, setHover] = useState(null);
  if (daily.length === 0) return html`<${Empty} icon="info" title="No history yet" />`;
  const shown = hover !== null ? daily[hover] : daily[daily.length - 1];
  const maxTracks = Math.max(0, ...daily.map((d) => d.tracks_completed || 0));
  const maxBytes = Math.max(0, ...daily.map((d) => d.bytes_written || 0));
  const mid = Math.floor((daily.length - 1) / 2);
  const series = [
    { key: 'tracks', title: 'Tracks completed per day', field: 'tracks_completed', max: maxTracks, fmt: (v) => compact(v) },
    { key: 'bytes', title: 'Data written per day', field: 'bytes_written', max: maxBytes, fmt: (v) => bytes(v) },
  ];
  return html`<div class="chart" onMouseLeave=${() => setHover(null)}>
    <p class="chart-readout" aria-live="off">
      <span class="chart-readout-day">${dayLabel(shown.date)}${hover === null ? ' (latest)' : ''}</span>
      <span>${count(shown.tracks_completed || 0)} tracks</span>
      <span>${bytes(shown.bytes_written || 0)}</span>
      <span>${count(shown.jobs_completed || 0)} jobs completed${shown.jobs_failed ? `, ${count(shown.jobs_failed)} failed` : ''}</span>
    </p>
    ${series.map(
      (s) => html`<figure key=${s.key} class="chart-multiple" aria-hidden="true">
        <figcaption class="chart-title">${s.title}</figcaption>
        <div class="chart-plot">
          <div class="chart-yaxis">
            <span>${s.fmt(s.max)}</span>
            <span>0</span>
          </div>
          <div class="chart-bars" style=${{ '--n': String(daily.length) }}>
            ${daily.map((d, i) => {
              const v = d[s.field] || 0;
              return html`<div
                key=${d.date}
                class=${hover === i ? 'chart-col is-hover' : 'chart-col'}
                onMouseEnter=${() => setHover(i)}
              >
                <span class=${v > 0 ? 'chart-bar' : 'chart-bar is-zero'} style=${{ '--v': String(s.max > 0 ? v / s.max : 0) }}></span>
              </div>`;
            })}
          </div>
        </div>
      </figure>`,
    )}
    <div class="chart-xaxis" aria-hidden="true">
      <span>${dayLabel(daily[0].date)}</span>
      ${daily.length > 2 ? html`<span>${dayLabel(daily[mid].date)}</span>` : null}
      <span>${dayLabel(daily[daily.length - 1].date)}</span>
    </div>
    <div class="sr-only"><table>
      <caption>Completed tracks and bytes written per day (UTC)</caption>
      <thead><tr><th scope="col">Date</th><th scope="col">Tracks completed</th><th scope="col">Bytes written</th><th scope="col">Jobs completed</th><th scope="col">Jobs failed</th></tr></thead>
      <tbody>
        ${daily.map(
          (d) => html`<tr key=${d.date}><th scope="row">${d.date}</th><td>${d.tracks_completed || 0}</td><td>${bytes(d.bytes_written || 0)}</td><td>${d.jobs_completed || 0}</td><td>${d.jobs_failed || 0}</td></tr>`,
        )}
      </tbody>
    </table></div>
  </div>`;
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
                return html`<li key=${e.sequence} class="log-tail-row" data-level=${e.level}>
                  <span class="log-time">${clockTime(e.time, false)}</span>
                  <span class="log-level" data-role=${lv.role}><span class="log-dot"></span>${lv.short}</span>
                  <span class="log-msg">${e.message}</span>
                </li>`;
              })}
            </ol>`}
  </${Card}>`;
}
