// The downloads feed shared by every page: connection state, the live
// event stream, retry and backoff, watched jobs with completion toasts,
// and screen-reader announcements.

import { useState, useEffect, useRef, useCallback } from './preact.js';
import { listDownloads, feedStreamUrl, parseData } from '../api.js';
import { applyFeedMessage, EMPTY_COUNTS } from '../lib/store.js';
import { StreamClient, createCoalescer, backoffDelay } from '../lib/stream.js';
import { jobTitle, typeLabel } from '../lib/format.js';
import { isTerminal } from '../lib/status.js';
import { BURST_MS, endsWatch, finishedToast } from '../lib/watch.js';

const ANNOUNCE_GAP_MS = 5000;

/**
 * Downloads feed, connection state and live announcements for App():
 * {feed, counts, conn, epoch, retryRef, retryFeed, subscribeFeed, watchJobs, announce, liveText}.
 */
export function useFeed({ toast, route }) {
  const [feed, setFeed] = useState({ counts: null, cursor: null });
  const [conn, setConn] = useState({ state: 'connecting', retryAt: null, downSince: null });
  const [epoch, setEpoch] = useState(0);
  const [liveText, setLiveText] = useState('');
  const listeners = useRef(new Set());
  const retryRef = useRef(() => {});
  const lastStatus = useRef(new Map());
  const announceQueue = useRef([]);
  const announceTimer = useRef(null);
  const lastAnnounce = useRef(0);

  // Jobs a page asked to watch (app.watchJobs, after New download submits
  // them): when they end, one visible toast per burst (lib/watch.js).
  const watched = useRef(new Set());
  const recentEnded = useRef(new Map());
  const endedBurst = useRef([]);
  const burstTimer = useRef(null);
  const routeRef = useRef(route);
  routeRef.current = route;
  const flushEnded = useCallback(() => {
    burstTimer.current = null;
    const jobs = endedBurst.current;
    endedBurst.current = [];
    if (jobs.length) toast(finishedToast(jobs, routeRef.current));
  }, [toast]);
  const noteEnded = useCallback(
    (job) => {
      watched.current.delete(job.id);
      endedBurst.current.push(job);
      if (burstTimer.current === null) burstTimer.current = setTimeout(flushEnded, BURST_MS);
    },
    [flushEnded],
  );
  const watchJobs = useCallback(
    (ids) => {
      for (const id of ids || []) {
        if (!id) continue;
        // The feed can report the end before the submit answer arrives.
        const done = recentEnded.current.get(id);
        if (done) noteEnded(done);
        else watched.current.add(id);
      }
      if (watched.current.size > 500) watched.current = new Set([...watched.current].slice(-500));
    },
    [noteEnded],
  );
  const noteEndedRef = useRef(noteEnded);
  noteEndedRef.current = noteEnded;

  const flushAnnouncements = useCallback(() => {
    announceTimer.current = null;
    const items = announceQueue.current;
    announceQueue.current = [];
    if (items.length === 0) return;
    lastAnnounce.current = Date.now();
    setLiveText(items.length > 3 ? `${items.slice(0, 3).join('. ')}. And ${items.length - 3} more.` : `${items.join('. ')}.`);
  }, []);
  const announce = useCallback(
    (text) => {
      announceQueue.current.push(text);
      if (announceTimer.current !== null) return;
      const wait = Math.max(0, lastAnnounce.current + ANNOUNCE_GAP_MS - Date.now());
      announceTimer.current = setTimeout(flushAnnouncements, wait);
    },
    [flushAnnouncements],
  );

  const subscribeFeed = useCallback((fn) => {
    listeners.current.add(fn);
    return () => listeners.current.delete(fn);
  }, []);

  // The downloads feed: one snapshot for counts and the cursor, then the
  // stream from that cursor. Every page subscribes to its batches.
  useEffect(() => {
    let stopped = false;
    let client = null;
    let bootTimer = null;
    let attempt = 0;
    let downSince = null;
    const noteTransitions = (batch) => {
      for (const msg of batch) {
        const job = msg.job;
        if (!job) continue;
        const prev = lastStatus.current.get(job.id);
        lastStatus.current.set(job.id, job.status);
        if (isTerminal(job.status)) {
          recentEnded.current.set(job.id, job);
          if (recentEnded.current.size > 50) recentEnded.current.delete(recentEnded.current.keys().next().value);
        } else recentEnded.current.delete(job.id);
        if (watched.current.has(job.id) && endsWatch(job)) {
          // Its toast is announced through the toasts' own live region.
          noteEndedRef.current(job);
        } else if (prev && prev !== job.status && (job.status === 'completed' || job.status === 'failed')) {
          announce(`${typeLabel(job.type)} ${jobTitle(job)} ${job.status}`);
        }
      }
      if (lastStatus.current.size > 1000) {
        const drop = lastStatus.current.size - 1000;
        let i = 0;
        for (const k of lastStatus.current.keys()) {
          if (i++ >= drop) break;
          lastStatus.current.delete(k);
        }
      }
    };
    const coalescer = createCoalescer((batch) => {
      setFeed((f) => batch.reduce(applyFeedMessage, f));
      for (const fn of listeners.current) fn(batch);
      noteTransitions(batch);
    }, 100);
    const refreshCounts = () =>
      listDownloads({ limit: 1 }).then(
        (body) => setFeed((f) => ({ ...f, counts: { ...EMPTY_COUNTS, ...body.status_counts } })),
        () => {},
      );
    const boot = async () => {
      bootTimer = null;
      try {
        const body = await listDownloads({ limit: 1 });
        if (stopped) return;
        for (const j of body.downloads || []) lastStatus.current.set(j.id, j.status);
        setFeed({ counts: { ...EMPTY_COUNTS, ...body.status_counts }, cursor: body.event_cursor ?? null });
        if (downSince !== null) setEpoch((e) => e + 1);
        client = new StreamClient({
          url: feedStreamUrl(),
          cursor: body.event_cursor ?? null,
          onEvent: (ev) => {
            if (ev.type !== 'download_upserted' && ev.type !== 'download_deleted') return;
            const msg = parseData(ev);
            if (msg) coalescer.push(msg);
          },
          onState: (s) => setConn(s),
          // Live again after an outage: the feed replays what it missed from
          // the cursor, and a new epoch tells every page to refetch what it
          // could not load meanwhile (error panels, defaults, previews).
          onReconnect: () => {
            refreshCounts();
            setEpoch((e) => e + 1);
          },
          onEnd: ({ status }) => setConn({ state: 'offline', retryAt: null, downSince: Date.now(), endStatus: status }),
        });
        retryRef.current = () => client.retryNow();
        client.start();
      } catch {
        if (stopped) return;
        if (downSince === null) downSince = Date.now();
        const delay = backoffDelay(attempt++);
        const state = Date.now() - downSince >= 60000 ? 'offline' : 'reconnecting';
        setConn({ state, retryAt: Date.now() + delay, downSince });
        bootTimer = setTimeout(boot, delay);
        retryRef.current = () => {
          if (bootTimer !== null) {
            clearTimeout(bootTimer);
            boot();
          }
        };
      }
    };
    // The device came back online: retry now instead of at the end of the
    // backoff, and let pages refetch what failed while requests could not
    // leave (the open stream itself may never have noticed).
    const onOnline = () => {
      retryRef.current();
      setEpoch((e) => e + 1);
    };
    window.addEventListener('online', onOnline);
    boot();
    return () => {
      window.removeEventListener('online', onOnline);
      if (burstTimer.current !== null) clearTimeout(burstTimer.current);
      stopped = true;
      if (bootTimer !== null) clearTimeout(bootTimer);
      coalescer.cancel();
      if (client) client.stop();
    };
  }, []);

  return { feed, counts: feed.counts, conn, epoch, retryRef, retryFeed: () => retryRef.current(), subscribeFeed, watchJobs, announce, liveText };
}
