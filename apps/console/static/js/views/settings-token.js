// The control of a write-only token field (wrapper.token). The API answers a
// set one as "..." alone, so its row says "Token set" with Replace and
// Remove, and Replace opens an empty box for the new one.

import { html, useState, useRef } from '../ui/preact.js';
import { Button } from '../ui/components.js';

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
            placeholder="Paste the token"
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
          <span class="token-state" id=${id} tabindex="-1">Token set</span>
          <span class="token-actions">
            <${Button} variant="ghost" size="sm" className="token-replace" onClick=${startReplace}>Replace</${Button}>
            <${Button} variant="danger" size="sm" className="token-remove" onClick=${() => onChange('')}>Remove</${Button}>
          </span>
        </div>`}
  </div>`;
}
