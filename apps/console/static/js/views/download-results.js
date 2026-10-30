// New download: what the last submit did, per link (accepted jobs as live
// job rows that open the drawer over this page, the rest with the reason
// and, where a later try may work, Retry), the state behind it, and the
// Browser shortcut (the bookmarklet).

import { html, useMemo, useState, useEffect, useRef, useCallback } from '../ui/preact.js';
import { getDownload } from '../api.js';
import { buildHash } from '../lib/route.js';
import { submitSummary, bookmarklet, applyFeedToResults, storedResults, parseStoredResults, seedJob, servedNote, RESULTS_KEY } from '../lib/submit.js';
import { readPref, writePref } from '../lib/prefs.js';
import { jobTitle, sentence } from '../lib/format.js';
import { useApp, Card, Button, LinkButton, Icon } from '../ui/components.js';
import { JobRow } from './queue.js';

const bare = (url) => url.replace(/^https?:\/\//i, '');

const session = () => {
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
};

// The last results outlive the page for the life of the tab, so leaving
// New download and coming back shows them again, and sessionStorage keeps
// them over a reload of the tab (where storage is refused, they last for
// the page view).
const EMPTY = { rows: [], overrides: {} };
let kept = null;
const keep = (next) => {
  kept = next;
  writePref(session(), RESULTS_KEY, storedResults(next));
  return next;
};

/** Accepted jobs read again when the page has not seen their feed batches; the rest wait for the feed. */
const REFRESH_MAX = 25;

/**
 * The page's results, {rows, overrides} (rows from lib/submit.js
 * resultRows), and a setter taking a value or an updater. Rows stay live
 * through the feed; restored rows and those left after a feed resync
 * (app.epoch) are read again with GET downloads/:id.
 */
export function useResults() {
  const app = useApp();
  if (kept === null) kept = readPref(session(), RESULTS_KEY, parseStoredResults) || EMPTY;
  const [results, setState] = useState(kept);
  const setResults = useCallback((next) => setState((cur) => keep(typeof next === 'function' ? next(cur) : next || EMPTY)), []);

  useEffect(
    () => app.subscribeFeed((batch) => setResults((r) => (r.rows.length ? { ...r, rows: applyFeedToResults(r.rows, batch) } : r))),
    [],
  );

  const rowsRef = useRef(results.rows);
  rowsRef.current = results.rows;
  const firstEpoch = useRef(null);
  useEffect(() => {
    // On mount only rows restored from an earlier visit are stale.
    const mount = firstEpoch.current === null;
    if (mount) firstEpoch.current = app.epoch;
    const ids = rowsRef.current.filter((r) => r.job && !r.deleted).map((r) => r.job.id).slice(0, REFRESH_MAX);
    if (ids.length === 0 || (!mount && app.epoch === firstEpoch.current)) return undefined;
    let off = false;
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

  return [results, setResults];
}

/**
 * Props: rows (lib/submit.js resultRows, kept live by the page; a job
 * shows its link's preview until Apple names it), params
 * (the page's hash params, kept when a row opens the drawer), busy (the
 * URLs being retried), onRetry(urls), onClear().
 */
export function ResultsCard({ rows, params, busy, onRetry, onClear }) {
  const retryable = rows.filter((r) => !r.job && r.view.retry).map((r) => r.url);
  const summary = submitSummary(rows);
  const actions = [
    retryable.length > 1
      ? html`<${Button} key="retry" size="sm" icon="rotate" className="new-results-retry" disabled=${busy.size > 0} onClick=${() => onRetry(retryable)}>Retry ${retryable.length}</${Button}>`
      : null,
    html`<${Button} key="clear" variant="ghost" size="sm" className="new-results-clear" onClick=${onClear}>Clear</${Button}>`,
  ];
  return html`<${Card} section="results" className="card-flush new-results-card" title="Submitted" labelId="new-results-title" actions=${actions}>
    <p id="new-results-summary" class="new-results-summary" role="status" tabindex="-1">${summary}</p>
    <ul class="job-list job-list-compact new-results" aria-labelledby="new-results-title">
      ${rows.map((r) => html`<li key=${r.url} class="new-result" data-status=${r.deleted ? 'deleted' : r.view.status}>
        ${r.job && !r.deleted
          ? html`<${JobRow} job=${seedJob(r.job, r.preview)} href=${buildHash('new', params, r.job.id)} />`
          : html`<${ResultRow} row=${r} params=${params} busy=${busy.has(r.url)} onRetry=${onRetry} />`}
      </li>`)}
    </ul>
  </${Card}>`;
}

function ResultRow({ row, params, busy, onRetry }) {
  const v = row.deleted ? { role: 'neutral', icon: 'circle-minus', label: 'Deleted', message: 'This job was deleted after it was queued' } : row.view;
  const served = servedNote(v.storefronts);
  const title = row.deleted && row.job ? jobTitle(row.job) : '';
  return html`<div class="new-result-row" data-role=${v.role}>
    <span class="new-result-icon"><${Icon} name=${v.icon} /></span>
    <span class="new-result-main">
      <span class="new-result-label">${v.label}</span>
      ${title ? html`<span class="new-result-title" dir="auto">${title}</span>` : null}
      <span class="new-result-url mono" title=${row.url}>${bare(row.url)}</span>
      ${v.message || served ? html`<span class="new-result-msg">${[sentence(v.message), served].filter(Boolean).join(' ')}</span>` : null}
      ${v.detail ? html`<span class="new-result-detail">Server: ${v.detail}</span>` : null}
    </span>
    <span class="new-result-actions">
      ${v.existingJobId ? html`<${LinkButton} size="sm" href=${buildHash('new', params, v.existingJobId)}>View job</${LinkButton}>` : null}
      ${!row.deleted && v.retry
        ? html`<${Button} size="sm" icon="rotate" aria-busy=${busy ? 'true' : undefined} disabled=${busy} onClick=${() => onRetry([row.url])}>${busy ? 'Retrying…' : 'Retry'}</${Button}>`
        : null}
    </span>
  </div>`;
}

/**
 * The bookmarklet as copyable text (never a link) in a closed "Browser
 * shortcut" disclosure under the form; hidden on a touch screen, where a
 * bookmarklet cannot be clicked. Opening it on an Apple Music page brings
 * that page here as #/new?url=. Built from this page's own address, so it
 * works under any path prefix.
 */
export function BrowserShortcut() {
  const app = useApp();
  const code = useMemo(() => bookmarklet(window.location.href), []);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      app.toast({ message: 'Bookmarklet copied', role: 'success' });
    } catch {
      const el = document.getElementById('new-bookmarklet-code');
      const sel = window.getSelection();
      if (el && sel) sel.selectAllChildren(el);
      app.toast({ message: 'The browser blocked copying. The code is selected: press Ctrl+C or ⌘C.', role: 'warning' });
    }
  };
  return html`<details class="new-shortcut">
    <summary class="new-shortcut-summary"><${Icon} name="chevron-down" size="sm" /><span>Browser shortcut</span></summary>
    <div class="new-shortcut-body">
      <div class="new-bookmarklet">
        <code id="new-bookmarklet-code" class="new-bookmarklet-code">${code}</code>
        <${Button} size="sm" icon="copy" className="new-bookmarklet-copy" onClick=${copy}>Copy</${Button}>
      </div>
      <ol class="new-steps">
        <li>Copy the code above.</li>
        <li>Add a bookmark in this browser and paste the code as its address (URL).</li>
        <li>On music.apple.com, open an album, song or playlist and click the bookmark.</li>
      </ol>
    </div>
  </details>`;
}
