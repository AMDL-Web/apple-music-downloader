// New download: what the last submit did, per link (accepted jobs as live
// job rows that open the drawer over this page, the rest with the reason
// and, where a later try may work, Retry), and the bookmarklet card.

import { html, useMemo } from '../ui/preact.js';
import { buildHash } from '../lib/route.js';
import { submitSummary, bookmarklet } from '../lib/submit.js';
import { useApp, Card, Button, LinkButton, Icon } from '../ui/components.js';
import { JobRow } from './queue.js';

const bare = (url) => url.replace(/^https?:\/\//i, '');

/**
 * Props: rows (lib/submit.js resultRows, kept live by the page), params
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
    <p class="new-results-summary" role="status">${summary}</p>
    <ul class="job-list job-list-compact new-results" aria-labelledby="new-results-title">
      ${rows.map((r) => html`<li key=${r.url} class="new-result" data-status=${r.deleted ? 'deleted' : r.view.status}>
        ${r.job && !r.deleted
          ? html`<${JobRow} job=${r.job} href=${buildHash('new', params, r.job.id)} />`
          : html`<${ResultRow} row=${r} params=${params} busy=${busy.has(r.url)} onRetry=${onRetry} />`}
      </li>`)}
    </ul>
  </${Card}>`;
}

function ResultRow({ row, params, busy, onRetry }) {
  const v = row.deleted ? { role: 'neutral', icon: 'circle-minus', label: 'Deleted', message: 'This job was deleted after it was queued.' } : row.view;
  const served = v.storefronts && v.storefronts.length ? `It serves ${v.storefronts.map((s) => s.toUpperCase()).join(', ')}.` : '';
  return html`<div class="new-result-row" data-role=${v.role}>
    <span class="new-result-icon"><${Icon} name=${v.icon} /></span>
    <span class="new-result-main">
      <span class="new-result-label">${v.label}</span>
      <span class="new-result-url mono" title=${row.url}>${bare(row.url)}</span>
      ${v.message || served ? html`<span class="new-result-msg">${[v.message ? `${v.message}.` : '', served].filter(Boolean).join(' ')}</span>` : null}
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
 * The bookmarklet as copyable text (never a link): opening it on an Apple
 * Music page brings that page here as #/new?url=. Built from this page's
 * own address, so it works under any path prefix.
 */
export function BookmarkletCard() {
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
  return html`<${Card} section="bookmarklet" className="new-bookmarklet-card" title="One-click from Apple Music" labelId="new-bookmarklet-title">
    <p class="new-hint">A bookmark that sends the Apple Music page you are on to this page.</p>
    <div class="new-bookmarklet">
      <code id="new-bookmarklet-code" class="new-bookmarklet-code">${code}</code>
      <${Button} size="sm" icon="copy" className="new-bookmarklet-copy" onClick=${copy}>Copy</${Button}>
    </div>
    <ol class="new-steps">
      <li>Copy the code above.</li>
      <li>Add a bookmark in this browser and paste the code as its address (URL).</li>
      <li>On music.apple.com, open an album, song or playlist and click the bookmark.</li>
    </ol>
  </${Card}>`;
}
