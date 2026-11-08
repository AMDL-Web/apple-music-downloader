// The Queue's run order: the running and queued jobs read on their own (so
// all of them show on top, past the pages loaded), kept live from the feed,
// and the queue reordered by dragging a queued row (ui/sortable.js; a grip
// over the cover for a mouse, a long-press on touch), by Alt+Up, Alt+Down,
// Alt+Home and Alt+End on a focused one, or by its menu's Move to top and
// Move to bottom. A move is
// drawn at once and sent as POST /downloads/{id}/move; a refusal puts the
// row back and says why, and a 409 reads the queue again.

import { html, useState, useEffect, useRef, useLayoutEffect } from '../ui/preact.js';
import { listDownloads, moveDownload } from '../api.js';
import { upsertJob, removeJob } from '../lib/store.js';
import { upNext, placeQueued, moveBody, keyMove, queuePlace } from '../lib/queue-order.js';
import { actionError } from '../lib/jobactions.js';
import { jobTitle } from '../lib/format.js';
import { useSortable } from '../ui/sortable.js';
import { Icon } from '../ui/components.js';

// The most queued or running jobs read at once (the API's page limit).
const LIMIT = 200;
const QUEUED = { statuses: ['queued'] };
const RUNNING = { statuses: ['running'] };

// The last lists read, so a page opened again draws them at once while it reads again.
let lastLists = { queue: [], running: [], ready: false };

const applyBatch = (list, batch, filter) => {
  let next = list;
  for (const m of batch) {
    if (m.type === 'download_deleted') next = removeJob(next, m.job_id);
    else if (m.type === 'download_upserted' && m.job) next = upsertJob(next, m.job, filter, { append: true });
  }
  return next;
};

/**
 * The jobs a grouped view draws on top whatever page they are on: the queued
 * ones (GET /downloads?status=queued&sort=queue_order) and the running ones,
 * read while `read` and kept live from the feed: { queue, running, ready,
 * setQueue, reload } (`ready` once a read answered, or the last one is drawn). While `grouped`, beforeChange() runs before every feed batch
 * that changes a job's status or place is drawn, so the rows slide there
 * (a start, a finish, a move, the burst of a renumbering) instead of jumping.
 */
export function useUpNext(app, read, grouped, key, beforeChange) {
  const [lists, setLists] = useState(() => lastLists);
  const seq = useRef(0);
  const before = useRef(beforeChange);
  before.current = beforeChange;
  const reload = () => {
    const mine = ++seq.current;
    if (!read) {
      setLists({ queue: [], running: [], ready: true });
      return;
    }
    Promise.all([
      listDownloads({ status: 'queued', sort: 'queue_order', order: 'asc', limit: LIMIT }),
      listDownloads({ status: 'running', sort: 'created_at', order: 'asc', limit: LIMIT }),
    ]).then(
      ([queued, running]) => {
        if (mine !== seq.current) return;
        lastLists = { queue: queued.downloads, running: running.downloads, ready: true };
        setLists(lastLists);
      },
      () => {
        // Nothing to draw on top: the list shows what its own reads hold.
        if (mine === seq.current) setLists((l) => (l.ready ? l : { ...l, ready: true }));
      },
    );
  };
  useEffect(reload, [read, key]);
  const held = useRef(lists);
  held.current = lists;
  const statuses = useRef(new Map());
  useEffect(() => {
    if (!grouped) return undefined;
    return app.subscribeFeed((batch) => {
      const seen = statuses.current;
      const order = new Map(held.current.queue.map((j) => [j.id, j.queue_order]));
      const moves = batch.some(
        (m) =>
          m.type === 'download_deleted' ||
          (m.job && ((seen.has(m.job.id) && seen.get(m.job.id) !== m.job.status) || (m.job.status === 'queued' && order.has(m.job.id) && order.get(m.job.id) !== m.job.queue_order) || !seen.has(m.job.id))),
      );
      for (const m of batch) if (m.job) seen.set(m.job.id, m.job.status);
      if (moves && before.current) before.current();
      if (!read) return;
      setLists((l) => {
        const queue = applyBatch(l.queue, batch, QUEUED);
        const running = applyBatch(l.running, batch, RUNNING);
        if (queue === l.queue && running === l.running) return l;
        lastLists = { ...l, queue, running };
        return lastLists;
      });
    });
  }, [read, grouped, key]);
  // Statuses already drawn, so a first sighting in the feed is not a change.
  const seeJobs = (jobs) => {
    for (const j of jobs) if (!statuses.current.has(j.id)) statuses.current.set(j.id, j.status);
  };
  const setQueue = (fn) => setLists((l) => ({ ...l, queue: fn(l.queue) }));
  return { queue: lists.queue, running: lists.running, ready: Boolean(lists.ready), setQueue, reload, seeJobs };
}

/**
 * Reordering the queue on the page: `rows` are the jobs as drawn (running,
 * queued in run order, the rest), `list` a ref to the <ul>, `on` whether the
 * view is in run order and not selecting. Returns { sort, ids, draggable,
 * keyDown(e, job), moveTo(job, 'top'|'bottom'), place(id), said }: `ids`
 * the queued ids in the order to draw (a held row at its slot).
 */
export function useQueueDrag({ app, rows, list, on, upnext }) {
  const [said, setSaid] = useState('');
  const refocus = useRef(null);
  const ids = upNext(rows).map((j) => j.id);
  const draggable = on && ids.length > 1;
  const jobOf = (id) => rows.find((j) => j.id === id);

  const send = (id, index, body) => {
    const job = jobOf(id);
    const old = upnext.queue.find((j) => j.id === id);
    upnext.setQueue((q) => placeQueued(q, id, index));
    const total = ids.length;
    setSaid(`${jobTitle(job)} moved to position ${index + 1} of ${total} in the queue.`);
    moveDownload(id, body).then(
      (res) => {
        if (res && res.job) upnext.setQueue((q) => upsertJob(q, res.job, QUEUED, { append: true }));
      },
      (error) => {
        sort.animate();
        if (old) upnext.setQueue((q) => q.map((j) => (j.id === id ? { ...j, queue_order: old.queue_order } : j)));
        app.toast({ message: actionError('move', error), role: 'danger' });
        if (error && error.status === 409) upnext.reload();
      },
    );
  };

  const sort = useSortable({
    container: list,
    lists: { queue: ids },
    canDrag: () => draggable,
    clamp: 'rows',
    handle: '.job-grip',
    ghostScope: '.page-queue',
    autoScroll: () => {
      // The sticky top bar and, on a phone, the fixed tab bar cover the page's edges.
      const edge = (sel) => {
        const el = document.querySelector(sel);
        const r = el && el.getBoundingClientRect();
        return r && r.height > 0 ? r : null;
      };
      const bar = edge('.topbar');
      const tabs = edge('.tabbar');
      return { top: bar ? Math.max(0, bar.bottom) : 0, bottom: tabs ? Math.max(0, window.innerHeight - tabs.top) : 0 };
    },
    rowOf: (id) => list.current && list.current.querySelector(`li[data-job-id="${CSS.escape(id)}"]`),
    onDrop: ({ id, to, lists, focused }) => {
      if (focused) refocus.current = id;
      send(id, to.index, moveBody(lists.queue, id));
    },
  });

  useLayoutEffect(() => {
    const id = refocus.current;
    refocus.current = null;
    if (!id || !list.current) return;
    const link = list.current.querySelector(`li[data-job-id="${CSS.escape(id)}"] .job-row`);
    if (link && document.activeElement !== link) link.focus({ preventScroll: true });
  });

  const keyDown = (e, job) => {
    if (!draggable || !e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || !['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    e.stopPropagation();
    if (sort.drag) return;
    const step = keyMove(ids, job.id, e.key);
    if (!step) return;
    sort.animate();
    refocus.current = job.id;
    send(job.id, step.index, step.body);
  };

  const moveTo = (job, where) => {
    sort.animate();
    send(job.id, where === 'top' ? 0 : ids.length, { to: where === 'top' ? 'first' : 'last' });
  };

  return { sort, ids: sort.lists.queue, draggable, keyDown, moveTo, place: (id) => (on ? queuePlace(ids, id) : {}), said, beforeFeed: () => sort.animate() };
}

/** The grip over a queued row's cover: what a mouse drags (decoration; the row has the keys). */
export function JobGrip() {
  return html`<span class="job-grip" aria-hidden="true"><${Icon} name="grip" /></span>`;
}
