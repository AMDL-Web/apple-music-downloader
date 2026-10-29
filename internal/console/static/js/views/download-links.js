// New download: the detected links. Each row shows at once what the link
// is (kind, storefront) from lib/links.js, then the lookup preview (cover,
// title, artist, tracks, year) when it arrives, any reason it cannot be
// sent or needs attention, and an earlier job of the same item. The page
// owns the lookup queues; rows read them. The first ROWS_SHOWN rows are
// listed, plus every one that needs a look (an issue, an earlier job still
// queued or running), until Show all.

import { html, useState } from '../ui/preact.js';
import { artPalette, paletteStyle, shortDate } from '../lib/format.js';
import { kindLabel, previewFacts, needsAttention, ROWS_SHOWN, LOOKUP_LIMIT } from '../lib/lookup.js';
import { Card, Button, Icon, Chip, Artwork, Skeleton } from '../ui/components.js';

/** The link without its scheme, for the mono line under a title. */
const bare = (url) => url.replace(/^https?:\/\//i, '');

// Types the quality probe is quick and meaningful for: one release. An
// artist or station would read every track of a whole catalog.
const QUALITY_TYPES = ['album', 'song'];

/**
 * Props: entries (scanLinks), statusOf(url) (the lookup status, with
 * 'idle' for a link past the lookup limit), issueOf(entry), priorOf(url)
 * (an earlier job, lib/lookup.js priorJob), jobHref(id), onRemove(url),
 * onRetry(url), onQuality(entry, data), skipped (links past the lookup
 * limit that would otherwise be looked up), dupes (repeats merged).
 */
export function LinksCard({ entries, statusOf, issueOf, priorOf, jobHref, onRemove, onRetry, onQuality, skipped, dupes }) {
  const [all, setAll] = useState(false);
  const blocked = (e) => Boolean((issueOf(e) || {}).blocking);
  const flagged = entries.filter(blocked).length;
  const note = [
    `${entries.length} ${entries.length === 1 ? 'link' : 'links'}`,
    dupes ? `${dupes} ${dupes === 1 ? 'duplicate' : 'duplicates'} merged` : '',
    flagged ? `${flagged} flagged` : '',
  ]
    .filter(Boolean)
    .join(' · ');
  const shown = all ? entries : entries.filter((e, i) => i < ROWS_SHOWN || needsAttention(issueOf(e), priorOf(e.url)));
  const hidden = entries.length - shown.length;
  // Hidden rows only have, at most, a quiet "Downloaded <date>" note.
  const hiddenPrior = all ? 0 : entries.filter((e) => !shown.includes(e) && priorOf(e.url)).length;
  const data = (e) => {
    const s = statusOf(e.url);
    return s.state === 'ready' ? s.data : null;
  };
  const facts = shown.map((e) => previewFacts(data(e)));
  const sf = (shown.find((e) => e.link && e.link.storefront) || {}).link;
  const legend = [
    sf ? html`<span key="sf" class="new-legend-item"><${Chip}>${sf.storefront.toUpperCase()}</${Chip}> storefront, the Apple Music country a link is read in</span>` : null,
    facts.some((f) => f && f.explicit) ? html`<span key="e" class="new-legend-item"><${Chip}>E</${Chip}> explicit</span>` : null,
    facts.some((f) => f && f.adm) ? html`<span key="adm" class="new-legend-item"><${Chip}>ADM</${Chip}> Apple Digital Master, mastered for Apple's high-resolution formats</span>` : null,
  ].filter(Boolean);
  return html`<${Card} section="links" className="card-flush new-links-card" title="Detected links" labelId="new-links-title" note=${note}>
    <ul class="new-links" aria-labelledby="new-links-title">
      ${shown.map(
        (e) => html`<${LinkRow}
          key=${e.url}
          entry=${e}
          status=${statusOf(e.url)}
          issue=${issueOf(e)}
          prior=${priorOf(e.url)}
          jobHref=${jobHref}
          onRemove=${onRemove}
          onRetry=${onRetry}
          onQuality=${onQuality}
        />`,
      )}
    </ul>
    ${hidden > 0 || (all && entries.length > ROWS_SHOWN)
      ? html`<div class="new-links-all">
          <${Button} size="sm" icon=${all ? 'chevron-up' : 'chevron-down'} className="new-links-toggle" aria-expanded=${String(all)} onClick=${() => setAll(!all)}
            >${all ? `Show the first ${ROWS_SHOWN}` : `Show all ${entries.length} links`}</${Button}>
          ${!all ? html`<span class="new-hint">${hidden} more not listed, ${hiddenPrior ? `${hiddenPrior} downloaded before` : 'none flagged so far'}.</span>` : null}
        </div>`
      : null}
    ${skipped > 0
      ? html`<p class="new-links-more">
          <${Icon} name="info" size="sm" />
          <span>Links are checked up to the first ${LOOKUP_LIMIT}; ${skipped} more ${skipped === 1 ? 'is' : 'are'} downloaded without a check.</span>
        </p>`
      : null}
    ${legend.length ? html`<p class="new-links-legend">${legend}</p>` : null}
  </${Card}>`;
}

/** The quiet note for an earlier job: in the queue now, or when it last finished. */
function PriorNote({ prior, href }) {
  const active = prior.state === 'active';
  const text = active ? (prior.job.status === 'running' ? 'Downloading now' : 'Already in the queue') : `Downloaded ${shortDate(prior.job.finished_at) || 'before'}`;
  return html`<a class="new-link-prior" data-state=${prior.state} href=${href} title="Open that job">
    <${Icon} name=${active ? 'clock' : 'circle-check'} size="sm" />
    <span>${text}</span>
  </a>`;
}

function LinkRow({ entry, status, issue, prior, jobHref, onRemove, onRetry, onQuality }) {
  const link = entry.link;
  const data = status.state === 'ready' ? status.data : null;
  const facts = previewFacts(data);
  const palette = data ? artPalette(data) : null;
  const kind = link ? link.kind : 'invalid';
  const typeText = data && data.type ? kindLabel(data.type) : link ? kindLabel(link.kind, link.trackId) : 'Not Apple Music';
  const loading = status.state === 'loading' || status.state === 'queued';
  const name = facts ? facts.title : bare(entry.url);
  const cls = ['new-link', palette ? 'has-palette' : ''].filter(Boolean).join(' ');
  const artist = kind === 'artist' && !(issue && issue.blocking);
  const sfTitle = link && link.noStorefront ? 'Storefront taken from the other links, since this link has none' : 'Storefront';
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
        ${link && link.storefront ? html`<${Chip} title=${sfTitle}>${link.storefront.toUpperCase()}</${Chip}>` : null}
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
      ${status.state === 'idle' && link && !issue ? html`<span class="new-link-note">Not checked</span>` : null}
      ${artist ? html`<span class="new-link-note" data-note="artist">Downloads every album by this artist, the whole discography</span>` : null}
      ${link && link.noStorefront && link.storefront && !issue
        ? html`<span class="new-link-note" data-note="storefront">No storefront in the link: read as ${link.storefront.toUpperCase()}, like the other links</span>`
        : null}
      ${prior && !(issue && issue.blocking) ? html`<${PriorNote} prior=${prior} href=${jobHref(prior.job.id)} />` : null}
      ${issue
        ? html`<span class="new-link-issue status-text" data-role=${issue.role} title=${issue.title || undefined}>
            <${Icon} name=${issue.role === 'danger' ? 'circle-x' : 'triangle-alert'} size="sm" />
            <span>${issue.message}${issue.blocking ? html`<span class="sr-only">. Not downloaded.</span>` : null}${issue.href
              ? html`. <a class="link new-link-fix" href=${issue.href}>${issue.action}</a>`
              : null}</span>
          </span>`
        : null}
    </div>
    <div class="new-link-actions">
      ${data && QUALITY_TYPES.includes(data.type)
        ? html`<${Button} size="sm" icon="disc" className="new-link-quality" title="Check which qualities each track offers" onClick=${() => onQuality(entry, data)}>Qualities</${Button}>`
        : null}
      ${status.state === 'error' && issue && !issue.blocking
        ? html`<${Button} size="sm" icon="rotate" className="new-link-retry" onClick=${() => onRetry(entry.url)}>Retry</${Button}>`
        : null}
      <${Button} variant="ghost" size="sm" icon="x" className="new-link-remove" aria-label=${`Remove ${name}`} title="Remove from the list" onClick=${() => onRemove(entry.url)} />
    </div>
  </li>`;
}
