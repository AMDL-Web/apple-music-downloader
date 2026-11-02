// New download: the questions a download asks before it goes
// (lib/download-ask.js): which quality, when the release has both lossless
// and Dolby Atmos and Settings lists both; and, for an item already in the
// library, whether to download it again (one line on the files it
// replaces) or add another quality beside it.

import { html, useState } from '../ui/preact.js';
import { CHOICE_LABEL, againText } from '../lib/download-ask.js';
import { Segmented } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';

/**
 * Props: title ("Download “…”"), asks ({codecs, again, slots}),
 * onDone({codec, slot}), onCancel(). The quality starts on the codec
 * Settings ranks first; Download again starts on the same quality.
 */
export function AskDialog({ title, asks, onDone, onCancel }) {
  const [codec, setCodec] = useState(asks.codecs[0] || '');
  const [slot, setSlot] = useState('');
  return html`<${ConfirmDialog}
    title=${title}
    confirmLabel=${!asks.again ? 'Download' : slot ? 'Add quality' : 'Download again'}
    confirmFocus
    onConfirm=${() => onDone({ codec, slot })}
    onCancel=${onCancel}
  >
    <div class="ask">
      ${asks.codecs.length
        ? html`<div class="ask-row" key="codec">
            <span class="ask-label">Quality</span>
            <${Segmented} label="Quality" options=${asks.codecs.map((c) => ({ value: c, label: CHOICE_LABEL[c] || c }))} value=${codec} onChange=${setCodec} />
          </div>`
        : null}
      ${asks.again && asks.slots.length
        ? html`<div class="ask-row" key="slot">
            <label class="ask-label" for="ask-slot">Quality</label>
            <select id="ask-slot" class="select" value=${slot} onChange=${(e) => setSlot(e.currentTarget.value)}>
              <option value="">Same as before</option>
              ${asks.slots.map((c) => html`<option key=${c} value=${c}>Add ${CHOICE_LABEL[c] || c}</option>`)}
            </select>
          </div>`
        : null}
      ${asks.again ? html`<p class="ask-note" key="again" data-hidden=${slot ? '' : undefined}>${againText(asks.again)}</p>` : null}
    </div>
  </${ConfirmDialog}>`;
}
