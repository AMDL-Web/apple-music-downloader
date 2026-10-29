// New download (#/new?url=, #/new/:id for the drawer): paste Apple Music
// links, see what each one is, set per-download options and submit. The
// text box is the source of the links; ?url= prefills it, and the page
// writes its links back into ?url= so a reload or the global paste keeps
// them; the whole text also lasts for the tab when the page is left. The
// page head comes from app.js. Parts: download-config.js (server defaults),
// download-lookup.js (previews), download-links.js (rows), download-options.js,
// download-quality.js, download-results.js; the logic is pure in
// lib/links.js, lib/lookup.js, lib/submit.js and lib/overrides.js.

import { html, useState, useEffect, useRef, useMemo, useCallback } from '../ui/preact.js';
import { createDownload } from '../api.js';
import { newDownloadUrls, buildHash } from '../lib/route.js';
import { scanLinks, removeLink, mergeLinks, duplicateCount } from '../lib/links.js';
import {
  chunk,
  pairResults,
  handledUrls,
  resultRows,
  submitSummary,
  classifySubmitError,
  requestOverrides,
} from '../lib/submit.js';
import { OPTIONS_KEY, optionDefaults, buildOverrides, applyOverrides, sanitizeOverrides, parseStoredOptions, describeOverrides } from '../lib/overrides.js';
import { readPref, writePref } from '../lib/prefs.js';
import { useApp, Card, Button, Notice } from '../ui/components.js';
import { useDebounced, useMedia } from '../ui/hooks.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { useServerConfig } from './download-config.js';
import { useLookups } from './download-lookup.js';
import { LinksCard } from './download-links.js';
import { OptionsCard } from './download-options.js';
import { QualitySheet } from './download-quality.js';
import { ResultsCard, BookmarkletCard, useResults } from './download-results.js';

const storage = () => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

const isMac = () => /mac|iphone|ipad/i.test((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '');

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

// The text box outlives the page for the life of the tab, so leaving New
// download through the sidebar and coming back keeps a draft, notes and
// links that are not Apple Music included (?url= keeps only the links).
let draft = '';

export function DownloadPage({ route }) {
  const app = useApp();
  const incoming = newDownloadUrls(route.params);
  // A link without a storefront borrows another link's, else the server's
  // catalog.default_storefront (GET /system), '' until that has loaded.
  const storefront = app.defaultStorefront;
  const [text, setText] = useState(() => mergeLinks(draft, incoming, storefront));
  draft = text;
  const entries = useMemo(() => scanLinks(text, storefront), [text, storefront]);
  const dupes = useMemo(() => duplicateCount(text, storefront), [text, storefront]);
  const settled = useDebounced(text, 350);
  // The submit error under the button (failure) belongs to the text it was
  // sent from and to the connection it failed on: an edit or a reconnect
  // clears it. A submit that drops handled links from the text does not.
  const [failure, setFailure] = useState('');
  const edit = (next) => {
    setFailure('');
    setText(next);
  };
  const epochFailed = useRef(app.epoch);
  useEffect(() => {
    if (epochFailed.current === app.epoch) return;
    epochFailed.current = app.epoch;
    setFailure('');
  }, [app.epoch]);
  const inputRef = useRef(null);

  // ?url= -> text: links that arrive in the address (the global paste on
  // this page, a bookmarklet, a shared link) join the text.
  const urlParam = route.params.url || '';
  useEffect(() => {
    if (incoming.length) edit((t) => mergeLinks(t, incoming, storefront));
  }, [urlParam]);

  // text -> ?url=: the links follow the text, so a reload keeps them, also
  // after the address lost them (n, the sidebar and the palette open bare
  // #/new). When only the address changed and it holds links the text does
  // not, the effect above is merging them; the next settled text writes
  // the address.
  const routeRef = useRef(route);
  routeRef.current = route;
  const settledSeen = useRef(settled);
  useEffect(() => {
    const r = routeRef.current;
    const urls = scanLinks(settled, storefront).map((e) => e.url);
    const inParam = newDownloadUrls(r.params);
    const textMoved = settledSeen.current !== settled;
    settledSeen.current = settled;
    if (sameList(urls, inParam)) return;
    if (!textMoved && scanLinks(inParam.join('\n'), storefront).some((e) => !urls.includes(e.url))) return;
    app.navigate(buildHash('new', { ...r.params, url: urls.join('\n') }, r.jobId), { replace: true });
  }, [settled, urlParam, storefront]);

  // Focus the text box once the shell has focused the heading (a fine
  // pointer only: on a phone it would open the keyboard unasked).
  useEffect(() => {
    if (!window.matchMedia('(pointer: fine)').matches) return undefined;
    const id = requestAnimationFrame(() => {
      const el = inputRef.current;
      const active = document.activeElement;
      if (!el || routeRef.current.jobId) return;
      if (active && active !== document.body && !active.classList.contains('page-title')) return;
      el.focus({ preventScroll: true });
      el.setSelectionRange(el.value.length, el.value.length);
    });
    return () => cancelAnimationFrame(id);
  }, []);

  // Server defaults and hooks.
  const { cfg, setCfgRun, hooks } = useServerConfig(app.epoch);

  const download = cfg.data && cfg.data.config ? cfg.data.config.download : null;
  const trackMode = cfg.data && cfg.data.config && cfg.data.config.catalog ? cfg.data.config.catalog.album_track_url_mode || '' : null;
  const defaults = useMemo(() => (download ? optionDefaults(download) : null), [JSON.stringify(download)]);
  const serverToken = Boolean(cfg.data && cfg.data.config && cfg.data.config.catalog && cfg.data.config.catalog.media_user_token);
  const enabledHooks = hooks.data && hooks.data.enabled ? hooks.data.entries.filter((e) => e.enabled).map((e) => e.name) : [];
  const hookNames = hooks.data ? hooks.data.entries.map((e) => e.name) : [];

  // The form: defaults, plus options this device remembers.
  const [values, setValues] = useState(null);
  const [hookSel, setHookSel] = useState(null);
  const [remember, setRemember] = useState(false);
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState('');
  useEffect(() => {
    if (!defaults || values) return;
    const stored = readPref(storage(), OPTIONS_KEY, parseStoredOptions);
    if (stored) {
      setRemember(true);
      if (stored.hooks) setHookSel(stored.hooks);
    }
    setValues(applyOverrides(defaults, stored || {}));
  }, [defaults]);
  // A remembered hook list keeps only names hooks.yaml still has; without
  // the hooks list it is dropped, since an unknown name fails the submit.
  const hooksPicked = !hookSel ? null : hooks.data ? hookSel.filter((n) => hookNames.includes(n)) : hooks.loading ? hookSel : null;

  const overrides = useMemo(
    () => (defaults && values ? buildOverrides(defaults, values, { hooks: hooksPicked, enabledHooks }) : {}),
    [defaults, values, hooksPicked ? `picked:${hooksPicked.join('\n')}` : 'all', enabledHooks.join('\n')],
  );
  useEffect(() => {
    if (!values) return;
    writePref(storage(), OPTIONS_KEY, remember ? JSON.stringify(sanitizeOverrides(overrides)) : '');
  }, [remember, overrides]);

  const setValue = useCallback((key, v) => setValues((cur) => ({ ...cur, [key]: v })), []);
  const resetOptions = () => {
    if (defaults) setValues(applyOverrides(defaults, {}));
    setHookSel(null);
    setRefusal('');
  };

  const { statusOf, issueOf, sendable, flagged, warned, skipped, needsToken, lookupStatus, queue, priorOf, forgetPrior } = useLookups({
    settled,
    entries,
    token,
    serverToken,
    trackMode,
    epoch: app.epoch,
    storefront,
  });
  const jobHref = (id) => buildHash('new', route.params, id);

  // Submit and results (kept for the life of the tab, without the token).
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState('');
  const [results, setResults] = useResults();
  const [retrying, setRetrying] = useState(() => new Set());
  const [quality, setQuality] = useState(null);
  const [confirm, setConfirm] = useState(null);

  /**
   * Sends `urls` in chunks the backend accepts, the typed token only with
   * a chunk that needs it; returns {pairs, failure}.
   */
  const send = async (urls, ov) => {
    const pairs = [];
    let fail = null;
    for (const part of chunk(urls)) {
      const body = requestOverrides(ov, part, token, lookupStatus);
      try {
        const resp = await createDownload(Object.keys(body).length ? { urls: part, overrides: body } : { urls: part });
        pairs.push(...pairResults(part, resp.results));
      } catch (err) {
        const c = classifySubmitError(err);
        if (c.kind !== 'results') {
          fail = c;
          break;
        }
        pairs.push(...pairResults(part, c.response.results));
      }
    }
    return { pairs, fail };
  };

  // An artist link queues the whole discography as one job: ask first.
  const artists = sendable.filter((u) => {
    const e = entries.find((x) => x.url === u);
    return e && e.link && e.link.kind === 'artist';
  });
  const submit = async (confirmed = false) => {
    if (busy || sendable.length === 0) return;
    if (artists.length && !confirmed) {
      setConfirm(
        artists.map((u) => {
          const s = statusOf(u);
          return (s.state === 'ready' && s.data && s.data.title) || u.replace(/^https?:\/\//i, '');
        }),
      );
      return;
    }
    setConfirm(null);
    setBusy(true);
    setFailure('');
    setRefusal('');
    const { pairs, fail } = await send(sendable, overrides);
    setBusy(false);
    if (fail) {
      if (fail.kind === 'overrides') {
        setRefusal(fail.message);
        setOpen(true);
      } else setFailure(fail.message);
    }
    if (!pairs.length) {
      app.toast({ message: fail ? fail.message : 'Nothing was submitted', role: 'danger' });
      return;
    }
    const rows = resultRows(pairs);
    setResults({ rows, overrides });
    const summary = submitSummary(rows);
    const ids = rows.filter((r) => r.job).map((r) => r.job.id);
    app.toast({ message: ids.length ? `Download started: ${summary}` : summary, role: ids.length ? 'success' : 'warning' });
    if (ids.length && app.watchJobs) app.watchJobs(ids);
    forgetPrior(pairs.map((p) => p.url));
    // Keep what was not handled: links not sent (flagged, or after a failed
    // chunk), links the server did not take, and anything typed meanwhile.
    // ?url= follows the text.
    setText((t) => [...handledUrls(pairs)].reduce(removeLink, t));
    // The button that had focus is now disabled: the summary takes it, on
    // both submit paths, so the result is where focus lands.
    requestAnimationFrame(() => {
      const el = document.getElementById('new-results-summary');
      if (el) el.focus({ preventScroll: false });
    });
  };

  const retry = async (urls) => {
    setRetrying((s) => new Set([...s, ...urls]));
    const { pairs, fail } = await send(urls, results.overrides);
    setRetrying((s) => new Set([...s].filter((u) => !urls.includes(u))));
    if (fail) app.toast({ message: fail.message, role: 'danger' });
    if (!pairs.length) return;
    const fresh = new Map(resultRows(pairs).map((r) => [r.url, r]));
    const ids = [...fresh.values()].filter((r) => r.job).map((r) => r.job.id);
    if (ids.length && app.watchJobs) app.watchJobs(ids);
    setResults((r) => ({ ...r, rows: r.rows.map((row) => fresh.get(row.url) || row) }));
    const done = handledUrls(pairs);
    if (done.size) setText((t) => [...done].reduce(removeLink, t));
    app.toast({ message: submitSummary([...fresh.values()]), role: [...fresh.values()].some((r) => r.job) ? 'success' : 'warning' });
  };

  // Clear removes the card that held focus; the text box takes it (the
  // heading on a touch screen, where focusing the box opens the keyboard).
  const clearResults = () => {
    setResults({ rows: [], overrides: {} });
    requestAnimationFrame(() => {
      if (document.activeElement && document.activeElement !== document.body) return;
      const fine = window.matchMedia('(pointer: fine)').matches;
      const el = fine ? inputRef.current : document.querySelector('.page-title');
      if (el) el.focus({ preventScroll: true });
    });
  };

  const removeEntry = (url) => {
    edit((t) => removeLink(t, url));
    if (inputRef.current) inputRef.current.focus({ preventScroll: true });
  };

  const onKeyDown = (e) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      submit();
    }
  };

  const jumpToOptions = () => {
    setOpen(true);
    requestAnimationFrame(() => {
      const el = document.querySelector('.new-options');
      if (el) el.scrollIntoView({ block: 'start', behavior: 'smooth' });
      const first = document.querySelector('#new-options-body :is(input, select, button):not([disabled])');
      if (first) first.focus({ preventScroll: true });
    });
  };

  const changes = describeOverrides(overrides);
  const optionsLine = !values ? 'Server defaults' : changes.length ? `${changes.length} ${changes.length === 1 ? 'option' : 'options'} changed` : 'Server defaults';
  const countLine = entries.length
    ? [
        `${entries.length} ${entries.length === 1 ? 'link' : 'links'}`,
        `${sendable.length} to download`,
        flagged ? `${flagged} flagged` : '',
        warned ? `${warned} to check` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    : 'No links yet';
  // The shortcut hint only where there is a keyboard to press it on.
  const keyboard = useMedia('(hover: hover) and (pointer: fine)');
  const keys = isMac() ? '⌘ Return' : 'Ctrl+Enter';

  return html`<div class="page page-new">
    <div class="new-main">
      <${Card} section="compose" className="new-compose" title="Apple Music links" labelId="new-compose-title">
        <form class="new-form" onSubmit=${(e) => { e.preventDefault(); submit(); }} onKeyDown=${onKeyDown}>
          <textarea
            id="new-input"
            class="new-input"
            ref=${inputRef}
            rows="6"
            value=${text}
            placeholder=${'https://music.apple.com/us/album/igor/1463409338\nPaste one link per line, or any text that contains links'}
            aria-labelledby="new-compose-title"
            aria-describedby="new-input-hint"
            autocomplete="off"
            autocapitalize="off"
            spellcheck=${false}
            onInput=${(e) => edit(e.currentTarget.value)}
          ></textarea>
          <p id="new-input-hint" class="new-hint">
            Albums, songs, playlists, artists and stations. Links are found anywhere in the text${keyboard ? `; ${keys} downloads` : ''}.
          </p>
          ${needsToken
            ? html`<div class="new-token">
                <label class="new-field-label" for="new-token">Media-user-token</label>
                <input
                  id="new-token"
                  class="new-token-input"
                  type="password"
                  autocomplete="off"
                  spellcheck=${false}
                  value=${token}
                  aria-describedby="new-token-hint"
                  onInput=${(e) => setToken(e.currentTarget.value)}
                />
                <p id="new-token-hint" class="new-hint">
                  Stations download with your Apple Music media-user-token; a private playlist uses it only to find the cover you chose. It goes with
                  this download only and is never stored here.${serverToken ? ' Leave it empty to use the token saved on the server.' : ''}
                </p>
              </div>`
            : null}
          <div class="new-submit">
            <div class="new-submit-info">
              <span class="new-count">${countLine}</span>
              <p class="new-options-line">
                <span class="new-options-state">Options: ${optionsLine}</span>
                <button type="button" class="new-options-jump link" aria-label="Edit options" onClick=${jumpToOptions}>Edit</button>
              </p>
            </div>
            <${Button}
              variant="primary"
              icon="download"
              type="submit"
              className="new-submit-btn"
              disabled=${busy || sendable.length === 0}
              aria-busy=${busy ? 'true' : undefined}
              title=${sendable.length === 0 ? 'Add at least one Apple Music link that can be downloaded' : undefined}
            >${busy ? 'Submitting…' : sendable.length ? `Download ${sendable.length}` : 'Download'}</${Button}>
          </div>
          ${failure ? html`<${Notice} role="danger" icon="circle-x">${failure}</${Notice}>` : null}
        </form>
      </${Card}>
      ${entries.length
        ? html`<${LinksCard}
            entries=${entries}
            statusOf=${statusOf}
            issueOf=${issueOf}
            priorOf=${priorOf}
            jobHref=${jobHref}
            skipped=${skipped}
            dupes=${dupes}
            onRemove=${removeEntry}
            onRetry=${(url) => queue.current.retry(url)}
            onQuality=${(entry, data) => setQuality({ url: entry.url, title: data.title || entry.url, tracks: data.track_count })}
          />`
        : null}
      ${results.rows.length
        ? html`<${ResultsCard}
            rows=${results.rows}
            params=${route.params}
            busy=${retrying}
            onRetry=${retry}
            onClear=${clearResults}
          />`
        : null}
    </div>
    <div class="new-aside">
      <${OptionsCard}
        cfg=${{ loading: cfg.loading, error: cfg.error, reload: () => setCfgRun((n) => n + 1) }}
        defaults=${defaults}
        values=${values}
        setValue=${setValue}
        hooks=${hooks}
        hookSel=${hooksPicked}
        setHookSel=${setHookSel}
        overrides=${overrides}
        open=${open}
        setOpen=${setOpen}
        remember=${remember}
        setRemember=${setRemember}
        onReset=${resetOptions}
        refusal=${refusal}
      />
      <${BookmarkletCard} />
    </div>
    ${quality ? html`<${QualitySheet} url=${quality.url} title=${quality.title} tracks=${quality.tracks} onClose=${() => setQuality(null)} />` : null}
    ${confirm
      ? html`<${ConfirmDialog}
          title=${confirm.length === 1 ? `Download everything by ${confirm[0]}?` : `Download everything by ${confirm.length} artists?`}
          confirmLabel=${`Download ${sendable.length}`}
          onConfirm=${() => submit(true)}
          onCancel=${() => setConfirm(null)}
        >
          <p>An artist link queues the artist's whole discography as one job: every album, often hundreds of tracks or more. You can cancel it from the queue.</p>
          ${confirm.length > 1 ? html`<p dir="auto">${confirm.join(', ')}</p>` : null}
        </${ConfirmDialog}>`
      : null}
  </div>`;
}
