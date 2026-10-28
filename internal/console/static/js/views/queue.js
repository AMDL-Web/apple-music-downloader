// Queue page: Active / Failed / All tabs, search and type filter, a dense
// job list kept live from the downloads feed, and a selection mode for
// Retry, Cancel and Delete on many jobs (views/queue-select.js).

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { jobTitle, jobSubtitle, typeLabel, artPalette, paletteStyle, jobFacts, isUnresolved, inputLabel, unresolvedHint, errorSummary } from '../lib/format.js';
import { jobStatusView, runningFirst } from '../lib/status.js';
import { upsertJob, removeJob, appendPage } from '../lib/store.js';
import { buildHash, queueFilters, queueQuery, queueMatchFilter, queueAppends } from '../lib/route.js';
import { toggleId, setIds, pruneSelection, rangeIds, selectedJobs, selectionState } from '../lib/selection.js';
import { actionPlan, jobAllows } from '../lib/jobactions.js';
import { useApp, Artwork, Badge, JobProgress, RelTime, Icon, Segmented, Empty, ErrorNote, SkeletonRows, Button, LinkButton } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';
import { useBulkRun, SelectionBar, BulkConfirm } from './queue-select.js';

const PAGE_SIZE = 50;

/**
 * One job in a list: artwork, title, type, progress, status, last update.
 * The row carries the job's status (data-status) and Apple palette
 * (--art-bg, --art-t1..t3) for skins; .job-facts is hidden unless a skin shows it.
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

  // The search box and the route's q follow each other. A q this page wrote
  // itself (ownQ) comes back through the route and must not reset a box the
  // user has typed on; any other change of the route's q (Back, Forward, a
  // link) replaces the box, and drops a debounced value that is now stale.
  const ownQ = useRef(null);
  useEffect(() => {
    if (q !== filters.q) {
      ownQ.current = q;
      setParams({ q });
    }
  }, [q]);
  useEffect(() => {
    if (filters.q === ownQ.current) return;
    ownQ.current = null;
    if (filters.q !== qInput) setQInput(filters.q);
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
    // The snapshot's total moves with the rows the feed adds and removes
    // (an estimate: a change to a job past the loaded pages is not seen).
    return app.subscribeFeed((batch) =>
      setState((s) => {
        const jobs = applyFeedToList(s.jobs, batch, filter, opts);
        return jobs === s.jobs ? s : { ...s, jobs, total: Math.max(jobs.length, s.total + jobs.length - s.jobs.length) };
      }),
    );
  }, [key]);

  // Selection: a Set of ids, judged against the current list on every render,
  // so a job the feed removes leaves it and a status change drops actions.
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState(() => new Set());
  const [confirm, setConfirm] = useState(null);
  const bulk = useBulkRun(app);
  // The rows as shown (the Active tab lifts running jobs), which is the order
  // Shift-click ranges follow.
  const rows = filters.tab === 'active' ? runningFirst(state.jobs) : state.jobs;
  const jobsRef = useRef(rows);
  jobsRef.current = rows;
  const anchorId = useRef(null);
  useEffect(() => setSelected((s) => pruneSelection(s, state.jobs)), [state.jobs]);

  const toggle = (id, range) => {
    const from = anchorId.current;
    anchorId.current = id;
    setSelected((s) => {
      const on = !s.has(id);
      return range && from ? setIds(s, rangeIds(jobsRef.current, from, id), on) : toggleId(s, id, on);
    });
  };
  const leaveSelection = () => {
    setSelecting(false);
    setSelected(new Set());
    anchorId.current = null;
  };
  const chosen = selectedJobs(selected, state.jobs);
  const plan = actionPlan(chosen);
  // A run disables or removes the control that had focus (the action
  // buttons, Retry all, the confirm dialog), and the feed then removes rows
  // and buttons; until a moment after it ends, focus that falls to the body
  // moves to the selection controls (the effect below).
  const rescueUntil = useRef(0);
  const runAction = async (action, targets) => {
    rescueUntil.current = Infinity;
    const reached = await bulk.start(action, targets);
    if (action === 'delete') setSelected((s) => setIds(s, reached, false));
    setConfirm(null);
    rescueUntil.current = Date.now() + 3000;
  };
  useEffect(() => {
    if (Date.now() > rescueUntil.current) return;
    const active = document.activeElement;
    if (active && active !== document.body) return;
    const target = RESCUE.map((q) => document.querySelector(q)).find(Boolean);
    if (target) target.focus({ preventScroll: true });
  });
  const onAction = (action) => {
    if (action === 'retry') runAction('retry', plan.retry);
    else setConfirm({ action, jobs: null });
  };
  const retryable = state.jobs.filter((j) => jobAllows(j, 'retry'));

  // Escape leaves selection mode; x toggles the focused row (and enters the
  // mode). Both stand down while typing, under the drawer or any overlay.
  const selectingRef = useRef(selecting);
  selectingRef.current = selecting;
  useEffect(() => {
    const onKey = (e) => {
      if (e.metaKey || e.ctrlKey || e.altKey || route.jobId || typing(e.target)) return;
      if (document.querySelector('.overlay-host, .drawer-host')) return;
      if (e.key === 'Escape') {
        if (!selectingRef.current) return;
        e.preventDefault();
        const inList = document.activeElement && document.activeElement.closest('.bulk-bar, .job-check');
        leaveSelection();
        const btn = document.querySelector('.queue-select');
        if (inList && btn) btn.focus();
        return;
      }
      if (e.key !== 'x') return;
      const li = document.activeElement && document.activeElement.closest('.page-queue [data-job-id]');
      if (!li) return;
      e.preventDefault();
      setSelecting(true);
      toggle(li.getAttribute('data-job-id'), false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [route.jobId]);

  const c = app.counts;
  const tabs = [
    { value: 'active', label: 'Active', count: c ? c.queued + c.running : undefined },
    { value: 'completed', label: 'Completed', count: c ? c.completed : undefined },
    { value: 'failed', label: 'Failed', count: c ? c.failed : undefined },
    { value: 'cancelled', label: 'Cancelled', count: c ? c.cancelled : undefined },
    { value: 'all', label: 'All', count: c ? c.total : undefined },
  ];
  const rowHref = (job) => buildHash('queue', route.params, job.id);

  let body;
  if (state.loading && state.jobs.length === 0) body = html`<${SkeletonRows} n=${6} kind="row" />`;
  else if (state.error && state.jobs.length === 0)
    body = html`<${ErrorNote} title="Couldn't load downloads" error=${state.error} onRetry=${() => load(0)} />`;
  else if (state.jobs.length === 0) body = html`<${QueueEmpty} filters=${filters} tabs=${tabs} onClear=${() => { setQInput(''); setParams({ q: '', type: '' }); }} />`;
  else
    body = html`<ul class=${selecting ? 'job-list is-selecting' : 'job-list'} aria-label="Jobs">
        ${rows.map((job) => {
          const on = selecting && selected.has(job.id);
          return html`<li key=${job.id} class="job-item" data-job-id=${job.id} data-selected=${on ? '' : undefined}>
            ${selecting
              ? html`<label class="job-check">
                  <input
                    type="checkbox"
                    class="job-check-input"
                    checked=${on}
                    aria-label=${`Select ${isUnresolved(job) ? inputLabel(job.input) : jobTitle(job)}`}
                    onClick=${(e) => toggle(job.id, e.shiftKey)}
                  />
                </label>`
              : null}
            <${JobRow}
              job=${job}
              href=${rowHref(job)}
              selected=${selecting ? on : undefined}
              onSelect=${selecting ? (e) => toggle(job.id, e.shiftKey) : undefined}
            />
          </li>`;
        })}
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
            placeholder="Search title, artist or URL"
            aria-label="Search jobs"
            value=${qInput}
            onInput=${(e) => setQInput(e.currentTarget.value)}
          />
        </label>
        <select class="select" aria-label="Job type" value=${filters.type} onChange=${(e) => setParams({ type: e.currentTarget.value })}>
          ${TYPE_OPTIONS.map(([v, l]) => html`<option key=${v} value=${v}>${l}</option>`)}
        </select>
      </div>
      ${selecting || state.jobs.length > 0
        ? html`<div class="queue-tools">
            ${filters.tab === 'failed' && retryable.length > 0
              ? html`<${Button}
                  icon="rotate"
                  className="queue-retry-all"
                  aria-label=${`Retry all ${retryable.length} loaded failed jobs`}
                  disabled=${Boolean(bulk.run)}
                  onClick=${() => runAction('retry', retryable)}
                >Retry all <span class="bulk-n">${retryable.length}</span></${Button}>`
              : null}
            <${Button}
              icon="square-check"
              className="queue-select"
              aria-pressed=${String(selecting)}
              aria-keyshortcuts="x"
              title="Select jobs (x selects the focused one, Esc leaves)"
              onClick=${() => (selecting ? leaveSelection() : setSelecting(true))}
            >Select</${Button}>
          </div>`
        : null}
    </div>
    ${state.error && state.jobs.length > 0 ? html`<${ErrorNote} title="Couldn't refresh downloads" error=${state.error} onRetry=${() => load(0)} />` : null}
    ${selecting || bulk.run
      ? html`<${SelectionBar}
          jobs=${state.jobs}
          selected=${selected}
          plan=${plan}
          selecting=${selecting}
          run=${bulk.run}
          onSelectAll=${() => setSelected(selectionState(selected, state.jobs) === 'all' ? new Set() : new Set(state.jobs.map((j) => j.id)))}
          onAction=${onAction}
        />`
      : null}
    <div class="card card-flush" data-section="jobs"><div class="card-body">${body}</div></div>
    ${confirm
      ? html`<${BulkConfirm}
          action=${confirm.action}
          jobs=${confirm.jobs || plan[confirm.action]}
          skipped=${confirm.jobs ? confirm.skipped : chosen.length - plan[confirm.action].length}
          run=${bulk.run}
          onConfirm=${() => {
            // The targets freeze here: rows the run deletes leave the live plan.
            const targets = plan[confirm.action];
            setConfirm({ action: confirm.action, jobs: targets, skipped: chosen.length - targets.length });
            runAction(confirm.action, targets);
          }}
          onCancel=${() => setConfirm(null)}
        />`
      : null}
  </div>`;
}

const RESCUE = ['.bulk-bar .bulk-action:not(:disabled)', '.bulk-check:not(:disabled)', '.queue-retry-all:not(:disabled)', '.queue-select', '.page-title'];

const typing = (el) =>
  Boolean(el) &&
  ((el.tagName === 'INPUT' && !['checkbox', 'radio', 'button', 'submit'].includes(el.type)) ||
    el.tagName === 'TEXTAREA' ||
    el.tagName === 'SELECT' ||
    el.isContentEditable);

const startAction = () => html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`;

function QueueEmpty({ filters, tabs, onClear }) {
  if (filters.q || filters.type) {
    // The search covers the current tab only; say so, and which tab.
    const tab = tabs.find((t) => t.value === filters.tab);
    const inTab = filters.tab !== 'all' && tab ? ` on the ${tab.label} tab` : '';
    return html`<${Empty}
      icon="search"
      title=${`No jobs match these filters${inTab}`}
      action=${html`<${Button} size="sm" onClick=${onClear}>Clear filters</${Button}>`}
    >${filters.tab !== 'all' ? 'Other tabs may match; All searches every job.' : null}</${Empty}>`;
  }
  if (filters.tab === 'failed') return html`<${Empty} icon="circle-check" title="No failed jobs" />`;
  if (filters.tab === 'completed') return html`<${Empty} icon="list" title="No completed jobs yet" />`;
  if (filters.tab === 'cancelled') return html`<${Empty} icon="list" title="No cancelled jobs" />`;
  if (filters.tab === 'active') {
    return html`<${Empty} icon="list" title="No active downloads" action=${startAction()}>
      Paste an Apple Music link anywhere in the console, or start one here.
    </${Empty}>`;
  }
  return html`<${Empty} icon="list" title="No jobs yet" action=${startAction()} />`;
}
