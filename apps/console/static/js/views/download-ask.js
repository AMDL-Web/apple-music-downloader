// New download: the questions a download asks before it goes
// (lib/download-ask.js): which quality, when the release has both lossless
// and Dolby Atmos and Settings lists both; and, for Download again, one line
// on the files it replaces.

import { html, useState } from '../ui/preact.js';
import { CHOICE_LABEL, againText } from '../lib/download-ask.js';
import { Segmented } from '../ui/components.js';
import { ConfirmDialog } from '../ui/dialogs.js';

/**
 * Props: title ("Download “…”"), asks ({codecs, again}), onDone({codec}),
 * onCancel(). The quality starts on the codec Settings ranks first.
 */
export function AskDialog({ title, asks, onDone, onCancel }) {
  const [codec, setCodec] = useState(asks.codecs[0] || '');
  return html`<${ConfirmDialog}
    title=${title}
    confirmLabel=${asks.again ? 'Download again' : 'Download'}
    confirmFocus
    onConfirm=${() => onDone({ codec })}
    onCancel=${onCancel}
  >
    <div class="ask">
      ${asks.codecs.length
        ? html`<div class="ask-row" key="codec">
            <span class="ask-label">Quality</span>
            <${Segmented} label="Quality" options=${asks.codecs.map((c) => ({ value: c, label: CHOICE_LABEL[c] || c }))} value=${codec} onChange=${setCodec} />
          </div>`
        : null}
      ${asks.again ? html`<p class="ask-note" key="again">${againText(asks.again)}</p>` : null}
    </div>
  </${ConfirmDialog}>`;
}
