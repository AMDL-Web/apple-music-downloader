// Settings (#/settings?section=): the runtime configuration by section, the
// Apple Music account, library sync and the configuration file. The page
// head comes from app.js; this is the body: a section list (a select under
// 640 px), the open section, and a sticky save bar while anything differs
// from the server. Save sends PUT /api/v1/config with only the changed
// leaves (lib/settings.js buildPatch); leaving with unsaved changes asks
// first, inside the console (the app's leave guard) and on unload.

import { html, useState, useEffect, useMemo, useRef, useCallback } from '../ui/preact.js';
import { getConfig, putConfig } from '../api.js';
import {
  SETTINGS_SECTIONS,
  sectionById,
  sectionFields,
  fieldByKey,
  flattenConfig,
  changedKeys,
  buildPatch,
  pinnedMap,
  fieldErrors,
  validateDraft,
  changesLabel,
  draftAfterSave,
} from '../lib/settings.js';
import { punctuated } from '../lib/format.js';
import { settingsSection, settingsHash, parseHash } from '../lib/route.js';
import { useApp, Card, Icon, Button, Notice, ErrorNote, SkeletonRows, Unavailable } from '../ui/components.js';
import { ConfirmDialog, isDialogOpen } from '../ui/dialogs.js';
import { SettingsField } from './settings-fields.js';
import { AccountPanel } from './settings-account.js';
import { LibrarySyncPanel, ConfigFilePanel, TemplateReference } from './settings-panels.js';

const EMPTY = { loaded: false, error: null, base: {}, pinned: {}, pinnedList: [], persisted: true, reloadError: '' };

export function SettingsPage({ route }) {
  const app = useApp();
  const section = sectionById(settingsSection(route.params));
  const [cfg, setCfg] = useState(EMPTY);
  const [draft, setDraft] = useState({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState(null);
  const [leaveTo, setLeaveTo] = useState(null);
  const [loadSeq, setLoadSeq] = useState(0);
  const [failSeq, setFailSeq] = useState(0);
  const errorRef = useRef(null);

  const dirtyKeys = useMemo(() => changedKeys(cfg.base, draft), [cfg.base, draft]);
  const dirty = dirtyKeys.length > 0;
  const checks = useMemo(() => validateDraft(Object.fromEntries(dirtyKeys.map((k) => [k, draft[k]]))), [dirtyKeys, draft]);
  const errors = { ...(saveError ? saveError.fields : {}), ...checks.errors };

  const apply = useCallback((res) => {
    setCfg({
      loaded: true,
      error: null,
      base: flattenConfig(res && res.config),
      pinned: pinnedMap(res && res.pinned),
      pinnedList: Array.isArray(res && res.pinned) ? res.pinned : [],
      persisted: !res || res.persisted !== false,
      reloadError: (res && res.reload_error) || '',
    });
  }, []);

  // Load on mount, after a reconnect and on Discard, but never under
  // unsaved edits: Discard is the way to see the server's values again.
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
  }, [app.epoch, loadSeq, apply]);

  const setValue = useCallback((key, value) => {
    setDraft((d) => ({ ...d, [key]: value }));
    setSaveError((e) => (e && e.fields[key] ? { ...e, fields: Object.fromEntries(Object.entries(e.fields).filter(([k]) => k !== key)) } : e));
  }, []);
  const revert = useCallback((key) => {
    setDraft((d) => Object.fromEntries(Object.entries(d).filter(([k]) => k !== key)));
    setSaveError((e) => (e && e.fields[key] ? { ...e, fields: Object.fromEntries(Object.entries(e.fields).filter(([k]) => k !== key)) } : e));
  }, []);
  const discard = () => {
    setDraft({});
    setSaveError(null);
    setLoadSeq((n) => n + 1);
    app.toast({ message: 'Changes discarded' });
  };

  const save = async () => {
    if (!dirty || saving) return;
    if (Object.keys(checks.errors).length) {
      setSaveError({ message: 'Fix the marked fields, then save again.', fields: {} });
      setFailSeq((n) => n + 1);
      return;
    }
    const body = buildPatch(cfg.base, draft, cfg.pinned);
    if (Object.keys(body).length === 0) {
      setDraft({});
      return;
    }
    // Fields stay editable while the request runs; an edit made meanwhile
    // stays in the draft.
    const sent = draft;
    setSaving(true);
    setSaveError(null);
    try {
      const res = await putConfig(body);
      apply(res);
      setDraft((d) => draftAfterSave(d, sent));
      app.toast({ message: `Saved ${dirtyKeys.length === 1 ? '1 setting' : `${dirtyKeys.length} settings`}`, role: 'success' });
    } catch (err) {
      setSaveError(fieldErrors(err));
      setFailSeq((n) => n + 1);
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

  const leave = () => {
    const to = leaveTo;
    if (app.setLeaveGuard) app.setLeaveGuard(null);
    setLeaveTo(null);
    setDraft({});
    if (app.resumeLeave) app.resumeLeave(to);
    else app.navigate(to);
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
    body = html`<${ConfigFilePanel} persisted=${cfg.persisted} pinned=${cfg.pinnedList} reloadError=${cfg.reloadError} dirty=${dirty} onReloaded=${apply} />`;
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
            pinnedEnv=${cfg.pinned[f.key]}
            error=${errors[f.key]}
            warning=${checks.warnings[f.key]}
            onChange=${(v) => setValue(f.key, v)}
            onRevert=${() => revert(f.key)}
          />`,
        )}
      </div>
      ${section.id === 'files' ? html`<${TemplateReference} />` : null}
      ${section.id === 'library_sync'
        ? html`<${LibrarySyncPanel} enabled=${cfg.base['library_sync.enabled'] === true} tokenSet=${Boolean(cfg.base['catalog.media_user_token'])} />`
        : null}`;
  }

  const navItem = (s) => {
    const marks = [dirtySections.has(s.id) ? 'unsaved changes' : '', errorSections.has(s.id) ? 'needs attention' : ''].filter(Boolean);
    return html`<li key=${s.id}>
      <a
        class="settings-nav-item"
        href=${settingsHash(s.id)}
        data-section=${s.id}
        data-dirty=${dirtySections.has(s.id) ? '' : undefined}
        data-invalid=${errorSections.has(s.id) ? '' : undefined}
        aria-current=${s.id === section.id ? 'true' : undefined}
      >
        <${Icon} name=${s.icon} />
        <span class="settings-nav-label">${s.label}</span>
        ${errorSections.has(s.id)
          ? html`<span class="settings-nav-mark" data-role="danger"><${Icon} name="circle-x" size="sm" /></span>`
          : dirtySections.has(s.id)
            ? html`<span class="settings-nav-mark settings-nav-dot" aria-hidden="true"></span>`
            : null}
        ${marks.length ? html`<span class="sr-only">, ${marks.join(', ')}</span>` : null}
      </a>
    </li>`;
  };

  const faultSections = SETTINGS_SECTIONS.filter((s) => errorSections.has(s.id) && s.id !== section.id);

  return html`<div class="page page-settings" data-section-active=${section.id} data-dirty=${dirty ? '' : undefined}>
    <nav class="settings-nav" aria-label="Settings sections">
      <ul class="settings-nav-list">${SETTINGS_SECTIONS.map(navItem)}</ul>
      <label class="settings-jump">
        <span class="settings-jump-label">Section</span>
        <select class="select" value=${section.id} onChange=${(e) => app.navigate(settingsHash(e.currentTarget.value))}>
          ${SETTINGS_SECTIONS.map(
            (s) => html`<option key=${s.id} value=${s.id}>${s.label}${errorSections.has(s.id) ? ' (needs attention)' : dirtySections.has(s.id) ? ' (unsaved)' : ''}</option>`,
          )}
        </select>
      </label>
    </nav>
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
      ? html`<div class="save-bar" role="region" aria-label="Unsaved changes" data-state=${saving ? 'saving' : 'dirty'}>
          <p class="save-bar-text">
            <span class="save-bar-count">${changesLabel(dirtyKeys.length)}</span>
            <span class="save-bar-where">${SETTINGS_SECTIONS.filter((s) => dirtySections.has(s.id)).map((s) => s.label).join(', ')}</span>
          </p>
          <div class="save-bar-actions">
            <${Button} variant="ghost" className="save-bar-discard" onClick=${discard} disabled=${saving}>Discard</${Button}>
            <${Button}
              variant="primary"
              icon="save"
              className="save-bar-save"
              aria-keyshortcuts="Meta+S Control+S"
              aria-disabled=${saving ? 'true' : undefined}
              onClick=${save}
            >${saving ? 'Saving…' : 'Save'}</${Button}>
          </div>
        </div>`
      : null}
    ${leaveTo
      ? html`<${ConfirmDialog}
          title="Leave without saving?"
          confirmLabel="Discard and leave"
          cancelLabel="Stay"
          danger
          onConfirm=${leave}
          onCancel=${() => setLeaveTo(null)}
        >
          <p>${changesLabel(dirtyKeys.length)} will be lost.</p>
        </${ConfirmDialog}>`
      : null}
  </div>`;
}

