// New download: the Available qualities sheet, read-only. Asks the backend
// which qualities each track of a previewed link offers (queryQuality) and
// lists each offered slot once, then only the tracks whose formats differ
// from the album's (all of them on Show tracks). The answer is kept per
// link (PROBES), so the row can say what the chosen preset will get.
// Closing the sheet cancels a probe still running.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { queryQuality } from '../api.js';
import { qualityOverview, qualitySpec, shortError, differingTracks, slotName } from '../lib/lookup.js';
import { count } from '../lib/format.js';
import { Dialog } from '../ui/dialogs.js';
import { Button, Icon, Chip, Notice } from '../ui/components.js';
import { useMedia } from '../ui/hooks.js';

const TRACKS_SHOWN = 50;

/** QualityResponses per link for the life of the tab, newest last. */
export const PROBES = new Map();
const PROBES_MAX = 50;

/** Props: url, title, tracks (the preview's track count, when known), onProbed() (an answer arrived), onClose. */
export function QualitySheet({ url, title, tracks: expected, onProbed, onClose }) {
  const [state, setState] = useState({ loading: true, data: null, error: null });
  const [all, setAll] = useState(false);
  const [run, setRun] = useState(0);
  const ctrl = useRef(null);
  const phone = useMedia('(max-width: 639px)');

  useEffect(() => {
    const c = new AbortController();
    ctrl.current = c;
    setState({ loading: true, data: null, error: null });
    queryQuality(url, c.signal).then(
      (data) => {
        if (c.signal.aborted) return;
        PROBES.delete(url);
        PROBES.set(url, data);
        if (PROBES.size > PROBES_MAX) PROBES.delete(PROBES.keys().next().value);
        setState({ loading: false, data, error: null });
        if (onProbed) onProbed();
      },
      (error) => !c.signal.aborted && setState({ loading: false, data: null, error }),
    );
    return () => c.abort();
  }, [url, run]);

  const data = state.data;
  const slots = data ? qualityOverview(data) : [];
  const offered = slots.filter((s) => s.tracks > 0);
  const missing = slots.filter((s) => s.tracks === 0);
  const tracks = data ? data.tracks : [];
  const differ = data ? differingTracks(data) : [];
  // Without Show tracks only the tracks that differ are listed.
  const listed = all ? tracks.map((t, i) => i) : differ;
  const shown = listed.slice(0, all ? tracks.length : TRACKS_SHOWN);

  return html`<${Dialog}
    name="quality"
    className="sheet sheet-quality"
    placement=${phone ? 'bottom' : 'center'}
    labelledBy="quality-title"
    onClose=${onClose}
  >
    <div class="sheet-head">
      <h2 id="quality-title" class="card-title">Available qualities</h2>
      <${Button} variant="ghost" size="sm" icon="x" aria-label="Close" onClick=${onClose} />
    </div>
    <p class="quality-for" dir="auto" title=${url}>${title}</p>
    ${state.loading
      ? html`<div class="quality-wait" role="status">
          <span class="pulse" data-role="info" aria-hidden="true"></span>
          <span>${expected > 1 ? `Reading the master playlist of each of ${count(expected)} tracks.` : "Reading the track's master playlist."} ${expected > 30 ? 'This can take a minute.' : ''}</span>
          <${Button} size="sm" className="quality-cancel" onClick=${onClose}>Cancel</${Button}>
        </div>`
      : null}
    ${state.error
      ? html`<${Notice}
          role="danger"
          icon="circle-x"
          action=${html`<${Button} size="sm" icon="rotate" onClick=${() => setRun((n) => n + 1)}>Retry</${Button}>`}
        >
          <strong>${state.error.status ? `The quality check failed (HTTP ${state.error.status}${state.error.code ? ` ${state.error.code}` : ''}).` : 'The quality check failed.'}</strong>
          <span class="quality-error-msg" title=${state.error.message}> ${shortError(state.error.message, 240)}</span>
        </${Notice}>`
      : null}
    ${data
      ? html`<div class="quality-body" tabindex="0" role="region" aria-label="Available qualities by track">
          <ul class="quality-slots" aria-label="Qualities offered">
            ${offered.map(
              (s) => html`<li key=${s.id} class="quality-slot" data-id=${s.id}>
                <span class="quality-slot-label">${slotName(s).label}${slotName(s).codec ? html`<span class="quality-slot-codec">${slotName(s).codec}</span>` : null}</span>
                <span class="quality-slot-count">${s.total === 1 ? '1 track' : s.tracks === s.total ? `All ${count(s.total)} tracks` : `${count(s.tracks)} of ${count(s.total)} tracks`}</span>
                ${s.best ? html`<span class="quality-slot-best">Up to ${s.best}</span>` : null}
              </li>`,
            )}
          </ul>
          ${missing.length ? html`<p class="new-hint">Not offered: ${missing.map((s) => s.label).join(', ')}.</p>` : null}
          ${tracks.length > 1 && !all
            ? html`<p class="quality-same">
                <span>${differ.length ? `${count(differ.length)} of ${count(tracks.length)} tracks offer other formats:` : `All ${count(tracks.length)} tracks offer the same formats.`}</span>
                <${Button} size="sm" className="quality-all" onClick=${() => setAll(true)}>Show tracks</${Button}>
              </p>`
            : null}
          ${shown.length
            ? html`<ol class="quality-tracks" aria-label="Tracks">
                ${shown.map((i) => {
                  const t = tracks[i];
                  const opts = (t.qualities || []).filter((q) => q.available);
                  return html`<li key=${`${i}-${t.song.id}`} class="quality-track">
                    <span class="quality-track-head">
                      <span class="quality-track-no">${i + 1}</span>
                      <span class="quality-track-name" dir="auto">${t.song.name}</span>
                      ${t.song.has_lyrics ? html`<${Chip} title="The catalog lists lyrics">Lyrics</${Chip}>` : null}
                    </span>
                    <span class="quality-track-opts">
                      ${opts.length
                        ? opts.map(
                            (q) => html`<span key=${q.id} class="quality-opt" data-id=${q.id} title=${q.description || ''}>
                              <span class="quality-opt-label">${q.label}</span>
                              ${qualitySpec(q) ? html`<span class="quality-opt-spec">${qualitySpec(q)}</span>` : null}
                            </span>`,
                          )
                        : html`<span class="status-text" data-role="warning"><${Icon} name="triangle-alert" size="sm" /><span>No quality available</span></span>`}
                    </span>
                  </li>`;
                })}
              </ol>`
            : null}
        </div>`
      : null}
  </${Dialog}>`;
}
