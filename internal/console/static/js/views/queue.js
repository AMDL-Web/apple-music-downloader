// Queue page: Active / Failed / All tabs, search and type filter, a dense
// job list kept live from the downloads feed.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { jobTitle, jobSubtitle, typeLabel, artPalette, paletteStyle, jobFacts } from '../lib/format.js';
import { jobStatusView } from '../lib/status.js';
import { upsertJob, removeJob, appendPage } from '../lib/store.js';
import { buildHash, queueFilters, queueQuery, queueMatchFilter, queueAppends } from '../lib/route.js';
import { useApp, Artwork, Badge, JobProgress, RelTime, Icon, Segmented, Empty, ErrorNote, SkeletonRows, Button, LinkButton } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';

const PAGE_SIZE = 50;

/**
 * One job in a list: artwork, title, type, progress, status, last update.
 * The row carries the job's status (data-status) and Apple palette
 * (--art-bg, --art-t1..t3) for skins; .job-facts is hidden unless a skin shows it.
 */
export function JobRow({ job, href }) {
  const title = jobTitle(job);
  const sub = jobSubtitle(job);
  const palette = artPalette(job);
  const f = jobFacts(job);
  return html`<a class=${palette ? 'job-row has-palette' : 'job-row'} href=${href} data-row data-status=${job.status} style=${paletteStyle(palette)}>
    <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${160} className="art-40" />
    <span class="job-main">
      <span class="job-facts">
        ${f.type ? html`<span class="job-fact" data-key="type">${f.type}</span>` : null}
        ${f.genre ? html`<span class="job-fact" data-key="genre" dir="auto">${f.genre}</span>` : null}
        ${f.year ? html`<span class="job-fact" data-key="year">${f.year}</span>` : null}
        ${f.started ? html`<span class="job-fact" data-key="started">Started ${f.started}</span>` : null}
      </span>
      <span class="job-title" dir="auto" title=${title}>${title}</span>
      <span class="job-sub">
        ${sub ? html`<span class="job-sub-artist" dir="auto">${sub}</span>` : null}
        <span class="job-sub-type">${typeLabel(job.type)}</span>
        ${job.storefront ? html`<span class="job-sub-sf">${job.storefront.toUpperCase()}</span>` : null}
      </span>
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

const TYPE_OPTIONS = [
  ['', 'All types'],
  ['album', 'Albums'],
  ['playlist', 'Playlists'],
  ['song', 'Songs'],
  ['artist', 'Artists'],
  ['station', 'Stations'],
];

export function QueuePage({ route }) {
  const app = useApp();
  const filters = queueFilters(route.params);
  const [qInput, setQInput] = useState(filters.q);
  const q = useDebounced(qInput, 300);
  const [state, setState] = useState({ jobs: [], total: 0, loading: true, error: null, more: false });
  const key = `${filters.tab}|${filters.type}|${filters.q}|${app.epoch}`;
  const seq = useRef(0);

  const setParams = (patch) => {
    app.navigate(buildHash('queue', { ...route.params, ...patch }, route.jobId), { replace: true });
  };

  useEffect(() => {
    if (q !== filters.q) setParams({ q });
  }, [q]);
  useEffect(() => {
    if (filters.q !== qInput && filters.q !== q) setQInput(filters.q);
  }, [filters.q]);

  const load = (offset) => {
    const mine = ++seq.current;
    if (offset === 0) setState((s) => ({ ...s, loading: true, error: null }));
    else setState((s) => ({ ...s, more: true }));
    listDownloads(queueQuery(filters, offset, PAGE_SIZE)).then(
      (body) => {
        if (mine !== seq.current) return;
        setState((s) => ({
          jobs: offset === 0 ? body.downloads : appendPage(s.jobs, body.downloads),
          total: body.total,
          loading: false,
          error: null,
          more: false,
        }));
      },
      (error) => {
        if (mine !== seq.current) return;
        setState((s) => ({ ...s, loading: false, more: false, error }));
      },
    );
  };

  useEffect(() => load(0), [key]);

  useEffect(() => {
    const filter = queueMatchFilter(filters);
    const opts = { append: queueAppends(filters) };
    return app.subscribeFeed((batch) => setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, filter, opts) })));
  }, [key]);

  const c = app.counts;
  const tabs = [
    { value: 'active', label: 'Active', count: c ? c.queued + c.running : undefined },
    { value: 'failed', label: 'Failed', count: c ? c.failed : undefined },
    { value: 'all', label: 'All', count: c ? c.total : undefined },
  ];
  const rowHref = (job) => buildHash('queue', route.params, job.id);

  let body;
  if (state.loading && state.jobs.length === 0) body = html`<${SkeletonRows} n=${6} kind="row" />`;
  else if (state.error && state.jobs.length === 0)
    body = html`<${ErrorNote} title="Couldn't load downloads" error=${state.error} onRetry=${() => load(0)} />`;
  else if (state.jobs.length === 0) body = html`<${QueueEmpty} filters=${filters} onClear=${() => { setQInput(''); setParams({ q: '', type: '' }); }} />`;
  else
    body = html`<ul class="job-list" aria-label="Jobs">
        ${state.jobs.map((job) => html`<li key=${job.id}><${JobRow} job=${job} href=${rowHref(job)} /></li>`)}
      </ul>
      ${state.jobs.length < state.total
        ? html`<div class="load-more">
            <${Button} onClick=${() => load(state.jobs.length)} disabled=${state.more}>${state.more ? 'Loading…' : `Load more (${state.total - state.jobs.length} left)`}</${Button}>
          </div>`
        : null}`;

  return html`<div class="page page-queue">
    <div class="toolbar">
      <${Segmented} label="Job status" options=${tabs} value=${filters.tab} onChange=${(tab) => setParams({ tab: tab === 'active' ? '' : tab })} />
      <div class="toolbar-filters">
        <label class="search">
          <${Icon} name="search" />
          <input
            type="search"
            data-filter
            placeholder="Search title or URL"
            aria-label="Search jobs"
            value=${qInput}
            onInput=${(e) => setQInput(e.currentTarget.value)}
          />
        </label>
        <select class="select" aria-label="Job type" value=${filters.type} onChange=${(e) => setParams({ type: e.currentTarget.value })}>
          ${TYPE_OPTIONS.map(([v, l]) => html`<option key=${v} value=${v}>${l}</option>`)}
        </select>
      </div>
    </div>
    ${state.error && state.jobs.length > 0 ? html`<${ErrorNote} title="Couldn't refresh downloads" error=${state.error} onRetry=${() => load(0)} />` : null}
    <div class="card card-flush" data-section="jobs"><div class="card-body">${body}</div></div>
  </div>`;
}

const startAction = () => html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`;

function QueueEmpty({ filters, onClear }) {
  if (filters.q || filters.type) {
    return html`<${Empty} icon="search" title="No jobs match these filters" action=${html`<${Button} size="sm" onClick=${onClear}>Clear filters</${Button}>`} />`;
  }
  if (filters.tab === 'failed') return html`<${Empty} icon="circle-check" title="No failed jobs" />`;
  if (filters.tab === 'active') {
    return html`<${Empty} icon="list" title="No active downloads" action=${startAction()}>
      Paste an Apple Music link anywhere in the console, or start one here.
    </${Empty}>`;
  }
  return html`<${Empty} icon="list" title="No jobs yet" action=${startAction()} />`;
}
