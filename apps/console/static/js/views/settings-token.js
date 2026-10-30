// The media user token field's control. A saved token is never put in the
// page: the row says "Token set · ends …a1b2" with Replace and Remove, and
// Replace opens an empty box for the new one. "Where do I find this?" opens
// three steps under the field.

import { html, useState, useRef } from '../ui/preact.js';
import { Button } from '../ui/components.js';
import { usePopoverClose } from './settings-template.js';

/** The last four characters of a token, for "ends …a1b2". */
export const tokenTail = (token) => String(token || '').slice(-4);

function WhereToFind({ id }) {
  const [open, setOpen] = useState(false);
  const root = useRef(null);
  const opener = useRef(null);
  usePopoverClose(open, root, opener, () => setOpen(false));
  return html`<div class="token-help" ref=${root}>
    <span ref=${opener}>
      <button type="button" class="link token-help-btn" aria-expanded=${open ? 'true' : 'false'} aria-controls=${`${id}-where`} onClick=${() => setOpen((o) => !o)}>
        Where do I find this?
      </button>
    </span>
    ${open
      ? html`<ol class="token-steps" id=${`${id}-where`}>
          <li>Open music.apple.com in a desktop browser and sign in.</li>
          <li>Open the developer tools, then the site's cookies (Application in Chrome, Storage in Safari and Firefox).</li>
          <li>Copy the value of the <span class="mono">media-user-token</span> cookie and paste it here.</li>
        </ol>`
      : null}
  </div>`;
}

export function TokenControl({ id, value, invalid, described, onChange }) {
  const set = typeof value === 'string' && value !== '';
  const [replacing, setReplacing] = useState(false);
  const [typed, setTyped] = useState('');
  const input = useRef(null);
  const editing = !set || replacing;
  const startReplace = () => {
    setTyped('');
    setReplacing(true);
    requestAnimationFrame(() => input.current && input.current.focus());
  };
  return html`<div class="token-field">
    ${editing
      ? html`<div class="token-row">
          <input
            type="password"
            class="field-input mono"
            id=${id}
            ref=${input}
            value=${replacing ? typed : ''}
            placeholder="Paste your media-user-token"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            aria-invalid=${invalid ? 'true' : undefined}
            aria-describedby=${described}
            onInput=${(e) => {
              const v = e.currentTarget.value.trim();
              setTyped(e.currentTarget.value);
              if (v) {
                onChange(v);
                if (!replacing) setReplacing(true);
              }
            }}
            onBlur=${() => {
              if (typed.trim()) setReplacing(false);
            }}
          />
          ${replacing && set ? html`<${Button} variant="ghost" size="sm" className="token-cancel" onClick=${() => setReplacing(false)}>Done</${Button}>` : null}
        </div>`
      : html`<div class="token-row token-set">
          <span class="token-state" id=${id} tabindex="-1">Token set · ends <span class="mono">…${tokenTail(value)}</span></span>
          <span class="token-actions">
            <${Button} variant="ghost" size="sm" className="token-replace" onClick=${startReplace}>Replace</${Button}>
            <${Button} variant="danger" size="sm" className="token-remove" onClick=${() => onChange('')}>Remove</${Button}>
          </span>
        </div>`}
    <${WhereToFind} id=${id} />
  </div>`;
}
