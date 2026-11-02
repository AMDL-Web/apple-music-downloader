// Record drawer (#/library/albums/:id, #/library/playlists/:id): one album or
// playlist record from GET /library/{kind}/{id}. The head (cover, title,
// artist, facts with the tracks the storefront withholds, its state and
// Paused), the actions (Pause or Resume, Download missing, Reset, Add
// quality, Check this album, Remove from amdl), how its last disk check
// ended, each track with its main slot's state and its other qualities as
// chips, and its runs. Kept fresh by the feed's resource_updated and
// check_updated; what it changes it tells the app (recordsChanged), since
// a pause, resume or removal with no run sends no resource_updated.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { getRecord, pauseRecord, resumeRecord, resetRecord, deleteRecord, downloadRecord, startCheck, listDownloads } from '../api.js';
import { artPalette, paletteStyle, artWidths, trackLength, count } from '../lib/format.js';
import { recordTitle, recordSub, recordArt, recordPalette, recordView, trackState, slotQuality, audioView, trackCountText, kindWord, commonQuality, checkScope, checkText, checkBody, resetNote } from '../lib/records.js';
import { slotChoices, CHOICE_LABEL } from '../lib/download-ask.js';
import { buildHash } from '../lib/route.js';
import { useApp, Artwork, Badge, Button, Empty, ErrorNote, SkeletonRows, StatusText, Chip, RelTime, Notice } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { MenuButton } from '../ui/menu.js';
import { JobRow } from './queue-row.js';

const HEAD_ART = artWidths(88, 112);
const FOCUSABLE = 'a[href], button:not([disabled]), summary, input:not([disabled]), select:not([disabled]), textarea, [tabindex]:not([tabindex="-1"])';
const REFRESH_MS = 800;
const RUNS = 5;

const RESET_ITEMS = [
  { id: 'audio', label: 'Download again' },
  { id: 'lyrics', label: 'Lyrics again' },
  { id: 'covers', label: 'Covers again' },
  { id: 'all', label: 'Everything again' },
];

/** The record and its last runs, read again when the feed says it changed. */
function useRecord(app, kind, id) {
  const [state, setState] = useState({ record: null, runs: [], error: null });
  const [stamp, setStamp] = useState(0);
  const key = `${kind}/${id}`;
  useEffect(() => setState({ record: null, runs: [], error: null }), [key]);
  useEffect(() => {
    let live = true;
    const res = kind === 'playlists' ? 'playlist' : 'album';
    Promise.allSettled([getRecord(kind, id), listDownloads({ resource_kind: res, resource_id: id, limit: RUNS })]).then(([r, j]) => {
      if (!live) return;
      setState((s) => ({
        record: r.status === 'fulfilled' ? r.value : s.record,
        error: r.status === 'rejected' ? r.reason : null,
        runs: j.status === 'fulfilled' && j.value && Array.isArray(j.value.downloads) ? j.value.downloads : s.runs,
      }));
    });
    return () => {
      live = false;
    };
  }, [key, stamp, app.epoch]);
  const timer = useRef(null);
  useEffect(() => {
    const res = kind === 'playlists' ? 'playlist' : 'album';
    const off = app.subscribeFeed((batch) => {
      const mine = batch.some(
        (m) =>
          (m.type === 'resource_updated' && m.resource_kind === res && m.resource_id === id) ||
          (m.type === 'check_updated' && m.check && m.check.finished_at) ||
          (m.type === 'download_upserted' && m.job && m.job.resource && m.job.resource.id === id),
      );
      if (!mine) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setStamp((n) => n + 1), REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, [key]);
  return [state, () => setStamp((n) => n + 1)];
}

export function RecordDrawer({ record: ref, onClose }) {
  const app = useApp();
  const { kind, id } = ref;
  const [{ record, runs, error }, reload] = useRecord(app, kind, id);
  const panel = useRef(null);

  useEffect(() => {
    const el = panel.current;
    if (!el) return undefined;
    const first = Array.from(el.querySelectorAll('[data-autofocus]')).find((n) => n.offsetParent !== null);
    if (first) first.focus();
    const onKey = (e) => {
      if (e.key !== 'Tab') return;
      const nodes = Array.from(el.querySelectorAll(FOCUSABLE)).filter((n) => n.offsetParent !== null);
      if (nodes.length === 0) return;
      const a = nodes[0];
      const z = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === a) {
        e.preventDefault();
        z.focus();
      } else if (!e.shiftKey && document.activeElement === z) {
        e.preventDefault();
        a.focus();
      }
    };
    el.addEventListener('keydown', onKey);
    return () => el.removeEventListener('keydown', onKey);
  }, [kind, id]);

  const missing = error && error.status === 404;
  return html`<div class="drawer-host">
    <div class="scrim" onClick=${onClose}></div>
    <aside class="drawer record-drawer" role="dialog" aria-modal="true" aria-labelledby="drawer-title" ref=${panel}>
      <div class="drawer-bar">
        <${Button} variant="ghost" icon="x" className="drawer-close" aria-label="Close" onClick=${onClose} data-autofocus />
        <${Button} variant="ghost" icon="chevron-left" className="drawer-back" aria-label="Back" onClick=${onClose} data-autofocus>Back</${Button}>
      </div>
      <div class="drawer-scroll">
        ${missing
          ? html`<h2 id="drawer-title" class="sr-only">Not in the library</h2><${Empty} icon="circle-slash" title=${`No ${kindWord(kind)} with this id in the library`} />`
          : !record && error
            ? html`<${ErrorNote} title="Couldn't load this record" error=${error} onRetry=${reload} />`
            : !record
              ? html`<h2 id="drawer-title" class="sr-only">Loading</h2><${SkeletonRows} n=${8} kind="track" />`
              : html`<${RecordBody} kind=${kind} record=${record} runs=${runs} reload=${reload} onClose=${onClose} />`}
      </div>
    </aside>
  </div>`;
}

function RecordBody({ kind, record, runs, reload, onClose }) {
  const app = useApp();
  const palette = artPalette(recordPalette(record));
  const { view, flags } = recordView(record);
  const sub = recordSub(record);
  const year = record.release_date ? record.release_date.slice(0, 4) : '';
  const meta = [kindWord(kind) === 'album' ? 'Album' : 'Playlist', year, record.genre, trackCountText(record), record.storefront_read ? record.storefront_read.toUpperCase() : ''].filter(Boolean).join(' · ');
  // The quality most tracks have is said once; a track shows a chip only where it differs, and for its other qualities.
  const usual = commonQuality(record);
  const [busy, setBusy] = useState('');
  const [ask, setAsk] = useState(null);
  const active = Boolean(record.active_job_id);
  const tracks = Array.isArray(record.tracks) ? record.tracks : [];
  const name = recordTitle(record);
  const res = kind === 'playlists' ? 'playlist' : 'album';

  const act = async (key, fn, done) => {
    setBusy(key);
    try {
      const out = await fn();
      if (out && out.job && out.job.id && app.watchJobs) app.watchJobs([out.job.id]);
      if (done) app.toast({ message: typeof done === 'function' ? done(out) : done, role: 'success' });
      reload();
      if (app.recordsChanged) app.recordsChanged();
      return true;
    } catch (e) {
      app.toast({ message: `Couldn't do that: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
      return false;
    } finally {
      setBusy('');
    }
  };
  const resetWhat = (what, songs) => act(`reset-${what}`, () => resetRecord(kind, record.id, songs ? { what, songs } : { what }), (out) => resetNote(out, record, `Downloading “${name}” again`));
  const onReset = (what) => (what === 'audio' || what === 'all' ? setAsk({ what }) : resetWhat(what));
  const addQuality = (codec) => act(`add-${codec}`, () => downloadRecord(kind, record.id, { codec }), `Adding ${CHOICE_LABEL[codec] || codec} to “${name}”`);
  // The last disk check of this record, from the feed: an aborted one
  // stays as a note with its way on, a finished one is said once.
  const [checked, setChecked] = useState(null);
  const said = useRef(0);
  useEffect(
    () =>
      app.subscribeFeed((batch) => {
        const scope = checkScope(kind, record.id);
        const mine = batch.filter((m) => m.type === 'check_updated' && m.check && m.check.scope === scope && m.check.finished_at).pop();
        if (!mine) return;
        setChecked(mine.check);
        if (!mine.check.aborted_reason && said.current !== mine.check.id) {
          said.current = mine.check.id;
          app.toast({ message: checkText(mine.check), role: 'success' });
        }
      }),
    [kind, record.id],
  );
  const check = (force = false) =>
    act('check', () => {
      setChecked(null);
      return startCheck(checkBody(checkScope(kind, record.id), force));
    }, 'Checking its files');
  const remove = async () => {
    setBusy('delete');
    try {
      await deleteRecord(kind, record.id);
      app.toast({ message: `Removed “${name}” from amdl`, role: 'success' });
      if (app.recordsChanged) app.recordsChanged();
      setAsk(null);
      onClose();
    } catch (e) {
      app.toast({ message: `Couldn't remove it: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
      setBusy('');
    }
  };
  const doneTracks = record.tracks_done || 0;
  return html`<div class="drawer-body">
    <header class="drawer-head" style=${paletteStyle(palette)}>
      <${Artwork} url=${recordArt(record)} bg=${palette && palette.bg} px=${240} widths=${HEAD_ART} sizes="(max-width: 639px) 88px, 112px" className="art-64" eager />
      <div class="drawer-head-text">
        <h2 id="drawer-title" class=${record.name ? 'drawer-title' : 'drawer-title is-unresolved'} dir="auto">${name}</h2>
        ${sub ? html`<p class="drawer-artist" dir="auto">${sub}</p>` : null}
        ${meta ? html`<p class="drawer-meta">${meta}</p>` : null}
        ${usual ? html`<p class="drawer-meta drawer-summary">${usual}</p>` : null}
        ${record.last_checked_at ? html`<p class="drawer-meta">Checked <${RelTime} value=${record.last_checked_at} /></p>` : null}
        <div class="drawer-badges">
          <${Badge} view=${view} />
          ${flags.map((f) => html`<span key=${f.key} class="badge record-flag" data-flag=${f.key}><span class="badge-label">${f.label}</span></span>`)}
        </div>
      </div>
    </header>

    <div class="drawer-actions">
      <div class="drawer-actions-row">
        ${record.paused
          ? html`<${Button} variant="primary" icon="play" disabled=${Boolean(busy)} onClick=${() => act('resume', () => resumeRecord(kind, record.id), 'Resumed')}>Resume</${Button}>`
          : html`<${Button} icon="pause" disabled=${Boolean(busy)} onClick=${() => act('pause', () => pauseRecord(kind, record.id), 'Paused')}>Pause</${Button}>`}
        ${record.tracks_missing
          ? html`<${Button} icon="download" disabled=${Boolean(busy) || active} title=${active ? 'Pause it first' : undefined} onClick=${() => act('missing', () => resetRecord(kind, record.id, { what: 'audio', states: ['missing'] }), (out) => resetNote(out, record, `Downloading ${count(record.tracks_missing)} missing`))}>Download missing</${Button}>`
          : null}
        <${MenuButton} className="record-reset" text="Reset" icon="rotate" label="Reset" title=${active ? 'Pause it first' : undefined} heading=${name} disabled=${Boolean(busy) || active} items=${RESET_ITEMS} onChoose=${onReset} />
        ${doneTracks
          ? html`<${MenuButton}
              className="record-add"
              text="Add quality"
              icon="plus"
              label="Add quality"
              title=${active ? 'Pause it first' : undefined}
              heading="Add quality"
              disabled=${Boolean(busy) || active}
              items=${slotChoices([]).map((c) => ({ id: c, label: CHOICE_LABEL[c] || c }))}
              onChoose=${addQuality}
            />`
          : null}
      </div>
      <div class="drawer-actions-row">
        <${Button} variant="ghost" icon="hard-drive" disabled=${Boolean(busy)} onClick=${() => check()}>Check ${kindWord(kind) === 'album' ? 'this album' : 'this playlist'}</${Button}>
        <${Button} variant="ghost" icon="trash" className="drawer-delete" disabled=${Boolean(busy) || active} title=${active ? 'Pause it first' : undefined} onClick=${() => setAsk({ what: 'delete' })}>Remove from amdl</${Button}>
      </div>
      ${checked && checked.aborted_reason
        ? html`<${Notice}
            role="warning"
            icon="triangle-alert"
            action=${checked.aborted_reason === 'too_many_missing'
              ? html`<${Button} size="sm" variant="ghost" disabled=${Boolean(busy)} onClick=${() => check(true)}>Mark them anyway</${Button}>`
              : null}
          >${checkText(checked)}</${Notice}>`
        : null}
    </div>

    <section class="drawer-section" aria-labelledby="tracks-title">
      <div class="section-head"><h3 id="tracks-title" class="section-title">Tracks</h3></div>
      ${tracks.length === 0
        ? html`<${Empty} icon="list" title=${record.resolved_at ? 'No tracks' : 'Not read yet'} />`
        : html`<ol class="rtracks">
            ${tracks.map((t, i) => html`<${RecordTrack} key=${t.song_id} t=${t} n=${t.position !== undefined ? t.position + 1 : t.song_number || i + 1} artist=${record.artist_name} usual=${usual} disabled=${Boolean(busy) || active} onReset=${(what) => resetWhat(what, [t.song_id])} />`)}
          </ol>`}
    </section>

    ${runs.length
      ? html`<section class="drawer-section" aria-labelledby="runs-title">
          <div class="section-head"><h3 id="runs-title" class="section-title">Runs</h3></div>
          <ul class="job-list record-runs">
            ${runs.map((j) => html`<li key=${j.id} class="job-item"><${JobRow} job=${j} href=${buildHash('queue', {}, j.id)} /></li>`)}
          </ul>
        </section>`
      : null}

    ${ask && ask.what === 'delete'
      ? html`<${ConfirmDialog}
          title=${`Remove “${name}” from amdl?`}
          confirmLabel="Remove"
          busyLabel="Removing…"
          danger
          busy=${busy === 'delete'}
          onConfirm=${remove}
          onCancel=${() => setAsk(null)}
        >The files stay. Adding it again picks them up.</${ConfirmDialog}>`
      : null}
    ${ask && (ask.what === 'audio' || ask.what === 'all')
      ? html`<${ConfirmDialog}
          title=${`Download “${name}” again?`}
          confirmLabel="Download again"
          busyLabel="Queueing…"
          busy=${busy.startsWith('reset')}
          onConfirm=${() => resetWhat(ask.what).then(() => setAsk(null))}
          onCancel=${() => setAsk(null)}
        >${doneTracks ? `Replaces the ${count(doneTracks)} downloaded ${doneTracks === 1 ? 'file' : 'files'} once each new copy is complete.` : 'Downloads every track again.'}</${ConfirmDialog}>`
      : null}
  </div>`;
}

const TRACK_ITEMS = [
  { id: 'audio', label: 'Download again' },
  { id: 'lyrics', label: 'Lyrics again' },
];

/** One track: number, title, its qualities as chips, its main slot's state, a menu to reset it. */
function RecordTrack({ t, n, artist, usual, disabled, onReset }) {
  const s = trackState(t);
  const slots = [s.main, ...s.extra].filter((f) => f && !(f.slot === 'main' && slotQuality(f) === usual));
  const done = s.main && s.main.audio_state === 'done';
  return html`<li class="rtrack" data-row tabindex="-1" data-state=${s.main ? s.main.audio_state : 'none'} data-wanted=${t.wanted ? undefined : 'false'}>
    <span class="track-index num">${n}</span>
    <span class="track-main">
      <span class="track-title" dir="auto" title=${t.name}>${t.name}</span>
      ${t.artist_name && t.artist_name !== artist ? html`<span class="track-artist" dir="auto">${t.artist_name}</span>` : null}
    </span>
    <span class="rtrack-slots">
      ${slots.map((f) => {
        const q = slotQuality(f);
        const v = audioView(f.audio_state);
        return q ? html`<span key=${f.slot} class="rtrack-slot" data-slot=${f.slot} data-state=${f.audio_state} title=${f.slot === 'main' ? `${q}: ${v.label}` : `Another quality, ${q}: ${v.label}`}><${Chip}>${q}</${Chip}></span>` : null;
      })}
    </span>
    <span class="rtrack-len num">${trackLength(t.duration_ms)}</span>
    <span class="rtrack-state"><${StatusText} view=${s.view} /></span>
    <span class="rtrack-menu">
      ${done || (s.main && ['failed', 'given_up', 'missing'].includes(s.main.audio_state))
        ? html`<${MenuButton} className="rtrack-btn" size="sm" label=${`Actions for ${t.name}`} title=${disabled ? 'Pause it first' : 'More actions'} heading=${t.name} disabled=${disabled} items=${TRACK_ITEMS} onChoose=${onReset} />`
        : null}
    </span>
    ${s.note ? html`<p class="track-error">${s.note}</p>` : null}
  </li>`;
}
