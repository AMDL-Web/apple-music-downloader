// New download: the detected links. Each row shows at once what the link
// is (kind, storefront) from lib/links.js, then the lookup preview (cover,
// title, artist, tracks, year) when it arrives, and any reason it cannot be
// sent or needs attention. The page owns the lookup queue; rows read it.

import { html } from '../ui/preact.js';
import { artPalette, paletteStyle } from '../lib/format.js';
import { kindLabel, previewFacts, PREVIEW_LIMIT } from '../lib/lookup.js';
import { Card, Button, Icon, Chip, Artwork, Skeleton } from '../ui/components.js';

/** The link without its scheme, for the mono line under a title. */
const bare = (url) => url.replace(/^https?:\/\//i, '');

/**
 * Props: entries (scanLinks), statusOf(url) (the lookup status, with
 * 'idle' for a link past the preview limit), issueOf(entry), onRemove(url),
 * onRetry(url), onQuality(entry, data), skipped (links past the preview
 * limit that would otherwise be previewed).
 */
export function LinksCard({ entries, statusOf, issueOf, onRemove, onRetry, onQuality, skipped }) {
  const flagged = entries.filter((e) => issueOf(e)).length;
  const note = [`${entries.length} ${entries.length === 1 ? 'link' : 'links'}`, flagged ? `${flagged} flagged` : ''].filter(Boolean).join(' · ');
  return html`<${Card} section="links" className="card-flush new-links-card" title="Detected links" labelId="new-links-title" note=${note}>
    <ul class="new-links" aria-labelledby="new-links-title">
      ${entries.map(
        (e) => html`<${LinkRow}
          key=${e.url}
          entry=${e}
          status=${statusOf(e.url)}
          issue=${issueOf(e)}
          onRemove=${onRemove}
          onRetry=${onRetry}
          onQuality=${onQuality}
        />`,
      )}
    </ul>
    ${skipped > 0
      ? html`<p class="new-links-more">
          <${Icon} name="info" size="sm" />
          <span>Previews cover the first ${PREVIEW_LIMIT} links; ${skipped} more ${skipped === 1 ? 'is' : 'are'} downloaded without one.</span>
        </p>`
      : null}
  </${Card}>`;
}

function LinkRow({ entry, status, issue, onRemove, onRetry, onQuality }) {
  const link = entry.link;
  const data = status.state === 'ready' ? status.data : null;
  const facts = previewFacts(data);
  const palette = data ? artPalette(data) : null;
  const kind = link ? link.kind : 'invalid';
  const typeText = data && data.type ? kindLabel(data.type) : link ? kindLabel(link.kind, link.trackId) : 'Not Apple Music';
  const loading = status.state === 'loading' || status.state === 'queued';
  const name = facts ? facts.title : bare(entry.url);
  const cls = ['new-link', palette ? 'has-palette' : ''].filter(Boolean).join(' ');
  return html`<li
    class=${cls}
    data-kind=${kind}
    data-state=${link ? status.state : 'invalid'}
    data-issue=${issue ? issue.code : undefined}
    data-blocking=${issue && issue.blocking ? '' : undefined}
    aria-busy=${loading ? 'true' : undefined}
    style=${paletteStyle(palette)}
  >
    ${data
      ? html`<${Artwork} url=${data.artwork_url} bg=${palette && palette.bg} px=${160} className="art-48 new-link-art" />`
      : html`<div class="art art-48 new-link-art" aria-hidden="true">
          ${loading ? html`<${Skeleton} kind="tile" />` : html`<span class="art-glyph"><${Icon} name=${link ? 'music' : 'circle-slash'} /></span>`}
        </div>`}
    <div class="new-link-main">
      <div class="new-link-tags">
        <${Chip}>${typeText}</${Chip}>
        ${link && link.storefront ? html`<${Chip} title="Storefront">${link.storefront.toUpperCase()}</${Chip}>` : null}
        ${facts && facts.explicit ? html`<${Chip} title="Explicit">E</${Chip}>` : null}
        ${facts && facts.adm ? html`<${Chip} title="Apple Digital Master">ADM</${Chip}>` : null}
        ${loading ? html`<span class="new-link-loading">Looking up…</span>` : null}
      </div>
      <span class=${facts ? 'new-link-title' : 'new-link-title is-url'} dir="auto" title=${name}>${name}</span>
      ${facts && (facts.sub || facts.facts.length)
        ? html`<span class="new-link-sub">
            ${facts.sub ? html`<span class="new-link-artist" dir="auto">${facts.sub}</span>` : null}
            ${facts.album ? html`<span class="new-link-album" dir="auto">${facts.album}</span>` : null}
            ${facts.facts
              .filter((f) => f.key !== 'type')
              .map((f) => html`<span key=${f.key} class="new-link-fact" data-key=${f.key} dir=${f.key === 'genre' ? 'auto' : undefined}>${f.text}</span>`)}
          </span>`
        : null}
      ${facts ? html`<span class="new-link-url mono" title=${entry.url}>${bare(entry.url)}</span>` : null}
      ${loading && !facts ? html`<${Skeleton} kind="text" />` : null}
      ${status.state === 'idle' && link && !issue ? html`<span class="new-link-note">No preview</span>` : null}
      ${issue
        ? html`<span class="new-link-issue status-text" data-role=${issue.role} title=${issue.title || undefined}>
            <${Icon} name=${issue.role === 'danger' ? 'circle-x' : 'triangle-alert'} size="sm" />
            <span>${issue.message}${issue.blocking ? html`<span class="sr-only">. Not downloaded.</span>` : null}</span>
          </span>`
        : null}
    </div>
    <div class="new-link-actions">
      ${data
        ? html`<${Button} size="sm" icon="disc" className="new-link-quality" title="Check which qualities each track offers" onClick=${() => onQuality(entry, data)}>Qualities</${Button}>`
        : null}
      ${status.state === 'error' && issue && !issue.blocking
        ? html`<${Button} size="sm" icon="rotate" className="new-link-retry" onClick=${() => onRetry(entry.url)}>Retry</${Button}>`
        : null}
      <${Button} variant="ghost" size="sm" icon="x" className="new-link-remove" aria-label=${`Remove ${name}`} title="Remove from the list" onClick=${() => onRemove(entry.url)} />
    </div>
  </li>`;
}
