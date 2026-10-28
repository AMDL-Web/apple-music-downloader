// New download: the quality sheet. Asks the backend which qualities each
// track of a previewed link offers (queryQuality) and lists them: a summary
// per quality slot, then every track with the formats it offers. Closing
// the sheet cancels a probe still running.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { queryQuality } from '../api.js';
import { qualityOverview, qualitySpec, shortError } from '../lib/lookup.js';
import { count } from '../lib/format.js';
import { Dialog } from '../ui/dialogs.js';
import { Button, Icon, Chip, Notice } from '../ui/components.js';
import { useMedia } from '../ui/hooks.js';

const TRACKS_SHOWN = 50;

export function QualitySheet({ url, title, onClose }) {
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
      (data) => !c.signal.aborted && setState({ loading: false, data, error: null }),
      (error) => !c.signal.aborted && setState({ loading: false, data: null, error }),
    );
    return () => c.abort();
  }, [url, run]);

  const data = state.data;
  const slots = data ? qualityOverview(data) : [];
  const offered = slots.filter((s) => s.tracks > 0);
  const missing = slots.filter((s) => s.tracks === 0);
  const tracks = data ? data.tracks : [];
  const shown = all ? tracks : tracks.slice(0, TRACKS_SHOWN);

  return html`<${Dialog}
    name="quality"
    className="sheet sheet-quality"
    placement=${phone ? 'bottom' : 'center'}
    labelledBy="quality-title"
    onClose=${onClose}
  >
    <div class="sheet-head">
      <h2 id="quality-title" class="card-title">Qualities</h2>
      <${Button} variant="ghost" size="sm" icon="x" aria-label="Close" onClick=${onClose} />
    </div>
    <p class="quality-for" dir="auto" title=${url}>${title}</p>
    ${state.loading
      ? html`<div class="quality-wait" role="status">
          <span class="pulse" data-role="info" aria-hidden="true"></span>
          <span>Reading each track's master playlist. A long album can take a minute.</span>
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
      ? html`<div class="quality-body">
          <ul class="quality-slots" aria-label="Qualities offered">
            ${offered.map(
              (s) => html`<li key=${s.id} class="quality-slot" data-id=${s.id}>
                <span class="quality-slot-label">${s.label}<span class="quality-slot-codec">${s.codec}</span></span>
                <span class="quality-slot-count">${s.total === 1 ? '1 track' : s.tracks === s.total ? `All ${count(s.total)} tracks` : `${count(s.tracks)} of ${count(s.total)} tracks`}</span>
                ${s.best ? html`<span class="quality-slot-best">Up to ${s.best}</span>` : null}
              </li>`,
            )}
          </ul>
          ${missing.length ? html`<p class="new-hint">Not offered: ${missing.map((s) => s.label).join(', ')}.</p>` : null}
          <ol class="quality-tracks" aria-label="Tracks">
            ${shown.map((t, i) => {
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
          </ol>
          ${tracks.length > shown.length
            ? html`<${Button} size="sm" className="quality-all" onClick=${() => setAll(true)}>Show all ${count(tracks.length)} tracks</${Button}>`
            : null}
        </div>`
      : null}
  </${Dialog}>`;
}
