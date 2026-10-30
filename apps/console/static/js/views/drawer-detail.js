// Job drawer: the snapshot and per-job event stream behind the drawer,
// replaying persisted history into Activity and tracking progress live,
// with feed-driven reopens after the stream ends.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { getDownload, jobStreamUrl, parseData } from '../api.js';
import { isTerminal } from '../lib/status.js';
import { detailFromSnapshot, applyJobEvents, mergeSnapshot } from '../lib/store.js';
import { StreamClient, claimSecondary, releaseSecondary, createCoalescer } from '../lib/stream.js';

/**
 * Snapshot, SSE stream and feed resync for JobDrawer:
 * {detail, error, streamEnded, refetch, reopen, detailRef, jobIdRef}.
 */
export function useDrawerDetail({ jobId, app }) {
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState(null);
  const [streamKey, setStreamKey] = useState(0);
  const [streamEnded, setStreamEnded] = useState(false);
  const detailRef = useRef(null);
  detailRef.current = detail;
  const jobIdRef = useRef(jobId);
  jobIdRef.current = jobId;
  const errorRef = useRef(error);
  errorRef.current = error;
  const streamEndedRef = useRef(streamEnded);
  streamEndedRef.current = streamEnded;

  const refetch = () =>
    getDownload(jobId).then(
      (body) => {
        if (jobIdRef.current !== jobId) return null;
        setError(null);
        setDetail((d) => (d ? mergeSnapshot(d, body) : detailFromSnapshot(body)));
        return body;
      },
      (err) => {
        if (jobIdRef.current === jobId) setError(err);
        return null;
      },
    );

  useEffect(() => {
    setDetail(null);
    setError(null);
    setStreamEnded(false);
    refetch();
  }, [jobId]);

  // Start the per-job stream once the snapshot (and its cursor) is known.
  const loaded = Boolean(detail && detail.job && detail.job.id === jobId);
  useEffect(() => {
    if (!loaded) return undefined;
    setStreamEnded(false);
    const coalescer = createCoalescer((batch) => setDetail((d) => (d ? applyJobEvents(d, batch) : d)), 100);
    // From the start, not from the snapshot's cursor: the events at or before
    // it only fill Activity (lib/store.js applyJobEvents), which is how a job
    // opened after it finished still shows what happened.
    const client = new StreamClient({
      url: jobStreamUrl(jobId),
      cursor: null,
      onEvent: (ev) => {
        const data = parseData(ev);
        if (data) coalescer.push(data);
      },
      onEnd: ({ status }) => {
        coalescer.flushNow();
        setStreamEnded(true);
        if (status === 404) setError({ status: 404, message: 'This job was deleted.' });
        else refetch();
      },
      onClose: () => {
        coalescer.flushNow();
        refetch();
      },
    });
    claimSecondary(client);
    client.start();
    return () => {
      coalescer.cancel();
      client.stop();
      releaseSecondary(client);
    };
  }, [loaded, jobId, streamKey]);

  // A finished job's stream ends (204), so later changes made elsewhere reach
  // the drawer only through the overview feed: a delete, or a retry that starts
  // a new run, which reopens the per-job stream from the snapshot's cursor.
  // One reopen at a time: the feed can deliver several upserts (and the
  // drawer's own Retry its own) before the reopened stream renders.
  const reopening = useRef(false);
  const reopen = (always) => {
    if (reopening.current) return;
    reopening.current = true;
    refetch().then((body) => {
      reopening.current = false;
      if (always || (body && body.job && !isTerminal(body.job.status))) setStreamKey((k) => k + 1);
    });
  };
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        for (const msg of batch) {
          if (msg.type === 'download_deleted' && msg.job_id === jobIdRef.current) {
            setError({ status: 404, message: 'This job was deleted.' });
            return;
          }
          if (msg.type === 'download_upserted' && msg.job && msg.job.id === jobIdRef.current && streamEndedRef.current) {
            reopen(false);
            return;
          }
        }
      }),
    [jobId],
  );
  // A feed resync (new epoch) may have skipped messages; catch up the same way.
  useEffect(() => {
    if (!app.epoch) return;
    if (streamEndedRef.current) reopen(false);
    // A first load that failed (offline, restarting) retries once the feed is back.
    else if (!detailRef.current && errorRef.current && errorRef.current.status !== 404) refetch();
  }, [app.epoch]);

  // Events that change the item set (resolved_input, retry, recovery) or
  // that the stream does not carry (motion artwork) need a fresh snapshot.
  useEffect(() => {
    if (!detail || !detail.needsRefetch) return undefined;
    const t = setTimeout(() => {
      setDetail((d) => (d ? { ...d, needsRefetch: false } : d));
      refetch();
    }, 300);
    return () => clearTimeout(t);
  }, [detail && detail.needsRefetch]);

  return { detail, error, streamEnded, refetch, reopen, detailRef, jobIdRef };
}
