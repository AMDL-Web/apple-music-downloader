// Library › Apple Music: the cached account library (GET
// /apple-library/{kind}): albums, songs, playlists and artists, a search,
// Sync now with the last sync, Load more. An album or catalog playlist amdl
// has a record of is marked and opens it; any other item with a catalog
// link has Add (POST /downloads). A playlist without a catalog link
// (Favourite Songs, an unshared own playlist) opens its tracks, each added
// on its own. An artist opens its albums in New download.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { defaultAppleAccount } from '../lib/accounts.js';
import { getAppleLibraryStatus, syncAppleLibrary, listAppleLibrary, listAppleLibraryTracks, listAlbumRecords, listPlaylistRecords, createDownload } from '../api.js';
import { libraryHash, recordHash, buildHash, settingsHash } from '../lib/route.js';
import { count, trackLength } from '../lib/format.js';
import { resultView } from '../lib/submit.js';
import { useApp, Icon, Segmented, ErrorNote, SkeletonJobRows, Button, Empty, Notice, Artwork, RelTime } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';

const PAGE = 50;
const KINDS = [
  { value: 'albums', label: 'Albums' },
  { value: 'songs', label: 'Songs' },
  { value: 'playlists', label: 'Playlists' },
  { value: 'artists', label: 'Artists' },
];
const MARK_PAGES = 10;
const REFRESH_MS = 1500;
const SYNC_POLL_MS = 2000;

/** The ids of every album and playlist record, to mark the items amdl has. */
function useRecordIds(app) {
  const [ids, setIds] = useState(() => new Set());
  const [stamp, setStamp] = useState(0);
  useEffect(() => {
    let live = true;
    const all = async (list) => {
      const out = [];
      for (let page = 0; page < MARK_PAGES; page++) {
        const r = await list({ limit: 500, offset: page * 500 });
        out.push(...r.items.map((x) => x.id));
        if (out.length >= r.total) break;
      }
      return out;
    };
    Promise.allSettled([all(listAlbumRecords), all(listPlaylistRecords)]).then(([a, p]) => {
      if (!live) return;
      setIds(new Set([...(a.status === 'fulfilled' ? a.value : []), ...(p.status === 'fulfilled' ? p.value : [])]));
    });
    return () => {
      live = false;
    };
  }, [stamp, app.epoch, app.recordsStamp]);
  const timer = useRef(null);
  useEffect(() => {
    const off = app.subscribeFeed((batch) => {
      if (!batch.some((m) => m.type === 'resource_updated')) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setStamp((n) => n + 1), REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, []);
  return [ids, () => setStamp((n) => n + 1)];
}

/** The default account's sync status, polled while a sync runs. */
function useAppleStatus(app) {
  const [status, setStatus] = useState({ data: null, error: null });
  const [stamp, setStamp] = useState(0);
  useEffect(() => {
    let live = true;
    getAppleLibraryStatus().then(
      (data) => live && setStatus({ data: defaultAppleAccount(data), error: null }),
      (error) => live && setStatus((s) => ({ data: s.data, error })),
    );
    return () => {
      live = false;
    };
  }, [stamp, app.epoch]);
  const running = Boolean(status.data && status.data.state === 'running');
  useEffect(() => {
    if (!running) return undefined;
    const t = setTimeout(() => setStamp((n) => n + 1), SYNC_POLL_MS);
    return () => clearTimeout(t);
  }, [running, stamp]);
  return [status, () => setStamp((n) => n + 1), running];
}

export function AppleLibrary({ filters }) {
  const app = useApp();
  const kind = filters.kind;
  const [qInput, setQInput] = useState(filters.q);
  const q = useDebounced(qInput, 300);
  useEffect(() => {
    if (q !== filters.q) app.navigate(libraryHash({ ...filters, q }), { replace: true });
  }, [q]);
  const [status, reloadStatus, syncing] = useAppleStatus(app);
  const [ids, reloadIds] = useRecordIds(app);
  const [page, setPage] = useState({ items: [], total: 0, loading: true, error: null, more: false, syncedAt: '' });
  const key = `${kind}|${filters.q}`;
  const load = (offset) => {
    setPage((s) => ({ ...s, loading: offset === 0 && s.items.length === 0, more: offset > 0 }));
    return listAppleLibrary(kind, { limit: PAGE, offset, ...(filters.q ? { q: filters.q } : {}) }).then(
      (r) => setPage((s) => ({ items: offset ? [...s.items, ...r.items] : r.items, total: r.total, loading: false, error: null, more: false, syncedAt: r.synced_at || '' })),
      (error) => setPage((s) => ({ ...s, loading: false, error, more: false })),
    );
  };
  useEffect(() => setPage({ items: [], total: 0, loading: true, error: null, more: false, syncedAt: '' }), [key]);
  const wasSyncing = useRef(syncing);
  useEffect(() => {
    load(0);
  }, [key, app.epoch]);
  useEffect(() => {
    if (wasSyncing.current && !syncing) load(0);
    wasSyncing.current = syncing;
  }, [syncing]);

  const [sent, setSent] = useState(() => new Map());
  const add = async (url, label) => {
    setSent((m) => new Map(m).set(url, { busy: true }));
    let result = null;
    try {
      const r = await createDownload({ urls: [url] });
      result = r.results && r.results[0];
    } catch (e) {
      result = e && e.details && e.details.results ? e.details.results[0] : null;
      if (!result) app.toast({ message: `Couldn't add “${label}”: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setSent((m) => new Map(m).set(url, { busy: false, view: result ? resultView(result) : null }));
    if (result && result.job && result.job.id && app.watchJobs) app.watchJobs([result.job.id]);
    reloadIds();
  };
  const sync = async () => {
    try {
      await syncAppleLibrary();
    } catch (e) {
      if (!(e && e.code === 'apple_library_sync_running')) app.toast({ message: `Couldn't sync: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    reloadStatus();
  };
  const [open, setOpen] = useState('');

  const st = status.data;
  const unavailable = (st && !st.available) || (page.error && page.error.code === 'apple_library_unavailable');
  const last = st && st.last_sync;
  const left = Math.max(0, page.total - page.items.length);
  if (unavailable) {
    return html`<${Notice} role="warning" icon="key" action=${html`<a class="link" href=${settingsHash('account')}>Account settings</a>`}>
      ${st && st.reason === 'disabled' ? 'The Apple Music library is turned off for this account.' : st && st.reason === 'token_refused' ? 'Apple refused the account’s token.' : 'No signed-in account to read the Apple Music library with.'}
    </${Notice}>`;
  }
  return html`<div class="apple-lib">
    <div class="toolbar">
      <${Segmented} label="Kind" size="sm" options=${KINDS.map((k) => ({ ...k, count: st && st.counts ? count(st.counts[k.value] || 0) : undefined }))} value=${kind} onChange=${(v) => app.navigate(libraryHash({ ...filters, kind: v, q: '' }))} />
      <div class="toolbar-filters">
        <label class="search">
          <${Icon} name="search" />
          <input type="search" data-filter placeholder="Filter this list" aria-label="Filter by name, artist or album" value=${qInput} onInput=${(e) => setQInput(e.currentTarget.value)} />
        </label>
      </div>
    </div>
    <div class="card library-check" data-section="apple-sync">
      <div class="card-body library-check-body">
        <span class="library-check-text">
          <${Icon} name="rotate" />
          <span>${syncing ? 'Syncing…' : last && last.finished_at ? html`Synced <${RelTime} value=${last.finished_at} />${last.ok ? '' : ' · failed'}` : 'Not synced yet'}</span>
        </span>
        <span class="library-check-actions">
          <${Button} size="sm" icon="rotate" disabled=${syncing} aria-busy=${syncing ? 'true' : undefined} onClick=${sync}>${syncing ? 'Syncing…' : 'Sync now'}</${Button}>
        </span>
      </div>
    </div>
    <div class="card card-flush" data-section="apple-items"><div class="card-body">
          ${page.error && page.items.length === 0
            ? html`<${ErrorNote} title="Couldn't load the Apple Music library" error=${page.error} onRetry=${() => load(0)} />`
            : page.loading && page.items.length === 0
              ? html`<${SkeletonJobRows} n=${6} />`
              : page.items.length === 0
                ? html`<${Empty} icon="music" title=${filters.q ? 'Nothing matches' : page.syncedAt ? 'Nothing here' : 'Not synced yet'} />`
                : html`<ul class="apple-list">
                    ${page.items.map((it) => html`<${AppleRow} key=${it.id} it=${it} kind=${kind} ids=${ids} sent=${sent} add=${add} open=${open === it.id} onOpen=${() => setOpen(open === it.id ? '' : it.id)} />`)}
                  </ul>`}
          ${left > 0
            ? html`<div class="load-more"><${Button} onClick=${() => load(page.items.length)} disabled=${page.more}>${page.more ? 'Loading…' : `Load more (${count(left)} left)`}</${Button}></div>`
            : null}
        </div></div>
  </div>`;
}

/** The item's catalog record id when amdl keeps one: an album's or a catalog playlist's. */
const recordOf = (it, kind, ids) => (it.catalog_id && (kind === 'albums' || kind === 'playlists') && ids.has(it.catalog_id) ? it.catalog_id : '');

function AppleRow({ it, kind, ids, sent, add, open, onOpen }) {
  const rec = recordOf(it, kind, ids);
  const s = it.url ? sent.get(it.url) : null;
  const sub = [it.artist_name, kind === 'songs' ? it.album_name : '', kind === 'albums' && it.track_count ? `${count(it.track_count)} ${it.track_count === 1 ? 'song' : 'songs'}` : ''].filter(Boolean).join(' · ');
  const libraryOnly = kind === 'playlists' && !it.catalog_id;
  let action = null;
  if (rec) action = html`<a class="btn btn-ghost btn-sm apple-mark" href=${recordHash(kind, rec)}><${Icon} name="circle-check" /><span>In Library</span></a>`;
  else if (kind === 'artists' && it.catalog_id) action = html`<a class="btn btn-secondary btn-sm" href=${buildHash('new', { q: it.name, artist: it.catalog_id })}><${Icon} name="chevron-right" /><span>Albums</span></a>`;
  else if (libraryOnly) action = html`<${Button} size="sm" icon=${open ? 'chevron-up' : 'chevron-down'} aria-expanded=${String(open)} onClick=${onOpen}>Tracks</${Button}>`;
  else if (s && s.view) action = html`<span class="status-text" data-role=${s.view.role}><${Icon} name=${s.view.icon} size="sm" /><span>${s.view.label}</span></span>`;
  else if (it.url) action = html`<${Button} size="sm" icon="plus" aria-busy=${s && s.busy ? 'true' : undefined} disabled=${Boolean(s && s.busy)} onClick=${() => add(it.url, it.name)}>Add</${Button}>`;
  return html`<li class="apple-item" data-kind=${kind} data-marked=${rec ? '' : undefined}>
    <div class="apple-row" data-row>
      <${Artwork} url=${it.artwork_url} px=${100} widths=${[40, 80, 120]} sizes="40px" className=${kind === 'artists' ? 'art-40 art-round' : 'art-40'} />
      <span class="apple-main">
        <span class="apple-title" dir="auto" title=${it.name}>${it.name}</span>
        ${sub ? html`<span class="apple-sub" dir="auto">${sub}</span>` : null}
      </span>
      <span class="apple-action">${action}</span>
    </div>
    ${open ? html`<${PlaylistTracks} id=${it.id} sent=${sent} add=${add} />` : null}
  </li>`;
}

/** A library-only playlist's tracks, each with Add when it has a catalog link. */
function PlaylistTracks({ id, sent, add }) {
  const [state, setState] = useState({ items: null, error: null });
  useEffect(() => {
    let live = true;
    listAppleLibraryTracks(id).then(
      (r) => live && setState({ items: r.items || [], error: null }),
      (error) => live && setState({ items: null, error }),
    );
    return () => {
      live = false;
    };
  }, [id]);
  if (state.error) return html`<${ErrorNote} title="Couldn't read its tracks" error=${state.error} />`;
  if (!state.items) return html`<${SkeletonJobRows} n=${3} />`;
  if (state.items.length === 0) return html`<p class="apple-tracks-empty">No tracks read yet</p>`;
  return html`<ol class="apple-tracks">
    ${state.items.map((t) => {
      const s = t.url ? sent.get(t.url) : null;
      return html`<li key=${t.position} class="apple-track" data-row>
        <span class="track-index num">${t.position + 1}</span>
        <span class="track-main">
          <span class="track-title" dir="auto">${t.name}</span>
          ${t.artist_name ? html`<span class="track-artist" dir="auto">${t.artist_name}</span>` : null}
        </span>
        <span class="rtrack-len num">${trackLength(t.duration_ms)}</span>
        <span class="apple-action">
          ${s && s.view
            ? html`<span class="status-text" data-role=${s.view.role}><${Icon} name=${s.view.icon} size="sm" /><span>${s.view.label}</span></span>`
            : t.url
              ? html`<${Button} size="sm" variant="ghost" icon="plus" aria-label=${`Add ${t.name}`} title="Add" disabled=${Boolean(s && s.busy)} onClick=${() => add(t.url, t.name)} />`
              : null}
        </span>
      </li>`;
    })}
  </ol>`;
}
