// Library page (#/library?tab=albums|playlists|apple&state=&flag=&q=&kind=):
// amdl's records. Albums and Playlists list records (GET /library/{kind})
// with a state and a flag filter and a search, Load more, a selection mode
// for Download again, Pause and Remove on many, and with a flag filter one
// button that resets every record it selects (POST /library/reset). The
// check strip above says the last disk check, checks the library and
// downloads what a check found missing. The Apple library tab is
// views/library-apple.js. A record opens its drawer (views/record-drawer.js).

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listAlbumRecords, listPlaylistRecords, getLibrarySummary, startCheck, resetRecords, resetRecord, pauseRecord, deleteRecord } from '../api.js';
import { libraryFilters, libraryHash, recordHash, LIBRARY_TABS } from '../lib/route.js';
import { recordQuery, recordTitle, kindWord, checkText, checkRunning, applyCheckFeed } from '../lib/records.js';
import { runLimited } from '../lib/bulk.js';
import { count } from '../lib/format.js';
import { toggleId, setIds, selectionState } from '../lib/selection.js';
import { useApp, Icon, Segmented, ErrorNote, SkeletonJobRows, Button, Empty, RelTime } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { RecordRow } from './record-row.js';
import { AppleLibrary } from './library-apple.js';

const PAGE = 50;
const REFRESH_MS = 1500;
const STATES = [
  ['', 'Any state'],
  ['complete', 'Downloaded'],
  ['partial', 'Partly downloaded'],
  ['none', 'Not downloaded'],
];
const FLAGS = [
  ['', 'Any'],
  ['failed', 'Failed'],
  ['given_up', 'Given up'],
  ['missing', 'Missing'],
  ['waiting', 'Waiting to retry'],
  ['paused', 'Paused'],
  ['unresolved', 'Not read yet'],
];
// The reset a flag filter offers for every record it selects.
const FLAG_RESET = { failed: 'Retry all', given_up: 'Retry all', missing: 'Download all missing' };
const TAB_LABEL = { albums: 'Albums', playlists: 'Playlists', apple: 'Apple Music' };

/** The summary (counts, the last check), kept fresh by the feed. */
function useSummary(app) {
  const [summary, setSummary] = useState(null);
  const [stamp, setStamp] = useState(0);
  useEffect(() => {
    let live = true;
    getLibrarySummary().then(
      (s) => live && setSummary(s),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [stamp, app.epoch]);
  const timer = useRef(null);
  useEffect(() => {
    const off = app.subscribeFeed((batch) => {
      setSummary((s) => (s ? { ...s, check: applyCheckFeed(s.check, batch) } : s));
      if (!batch.some((m) => m.type === 'resource_updated' || (m.type === 'check_updated' && m.check && m.check.finished_at))) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setStamp((n) => n + 1), REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, []);
  return [summary, () => setStamp((n) => n + 1)];
}

/** One page list of records for the filters, read again on the feed's record news and a finished disk check. */
function useRecords(app, f, enabled) {
  const [state, setState] = useState({ items: [], total: 0, loading: true, error: null, more: false });
  const [stamp, setStamp] = useState(0);
  const key = `${f.tab}|${f.state}|${f.flag}|${f.q}`;
  const list = f.tab === 'playlists' ? listPlaylistRecords : listAlbumRecords;
  const load = (offset) => {
    setState((s) => ({ ...s, loading: offset === 0 && s.items.length === 0, more: offset > 0 }));
    return list(recordQuery(f, offset, offset ? PAGE : Math.max(PAGE, state.items.length))).then(
      (r) => setState((s) => ({ items: offset ? [...s.items, ...r.items.filter((x) => !s.items.some((y) => y.id === x.id))] : r.items, total: r.total, loading: false, error: null, more: false })),
      (error) => setState((s) => ({ ...s, loading: false, error, more: false })),
    );
  };
  useEffect(() => {
    if (enabled) load(0);
  }, [key, stamp, app.epoch, enabled]);
  useEffect(() => setState({ items: [], total: 0, loading: true, error: null, more: false }), [key]);
  const timer = useRef(null);
  useEffect(() => {
    const off = app.subscribeFeed((batch) => {
      if (!batch.some((m) => m.type === 'resource_updated' || (m.type === 'check_updated' && m.check && m.check.finished_at))) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setStamp((n) => n + 1), REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, []);
  return [state, load, () => setStamp((n) => n + 1)];
}

export function LibraryPage({ route }) {
  const app = useApp();
  const f = libraryFilters(route.params);
  const [qInput, setQInput] = useState(f.q);
  const q = useDebounced(qInput, 300);
  const setParams = (patch) => app.navigate(libraryHash({ ...f, ...patch }), { replace: true });
  useEffect(() => {
    if (q !== f.q) setParams({ q });
  }, [q]);
  useEffect(() => setQInput(f.q), [f.tab]);
  const records = f.tab !== 'apple';
  const [summary, reloadSummary] = useSummary(app);
  const [state, load, reload] = useRecords(app, f, records);
  const kind = f.tab === 'playlists' ? 'playlists' : 'albums';

  const tabs = LIBRARY_TABS.map((t) => {
    const c = summary && t !== 'apple' && summary[t] ? count(summary[t].total) : undefined;
    return { value: t, label: TAB_LABEL[t], count: t === 'apple' ? undefined : c || (summary ? '0' : '') };
  });

  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState(() => new Set());
  useEffect(() => {
    setSelecting(false);
    setSelected(new Set());
  }, [f.tab]);
  const chosen = state.items.filter((r) => selected.has(r.id));
  const [confirm, setConfirm] = useState(null);
  const [busy, setBusy] = useState('');

  const bulk = async (action) => {
    const fn = { again: (r) => resetRecord(kind, r.id, { what: 'audio' }), pause: (r) => pauseRecord(kind, r.id), delete: (r) => deleteRecord(kind, r.id) }[action];
    setBusy(action);
    const out = await runLimited(chosen, 4, fn);
    const failed = out.filter((o) => !o.ok);
    const verb = { again: 'Queued', pause: 'Paused', delete: 'Removed' }[action];
    app.toast(
      failed.length
        ? { message: `${verb} ${count(out.length - failed.length)} of ${count(out.length)}. ${failed.length} failed: ${failed.slice(0, 3).map((o) => recordTitle(o.item)).join(', ')} (${(failed[0].error && failed[0].error.message) || 'the request failed'}).`, role: 'danger' }
        : { message: `${verb} ${count(out.length)} ${kindWord(kind, out.length)}`, role: 'success' },
    );
    setBusy('');
    setConfirm(null);
    setSelected(new Set());
    setSelecting(false);
    reload();
    reloadSummary();
  };

  const resetFiltered = async () => {
    setBusy('filter');
    try {
      const r = await resetRecords({ what: 'audio', kind, filter: { flag: [f.flag] } });
      const skipped = r.skipped_active ? `, ${count(r.skipped_active)} downloading now left as they are` : '';
      app.toast({ message: `Queued ${count(r.reset)} ${kindWord(kind, r.reset)}${skipped}`, role: 'success' });
    } catch (e) {
      app.toast({ message: `Couldn't reset them: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy('');
    reload();
    reloadSummary();
  };

  const check = summary && summary.check;
  const checking = checkRunning(check);
  const runCheck = async (force = false) => {
    setBusy('check');
    try {
      await startCheck(force ? { force: true } : {});
      reloadSummary();
    } catch (e) {
      app.toast({ message: `Couldn't check the files: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy('');
    setConfirm(null);
  };
  const missing = summary ? (summary.albums.flags.missing || 0) + (summary.playlists.flags.missing || 0) : 0;
  const downloadMissing = async () => {
    setBusy('missing');
    try {
      const r = await resetRecords({ what: 'audio', filter: { flag: ['missing'] } });
      app.toast({ message: `Downloading the missing files of ${count(r.reset)} ${r.reset === 1 ? 'record' : 'records'}`, role: 'success' });
    } catch (e) {
      app.toast({ message: `Couldn't download them: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy('');
    reloadSummary();
  };

  const left = Math.max(0, state.total - state.items.length);
  const filtered = Boolean(f.state || f.flag || f.q);
  return html`<div class="page page-library" data-tab=${f.tab} data-selecting=${selecting ? '' : undefined}>
    <div class="toolbar">
      <${Segmented} label="Library" options=${tabs} value=${f.tab} onChange=${(tab) => app.navigate(libraryHash({ tab }))} />
      ${records
        ? html`<div class="toolbar-filters">
            <label class="search">
              <${Icon} name="search" />
              <input type="search" data-filter placeholder="Filter this list" aria-label=${`Filter ${TAB_LABEL[f.tab].toLowerCase()} by name or artist`} value=${qInput} onInput=${(e) => setQInput(e.currentTarget.value)} />
            </label>
            <select class="select" aria-label="State" value=${f.state} onChange=${(e) => setParams({ state: e.currentTarget.value })}>
              ${STATES.map(([v, l]) => html`<option key=${v} value=${v}>${l}</option>`)}
            </select>
            <select class="select" aria-label="Flag" value=${f.flag} onChange=${(e) => setParams({ flag: e.currentTarget.value })}>
              ${FLAGS.map(([v, l]) => html`<option key=${v} value=${v}>${v ? l : 'Any flag'}</option>`)}
            </select>
          </div>
          <div class="queue-tools">
            <${Button}
              icon="square-check"
              className="queue-select"
              aria-pressed=${String(selecting)}
              disabled=${!selecting && state.items.length === 0}
              onClick=${() => {
                setSelecting(!selecting);
                setSelected(new Set());
              }}
            >${selecting ? 'Done' : 'Select'}</${Button}>
          </div>`
        : null}
    </div>
    ${records
      ? html`<div class="card library-check" data-section="check">
          <div class="card-body library-check-body">
            <span class="library-check-text">
              <${Icon} name="hard-drive" />
              <span>${check ? [checkText(check), check.finished_at ? html` · <${RelTime} key="t" value=${check.finished_at} />` : null] : 'Not checked yet'}</span>
            </span>
            <span class="library-check-actions">
              ${missing
                ? html`<${Button} size="sm" icon="download" disabled=${Boolean(busy)} onClick=${downloadMissing}>Download ${count(missing)} missing</${Button}>`
                : null}
              ${check && check.aborted_reason === 'too_many_missing'
                ? html`<${Button} size="sm" variant="ghost" disabled=${Boolean(busy) || checking} onClick=${() => setConfirm({ action: 'force' })}>Mark them anyway</${Button}>`
                : null}
              <${Button} size="sm" icon="hard-drive" className="library-check-btn" disabled=${Boolean(busy) || checking} aria-busy=${checking ? 'true' : undefined} onClick=${() => runCheck(false)}>${checking ? 'Checking…' : 'Check disk'}</${Button}>
            </span>
          </div>
        </div>`
      : null}
    ${records && selecting
      ? html`<div class="bulk-bar" role="toolbar" aria-label="Selected records">
          <label class="bulk-all">
            <input
              type="checkbox"
              class="bulk-check"
              checked=${selectionState(selected, state.items) === 'all'}
              indeterminate=${selectionState(selected, state.items) === 'some'}
              onChange=${() => setSelected((s) => setIds(s, state.items.map((r) => r.id), selectionState(s, state.items) !== 'all'))}
              aria-label="Select all shown"
            />
          </label>
          <span class="bulk-count">${count(chosen.length)} selected</span>
          <span class="bulk-actions">
            <${Button} size="sm" icon="rotate" className="bulk-action" disabled=${!chosen.length || Boolean(busy)} onClick=${() => setConfirm({ action: 'again' })}>Download again</${Button}>
            <${Button} size="sm" icon="pause" className="bulk-action" disabled=${!chosen.length || Boolean(busy)} onClick=${() => bulk('pause')}>Pause</${Button}>
            <${Button} size="sm" icon="trash" variant="danger" className="bulk-action" disabled=${!chosen.length || Boolean(busy)} onClick=${() => setConfirm({ action: 'delete' })}>Remove</${Button}>
          </span>
        </div>`
      : null}
    ${records && FLAG_RESET[f.flag] && state.items.length && !selecting
      ? html`<div class="library-flag-bar">
          <${Button} size="sm" icon="rotate" disabled=${Boolean(busy)} onClick=${resetFiltered}>${FLAG_RESET[f.flag]}</${Button}>
        </div>`
      : null}
    ${records
      ? html`<div class="card card-flush" data-section="records"><div class="card-body">
          ${state.error && state.items.length === 0
            ? html`<${ErrorNote} title=${`Couldn't load the ${kindWord(kind, 2)}`} error=${state.error} onRetry=${() => load(0)} />`
            : state.loading && state.items.length === 0
              ? html`<${SkeletonJobRows} n=${6} />`
              : state.items.length === 0
                ? html`<${Empty} icon="disc" title=${filtered ? `No ${kindWord(kind, 2)} match` : `No ${kindWord(kind, 2)} yet`} action=${filtered ? html`<${Button} size="sm" onClick=${() => app.navigate(libraryHash({ tab: f.tab }))}>Clear filters</${Button}>` : null} />`
                : html`<ul class="job-list${selecting ? ' is-selecting' : ''}">
                    ${state.items.map(
                      (r) => html`<li key=${r.id} class="job-item" data-selected=${selected.has(r.id) ? '' : undefined}>
                        ${selecting
                          ? html`<label class="job-check"><input type="checkbox" class="job-check-input" checked=${selected.has(r.id)} aria-label=${`Select ${recordTitle(r)}`} onChange=${(e) => setSelected((s) => toggleId(s, r.id, e.currentTarget.checked))} /></label>`
                          : null}
                        <${RecordRow}
                          record=${r}
                          href=${recordHash(kind, r.id, route.params)}
                          selected=${selected.has(r.id)}
                          onSelect=${selecting ? () => setSelected((s) => toggleId(s, r.id, !s.has(r.id))) : undefined}
                        />
                      </li>`,
                    )}
                  </ul>`}
          ${left > 0
            ? html`<div class="load-more"><${Button} onClick=${() => load(state.items.length)} disabled=${state.more}>${state.more ? 'Loading…' : `Load more (${count(left)} left)`}</${Button}></div>`
            : null}
        </div></div>`
      : html`<${AppleLibrary} route=${route} filters=${f} />`}
    ${confirm && confirm.action === 'delete'
      ? html`<${ConfirmDialog}
          title=${`Remove ${count(chosen.length)} ${kindWord(kind, chosen.length)} from amdl?`}
          confirmLabel="Remove"
          busyLabel="Removing…"
          danger
          busy=${busy === 'delete'}
          onConfirm=${() => bulk('delete')}
          onCancel=${() => setConfirm(null)}
        >The files stay. Adding them again picks them up.</${ConfirmDialog}>`
      : null}
    ${confirm && confirm.action === 'again'
      ? html`<${ConfirmDialog}
          title=${`Download ${count(chosen.length)} ${kindWord(kind, chosen.length)} again?`}
          confirmLabel="Download again"
          busyLabel="Queueing…"
          busy=${busy === 'again'}
          onConfirm=${() => bulk('again')}
          onCancel=${() => setConfirm(null)}
        >Replaces their downloaded files once each new copy is complete.</${ConfirmDialog}>`
      : null}
    ${confirm && confirm.action === 'force'
      ? html`<${ConfirmDialog}
          title="Mark the missing files?"
          confirmLabel="Check and mark"
          busyLabel="Starting…"
          danger
          busy=${busy === 'check'}
          onConfirm=${() => runCheck(true)}
          onCancel=${() => setConfirm(null)}
        >More than half the files were not found; if the library folder is not mounted, every one of them is marked missing.</${ConfirmDialog}>`
      : null}
  </div>`;
}
