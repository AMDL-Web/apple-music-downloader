// The Queue's run order: the queued jobs read on their own in run order
// (so the whole queue shows, past the pages loaded), kept live from the
// feed, and reordered by dragging a queued row (ui/sortable.js; a grip over
// the cover for a mouse, a long-press on touch), by Alt+Up and Alt+Down on a
// focused one, or by its menu's Move to top and Move to bottom. A move is
// drawn at once and sent as POST /downloads/{id}/move; a refusal puts the
// row back and says why, and a 409 reads the queue again.

import { html, useState, useEffect, useRef, useLayoutEffect } from '../ui/preact.js';
import { listDownloads, moveDownload } from '../api.js';
import { upsertJob, removeJob } from '../lib/store.js';
import { upNext, placeQueued, moveBody, stepIndex, queuePlace } from '../lib/queue-order.js';
import { actionError } from '../lib/jobactions.js';
import { jobTitle } from '../lib/format.js';
import { useSortable } from '../ui/sortable.js';
import { Icon } from '../ui/components.js';

// The most queued jobs read at once (the API's page limit).
const QUEUE_LIMIT = 200;
const QUEUED = { statuses: ['queued'] };

/**
 * The queued jobs, read with GET /downloads?status=queued&sort=queue_order
 * while `on`, and kept live from the feed: { queue, setQueue, reload }.
 * beforeChange() runs before a feed batch that changes the queue is drawn,
 * so the rows can slide to their new places.
 */
export function useUpNext(app, on, key, beforeChange) {
  const [queue, setQueue] = useState([]);
  const seq = useRef(0);
  const before = useRef(beforeChange);
  before.current = beforeChange;
  const reload = () => {
    const mine = ++seq.current;
    if (!on) {
      setQueue([]);
      return;
    }
    listDownloads({ status: 'queued', sort: 'queue_order', order: 'asc', limit: QUEUE_LIMIT }).then(
      (body) => {
        if (mine === seq.current) setQueue(body.downloads);
      },
      () => {
        // The page's own list still shows the queued jobs it read.
      },
    );
  };
  useEffect(reload, [on, key]);
  const queueRef = useRef(queue);
  queueRef.current = queue;
  const statuses = useRef(new Map());
  for (const j of queue) statuses.current.set(j.id, j.status);
  useEffect(() => {
    if (!on) return undefined;
    return app.subscribeFeed((batch) => {
      // A batch that changes a job's status or place slides the rows there.
      const seen = statuses.current;
      const moves = batch.some(
        (m) =>
          m.type === 'download_deleted' ||
          (m.job && (seen.get(m.job.id) !== m.job.status || (m.job.status === 'queued' && queueRef.current.some((j) => j.id === m.job.id && j.queue_order !== m.job.queue_order)))),
      );
      for (const m of batch) if (m.job) seen.set(m.job.id, m.job.status);
      if (moves && before.current) before.current();
      const held = new Set(queueRef.current.map((j) => j.id));
      const touches = batch.some((m) => (m.job && (held.has(m.job.id) || m.job.status === 'queued')) || (m.type === 'download_deleted' && held.has(m.job_id)));
      if (!touches) return;
      setQueue((q) => {
        let next = q;
        for (const m of batch) {
          if (m.type === 'download_deleted') next = removeJob(next, m.job_id);
          else if (m.type === 'download_upserted' && m.job) next = upsertJob(next, m.job, QUEUED, { append: true });
        }
        return next;
      });
    });
  }, [on, key]);
  return { queue, setQueue, reload };
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
    if (!draggable || !e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
    e.preventDefault();
    e.stopPropagation();
    if (sort.drag) return;
    const to = stepIndex(ids, job.id, e.key === 'ArrowUp' ? -1 : 1);
    if (to == null) return;
    const after = ids.filter((x) => x !== job.id);
    after.splice(to, 0, job.id);
    sort.animate();
    refocus.current = job.id;
    send(job.id, to, moveBody(after, job.id));
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
