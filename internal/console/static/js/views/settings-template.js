// A path template field's control: the text box, a "Variables" button whose
// panel inserts variables at the caret (it stays open for the next one until
// Done, Escape or a press outside), and the path the template gives a sample
// track, updated while typing (lib/settings-template.js).

import { html, useState, useRef, useEffect } from '../ui/preact.js';
import { TEMPLATE_VARIABLES, templateExample, templateKind } from '../lib/settings-template.js';
import { Button } from '../ui/components.js';

// Inserted with :02d, the form a track number is nearly always wanted in.
const PADDED_ON_INSERT = new Set(['SongNumber', 'TrackNumber']);

/**
 * Closes a popover on Escape (focus back to `opener`) and on a pointer
 * press outside `root`, while `open`.
 */
export function usePopoverClose(open, root, opener, close) {
  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      e.stopPropagation();
      close();
      const btn = opener.current && opener.current.querySelector('button');
      if (btn) btn.focus();
    };
    const onDown = (e) => {
      if (root.current && !root.current.contains(e.target)) close();
    };
    document.addEventListener('keydown', onKey, true);
    document.addEventListener('pointerdown', onDown, true);
    return () => {
      document.removeEventListener('keydown', onKey, true);
      document.removeEventListener('pointerdown', onDown, true);
    };
  }, [open]);
}

const DESCRIBED = new Map(TEMPLATE_VARIABLES);
const CHIP_HINT = 'Pick a variable to insert it at the cursor.';

export function TemplateControl({ field, id, value, invalid, described, onChange }) {
  const [open, setOpen] = useState(false);
  // The chip last focused, pointed at or inserted, whose meaning shows under the chips.
  const [active, setActive] = useState('');
  const input = useRef(null);
  const root = useRef(null);
  const opener = useRef(null);
  // Where the next variable goes: the box's selection, kept while focus is in the panel.
  const caret = useRef(null);
  const text = value === undefined ? '' : String(value);
  usePopoverClose(open, root, opener, () => setOpen(false));

  const remember = (e) => {
    caret.current = [e.currentTarget.selectionStart, e.currentTarget.selectionEnd];
  };
  const insert = (name) => {
    const token = PADDED_ON_INSERT.has(name) ? `{${name}:02d}` : `{${name}}`;
    const [start, end] = caret.current || [text.length, text.length];
    onChange(text.slice(0, start) + token + text.slice(end));
    const at = start + token.length;
    caret.current = [at, at];
    setActive(name);
    requestAnimationFrame(() => {
      if (input.current) input.current.setSelectionRange(at, at);
    });
  };
  const done = () => {
    setOpen(false);
    const [, at] = caret.current || [text.length, text.length];
    requestAnimationFrame(() => {
      if (!input.current) return;
      input.current.focus();
      input.current.setSelectionRange(at, at);
    });
  };

  return html`<div class="template-field" ref=${root}>
    <div class="template-row">
      <input
        type="text"
        class="field-input mono"
        id=${id}
        ref=${input}
        value=${text}
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        aria-invalid=${invalid ? 'true' : undefined}
        aria-describedby=${`${described} ${id}-example`}
        onInput=${(e) => {
          remember(e);
          onChange(e.currentTarget.value);
        }}
        onSelect=${remember}
        onBlur=${remember}
      />
      <span ref=${opener} class="template-vars-anchor">
        <${Button}
          variant="secondary"
          className="template-vars-btn"
          aria-expanded=${open ? 'true' : 'false'}
          aria-controls=${`${id}-vars`}
          aria-label=${`Variables for ${field.label}`}
          onClick=${() => {
            setActive('');
            setOpen((o) => !o);
          }}
        >Variables</${Button}>
      </span>
    </div>
    ${open
      ? html`<div class="template-popover" id=${`${id}-vars`} role="group" aria-label=${`Insert a variable into ${field.label}`}>
          <p class="template-popover-note">"/" starts a folder; ".m4a" is added to the last part.</p>
          <div class="template-chips">
            ${TEMPLATE_VARIABLES.filter(([name]) => name !== 'Tag').map(
              ([name, what]) => html`<button
                key=${name}
                type="button"
                class="template-chip"
                data-var=${name}
                title=${what}
                aria-describedby=${`${id}-var-desc`}
                onFocus=${() => setActive(name)}
                onPointerEnter=${() => setActive(name)}
                onClick=${() => insert(name)}
              >
                {${name}}
              </button>`,
            )}
          </div>
          <div class="template-popover-foot">
            <p class="template-var-desc" id=${`${id}-var-desc`}>
              ${active ? html`<span class="mono">{${active}}</span>: ${DESCRIBED.get(active)}.` : CHIP_HINT}
            </p>
            <${Button} variant="secondary" size="sm" className="template-vars-done" onClick=${done}>Done</${Button}>
          </div>
        </div>`
      : null}
    <p class="template-example" id=${`${id}-example`}>
      <span class="template-example-label">Example</span> <span class="mono wrap-any">${templateExample(text, templateKind(field.key))}</span>
    </p>
  </div>`;
}
