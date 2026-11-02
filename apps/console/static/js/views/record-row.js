// One record row for the lists that show records (the Library's Albums and
// Playlists, Activity's Paused group): cover, title and artist, its flags,
// its state badge and when it last changed, as a job row is drawn (the
// .job-row frame), linking to the record's drawer.

import { html } from '../ui/preact.js';
import { artPalette, paletteStyle, artWidths } from '../lib/format.js';
import { recordTitle, recordSub, recordArt, recordPalette, recordView } from '../lib/records.js';
import { Artwork, Badge, RelTime, Icon } from '../ui/components.js';

const ROW_ART = artWidths(40, 48);

/**
 * Props: record, href, selected, onSelect(e) (selection mode: a plain click
 * selects), bare (no state badge, where the list already says it).
 */
export function RecordRow({ record, href, selected, onSelect, bare = false }) {
  const palette = artPalette(recordPalette(record));
  const { view, flags } = recordView(record);
  const sub = recordSub(record);
  const onClick = onSelect
    ? (e) => {
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.altKey) return;
        e.preventDefault();
        onSelect(e);
      }
    : undefined;
  return html`<a
    class=${palette ? 'job-row record-row has-palette' : 'job-row record-row'}
    href=${href}
    data-row
    data-progress="off"
    data-state=${record.state}
    data-selected=${selected ? '' : undefined}
    style=${paletteStyle(palette)}
    onClick=${onClick}
  >
    <${Artwork} url=${recordArt(record)} bg=${palette && palette.bg} px=${100} widths=${ROW_ART} sizes="48px" className="art-40" />
    <span class="job-main">
      <span class=${record.name ? 'job-title' : 'job-title is-unresolved'} dir="auto" title=${recordTitle(record)}>${recordTitle(record)}</span>
      ${sub || flags.length
        ? html`<span class="job-sub">
            ${sub ? html`<span dir="auto">${sub}</span>` : null}
            ${flags.map((f) => html`<span key=${f.key} class="record-flag" data-flag=${f.key}>${f.label}</span>`)}
          </span>`
        : null}
    </span>
    <span class="job-progress"></span>
    <span class="job-status">${bare ? null : html`<${Badge} view=${view} />`}</span>
    <span class="job-updated"><${RelTime} value=${record.updated_at} /></span>
    <span class="job-chevron"><${Icon} name="chevron-right" /></span>
  </a>`;
}
