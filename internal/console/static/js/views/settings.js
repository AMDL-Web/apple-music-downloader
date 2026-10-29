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
  FIELD_KEYS,
  sectionById,
  groupById,
  fieldByKey,
} from '../lib/settings.js';
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
import { settingsSection, settingsField, settingsHash, parseHash } from '../lib/route.js';
import { readPref, writePref } from '../lib/prefs.js';
import { useApp, Card, Notice } from '../ui/components.js';
import { ConfirmDialog, isDialogOpen } from '../ui/dialogs.js';
import { focusField } from '../ui/field-focus.js';
import { SectionBody, groupDomId } from './settings-section.js';
import { SettingsNav, SaveBar } from './settings-nav.js';

// "Show config keys and defaults" is a per-device preference.
const KEYS_PREF = 'amdl.console.settings.keys';
const storage = () => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

const EMPTY = { loaded: false, error: null, base: {}, persisted: true, reloadError: '' };

export function SettingsPage({ route }) {
  const app = useApp();
  const asked = settingsSection(route.params);
  const section = sectionById(asked, settingsField(route.params));
  const [showKeys, setShowKeys] = useState(() => readPref(storage(), KEYS_PREF, (v) => v === '1'));
  const changeShowKeys = (on) => {
    setShowKeys(on);
    writePref(storage(), KEYS_PREF, on ? '1' : '0');
  };
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

  // Resolves to whether everything was saved (Save in the leave dialog goes
  // on only then). A field marked invalid is not sent: Save takes focus to
  // the first one instead, opening its section when it is elsewhere.
  const save = async () => {
    if (!dirty || saving) return false;
    const bad = firstErrorKey(checks.errors);
    if (bad) {
      const where = sectionOf(bad);
      if (where === section.id) focusField(bad);
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

  // A ?section= naming an Advanced group (an earlier section) opens Advanced
  // at that group once the fields are there; a ?field= focuses instead.
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

  const form = { valueOf, dirtyKeys, errors, warnings, setValue, revert, showKeys };
  const body = html`<${SectionBody}
    section=${section}
    cfg=${cfg}
    form=${form}
    dirty=${dirty}
    onRetry=${() => setLoadSeq((n) => n + 1)}
    onReloaded=${apply}
    setShowKeys=${changeShowKeys}
  />`;

  const faultKeys = saveError ? FIELD_KEYS.filter((k) => saveError.fields[k]) : [];

  return html`<div class="page page-settings" data-section-active=${section.id} data-dirty=${dirty ? '' : undefined}>
    <${SettingsNav} section=${section} dirty=${dirtySections} invalid=${errorSections} navigate=${app.navigate} />
    <div class="settings-main">
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

