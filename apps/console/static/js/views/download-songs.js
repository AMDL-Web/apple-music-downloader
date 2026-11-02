// New download: the search's songs, rows on a grouped surface (a 48 px
// cover, the title with E and quality tags, one meta line, a note when the
// item is in the library or the queue, Download), and their placeholders.
// download-search.js lays them out.

import { html } from '../ui/preact.js';
import { artPalette, paletteStyle, artWidths } from '../lib/format.js';
import { hitView } from '../lib/search.js';
import { Button, Artwork, Skeleton } from '../ui/components.js';
import { MarkNote, markHref } from './download-mark.js';

const HIT_ART = artWidths(48);

/**
 * A song: a row on the Songs surface. Download is its one focus stop
 * (data-hit-primary). While it is sent, and once the song is in the queue,
 * it is aria-disabled rather than disabled, so focus and the arrow keys
 * stay on it. A song its album's record has done (`resource`), or one this
 * page watched download, downloads again instead.
 */
export function HitRow({ hit, row }) {
  const v = hitView(hit);
  const palette = artPalette(hit);
  const mark = row.markOf(hit);
  const busy = row.busy.has(hit.url);
  const queued = Boolean(mark && mark.state === 'active');
  const again = Boolean(mark && mark.state === 'complete');
  const cls = ['new-hit', palette ? 'has-palette' : ''].filter(Boolean).join(' ');
  const meta = v.facts.map((f) => f.text).join(' · ');
  return html`<li class=${cls} data-kind=${hit.type} data-id=${hit.id} data-prior=${mark ? mark.state : undefined} style=${paletteStyle(palette)}>
    <${Artwork} url=${hit.artwork_url} bg=${palette && palette.bg} px=${100} widths=${HIT_ART} sizes="48px" className="art-48 new-hit-art" />
    <div class="new-hit-main">
      <span class="new-hit-head">
        <span class="new-hit-title" dir="auto" title=${v.title}>${v.title}</span>
        ${v.explicit ? html`<span class="new-link-explicit" title="Explicit" aria-label="Explicit">E</span>` : null}
        ${v.quality.map((q) => html`<span key=${q} class="chip new-hit-quality">${q}</span>`)}
      </span>
      <span class="new-hit-sub" title=${meta}>
        ${v.facts.map((f) => html`<span key=${f.key} class="new-hit-fact" data-key=${f.key} dir=${f.auto ? 'auto' : undefined}>${f.text}</span>`)}
      </span>
      ${mark ? html`<${MarkNote} mark=${mark} href=${markHref(mark, row.jobHref)} focusable=${false} />` : null}
    </div>
    <${Button}
      size="sm"
      variant="ghost"
      icon=${again ? 'rotate' : 'download'}
      className="new-hit-download"
      data-hit-primary
      aria-disabled=${busy || queued ? 'true' : undefined}
      aria-busy=${busy ? 'true' : undefined}
      aria-label=${queued ? `${v.title} is in the queue` : again ? `Download ${v.title} again` : `Download ${v.title}`}
      title=${queued ? 'Already in the queue' : again ? 'Download again' : 'Download'}
      onClick=${() => busy || queued || (again ? row.onAgain(hit, mark) : row.onDownload(hit))}
    />
  </li>`;
}

/** Placeholder rows while the first answer loads. */
export function SkeletonHits({ n }) {
  return html`<ul class="new-hits" aria-hidden="true">
    ${Array.from(
      { length: n },
      (_, i) => html`<li key=${i} class="new-hit is-skel">
        <span class="art art-48 new-hit-art"><${Skeleton} kind="tile" /></span>
        <span class="new-hit-main">
          <span class="new-hit-head"><span class="skel skel-lh new-hit-skel-title"></span></span>
          <span class="new-hit-sub"><span class="skel skel-lh skel-short"></span></span>
        </span>
      </li>`,
    )}
  </ul>`;
}
