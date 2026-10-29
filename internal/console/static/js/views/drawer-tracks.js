// The job drawer's track sections: the track strip, the key to the codec chips
// and the track list. A track is one line of number, title, length and size;
// its artist, codec chip and status with stage meters appear only when they
// say something (lib/jobview.js trackView).

import { html } from '../ui/preact.js';
import { trackLength, bytes, codecMeaning, codecLegend } from '../lib/format.js';
import { albumCodec, trackView } from '../lib/jobview.js';
import { itemStatusView } from '../lib/status.js';
import { StatusText, StageTrack, Icon, Chip } from '../ui/components.js';

export function TrackStrip({ items }) {
  return html`<div class="track-strip" aria-hidden="true">
    ${items.map((it) => {
      const v = itemStatusView(it);
      return html`<span key=${it.id} class="track-block" data-role=${v.role} title=${`${it.index}. ${it.title || ''}: ${v.label}`}></span>`;
    })}
  </div>`;
}

// A status message that only repeats the status label adds nothing under it.
const sameWords = (a, b) => String(a).trim().replace(/[.\s]+$/, '').toLowerCase() === String(b).trim().toLowerCase();

/** The key to the codec chips below it: "ALAC 24/44.1 = Lossless 24-bit / 44.1 kHz". */
export function TrackLegend({ items }) {
  const legend = codecLegend(items);
  if (legend.length === 0) return null;
  return html`<p class="track-legend">
    ${legend.map((k) => html`<span key=${k.label} class="track-legend-item"><span class="track-legend-label">${k.label}</span> ${k.text}</span>`)}
  </p>`;
}

export function TrackList({ items, albumArtist }) {
  const mode = albumCodec(items);
  return html`<ol class="tracks">
    ${items.map((it) => {
      const v = itemStatusView(it);
      const t = trackView(it, albumArtist, mode);
      return html`<li key=${it.id} class="track" data-row tabindex="-1" data-status=${it.status} data-detail=${t.status ? '' : undefined}>
        <span class="track-index num">${it.index}</span>
        <span class="track-main">
          <span class="track-title" dir="auto" title=${it.title}>${it.title || html`<span class="muted">Untitled</span>`}</span>
          ${t.artist ? html`<span class="track-artist" dir="auto">${t.artist}</span>` : null}
        </span>
        <span class="track-codec">${t.codec ? html`<${Chip} title=${codecMeaning(it) || undefined}>${t.codec}</${Chip}>` : null}</span>
        <span class="track-len num">${trackLength(it.duration_ms)}</span>
        <span class="track-size num">${it.file_size > 0 ? bytes(it.file_size) : ''}</span>
        ${t.status
          ? html`<span class="track-stages"><${StageTrack} item=${it} /></span>
              <span class="track-status">
                <${StatusText} view=${v} />
                ${v.retry ? html`<span class="badge" data-role="warning"><${Icon} name="rotate" size="sm" /><span class="badge-label">${v.retry}</span></span>` : null}
              </span>`
          : null}
        ${it.status === 'failed' && it.error
          ? html`<p class="track-error">${it.error}</p>`
          : it.status_message && !['completed', 'skipped_existing'].includes(it.status) && !sameWords(it.status_message, v.label)
            ? html`<p class="track-note">${it.status_message}</p>`
            : null}
      </li>`;
    })}
  </ol>`;
}
