// What the Queue's tabs count and its Done and Failed tabs head with: the
// library summary (records per state and flag), the Failed records (one
// read of flag=failed,given_up,missing for its union total), the runs of
// no record (stations, the 1.x history) per status, and the adds that could not be read. Read again on the
// feed's record news, an ended station run, a finished disk check, an
// action (app.recordsStamp) and the page epoch.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { getLibrarySummary, listAlbumRecords, listPlaylistRecords, listDownloads, listPendingAdds } from '../api.js';
import { tabCounts, FAILED_FLAGS } from '../lib/queue-rows.js';

const REFRESH_MS = 1500;

// The last answers, so a page opened again counts at once.
let last = null;

const total = (p) => p.then((b) => (b && typeof b.total === 'number' ? b.total : 0));

/** {summary, counts, failedAdds, addsKnown, reload}: counts as lib/queue-rows.js tabCounts. */
export function useQueueFacts(app) {
  const [facts, setFacts] = useState(() => last || { summary: null, failedRecords: null, stations: null, failedAdds: null });
  const [stamp, setStamp] = useState(0);
  useEffect(() => {
    let live = true;
    Promise.allSettled([
      getLibrarySummary(),
      Promise.all([total(listAlbumRecords({ flag: FAILED_FLAGS, limit: 1 })), total(listPlaylistRecords({ flag: FAILED_FLAGS, limit: 1 }))]),
      listDownloads({ resource_kind: 'none', limit: 1 }),
      listPendingAdds({ state: 'failed' }),
    ]).then(([s, f, st, p]) => {
      if (!live) return;
      setFacts((x) => {
        const next = {
          summary: s.status === 'fulfilled' ? s.value : x.summary,
          failedRecords: f.status === 'fulfilled' ? { albums: f.value[0], playlists: f.value[1] } : x.failedRecords,
          stations: st.status === 'fulfilled' && st.value ? { ...(st.value.status_counts || {}), total: st.value.total || 0 } : x.stations,
          // A failed read leaves the adds unknown as none, so the Failed list still draws.
          failedAdds: p.status === 'fulfilled' && p.value && Array.isArray(p.value.items) ? p.value.items : x.failedAdds || [],
        };
        last = next;
        return next;
      });
    });
    return () => {
      live = false;
    };
  }, [stamp, app.epoch, app.recordsStamp]);
  const timer = useRef(null);
  useEffect(() => {
    const off = app.subscribeFeed((batch) => {
      const news = batch.some(
        (m) =>
          m.type === 'resource_updated' ||
          m.type === 'resource_deleted' ||
          m.type === 'download_deleted' ||
          (m.type === 'check_updated' && m.check && m.check.finished_at) ||
          (m.type === 'download_upserted' && m.job && !m.job.resource && !['running'].includes(m.job.status)),
      );
      // At most one read per REFRESH_MS while news keeps coming.
      if (!news || timer.current) return;
      timer.current = setTimeout(() => {
        timer.current = null;
        setStamp((n) => n + 1);
      }, REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, []);
  const counts = tabCounts({
    summary: facts.summary,
    failedRecords: facts.failedRecords,
    stations: facts.stations,
    pendingFailed: facts.failedAdds ? facts.failedAdds.length : undefined,
    feed: app.counts,
  });
  return { summary: facts.summary, counts, failedAdds: facts.failedAdds || [], addsKnown: facts.failedAdds !== null, reload: () => setStamp((n) => n + 1) };
}
