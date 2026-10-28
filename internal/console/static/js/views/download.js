// New download (#/new?url=, #/new/:id for the drawer): paste Apple Music
// links, see what each one is, set per-download options and submit. The
// text box is the source of the links; ?url= prefills it, and the page
// writes its links back into ?url= so a reload or the global paste keeps
// them. The page head comes from app.js. Parts: download-links.js (rows),
// download-options.js, download-quality.js, download-results.js; the logic
// is pure in lib/links.js, lib/lookup.js, lib/submit.js and lib/overrides.js.

import { html, useState, useEffect, useRef, useMemo, useCallback } from '../ui/preact.js';
import { createDownload, getConfig, getDownload, getHooks, lookupLink } from '../api.js';
import { newDownloadUrls, buildHash } from '../lib/route.js';
import { scanLinks, removeLink, parseLink } from '../lib/links.js';
import { createLookupQueue, PREVIEW_KINDS, PREVIEW_LIMIT } from '../lib/lookup.js';
import {
  linkIssue,
  submittable,
  chunk,
  pairResults,
  keptAfterSubmit,
  resultRows,
  applyFeedToResults,
  submitSummary,
  classifySubmitError,
  needsMediaUserToken,
  requestOverrides,
} from '../lib/submit.js';
import { OPTIONS_KEY, optionDefaults, buildOverrides, applyOverrides, sanitizeOverrides, parseStoredOptions, describeOverrides } from '../lib/overrides.js';
import { readPref, writePref } from '../lib/prefs.js';
import { useApp, Card, Button, Notice } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';
import { LinksCard } from './download-links.js';
import { OptionsCard } from './download-options.js';
import { QualitySheet } from './download-quality.js';
import { ResultsCard, BookmarkletCard } from './download-results.js';

// Previews outlive the page, so coming back does not look links up again.
const LOOKUPS = new Map();

const isPrivate = (url) => {
  const link = parseLink(url);
  return Boolean(link && link.private);
};

const storage = () => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

const isMac = () => /mac|iphone|ipad/i.test((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '');

/** Text with `urls` appended on new lines, skipping links it already holds. */
function mergeLinks(text, urls) {
  const have = new Set(scanLinks(text).map((e) => e.url));
  const add = [];
  for (const u of urls) {
    const e = scanLinks(u)[0];
    const key = e ? e.url : u;
    if (have.has(key)) continue;
    have.add(key);
    add.push(key);
  }
  if (!add.length) return text;
  const head = text.replace(/\s+$/, '');
  return head ? `${head}\n${add.join('\n')}` : add.join('\n');
}

const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

export function DownloadPage({ route }) {
  const app = useApp();
  const incoming = newDownloadUrls(route.params);
  const [text, setText] = useState(() => mergeLinks('', incoming));
  const entries = useMemo(() => scanLinks(text), [text]);
  const settled = useDebounced(text, 350);
  const inputRef = useRef(null);

  // ?url= -> text: links that arrive in the address (the global paste on
  // this page, a bookmarklet, a shared link) join the text.
  const urlParam = route.params.url || '';
  useEffect(() => {
    if (incoming.length) setText((t) => mergeLinks(t, incoming));
  }, [urlParam]);

  // text -> ?url=: the links follow the text, so a reload keeps them.
  const routeRef = useRef(route);
  routeRef.current = route;
  useEffect(() => {
    const r = routeRef.current;
    const urls = scanLinks(settled).map((e) => e.url);
    if (sameList(urls, newDownloadUrls(r.params))) return;
    app.navigate(buildHash('new', { ...r.params, url: urls.join('\n') }, r.jobId), { replace: true });
  }, [settled]);

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
  const [cfg, setCfg] = useState({ loading: true, data: null, error: null });
  const [cfgRun, setCfgRun] = useState(0);
  useEffect(() => {
    let off = false;
    setCfg((c) => ({ ...c, loading: true }));
    getConfig().then(
      (data) => !off && setCfg({ loading: false, data, error: null }),
      (error) => !off && setCfg((c) => ({ loading: false, data: c.data, error })),
    );
    return () => {
      off = true;
    };
  }, [cfgRun, app.epoch]);
  const [hooks, setHooks] = useState({ loading: true, data: null, error: null });
  useEffect(() => {
    let off = false;
    getHooks().then(
      (data) => !off && setHooks({ loading: false, data, error: null }),
      (error) => !off && setHooks({ loading: false, data: null, error }),
    );
    return () => {
      off = true;
    };
  }, [app.epoch]);

  const download = cfg.data && cfg.data.config ? cfg.data.config.download : null;
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

  // Previews: the first PREVIEW_LIMIT previewable links, 4 at a time. A
  // private playlist is read with the typed token once it settles; answers
  // read with it leave the shared cache with the page, whose field starts
  // empty next time.
  const settledToken = useDebounced(token.trim(), 350);
  const tokenRef = useRef(settledToken);
  tokenRef.current = settledToken;
  const readWithToken = useRef(new Set());
  const [, setTick] = useState(0);
  const queue = useRef(null);
  if (!queue.current) {
    queue.current = createLookupQueue({
      lookup: (url, signal) => {
        const t = isPrivate(url) ? tokenRef.current : '';
        if (t) readWithToken.current.add(url);
        else readWithToken.current.delete(url);
        return lookupLink(url, { signal, mediaUserToken: t });
      },
      cache: LOOKUPS,
      onChange: () => setTick((n) => n + 1),
    });
  }
  useEffect(
    () => () => {
      queue.current.dispose();
      for (const url of readWithToken.current) LOOKUPS.delete(url);
    },
    [],
  );
  const previewable = (list) => list.filter((e) => e.link && PREVIEW_KINDS.includes(e.link.kind));
  const wanted = useMemo(() => previewable(scanLinks(settled)).slice(0, PREVIEW_LIMIT).map((e) => e.url), [settled]);
  useEffect(() => queue.current.want(wanted), [wanted.join('\n')]);
  // A changed token reads private playlists again.
  const tokenSeen = useRef(settledToken);
  useEffect(() => {
    if (tokenSeen.current === settledToken) return;
    tokenSeen.current = settledToken;
    queue.current.forget([...new Set([...wanted, ...LOOKUPS.keys()])].filter(isPrivate));
  }, [settledToken]);
  const previewSet = new Set(previewable(entries).slice(0, PREVIEW_LIMIT).map((e) => e.url));
  const skipped = Math.max(0, previewable(entries).length - PREVIEW_LIMIT);
  const statusOf = (url) => {
    const s = queue.current.status(url);
    return s.state === 'idle' && previewSet.has(url) ? { state: 'queued' } : s;
  };

  const hasToken = token.trim() !== '' || serverToken;
  const issueOf = (e) => linkIssue(e, e.link && PREVIEW_KINDS.includes(e.link.kind) ? queue.current.status(e.url) : null, { hasToken });
  const sendable = submittable(entries, issueOf);
  const flagged = entries.filter((e) => issueOf(e)).length;
  const lookupStatus = (url) => queue.current.status(url);
  const needsToken = needsMediaUserToken(entries.map((e) => e.url), lookupStatus);

  // Submit and results (without the token).
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const [refusal, setRefusal] = useState('');
  const [results, setResults] = useState({ rows: [], overrides: {} });
  const [retrying, setRetrying] = useState(() => new Set());
  const [quality, setQuality] = useState(null);

  useEffect(
    () => app.subscribeFeed((batch) => setResults((r) => (r.rows.length ? { ...r, rows: applyFeedToResults(r.rows, batch) } : r))),
    [],
  );
  // After the feed resyncs (an outage), the accepted jobs are read again.
  const rowsRef = useRef(results.rows);
  rowsRef.current = results.rows;
  const firstEpoch = useRef(app.epoch);
  useEffect(() => {
    if (app.epoch === firstEpoch.current) return undefined;
    let off = false;
    const ids = rowsRef.current.filter((r) => r.job && !r.deleted).map((r) => r.job.id).slice(0, 25);
    Promise.all(
      ids.map((id) =>
        getDownload(id).then(
          (body) => ({ type: 'download_upserted', job: body.job }),
          (err) => (err.status === 404 ? { type: 'download_deleted', job_id: id } : null),
        ),
      ),
    ).then((msgs) => !off && setResults((r) => ({ ...r, rows: applyFeedToResults(r.rows, msgs) })));
    return () => {
      off = true;
    };
  }, [app.epoch]);

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

  const submit = async () => {
    if (busy || sendable.length === 0) return;
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
    const queued = rows.some((r) => r.view.status === 'accepted');
    app.toast({ message: queued ? `Download started: ${summary}` : summary, role: queued ? 'success' : 'warning' });
    // Keep what was not handled: links not sent (flagged, or after a failed
    // chunk) and links the server did not take.
    const sent = new Set(pairs.map((p) => p.url));
    const kept = new Set(keptAfterSubmit(pairs));
    const rest = entries.filter((e) => !sent.has(e.url) || kept.has(e.url)).map((e) => e.url);
    setText(rest.join('\n'));
    app.navigate(buildHash('new', { ...route.params, url: rest.join('\n') }, route.jobId), { replace: true });
  };

  const retry = async (urls) => {
    setRetrying((s) => new Set([...s, ...urls]));
    const { pairs, fail } = await send(urls, results.overrides);
    setRetrying((s) => new Set([...s].filter((u) => !urls.includes(u))));
    if (fail) app.toast({ message: fail.message, role: 'danger' });
    if (!pairs.length) return;
    const fresh = new Map(resultRows(pairs).map((r) => [r.url, r]));
    setResults((r) => ({ ...r, rows: r.rows.map((row) => fresh.get(row.url) || row) }));
    const done = new Set(pairs.map((p) => p.url));
    for (const u of keptAfterSubmit(pairs)) done.delete(u);
    if (done.size) setText((t) => [...done].reduce(removeLink, t));
    app.toast({ message: submitSummary([...fresh.values()]), role: [...fresh.values()].some((r) => r.job) ? 'success' : 'warning' });
  };

  const removeEntry = (url) => {
    setText((t) => removeLink(t, url));
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
    ? [`${entries.length} ${entries.length === 1 ? 'link' : 'links'}`, `${sendable.length} to download`, flagged ? `${flagged} flagged` : ''].filter(Boolean).join(' · ')
    : 'No links yet';
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
            onInput=${(e) => setText(e.currentTarget.value)}
          ></textarea>
          <p id="new-input-hint" class="new-hint">
            Albums, songs, playlists, artists and stations. Links are found anywhere in the text; ${keys} downloads.
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
                  Stations and private playlists are read with your Apple Music media-user-token. It goes with this download only and is never
                  stored here.${serverToken ? ' Leave it empty to use the token saved on the server.' : ''}
                </p>
              </div>`
            : null}
          <div class="new-submit">
            <div class="new-submit-info">
              <span class="new-count">${countLine}</span>
              <button type="button" class="new-options-jump link" onClick=${jumpToOptions}>Options: ${optionsLine}</button>
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
      ${results.rows.length
        ? html`<${ResultsCard}
            rows=${results.rows}
            params=${route.params}
            busy=${retrying}
            onRetry=${retry}
            onClear=${() => setResults({ rows: [], overrides: {} })}
          />`
        : null}
      ${entries.length
        ? html`<${LinksCard}
            entries=${entries}
            statusOf=${statusOf}
            issueOf=${issueOf}
            skipped=${skipped}
            onRemove=${removeEntry}
            onRetry=${(url) => queue.current.retry(url)}
            onQuality=${(entry, data) => setQuality({ url: entry.url, title: data.title || entry.url })}
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
    ${quality ? html`<${QualitySheet} url=${quality.url} title=${quality.title} onClose=${() => setQuality(null)} />` : null}
  </div>`;
}
