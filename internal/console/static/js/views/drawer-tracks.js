// The job drawer's per-job sections: the artwork palette swatches, the track
// strip and the track list with its stage meters.

import { html } from '../ui/preact.js';
import { trackLength, bytes, codecLabel, isHiRes } from '../lib/format.js';
import { itemStatusView } from '../lib/status.js';
import { StatusText, StageTrack, Icon, Chip } from '../ui/components.js';

function Swatch({ hex, label }) {
  if (!hex || !/^[0-9a-f]{6}$/i.test(hex)) return null;
  return html`<li class="swatch" title=${`${label} ${hex}`}>
    <span class="swatch-dot" style=${{ '--swatch': `#${hex}` }}></span>
    <span class="swatch-label mono">${hex}</span>
  </li>`;
}

export function Palette({ job }) {
  if (!job.artwork_bg_color) return null;
  const still = ['bg_color', 'text_color1', 'text_color2', 'text_color3', 'text_color4'];
  const names = ['Background', 'Text 1', 'Text 2', 'Text 3', 'Text 4'];
  return html`<section class="drawer-section" aria-labelledby="palette-title">
    <h3 id="palette-title" class="section-title">Artwork palette</h3>
    <ul class="swatches" aria-label="Cover colors">
      ${still.map((k, i) => html`<${Swatch} key=${k} hex=${job[`artwork_${k}`]} label=${names[i]} />`)}
    </ul>
    ${job.motion_artwork_bg_color
      ? html`<p class="small muted">Motion cover</p>
          <ul class="swatches" aria-label="Motion cover colors">
            ${still.map((k, i) => html`<${Swatch} key=${k} hex=${job[`motion_artwork_${k}`]} label=${names[i]} />`)}
          </ul>`
      : null}
  </section>`;
}

export function TrackStrip({ items }) {
  return html`<div class="track-strip" aria-hidden="true">
    ${items.map((it) => {
      const v = itemStatusView(it);
      return html`<span key=${it.id} class="track-block" data-role=${v.role} title=${`${it.index}. ${it.title || ''}: ${v.label}`}></span>`;
    })}
  </div>`;
}

export function TrackList({ items }) {
  return html`<ol class="tracks">
    ${items.map((it) => {
      const v = itemStatusView(it);
      const codec = codecLabel(it);
      return html`<li key=${it.id} class="track" data-row tabindex="-1">
        <span class="track-index num">${it.index}</span>
        <span class="track-main">
          <span class="track-title" dir="auto" title=${it.title}>${it.title || html`<span class="muted">Untitled</span>`}</span>
          ${it.artist ? html`<span class="track-artist" dir="auto">${it.artist}</span>` : null}
        </span>
        <span class="track-len num">${trackLength(it.duration_ms)}</span>
        <span class="track-codec">${codec ? html`<${Chip} title=${isHiRes(it) ? 'High resolution' : undefined}>${codec}</${Chip}>` : null}</span>
        <span class="track-stages"><${StageTrack} item=${it} /></span>
        <span class="track-status">
          <${StatusText} view=${v} />
          ${v.retry ? html`<span class="badge" data-role="warning"><${Icon} name="rotate" size="sm" /><span class="badge-label">${v.retry}</span></span>` : null}
        </span>
        <span class="track-size num">${it.file_size > 0 ? bytes(it.file_size) : ''}</span>
        ${it.status === 'failed' && it.error
          ? html`<p class="track-error">${it.error}</p>`
          : it.status_message && !['completed', 'skipped_existing'].includes(it.status)
            ? html`<p class="track-note">${it.status_message}</p>`
            : null}
      </li>`;
    })}
  </ol>`;
}
