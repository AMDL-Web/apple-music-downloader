// Activity page (#/queue): amdl's records (albums and catalog playlists, one
// row each, however often they were downloaded) and station runs. All /
// Active / Done / Failed tabs with their counts, search and type filter.
// The queued and running runs head All and Active in run order (a record's
// run as its record's row), the rest follows newest activity first, read
// as streams that page on their own (views/queue-streams.js) as the list
// nears its end. Done heads with the disk check, Failed with the adds that
// could not be read. Each row has a "…" menu; a selection mode acts on many.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { count } from '../lib/format.js';
import { buildHash, queueFilters, rememberQueue } from '../lib/route.js';
import { toggleId, setIds, rangeIds, selectionState } from '../lib/selection.js';
import { orderable, arrange } from '../lib/queue-order.js';
import { mergeStreams, withoutLive, liveRow, liveMatches, rowPlan } from '../lib/queue-rows.js';
import { isNotFound } from '../lib/jobview.js';
import { resetRecords, retryDownload, resetRecord } from '../api.js';
import { useApp, Icon, Segmented, ErrorNote, SkeletonJobRows, Button } from '../ui/components.js';
import { useDebounced, useWatchShown, useLate } from '../ui/hooks.js';
import { useBulkRun, SelectionBar, BulkConfirm } from './queue-select.js';
import { JobRow, applyFeedToList } from './queue-row.js';
import { useUpNext, useQueueDrag } from './queue-order.js';
import { useChipRow } from './queue-chips.js';
import { QueueEmpty } from './queue-empty.js';
import { useQueueStreams } from './queue-streams.js';
import { useQueueFacts } from './queue-facts.js';
import { QueueRows, retryRow } from './queue-body.js';
import { MissingBar } from './queue-missing.js';
import { FailedAdds } from './queue-failed-adds.js';
import { useMoreNearEnd } from './queue-more.js';

export { JobRow, applyFeedToList };

const TYPE_OPTIONS = [
  ['', 'All types'],
  ['album', 'Albums'],
  ['playlist', 'Playlists'],
  ['station', 'Stations'],
];

const HOLD_MS = 400;
const SKELETON_MS = 300;

export function QueuePage({ route }) {
  const app = useApp();
  const filters = queueFilters(route.params);
  const [qInput, setQInput] = useState(filters.q);
  const q = useDebounced(qInput, 300);
  const listKey = `queue|${filters.tab}|${filters.type}|${filters.q}`;
  const key = `${listKey}|${app.epoch}`;
  // All and Active draw the queued and running runs on top in run order;
  // with no search or type the queue can be reordered.
  const grouped = filters.tab === 'all' || filters.tab === 'active';
  const ordered = orderable(filters);
  const slide = useRef(null);
  const upnext = useUpNext(app, true, true, `queue|${app.epoch}`, () => slide.current && slide.current());
  const streams = useQueueStreams(app, filters, listKey, key);
  const facts = useQueueFacts(app);
  const listRef = useRef(null);

  const setParams = (patch) => {
    app.navigate(buildHash('queue', { ...route.params, ...patch }, route.jobId), { replace: true });
  };
  useEffect(() => {
    rememberQueue({ tab: route.params.tab || '', type: filters.type, q: filters.q });
  }, [route.params.tab, filters.type, filters.q]);
  const chooseTab = (tab) => setParams({ tab });

  // The tabs carry the counts: on a cold load they wait unseen for them (or
  // HOLD_MS) instead of widening when the numbers arrive.
  const c = facts.counts;
  const haveCounts = [c.all, c.active, c.done, c.failed].every((n) => n !== undefined);
  const [held, setHeld] = useState(!haveCounts);
  useEffect(() => {
    if (haveCounts) setHeld(false);
    if (!held || haveCounts) return undefined;
    const t = setTimeout(() => setHeld(false), HOLD_MS);
    return () => clearTimeout(t);
  }, [haveCounts]);

  // The search box and the route's q follow each other (a q this page wrote
  // itself comes back through the route and must not reset the box).
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

  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState(() => new Set());
  const [confirm, setConfirm] = useState(null);
  const bulk = useBulkRun(app);

  // The live part: running runs first by start, then the queue in run
  // order (a held row at its slot), filtered as the list is.
  const live = arrange([], [...upnext.running, ...upnext.queue]).filter((j) => liveMatches(j, filters));
  upnext.seeJobs(live);
  const order = useQueueDrag({ app, rows: live, list: listRef, on: ordered && !selecting && !bulk.run, upnext });
  slide.current = order.beforeFeed;
  const liveOrdered = ordered ? inQueueOrder(live, order.ids) : live;
  const liveById = new Map([...upnext.running, ...upnext.queue].map((j) => [j.id, j]));

  const merged = mergeStreams(streams.streams);
  const byKey = new Map(merged.rows.map((r) => [r.id, r.record]));
  const head = grouped ? liveOrdered.map((j) => liveRow(j, byKey)) : [];
  // A record whose run is under way draws that run; one in the live part shows there only.
  const rest =
    filters.tab === 'active'
      ? []
      : withoutLive(merged.rows, grouped ? live : []).map((r) => {
          const run = r.record && r.record.active_job_id && liveById.get(r.record.active_job_id);
          return run ? { ...r, live: run } : r;
        });
  const fresh = [...head, ...rest];
  // Failed's adds head its list: they are known before the list is drawn, so it does not move under them.
  const ready = upnext.ready && (filters.tab === 'active' || (!streams.loading && streams.key === listKey)) && (filters.tab !== 'failed' || facts.addsKnown);
  // Another view's rows stay drawn until this one answers: a quick answer never flashes the skeleton.
  const drawn = useRef([]);
  const rows = ready || fresh.length ? fresh : drawn.current;
  drawn.current = rows;
  useMoreNearEnd(listRef, merged.next, Boolean(streams.more) || !ready, () => streams.readMore(merged.next));
  // What the view holds in all: every stream's total and the live part, a live record counted once.
  const total = Object.values(streams.streams).reduce((n, s) => n + s.total, 0) + head.length - (merged.rows.length - rest.length);

  // A run that finishes leaves Active at once; its toast (Open) says where it went.
  useWatchShown(app, filters.tab === 'active' ? live.map((j) => j.id) : []);
  const rowsRef = useRef(rows);
  rowsRef.current = rows;
  const anchor = useRef(null);
  useEffect(() => {
    setSelected((s) => (s.size === 0 ? s : new Set()));
    anchor.current = null;
  }, [filters.tab]);
  const toggle = (id, range) => {
    const from = anchor.current;
    anchor.current = id;
    setSelected((s) => {
      const on = !s.has(id);
      return range && from ? setIds(s, rangeIds(rowsRef.current, from, id), on) : toggleId(s, id, on);
    });
  };
  const leaveSelection = () => {
    setSelecting(false);
    setSelected(new Set());
    anchor.current = null;
  };
  const chosen = rows.filter((r) => selected.has(r.id));
  const plan = rowPlan(chosen);

  // An action that may take a row out of the list (a menu's, Retry, a bulk
  // run): until a moment after, focus that falls to the body moves to the
  // "…" of the row after it, else to the selection controls.
  const rescueUntil = useRef(0);
  const rescueTo = useRef(null);
  const onLeaving = (id) => {
    const li = document.querySelector(`.page-queue li[data-row-key="${CSS.escape(id)}"]`);
    const next = li && (li.nextElementSibling || li.previousElementSibling);
    rescueTo.current = next ? next.getAttribute('data-row-key') : null;
    rescueUntil.current = Date.now() + 3000;
  };
  useEffect(() => {
    if (Date.now() > rescueUntil.current) return;
    const active = document.activeElement;
    if (active && active !== document.body) return;
    const row = rescueTo.current && document.querySelector(`.page-queue li[data-row-key="${CSS.escape(rescueTo.current)}"] .job-menu-btn`);
    const target = row || RESCUE.map((s) => document.querySelector(s)).find(Boolean);
    if (target) target.focus({ preventScroll: true });
  });
  const runAction = async (action, targets) => {
    rescueUntil.current = Infinity;
    await bulk.start(action, targets);
    setSelected((s) => setIds(s, targets.map((r) => r.id), false));
    setConfirm(null);
    rescueUntil.current = Date.now() + 3000;
  };
  const onAction = (action) => (action === 'again' || action === 'remove' ? setConfirm({ action, rows: null }) : runAction(action, plan[action]));

  // Failed: Retry all resets every failed, given-up and missing track of the
  // records and retries the failed station runs shown (not a link Apple has nothing at).
  const [retrying, setRetrying] = useState(false);
  const stationRetries = rest.filter((r) => r.kind === 'station' && r.job.status === 'failed' && !isNotFound(r.job.error));
  const retryAll = async () => {
    setRetrying(true);
    const [recs, runs] = await Promise.allSettled([
      resetRecords({ what: 'audio', filter: { flag: ['failed', 'given_up', 'missing'] } }),
      Promise.allSettled(stationRetries.map((r) => retryDownload(r.job.id))),
    ]);
    const reset = recs.status === 'fulfilled' ? recs.value.reset || 0 : 0;
    const again = runs.status === 'fulfilled' ? runs.value.filter((o) => o.status === 'fulfilled').length : 0;
    const failed = recs.status === 'rejected' || (runs.status === 'fulfilled' && runs.value.some((o) => o.status === 'rejected'));
    app.toast({ message: `Queued ${count(reset + again)} again${failed ? '; some could not be retried' : ''}`, role: failed ? 'danger' : 'success' });
    if (app.recordsChanged) app.recordsChanged();
    setRetrying(false);
  };

  // Escape leaves selection mode; x toggles the focused row (and enters the
  // mode). Both stand down while typing, under a drawer or any overlay.
  const selectingRef = useRef(selecting);
  selectingRef.current = selecting;
  useEffect(() => {
    const onKey = (e) => {
      if (e.metaKey || e.ctrlKey || e.altKey || route.jobId || route.record || typing(e.target)) return;
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
      const li = document.activeElement && document.activeElement.closest('.page-queue [data-row-key]');
      if (!li) return;
      e.preventDefault();
      setSelecting(true);
      toggle(li.getAttribute('data-row-key'), false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [route.jobId, route.record]);

  // A count keeps its room until it arrives; the failed count is the one
  // number that asks for attention: danger text.
  const num = (n, danger = false) => html`<span class=${danger && n > 0 ? 'seg-num seg-count-danger' : 'seg-num'}>${n === undefined ? ' ' : count(n)}</span>`;
  const tabs = [
    { value: 'all', label: 'All', count: num(c.all) },
    { value: 'active', label: 'Active', count: num(c.active) },
    { value: 'done', label: 'Done', count: num(c.done) },
    { value: 'failed', label: 'Failed', count: num(c.failed, true) },
  ];
  // On a narrow phone the tabs scroll sideways; the open one stays in view.
  useChipRow(`${filters.tab}|${haveCounts}`);

  const showCount = filters.q && ready;
  const showRetryAll = filters.tab === 'failed' && !selecting && rows.length > 0;
  const listBar =
    rows.length > 0 && (showCount || showRetryAll)
      ? html`<div class="queue-list-bar">
          ${showCount ? html`<span class="queue-count" role="status">${count(total)} ${total === 1 ? 'result' : 'results'}</span>` : null}
          ${showRetryAll
            ? html`<${Button} icon="rotate" size="sm" className="queue-retry-all" disabled=${retrying || Boolean(bulk.run)} onClick=${retryAll}>Retry all</${Button}>`
            : null}
        </div>`
      : null;

  // The skeleton waits SKELETON_MS: a quick first answer draws its rows into the empty card.
  const slow = useLate(!ready && rows.length === 0, SKELETON_MS);
  let body;
  if (!ready && rows.length === 0) body = slow ? html`<${SkeletonJobRows} n=${6} />` : null;
  else if (streams.error && rows.length === 0) body = html`<${ErrorNote} title="Couldn't load the list" error=${streams.error} onRetry=${streams.reload} />`;
  else if (rows.length === 0)
    body = html`<${QueueEmpty} filters=${filters} all=${c.all} onClear=${() => { setQInput(''); setParams({ q: '', type: '' }); }} onShowAll=${() => chooseTab('all')} />`;
  else
    body = [
      html`<ul key="list" class=${selecting ? 'job-list is-selecting' : 'job-list'} aria-label="Downloads" ref=${listRef} data-sort-list=${ordered ? 'queue' : undefined}>
        <${QueueRows}
          rows=${rows}
          params=${route.params}
          selecting=${selecting}
          selected=${selected}
          onToggle=${toggle}
          order=${order}
          grouped=${grouped}
          busy=${Boolean(bulk.run)}
          onLeaving=${onLeaving}
          onRetry=${(row) => retryRow(app, row, resetRecord)}
        />
      </ul>`,
      streams.more ? html`<div key="more" class="queue-more" aria-hidden="true"><${SkeletonJobRows} n=${2} /></div>` : null,
    ];

  return html`<div class="page page-queue" data-tab=${filters.tab} data-selecting=${selecting || bulk.run ? '' : undefined} data-held=${held ? '' : undefined}>
    <div class="toolbar">
      <${Segmented} label="Show" options=${tabs} value=${filters.tab} onChange=${chooseTab} />
      <div class="toolbar-filters">
        <label class="search">
          <${Icon} name="search" />
          <input
            type="search"
            data-filter
            placeholder="Filter this list"
            aria-label="Filter by title, artist or link"
            value=${qInput}
            onInput=${(e) => setQInput(e.currentTarget.value)}
          />
        </label>
        <select class="select" aria-label="Type" value=${filters.type} onChange=${(e) => setParams({ type: e.currentTarget.value })}>
          ${TYPE_OPTIONS.map(([v, l]) => html`<option key=${v} value=${v}>${l}</option>`)}
        </select>
      </div>
      <div class="queue-tools">
        <${Button}
          icon="square-check"
          className="queue-select"
          aria-pressed=${String(selecting)}
          aria-keyshortcuts="x"
          title="Select (x selects the focused row, Esc leaves)"
          disabled=${!selecting && rows.length === 0}
          onClick=${() => (selecting ? leaveSelection() : setSelecting(true))}
        >${selecting ? 'Done' : 'Select'}</${Button}>
      </div>
    </div>
    ${filters.tab === 'done' ? html`<${MissingBar} summary=${facts.summary} onChanged=${facts.reload} />` : null}
    ${filters.tab === 'failed' && ready ? html`<${FailedAdds} items=${facts.failedAdds} onChanged=${facts.reload} />` : null}
    ${streams.error && rows.length > 0 ? html`<${ErrorNote} title="Couldn't refresh the list" error=${streams.error} onRetry=${streams.reload} />` : null}
    ${selecting || bulk.run
      ? html`<${SelectionBar}
          rows=${rows}
          selected=${selected}
          count=${chosen.length}
          shown=${chosen.length}
          plan=${plan}
          selecting=${selecting}
          run=${bulk.run}
          onSelectAll=${() => setSelected((s) => setIds(s, rows.map((r) => r.id), selectionState(s, rows) !== 'all'))}
          onAction=${onAction}
        />`
      : null}
    ${filters.tab === 'failed' && !facts.addsKnown ? null : html`<div class="card card-flush" data-section="jobs"><div class="card-body">${listBar}${body}</div></div>`}
    <p class="sr-only" role="status" aria-live="polite">${order.said}</p>
    ${confirm
      ? html`<${BulkConfirm}
          action=${confirm.action}
          rows=${confirm.rows || plan[confirm.action]}
          skipped=${confirm.rows ? confirm.skipped : chosen.length - plan[confirm.action].length}
          run=${bulk.run}
          onConfirm=${() => {
            // The targets freeze here: rows the run removes leave the live plan.
            const targets = plan[confirm.action];
            setConfirm({ action: confirm.action, rows: targets, skipped: chosen.length - targets.length });
            runAction(confirm.action, targets);
          }}
          onCancel=${() => setConfirm(null)}
        />`
      : null}
  </div>`;
}

/** `jobs` with the queued ones in the order of `ids` (a held row at its slot). */
function inQueueOrder(jobs, ids) {
  const byId = new Map(jobs.map((j) => [j.id, j]));
  const queued = ids.map((id) => byId.get(id)).filter(Boolean);
  return [...jobs.filter((j) => j.status === 'running'), ...queued, ...jobs.filter((j) => j.status !== 'running' && j.status !== 'queued')];
}

const RESCUE = ['.bulk-bar .bulk-action:not(:disabled)', '.bulk-check:not(:disabled)', '.queue-retry-all:not(:disabled)', '.queue-select', '.page-title'];

const typing = (el) =>
  Boolean(el) &&
  ((el.tagName === 'INPUT' && !['checkbox', 'radio', 'button', 'submit'].includes(el.type)) ||
    el.tagName === 'TEXTAREA' ||
    el.tagName === 'SELECT' ||
    el.isContentEditable);
