// Settings (#/settings?section=): the runtime configuration by section, the
// Apple Music account, library sync and the configuration file. The page
// head comes from app.js; this is the body: a section list (a select under
// 640 px), the open section, and a sticky save bar while anything differs
// from the server. Save sends PUT /api/v1/config with only the changed
// leaves (lib/settings-draft.js buildPatch); leaving with unsaved changes asks
// first, inside the console (the app's leave guard) and on unload.

import { html, useState, useEffect, useMemo, useRef, useCallback } from '../ui/preact.js';
import { getConfig, putConfig } from '../api.js';
import {
  SETTINGS_SECTIONS,
  sectionById,
  sectionFields,
  fieldByKey,
} from '../lib/settings.js';
import {
  flattenConfig,
  changedKeys,
  buildPatch,
  fieldErrors,
  validateDraft,
  savedWarnings,
  lossSummary,
  unsavedAnnouncement,
  changesLabel,
  draftAfterSave,
} from '../lib/settings-draft.js';
import { punctuated } from '../lib/format.js';
import { settingsSection, settingsHash, parseHash } from '../lib/route.js';
import { useApp, Card, Notice, ErrorNote, SkeletonRows, Unavailable } from '../ui/components.js';
import { ConfirmDialog, isDialogOpen } from '../ui/dialogs.js';
import { SettingsField } from './settings-fields.js';
import { AccountPanel } from './settings-account.js';
import { LibrarySyncPanel, ConfigFilePanel, TemplateReference } from './settings-panels.js';
import { SettingsNav, SaveBar } from './settings-nav.js';

/** What a confirmation would lose: the count, the sections and the field names. */
function LossText({ keys, lead }) {
  const t = lossSummary(keys);
  return html`<p>${lead} ${changesLabel(t.count)} in ${t.sections}: ${t.fields}.</p>
    <p>Saved settings are not affected.</p>`;
}

const EMPTY = { loaded: false, error: null, base: {}, persisted: true, reloadError: '' };

export function SettingsPage({ route }) {
  const app = useApp();
  const section = sectionById(settingsSection(route.params));
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

  const dirtyKeys = useMemo(() => changedKeys(cfg.base, draft), [cfg.base, draft]);
  const dirty = dirtyKeys.length > 0;
  const checks = useMemo(() => validateDraft(Object.fromEntries(dirtyKeys.map((k) => [k, draft[k]]))), [dirtyKeys, draft]);
  const errors = { ...(saveError ? saveError.fields : {}), ...checks.errors };
  // A saved value keeps its warning; a field under edit has its own.
  const warnings = useMemo(
    () => ({ ...savedWarnings(cfg.base, Object.fromEntries(dirtyKeys.map((k) => [k, true]))), ...checks.warnings }),
    [cfg.base, dirtyKeys, checks],
  );

  const apply = useCallback((res) => {
    setCfg({
      loaded: true,
      error: null,
      base: flattenConfig(res && res.config),
      persisted: !res || res.persisted !== false,
      reloadError: (res && res.reload_error) || '',
    });
  }, []);

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

  // Resolves to whether everything was saved (Save and leave goes on only then).
  const save = async () => {
    if (!dirty || saving) return false;
    if (Object.keys(checks.errors).length) {
      setSaveError({ message: 'Fix the marked fields, then save again.', fields: {} });
      setFailSeq((n) => n + 1);
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
      apply(res);
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

  const leave = (to = leaveTo) => {
    if (app.setLeaveGuard) app.setLeaveGuard(null);
    setLeaveTo(null);
    setDraft({});
    if (app.resumeLeave) app.resumeLeave(to);
    else app.navigate(to);
  };

  // Save and leave: a failed save keeps the page, with its error shown.
  const saveAndLeave = async () => {
    const to = leaveTo;
    if (await save()) leave(to);
    else setLeaveTo(null);
  };

  const sectionOf = (key) => (fieldByKey(key) || {}).section;
  const dirtySections = new Set(dirtyKeys.map(sectionOf));
  const errorSections = new Set(Object.keys(errors).map(sectionOf).filter(Boolean));
  const valueOf = (key) => (Object.hasOwn(draft, key) ? draft[key] : cfg.base[key]);

  let body;
  if (section.id === 'account') body = html`<${AccountPanel} />`;
  else if (cfg.error && !cfg.loaded)
    body =
      cfg.error.status === 404
        ? html`<${Unavailable} what="Runtime configuration (GET /api/v1/config)" />`
        : html`<${ErrorNote} title="Couldn't load the settings" error=${cfg.error} onRetry=${() => setLoadSeq((n) => n + 1)} />`;
  else if (!cfg.loaded) body = html`<${SkeletonRows} n=${6} kind="line" />`;
  else if (section.id === 'config_file')
    body = html`<${ConfigFilePanel} persisted=${cfg.persisted} reloadError=${cfg.reloadError} dirty=${dirty} base=${cfg.base} onReloaded=${apply} />`;
  else {
    const fields = sectionFields(section.id);
    body = html`
      ${section.id === 'files'
        ? html`<${Notice} role="warning" icon="triangle-alert">
            Changing a folder or a template changes where new downloads are saved. Tracks already saved under the old names are
            not recognized as downloaded, so they download again.
          </${Notice}>`
        : null}
      <div class="field-list">
        ${fields.map(
          (f) => html`<${SettingsField}
            key=${f.key}
            field=${f}
            value=${valueOf(f.key)}
            changed=${dirtyKeys.includes(f.key)}
            error=${errors[f.key]}
            warning=${warnings[f.key]}
            onChange=${(v) => setValue(f.key, v)}
            onRevert=${() => revert(f.key)}
          />`,
        )}
      </div>
      ${section.id === 'files' ? html`<${TemplateReference} />` : null}
      ${section.id === 'library_sync'
        ? html`<${LibrarySyncPanel} enabled=${cfg.base['library_sync.enabled'] === true} interval=${cfg.base['library_sync.interval_minutes']} tokenSet=${Boolean(cfg.base['catalog.media_user_token'])} />`
        : null}`;
  }

  const faultSections = SETTINGS_SECTIONS.filter((s) => errorSections.has(s.id) && s.id !== section.id);

  return html`<div class="page page-settings" data-section-active=${section.id} data-dirty=${dirty ? '' : undefined}>
    <${SettingsNav} section=${section} dirty=${dirtySections} invalid=${errorSections} navigate=${app.navigate} />
    <div class="settings-main">
      ${saveError
        ? html`<div class="settings-error" tabindex="-1" ref=${errorRef} role="alert">
            <${Notice} role="danger" icon="circle-x">
              <strong>Not saved.</strong> <span class="wrap-any">${punctuated(saveError.message)}</span>
              ${faultSections.length
                ? html` <span>See ${faultSections.map((s, i) => html`${i ? ', ' : ''}<a key=${s.id} class="link" href=${settingsHash(s.id)}>${s.label}</a>`)}.</span>`
                : null}
            </${Notice}>
          </div>`
        : null}
      <${Card} section=${section.id} className="settings-section" title=${section.label} labelId="settings-section-title">
        <p class="settings-intro">${section.intro}</p>
        ${body}
      </${Card}>
    </div>
    ${dirty || saving
      ? html`<${SaveBar} count=${dirtyKeys.length} sections=${dirtySections} saving=${saving} onDiscard=${askDiscard} onSave=${save} />`
      : null}
    ${leaveTo
      ? html`<${ConfirmDialog}
          title="Leave without saving?"
          confirmLabel="Discard and leave"
          cancelLabel="Stay"
          altLabel=${Object.keys(checks.errors).length ? '' : 'Save and leave'}
          onAlt=${saveAndLeave}
          busy=${saving}
          busyLabel="Saving…"
          danger
          onConfirm=${() => leave()}
          onCancel=${() => setLeaveTo(null)}
        >
          <${LossText} keys=${dirtyKeys} lead="Leaving loses" />
        </${ConfirmDialog}>`
      : null}
    ${discarding
      ? html`<${ConfirmDialog}
          title="Discard all unsaved changes?"
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          danger
          onConfirm=${discard}
          onCancel=${() => setDiscarding(false)}
        >
          <${LossText} keys=${dirtyKeys} lead="Discarding loses" />
        </${ConfirmDialog}>`
      : null}
    <div class="sr-only" role="status" aria-live="polite">${live}</div>
  </div>`;
}

