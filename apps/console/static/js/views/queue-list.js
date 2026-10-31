// The Queue's job list: the pages read from GET /downloads for the open
// tab and filters, drawn at once from the last read of the same list, and
// kept live from the downloads feed.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { appendPage } from '../lib/store.js';
import { queueQuery, queueMatchFilter, queueAppends } from '../lib/route.js';
import { cached, remember } from '../lib/cache.js';
import { applyFeedToList } from './queue-row.js';

const PAGE_SIZE = 50;

/**
 * The list of a Queue view: { state: {jobs, total, loading, error, more},
 * load(offset), known }. `listKey` names the view (tab, type, search) for
 * the cache; `key` adds the app's epoch, and a new key reads again.
 * `known` maps every job id the feed or a page showed to its latest
 * version; onDeleted(id) runs for a job the feed deletes.
 */
export function useQueueList(app, filters, listKey, key, onDeleted) {
  // A list seen before in this tab is drawn at once and read again behind it.
  const [state, setState] = useState(() => {
    const last = cached(listKey);
    return last ? { jobs: last.jobs, total: last.total, loading: true, error: null, more: false } : { jobs: [], total: 0, loading: true, error: null, more: false };
  });
  const seq = useRef(0);
  const known = useRef(new Map());
  const deleted = useRef(onDeleted);
  deleted.current = onDeleted;

  const load = (offset) => {
    const mine = ++seq.current;
    const last = offset === 0 ? cached(listKey) : null;
    if (last) setState((s) => ({ ...s, jobs: last.jobs, total: last.total, loading: true, error: null }));
    else if (offset === 0) setState((s) => ({ ...s, loading: true, error: null }));
    else setState((s) => ({ ...s, more: true }));
    listDownloads(queueQuery(filters, offset, PAGE_SIZE)).then(
      (body) => {
        if (mine !== seq.current) return;
        if (offset === 0) remember(listKey, { jobs: body.downloads, total: body.total });
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

  useEffect(() => {
    load(0);
  }, [key]);

  useEffect(() => {
    const filter = queueMatchFilter(filters);
    const opts = { append: queueAppends(filters) };
    // The snapshot's total moves with the rows the feed adds and removes
    // (an estimate: a change to a job past the loaded pages is not seen).
    return app.subscribeFeed((batch) => {
      // Selected jobs the filters hide stay selected, so the registry of jobs
      // follows the whole feed, not only the rows of this list.
      for (const msg of batch) {
        if (msg.type === 'download_deleted') {
          known.current.delete(msg.job_id);
          deleted.current(msg.job_id);
        } else if (msg.type === 'download_upserted' && msg.job) {
          const old = known.current.get(msg.job.id);
          if (!old || Date.parse(msg.job.updated_at) >= Date.parse(old.updated_at)) known.current.set(msg.job.id, msg.job);
        }
      }
      setState((s) => {
        const jobs = applyFeedToList(s.jobs, batch, filter, opts);
        return jobs === s.jobs ? s : { ...s, jobs, total: Math.max(jobs.length, s.total + jobs.length - s.jobs.length) };
      });
    });
  }, [key]);

  return { state, load, known };
}
