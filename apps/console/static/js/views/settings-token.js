// The media user token field's control. The API answers a saved token
// redacted ("..." and its last four characters), so the page never holds it:
// the row says "Token set · ends …a1b2" with Replace and Remove, and
// Replace opens an empty box for the new one. "Where do I find this?" opens
// three steps under the field. While the backend reads the wrapper's own
// token (`wrapperToken`, GET /wrapper/status, masked the same way), the field
// names that one instead, and a saved token gets a second line with Remove.
// Another token field (wrapper.token) is write-only: the API answers a set one
// as "..." alone, so its row says "Token set", with no help.

import { html, useState, useRef } from '../ui/preact.js';
import { Button } from '../ui/components.js';
import { usePopoverClose } from './settings-template.js';

/** The last four characters of the redacted token, for "ends …a1b2". */
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

/**
 * The wrapper's token in use, and a saved one it leaves unused. A saved token
 * arrives redacted ("..."); anything else is an unsaved draft, typed before
 * the wrapper's token appeared.
 */
function WrapperToken({ id, wrapperToken, value, onChange }) {
  const set = typeof value === 'string' && value !== '';
  const saved = set && value.startsWith('...');
  return html`<div class="token-field" data-source="wrapper">
    <div class="token-row token-set">
      <span class="token-state" id=${id} tabindex="-1">From the wrapper · ends <span class="mono">…${tokenTail(wrapperToken)}</span></span>
    </div>
    ${set
      ? html`<div class="token-row token-set token-unused">
          <span class="token-state">${saved ? 'Saved' : 'Unsaved'} token · ends <span class="mono">…${tokenTail(value)}</span> · not used</span>
          <span class="token-actions">
            <${Button} variant="danger" size="sm" className="token-remove" onClick=${() => onChange('')}>Remove</${Button}>
          </span>
        </div>`
      : null}
  </div>`;
}

export function TokenControl({ field, id, value, invalid, described, onChange, wrapperToken = '' }) {
  const userToken = !field || field.key === 'catalog.media_user_token';
  if (wrapperToken && userToken) return html`<${WrapperToken} id=${id} wrapperToken=${wrapperToken} value=${value} onChange=${onChange} />`;
  return html`<${TypedToken} id=${id} value=${value} invalid=${invalid} described=${described} onChange=${onChange} userToken=${userToken} />`;
}

function TypedToken({ id, value, invalid, described, onChange, userToken = true }) {
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
            placeholder=${userToken ? 'Paste your media-user-token' : 'Paste the token'}
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
          <span class="token-state" id=${id} tabindex="-1"
            >${userToken ? html`Token set · ends <span class="mono">…${tokenTail(value)}</span>` : 'Token set'}</span
          >
          <span class="token-actions">
            <${Button} variant="ghost" size="sm" className="token-replace" onClick=${startReplace}>Replace</${Button}>
            <${Button} variant="danger" size="sm" className="token-remove" onClick=${() => onChange('')}>Remove</${Button}>
          </span>
        </div>`}
    ${userToken ? html`<${WhereToFind} id=${id} />` : null}
  </div>`;
}
