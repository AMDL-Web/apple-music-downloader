// New download (#/new?url=&q=&artist=, #/new/:id for the drawer): paste
// links or search the catalog, choose the quality and submit. One column:
// the text box, the detected links or the search results, the submit bar
// (quality, Overwrite, the count, Download; it stays in view), then what
// the last submit did. The text box is the source of the links and the
// term and follows the address both ways; it lasts for the tab. Parts:
// download-config.js, download-address.js, download-lookup.js,
// download-links.js, download-search*.js, download-preset.js,
// download-quality.js, download-results.js; the logic is pure in lib/.

import { html, useState, useEffect, useRef, useMemo, useCallback } from '../ui/preact.js';
import { createDownload } from '../api.js';
import { newDownloadUrls, buildHash } from '../lib/route.js';
import { scanLinks, removeLink, mergeLinks, duplicateCount } from '../lib/links.js';
import { chunk, pairResults, handledUrls, resultRows, submitSummary, classifySubmitError, submittable, downloadLabel, countLine, servedRegions } from '../lib/submit.js';
import { optionDefaults, presetOverrides, presetValues, qualityText, probedQuality } from '../lib/overrides.js';
import { isPicked, qualityOverview } from '../lib/lookup.js';
import { searchTerm, searchCountLine, SEARCH_DEBOUNCE_MS } from '../lib/search.js';
import { useApp, Card } from '../ui/components.js';
import { useDebounced } from '../ui/hooks.js';
import { useStatus } from '../ui/system-status.js';
import { ConfirmDialog } from '../ui/dialogs.js';
import { useServerConfig } from './download-config.js';
import { useLookups } from './download-lookup.js';
import { LinksCard } from './download-links.js';
import { QualityControls, CustomSheet } from './download-preset.js';
import { SubmitBar, PasteButton, useBarHeight } from './download-bar.js';
import { QualitySheet, PROBES } from './download-quality.js';
import { ResultsCard, BrowserShortcut, useResults } from './download-results.js';
import { SearchCard } from './download-search.js';
import { useSearch, useHitActions, toResults } from './download-search-state.js';
import { useAddressSync } from './download-address.js';

// The text box outlives the page for the life of the tab, so leaving New
// download through the sidebar and coming back keeps a draft, notes and
// links that are not Apple Music included (?url= keeps only the links).
let draft = '';
// The quality choice lasts for the tab too, so the next album can follow
// with the same one: a PRESETS id or 'custom' with its values.
let chosen = { preset: 'follow', custom: null };

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
  // Words and no link: the box searches the catalog.
  const { term, tooLong } = searchTerm(text, entries);
  const searching = Boolean(term) || tooLong;
  const artistId = term ? route.params.artist || '' : '';
  const search = useSearch({ term, debounced: useDebounced(term, SEARCH_DEBOUNCE_MS), artistId, epoch: app.epoch, subscribeFeed: app.subscribeFeed });
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

  // The address and the text box follow each other (download-address.js).
  const routeRef = useAddressSync({ app, route, incoming, text, settled, storefront, edit });

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

  // Server defaults.
  const { cfg, setCfgRun } = useServerConfig(app.epoch);

  const download = cfg.data && cfg.data.config ? cfg.data.config.download : null;
  const trackMode = cfg.data && cfg.data.config && cfg.data.config.catalog ? cfg.data.config.catalog.album_track_url_mode || '' : null;
  const defaults = useMemo(() => (download ? optionDefaults(download) : null), [JSON.stringify(download)]);
  // Whether Settings holds a media-user-token; null until the config loads.
  const serverToken = cfg.data && cfg.data.config && cfg.data.config.catalog ? Boolean(cfg.data.config.catalog.media_user_token) : null;

  // The quality preset and the Overwrite switch (null: as Settings has it).
  const [choice, setChoiceState] = useState(chosen);
  const setChoice = (next) => {
    chosen = next;
    setChoiceState(next);
    setRefusal('');
  };
  const [overwrite, setOverwrite] = useState(null);
  const [customOpen, setCustomOpen] = useState(false);
  const overrides = useMemo(
    () => (defaults ? presetOverrides(defaults, choice.preset, choice.custom, overwrite) : {}),
    [defaults, choice, overwrite],
  );
  const qualitySelect = () => document.getElementById('new-quality');
  const closeCustom = () => {
    setCustomOpen(false);
    requestAnimationFrame(() => {
      const el = qualitySelect();
      if (el && document.activeElement === document.body) el.focus({ preventScroll: true });
    });
  };

  // The storefronts the wrapper serves, to flag a link before the submit
  // refuses it; null (flag nothing) while unknown or in simulate mode.
  const { wrapper } = useStatus(['wrapper']);
  const regions = servedRegions(wrapper, app.simulate);

  const { statusOf, issueOf, sendable: allowed, skipped, queue, priorOf, forgetPrior } = useLookups({
    settled,
    entries,
    serverToken,
    regions,
    trackMode,
    epoch: app.epoch,
    storefront,
    subscribeFeed: app.subscribeFeed,
  });
  const jobHref = (id) => buildHash('new', route.params, id);
  // The checked rows are what the submit sends: a row already in the
  // library or the queue starts unchecked unless the person checks it.
  const [picks, setPicks] = useState(() => new Map());
  const onPick = (url, on) => setPicks((m) => new Map(m).set(url, on));
  const sendable = allowed.filter((u) => isPicked(u, priorOf(u), picks));
  // What the chosen preset resolves to, for the Will download lines.
  const chosenValues = defaults ? presetValues(defaults, choice.preset, choice.custom) : null;
  const overwriteOn = overwrite === null ? Boolean(defaults && defaults.force_overwrite) : overwrite;
  const previewOf = (url) => {
    const st = statusOf(url);
    return st.state === 'ready' ? st.data : null;
  };

  // Submit and results (kept for the life of the tab).
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState('');
  const [results, setResults] = useResults();
  const [retrying, setRetrying] = useState(() => new Set());
  const [quality, setQuality] = useState(null);
  // Bumped when the quality sheet has read a link's qualities (PROBES).
  const [, setProbed] = useState(0);
  const [confirm, setConfirm] = useState(null);

  /** Sends `urls` in chunks the backend accepts; returns {pairs, failure}. */
  const send = async (urls, ov) => {
    const pairs = [];
    let fail = null;
    for (const part of chunk(urls)) {
      try {
        const resp = await createDownload(Object.keys(ov).length ? { urls: part, overrides: ov } : { urls: part });
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
  /**
   * Shows what a submit did: its failure under the bar, its rows in the
   * results (after the rows already there when `keep`, a search hit's
   * download), a toast; returns false when nothing was submitted.
   */
  const report = (pairs, fail, previewFor, keep = false) => {
    if (fail) {
      if (fail.kind === 'overrides') setRefusal(fail.message);
      else setFailure(fail.message);
    }
    if (!pairs.length) {
      app.toast({ message: fail ? fail.message : 'Nothing was submitted', role: 'danger' });
      return false;
    }
    const rows = resultRows(pairs, previewFor);
    setResults((r) => ({ rows: keep ? [...rows, ...r.rows.filter((x) => !rows.some((n) => n.url === x.url))] : rows, overrides }));
    const ids = rows.filter((r) => r.job).map((r) => r.job.id);
    app.toast({
      message: submitSummary(rows),
      role: ids.length ? 'success' : 'warning',
      action: ids.length ? { label: 'View in Queue', hash: '#/queue' } : null,
    });
    if (ids.length && app.watchJobs) app.watchJobs(ids);
    return true;
  };

  const submit = async (confirmed = false) => {
    if (busy || sendable.length === 0) return;
    if (artists.length && !confirmed) {
      setConfirm({
        names: artists.map((u) => {
          const s = statusOf(u);
          return (s.state === 'ready' && s.data && s.data.title) || u.replace(/^https?:\/\//i, '');
        }),
        label: `Download ${sendable.length}`,
        run: () => submit(true),
      });
      return;
    }
    setConfirm(null);
    setBusy(true);
    setFailure('');
    setRefusal('');
    const { pairs, fail } = await send(sendable, overrides);
    setBusy(false);
    if (!report(pairs, fail, previewOf)) return;
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

  const hits = useHitActions({ app, route, term, artistId, search, send, report, overrides, setConfirm, clearNotes: () => (setFailure(''), setRefusal('')) });

  const retry = async (urls) => {
    setRetrying((s) => new Set([...s, ...urls]));
    const { pairs, fail } = await send(urls, results.overrides);
    setRetrying((s) => new Set([...s].filter((u) => !urls.includes(u))));
    if (fail) app.toast({ message: fail.message, role: 'danger' });
    if (!pairs.length) return;
    const fresh = new Map(resultRows(pairs, previewOf).map((r) => [r.url, r]));
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
      return;
    }
    if (term) toResults(e, term, search);
  };

  // The count line is announced (role=status), so it follows the settled
  // text, not every keystroke.
  const settledEntries = useMemo(() => scanLinks(settled, storefront), [settled, storefront]);
  const settledSend = submittable(settledEntries, issueOf);
  const priorState = (url) => (priorOf(url) || {}).state;
  const linkCount = countLine({
    links: settledEntries.length,
    fresh: settledSend.filter((u) => !priorOf(u)).length,
    library: settledSend.filter((u) => priorState(u) === 'done').length,
    queued: settledSend.filter((u) => priorState(u) === 'active').length,
    blocked: settledEntries.filter((e) => (issueOf(e) || {}).blocking).length,
    warned: settledEntries.filter((e) => {
      const i = issueOf(e);
      return i && !i.blocking;
    }).length,
  });
  const kindOf = (url) => {
    const st = statusOf(url);
    if (st.state === 'ready' && st.data && st.data.type) return st.data.type;
    const e = entries.find((x) => x.url === url);
    const link = e && e.link;
    return link ? (link.kind === 'album' && link.trackId && trackMode !== 'album' ? 'song' : link.kind) : 'item';
  };
  const count = searching ? searchCountLine(search.search, search.artist.status !== 'idle' ? search.artist : null) : linkCount;
  const label = busy ? 'Submitting…' : searching ? 'Choose a result' : downloadLabel(sendable.map(kindOf), entries.length > 0, allowed.length > 0);

  // Under 640 px the submit bar is fixed above the tab bar; the page keeps
  // room for it below the last row.
  const [barRef, barH] = useBarHeight();


  return html`<div class="page page-new" style=${barH ? { '--new-bar-h': `${barH}px` } : undefined}>
    <${Card}
      section="compose"
      className="new-compose"
      title="Search or paste links"
      labelId="new-compose-title"
      actions=${html`<${PasteButton} storefront=${storefront} onLinks=${(urls) => edit((t) => mergeLinks(t, urls, storefront))} />`}
    >
      <textarea
        id="new-input"
        class="new-input"
        ref=${inputRef}
        rows="6"
        value=${text}
        placeholder="Search for a song, album or artist, or paste Apple Music links"
        aria-labelledby="new-compose-title"
        aria-keyshortcuts=${term ? 'ArrowDown Enter' : 'Control+Enter Meta+Enter'}
        autocomplete="off"
        autocapitalize="off"
        spellcheck=${false}
        onInput=${(e) => edit(e.currentTarget.value)}
        onKeyDown=${onKeyDown}
      ></textarea>
    </${Card}>
    ${searching
      ? html`<${SearchCard}
          s=${search}
          term=${term}
          tooLong=${tooLong}
          inputRef=${inputRef}
          openHit=${hits.openedHit}
          focusArtist=${hits.focusArtist}
          row=${{ busy: hits.busy, jobHref, onDownload: hits.download, onOpenArtist: hits.openArtist, priorOf: search.priorOf }}
          onBack=${hits.closeArtist}
          onDownloadAll=${hits.download}
        />`
      : null}
    ${entries.length
      ? html`<${LinksCard}
          entries=${entries}
          statusOf=${statusOf}
          issueOf=${issueOf}
          priorOf=${priorOf}
          picked=${(url) => isPicked(url, priorOf(url), picks)}
          skipped=${skipped}
          dupes=${dupes}
          asks=${chosenValues ? qualityText(chosenValues) : ''}
          row=${{
            jobHref,
            onRemove: removeEntry,
            onRetry: (url) => queue.current.retry(url),
            onQuality: (entry, data) => setQuality({ url: entry.url, title: data.title || entry.url, tracks: data.track_count }),
            onPick,
            overwrite: overwriteOn,
            storefront,
            probed: (url) => (chosenValues && PROBES.has(url) ? probedQuality(chosenValues, qualityOverview(PROBES.get(url))) : null),
          }}
        />`
      : null}
    <${SubmitBar}
      barRef=${barRef}
      count=${count}
      controls=${html`<${QualityControls}
        defaults=${defaults}
        preset=${choice.preset}
        custom=${choice.custom}
        asks=${chosenValues ? qualityText(chosenValues) : ''}
        onPreset=${(preset) => setChoice({ ...choice, preset })}
        onCustom=${() => setCustomOpen(true)}
        overwrite=${overwriteOn}
        onOverwrite=${(v) => setOverwrite(defaults && v === defaults.force_overwrite ? null : v)}
      />`}
      label=${label}
      disabled=${busy || sendable.length === 0}
      busy=${busy}
      title=${sendable.length === 0 && entries.length ? 'No checked link can be downloaded' : undefined}
      onSubmit=${() => submit()}
      failure=${failure}
      refusal=${refusal}
      onCustom=${() => setCustomOpen(true)}
      cfgError=${Boolean(cfg.error && !defaults)}
      onRetryCfg=${() => setCfgRun((n) => n + 1)}
    />
    ${results.rows.length
      ? html`<${ResultsCard}
          rows=${results.rows}
          params=${route.params}
          busy=${retrying}
          onRetry=${retry}
          onClear=${clearResults}
        />`
      : null}
    <${BrowserShortcut} />
    ${customOpen && defaults
      ? html`<${CustomSheet}
          defaults=${defaults}
          start=${presetValues(defaults, choice.preset, choice.custom)}
          onDone=${(custom) => {
            setChoice({ preset: 'custom', custom });
            closeCustom();
          }}
          onFollow=${() => {
            setChoice({ preset: 'follow', custom: choice.custom });
            closeCustom();
          }}
          onClose=${closeCustom}
        />`
      : null}
    ${quality
      ? html`<${QualitySheet}
          url=${quality.url}
          title=${quality.title}
          tracks=${quality.tracks}
          onProbed=${() => setProbed((n) => n + 1)}
          onClose=${() => setQuality(null)}
        />`
      : null}
    ${confirm
      ? html`<${ConfirmDialog}
          title=${confirm.names.length === 1 ? `Download everything by ${confirm.names[0]}?` : `Download everything by ${confirm.names.length} artists?`}
          confirmLabel=${confirm.label}
          onConfirm=${() => {
            setConfirm(null);
            confirm.run();
          }}
          onCancel=${() => setConfirm(null)}
        >
          <p>An artist link queues the artist's whole discography as one job: every album, often hundreds of tracks or more. You can cancel it from the queue.</p>
          ${confirm.names.length > 1 ? html`<p dir="auto">${confirm.names.join(', ')}</p>` : null}
        </${ConfirmDialog}>`
      : null}
  </div>`;
}
