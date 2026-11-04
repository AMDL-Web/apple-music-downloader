// The Queue's streams: the record lists (GET /library/albums and
// /library/playlists, sort=activity) and the station runs (GET
// /downloads?type=station) a tab reads, each paged on its own and merged by
// lib/queue-rows.js. A list seen before is drawn at once and read again
// behind it; a read again (the feed's news, an action, the page epoch) reads
// each stream down to what it already holds, so the pages read stay; an
// answer that an even newer read overtook is dropped. The feed patches a
// record or station run in place at once.

import { useState, useEffect, useRef } from '../ui/preact.js';
import { listAlbumRecords, listPlaylistRecords, listDownloads } from '../api.js';
import { cached, remember } from '../lib/cache.js';
import { tabStreams, streamQuery, appendItems, patchStream, dropFromStream, recordOnTab, stationOnTab, STREAM_PAGE } from '../lib/queue-rows.js';

const REFRESH_MS = 1500;

/** One page of a stream as {items, total}. */
function readPage(name, filters, offset, limit) {
  const q = streamQuery(name, filters, offset, limit);
  if (name === 'stations') return listDownloads(q).then((b) => ({ items: (b && b.downloads) || [], total: (b && b.total) || 0 }));
  const call = name === 'playlists' ? listPlaylistRecords : listAlbumRecords;
  return call(q).then((b) => ({ items: (b && b.items) || [], total: (b && b.total) || 0 }));
}

/** The first `want` items of a stream, a page of STREAM_PAGE at a time, all of them when it holds fewer. */
async function readTo(name, filters, want) {
  let all = { items: [], total: 0 };
  for (;;) {
    const page = await readPage(name, filters, all.items.length, STREAM_PAGE);
    const items = appendItems(all.items, page.items);
    const short = items.length === all.items.length;
    all = { items, total: page.total };
    if (short || items.length >= all.total || items.length >= want) return all;
  }
}

/**
 * The streams of a Queue view: { streams, loading, error, more, reload,
 * readMore(name) }. `streams` maps each stream the view reads to {items,
 * total}; `listKey` names the view (tab, type, search) for the cache and
 * `key` adds the page epoch, and a new key reads again.
 */
export function useQueueStreams(app, filters, listKey, key) {
  const names = tabStreams(filters);
  const empty = () => Object.fromEntries(names.map((n) => [n, { items: [], total: 0 }]));
  const [state, setState] = useState(() => {
    const last = cached(listKey);
    return { key: listKey, streams: last || empty(), loading: names.length > 0, error: null, more: '' };
  });
  const gen = useRef(0);
  // The streams drawn and the view they belong to: another view's stay drawn until this one answers.
  const held = useRef(state);
  held.current = state;

  const reload = () => {
    const mine = ++gen.current;
    if (!names.length) {
      setState({ key: listKey, streams: {}, loading: false, error: null, more: '' });
      return;
    }
    const now = held.current.key === listKey ? held.current.streams : {};
    Promise.all(names.map((n) => readTo(n, filters, Math.max(STREAM_PAGE, (now[n] && now[n].items.length) || 0)))).then(
      (pages) => {
        if (mine !== gen.current) return;
        const streams = Object.fromEntries(names.map((n, i) => [n, pages[i]]));
        remember(listKey, streams);
        setState({ key: listKey, streams, loading: false, error: null, more: '' });
      },
      (error) => {
        // The view answered, with an error: its note (and Retry) replaces another view's rows.
        if (mine === gen.current) setState((s) => (s.key === listKey ? { ...s, loading: false, error, more: '' } : { key: listKey, streams: empty(), loading: false, error, more: '' }));
      },
    );
  };

  const readMore = (name) => {
    const s = held.current.key === listKey ? held.current.streams[name] : null;
    if (!s || state.more || s.items.length >= s.total) return;
    const offset = s.items.length;
    const mine = gen.current;
    setState((x) => ({ ...x, more: name }));
    readPage(name, filters, offset, STREAM_PAGE).then(
      (page) => {
        if (mine !== gen.current) return;
        setState((x) => {
          const cur = x.key === listKey ? x.streams[name] : null;
          // A read again since then holds this page already.
          if (!cur || cur.items.length !== offset) return { ...x, more: '' };
          const items = appendItems(cur.items, page.items);
          const streams = { ...x.streams, [name]: { items, total: items.length === cur.items.length ? items.length : page.total } };
          remember(listKey, streams);
          return { ...x, streams, more: '' };
        });
      },
      () => {
        if (mine === gen.current) setState((x) => ({ ...x, more: '' }));
      },
    );
  };

  // A new view: the last answer of the same view, else the rows drawn stay, while it reads.
  useEffect(() => {
    const last = cached(listKey);
    if (last) held.current = { key: listKey, streams: last, loading: true, error: null, more: '' };
    else held.current = { ...held.current, loading: names.length > 0, error: null, more: '' };
    setState(held.current);
    reload();
  }, [key]);
  useEffect(() => {
    if (app.recordsStamp) reload();
  }, [app.recordsStamp]);

  const timer = useRef(null);
  useEffect(() => {
    const tab = filters.tab;
    const reads = new Set(names);
    const off = app.subscribeFeed((batch) => {
      // Record news, a station run that ended and a finished disk check read the lists again.
      const news = batch.some(
        (m) =>
          m.type === 'resource_updated' ||
          m.type === 'resource_deleted' ||
          (m.type === 'check_updated' && m.check && m.check.finished_at) ||
          (m.type === 'download_upserted' && m.job && !m.job.resource && !['queued', 'running'].includes(m.job.status)),
      );
      setState((s) => {
        let streams = s.streams;
        for (const m of batch) {
          if (m.type === 'resource_updated' && m.resource) {
            const name = m.resource_kind === 'playlist' ? 'playlists' : 'albums';
            if (!reads.has(name) || filters.q) continue;
            const next = patchStream(name, streams[name], m.resource, recordOnTab(m.resource, tab));
            if (next !== streams[name]) streams = { ...streams, [name]: next };
          } else if (m.type === 'download_upserted' && m.job && !m.job.resource && m.job.type === 'station') {
            if (!reads.has('stations') || filters.q) continue;
            const next = patchStream('stations', streams.stations, m.job, stationOnTab(m.job, tab));
            if (next !== streams.stations) streams = { ...streams, stations: next };
          } else if (m.type === 'resource_deleted') {
            const name = m.resource_kind === 'playlist' ? 'playlists' : 'albums';
            if (!reads.has(name)) continue;
            const next = dropFromStream(streams[name], m.resource_id);
            if (next !== streams[name]) streams = { ...streams, [name]: next };
          } else if (m.type === 'download_deleted' && reads.has('stations')) {
            const next = dropFromStream(streams.stations, m.job_id);
            if (next !== streams.stations) streams = { ...streams, stations: next };
          }
        }
        return streams === s.streams || s.key !== listKey ? s : { ...s, streams };
      });
      // At most one read per REFRESH_MS while news keeps coming.
      if (!news || timer.current) return;
      timer.current = setTimeout(() => {
        timer.current = null;
        reload();
      }, REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
      timer.current = null;
    };
  }, [key]);

  return { ...state, reload, readMore };
}
