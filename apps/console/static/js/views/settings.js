// Settings (#/settings?section=): the server's status and its runtime
// configuration in one page. The page head comes from app.js; this is the
// body: the section list beside the status board (#/settings) or one open
// section, its status head above its fields (on a phone, the list with live
// summaries first and each section its own view with a way back), and a
// fixed save bar while anything differs from the server. Save sends PUT
// /api/v1/config with only the changed leaves (lib/settings-draft.js
// buildPatch); leaving with unsaved changes asks first, inside the console
// (the app's leave guard) and on unload. Status comes from the shared store
// (ui/system-status.js), every key of it, since the board shows them all.

import { html, useState, useEffect, useLayoutEffect, useMemo, useRef, useCallback } from '../ui/preact.js';
import { useMedia, useNow } from '../ui/hooks.js';
import { getConfig, putConfig } from '../api.js';
import { useStatus, primeStatus, invalidateStatus, refreshStatus } from '../ui/system-status.js';
import { FIELD_KEYS, fieldByKey } from '../lib/settings.js';
import { SETTINGS_SECTIONS, BOARD_SECTIONS, sectionById, groupById } from '../lib/settings-sections.js';
import { domainView } from '../lib/domains.js';
import { refreshKeys } from '../lib/facts.js';
import {
  flattenConfig,
  changedKeys,
  buildPatch,
  fieldErrors,
  validateDraft,
  savedWarnings,
  changedSentence,
  firstErrorKey,
  labelOf,
  unsavedAnnouncement,
  draftAfterSave,
} from '../lib/settings-draft.js';
import { punctuated } from '../lib/format.js';
import { settingsSection, settingsField, settingsHash, parseHash, buildHash } from '../lib/route.js';
import { useApp, Notice, LinkButton } from '../ui/components.js';
import { ConfirmDialog, isDialogOpen } from '../ui/dialogs.js';
import { focusField } from '../ui/field-focus.js';
import { SectionBody, groupDomId } from './settings-section.js';
import { SettingsNav, SettingsList, SettingsBack, SaveBar } from './settings-nav.js';
import { DomainHead, RefreshButton } from './settings-head.js';
import { SettingsBoard } from './settings-board.js';
import { useShowKeys } from './settings-keys.js';

const EMPTY = { loaded: false, error: null, base: {}, persisted: true, reloadError: '' };
const STATUS_KEYS = ['health', 'system', 'wrapper', 'librarySync', 'hooks', 'stats', 'logs', 'config'];

// The head actions that read a domain again (D18), by section, and the keys they read.
const REFRESH = {
  account: { label: 'Refresh session status', keys: ['wrapper'] },
  jobs: { label: 'Refresh jobs status', keys: ['system', 'stats'] },
  library_sync: { label: 'Refresh library sync status', keys: ['librarySync'] },
  hooks: { label: 'Refresh hooks status', keys: ['hooks', 'stats'] },
};

/** Lyrics & artwork's line on the phone list: "5000 × 5000 JPG · LRC", from the saved values. */
function artworkSummary(base) {
  const size = base['download.cover_size'];
  const fmt = base['download.cover_format'];
  const files = Array.isArray(base['download.lyrics_files']) ? base['download.lyrics_files'] : [];
  const cover = [typeof size === 'string' ? size.replace(/x/, ' × ') : '', typeof fmt === 'string' ? fmt.toUpperCase() : ''].filter(Boolean).join(' ');
  return [cover, files.map((f) => String(f).toUpperCase()).join(' + ')].filter(Boolean).join(' · ');
}

export function SettingsPage({ route }) {
  const app = useApp();
  const asked = settingsSection(route.params);
  const section = sectionById(asked, settingsField(route.params));
  // #/settings without a section is the status board; a phone shows the
  // list of sections instead, each its own view.
  const phone = useMedia('(max-width: 639px)');
  const board = !asked && !settingsField(route.params);
  const listView = phone && board;
  const boardView = !phone && board;
  const [showKeys] = useShowKeys();
  const st = useStatus(STATUS_KEYS);
  const now = useNow(15000);
  const offline = app.conn && app.conn.state === 'offline';
  const ctx = { now, counts: app.counts, conn: app.conn, simulate: app.simulate };
  const views = Object.fromEntries(BOARD_SECTIONS.map((s) => [s.id, domainView(s.id, st, ctx)]));
  const [cfg, setCfg] = useState(EMPTY);
  const [draft, setDraft] = useState({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState(null);
  const [leaveTo, setLeaveTo] = useState(null);
  const [discarding, setDiscarding] = useState(false);
  const [live, setLive] = useState('');
  const [loadSeq, setLoadSeq] = useState(0);
  const [failSeq, setFailSeq] = useState(0);
  const errorRef = useRef(null);

  const sectionOf = (key) => (fieldByKey(key) || {}).section;
  const dirtyKeys = useMemo(() => changedKeys(cfg.base, draft), [cfg.base, draft]);
  const dirty = dirtyKeys.length > 0;
  const checks = useMemo(() => validateDraft(Object.fromEntries(dirtyKeys.map((k) => [k, draft[k]]))), [dirtyKeys, draft]);
  const errors = { ...(saveError ? saveError.fields : {}), ...checks.errors };
  // A saved value keeps its warning; a field under edit has its own.
  const warnings = useMemo(
    () => ({ ...savedWarnings(cfg.base, Object.fromEntries(dirtyKeys.map((k) => [k, true]))), ...checks.warnings }),
    [cfg.base, dirtyKeys, checks],
  );

  // Every answer is also the status store's display-only config (System's summary).
  const apply = useCallback((res) => {
    primeStatus('config', res);
    setCfg({
      loaded: true,
      error: null,
      base: flattenConfig(res && res.config),
      persisted: !res || res.persisted !== false,
      reloadError: (res && res.reload_error) || '',
    });
  }, []);

  // A save can change what /system and /library-sync report (simulate,
  // folders, library sync): the keys its changes touch are read again
  // (lib/facts.js refreshKeys). A reload from the file may change any.
  const applyWritten = useCallback(
    (res, keys) => {
      apply(res);
      invalidateStatus(keys ? refreshKeys(keys) : ['system', 'librarySync']);
    },
    [apply],
  );

  // Load on mount, on Retry and after Discard, but never under unsaved
  // edits: Discard is the way to see the server's values again.
  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;
  useEffect(() => {
    if (dirtyRef.current) return undefined;
    let cancelled = false;
    getConfig().then(
      (res) => !cancelled && apply(res),
      (error) => !cancelled && setCfg((c) => ({ ...c, error })),
    );
    return () => {
      cancelled = true;
    };
  }, [loadSeq, apply]);

  // Announce the unsaved count politely, after the typing pauses. A save or
  // discard that empties the draft has its own toast, so it stays quiet here.
  const prevCount = useRef(0);
  const quietZero = useRef(false);
  useEffect(() => {
    const n = dirtyKeys.length;
    if (n === prevCount.current) return undefined;
    prevCount.current = n;
    if (n > 0) quietZero.current = false;
    else if (quietZero.current) {
      quietZero.current = false;
      return undefined;
    }
    const t = setTimeout(() => setLive(unsavedAnnouncement(n)), 700);
    return () => clearTimeout(t);
  }, [dirtyKeys.length]);

  const setValue = useCallback((key, value) => {
    setDraft((d) => ({ ...d, [key]: value }));
    setSaveError((e) => (e && e.fields[key] ? { ...e, fields: Object.fromEntries(Object.entries(e.fields).filter(([k]) => k !== key)) } : e));
  }, []);
  const revert = useCallback((key) => {
    setDraft((d) => Object.fromEntries(Object.entries(d).filter(([k]) => k !== key)));
    setSaveError((e) => (e && e.fields[key] ? { ...e, fields: Object.fromEntries(Object.entries(e.fields).filter(([k]) => k !== key)) } : e));
  }, []);
  const discard = () => {
    quietZero.current = true;
    setDiscarding(false);
    setDraft({});
    setSaveError(null);
    setLoadSeq((n) => n + 1);
    app.toast({ message: 'Changes discarded' });
  };
  // One edited field goes straight back; several ask first.
  const askDiscard = () => (dirtyKeys.length > 1 ? setDiscarding(true) : discard());

  // Resolves to whether everything was saved (Save in the leave dialog goes
  // on only then). A field marked invalid is not sent: Save takes focus to
  // the first one instead, opening its section when it is elsewhere.
  const save = async () => {
    if (!dirty || saving) return false;
    const bad = firstErrorKey(checks.errors);
    if (bad) {
      const where = sectionOf(bad);
      if (!board && where === section.id) focusField(bad);
      else app.navigate(settingsHash(where, bad));
      setLive(`Not saved: ${labelOf(bad)}. ${checks.errors[bad]}.`);
      return false;
    }
    const body = buildPatch(cfg.base, draft);
    if (Object.keys(body).length === 0) {
      setDraft({});
      return true;
    }
    // Fields stay editable while the request runs; an edit made meanwhile
    // stays in the draft.
    const sent = draft;
    setSaving(true);
    setSaveError(null);
    try {
      const res = await putConfig(body);
      quietZero.current = true;
      applyWritten(res, dirtyKeys);
      setDraft((d) => draftAfterSave(d, sent));
      app.toast({ message: `Saved ${dirtyKeys.length === 1 ? '1 setting' : `${dirtyKeys.length} settings`}`, role: 'success' });
      return true;
    } catch (err) {
      setSaveError(fieldErrors(err, Object.keys(body).flatMap((sec) => Object.keys(body[sec]).map((k) => `${sec}.${k}`))));
      setFailSeq((n) => n + 1);
      return false;
    } finally {
      setSaving(false);
    }
  };
  const saveRef = useRef(save);
  saveRef.current = save;

  // Each failed save moves focus to its notice, which links to the sections
  // at fault; editing a field afterwards does not.
  useEffect(() => {
    if (failSeq > 0 && errorRef.current) errorRef.current.focus({ preventScroll: false });
  }, [failSeq]);

  // Cmd/Ctrl+S saves while this page is open and no dialog (the leave
  // confirmation among them) is up.
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === 's' || e.key === 'S')) {
        e.preventDefault();
        if (!isDialogOpen()) saveRef.current();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Unsaved changes: ask before the tab closes, and before the console
  // shows another page (section changes stay on this page and never ask).
  useEffect(() => {
    if (!dirty) return undefined;
    const onUnload = (e) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', onUnload);
    if (app.setLeaveGuard) {
      app.setLeaveGuard((hash) => {
        if (parseHash(hash).page === 'settings') return true;
        setLeaveTo(hash);
        return false;
      });
    }
    return () => {
      window.removeEventListener('beforeunload', onUnload);
      if (app.setLeaveGuard) app.setLeaveGuard(null);
    };
  }, [dirty, app.setLeaveGuard]);

  // The leave question is moot once nothing is unsaved.
  useEffect(() => {
    if (!dirty && !saving) setLeaveTo(null);
  }, [dirty, saving]);

  // A new section opens at its top (the section list is sticky).
  const shownSection = useRef(section.id);
  useEffect(() => {
    if (shownSection.current === section.id) return;
    shownSection.current = section.id;
    window.scrollTo(0, 0);
  }, [section.id]);

  // A ?section= naming a sub-group opens its section at that group once the
  // fields are there; a ?field= focuses instead.
  const group = groupById(asked) && !settingsField(route.params) ? asked : '';
  useEffect(() => {
    if (!group || !cfg.loaded) return;
    const el = document.getElementById(groupDomId(group));
    if (el) el.scrollIntoView({ block: 'start' });
  }, [group, cfg.loaded]);

  const leave = (to = leaveTo) => {
    if (app.setLeaveGuard) app.setLeaveGuard(null);
    setLeaveTo(null);
    setDraft({});
    if (app.resumeLeave) app.resumeLeave(to);
    else app.navigate(to);
  };

  // Save in the leave dialog: a failed save keeps the page, with its error shown.
  const saveAndLeave = async () => {
    const to = leaveTo;
    if (await save()) leave(to);
    else setLeaveTo(null);
  };

  const dirtySections = new Set(dirtyKeys.map(sectionOf));
  const errorSections = new Set(Object.keys(errors).map(sectionOf).filter(Boolean));
  const valueOf = (key) => (Object.hasOwn(draft, key) ? draft[key] : cfg.base[key]);

  // The fixed save bar spans the main column: its left edge and width follow
  // it (a style object, so the CSP allows it).
  const mainRef = useRef(null);
  const [place, setPlace] = useState(null);
  useLayoutEffect(() => {
    const el = mainRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return undefined;
    const measure = () => {
      const r = el.getBoundingClientRect();
      setPlace((p) => (p && p['--save-x'] === `${r.left}px` && p['--save-w'] === `${r.width}px` ? p : { '--save-x': `${r.left}px`, '--save-w': `${r.width}px` }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener('resize', measure);
    return () => {
      ro.disconnect();
      window.removeEventListener('resize', measure);
    };
  }, []);

  // Say a domain's role change once ("Storage & files: Low space"), never a poll tick.
  const roles = useRef(null);
  useEffect(() => {
    const next = Object.fromEntries(BOARD_SECTIONS.map((s) => [s.id, views[s.id] && views[s.id].state === 'ok' ? views[s.id].role || '' : null]));
    const prev = roles.current;
    roles.current = next;
    if (!prev) return;
    const changed = BOARD_SECTIONS.filter((s) => prev[s.id] !== null && next[s.id] !== null && prev[s.id] !== next[s.id]);
    if (changed.length) setLive(changed.map((s) => `${s.label}: ${views[s.id].tag ? views[s.id].tag.label : 'OK'}`).join('. '));
  });

  const form = { valueOf, dirtyKeys, errors, warnings, setValue, revert, showKeys };
  const refresh = REFRESH[section.id];
  const actions =
    section.id === 'logging'
      ? html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('logs', { level: 'error' })}>Open Logs</${LinkButton}>`
      : refresh
        ? html`<${RefreshButton} label=${refresh.label} disabled=${offline} onRefresh=${() => Promise.all(refresh.keys.map(refreshStatus))} />`
        : null;
  const summaries = Object.fromEntries(
    SETTINGS_SECTIONS.map((s) => [s.id, s.id === 'lyrics_artwork' ? (cfg.loaded ? { text: artworkSummary(cfg.base) } : null) : views[s.id] && views[s.id].summary]),
  );

  const faultKeys = saveError ? FIELD_KEYS.filter((k) => saveError.fields[k]) : [];
  let main;
  if (listView) main = html`<${SettingsList} dirty=${dirtySections} invalid=${errorSections} views=${views} summaries=${summaries} />`;
  else if (boardView) main = html`<${SettingsBoard} views=${views} />`;
  else
    main = html`${phone ? html`<${SettingsBack} />` : null}
      <section key=${section.id} class="card settings-section" data-domain=${section.id} aria-labelledby="settings-section-title">
        <${DomainHead} section=${section} view=${views[section.id] || null} actions=${actions} titleId="settings-section-title" />
        <div class="settings-section-body">
          <${SectionBody}
            section=${section}
            cfg=${cfg}
            form=${form}
            dirty=${dirty}
            onRetry=${() => setLoadSeq((n) => n + 1)}
            onReloaded=${applyWritten}
            st=${st}
            now=${now}
            offline=${offline}
          />
        </div>
      </section>`;

  return html`<div
    class="page page-settings"
    data-section-active=${listView || boardView ? undefined : section.id}
    data-view=${phone ? (listView ? 'list' : 'section') : boardView ? 'board' : 'section'}
    data-dirty=${dirty ? '' : undefined}
  >
    <${SettingsNav} section=${board ? null : section} dirty=${dirtySections} invalid=${errorSections} views=${views} />
    <div class="settings-main" ref=${mainRef}>
      ${saveError
        ? html`<div class="settings-error" tabindex="-1" ref=${errorRef} role="alert">
            <${Notice} role="danger" icon="circle-x">
              <strong>Not saved.</strong> <span class="wrap-any">${punctuated(saveError.message)}</span>
              ${faultKeys.length
                ? html` <span>See ${faultKeys.map((k, i) => html`${i ? ', ' : ''}<a key=${k} class="link" href=${settingsHash(sectionOf(k), k)}>${labelOf(k)}</a>`)}.</span>`
                : null}
            </${Notice}>
          </div>`
        : null}
      ${main}
    </div>
    ${dirty || saving
      ? html`<${SaveBar} count=${dirtyKeys.length} sections=${dirtySections} saving=${saving} onDiscard=${askDiscard} onSave=${save} place=${place} />`
      : null}
    ${leaveTo
      ? html`<${ConfirmDialog}
          title="Save changes?"
          confirmLabel="Don't Save"
          cancelLabel="Cancel"
          altLabel=${Object.keys(checks.errors).length ? '' : 'Save'}
          onAlt=${saveAndLeave}
          busy=${saving}
          busyLabel="Saving…"
          danger
          onConfirm=${() => leave()}
          onCancel=${() => setLeaveTo(null)}
        >
          <p>${changedSentence(dirtyKeys)}</p>
        </${ConfirmDialog}>`
      : null}
    ${discarding
      ? html`<${ConfirmDialog}
          title=${`Discard ${dirtyKeys.length} changes?`}
          confirmLabel="Discard"
          cancelLabel="Keep Editing"
          danger
          onConfirm=${discard}
          onCancel=${() => setDiscarding(false)}
        >
          <p>${changedSentence(dirtyKeys)}</p>
        </${ConfirmDialog}>`
      : null}
    <div class="sr-only" role="status" aria-live="polite">${live}</div>
  </div>`;
}

