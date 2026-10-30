// New download: the compose field that holds the links and the search
// term (a search or link glyph, the text box, Clear or, on a touch screen,
// Paste), and the option row under it.

import { html, useLayoutEffect } from '../ui/preact.js';
import { mergeLinks } from '../lib/links.js';
import { Icon, Button } from '../ui/components.js';
import { PasteButton } from './download-bar.js';

// Browsers without field-sizing grow the box from its scroll height.
const SIZES_ITSELF = typeof CSS !== 'undefined' && CSS.supports && CSS.supports('field-sizing', 'content');

/** Empties the box as one undoable edit, so ⌘Z brings the text back. */
export function clearBox(el, edit) {
  if (!el) return;
  el.focus({ preventScroll: true });
  el.select();
  if (!document.execCommand('delete')) edit('');
}

/**
 * Props: text, inputRef, term (the search term, '' when the box holds
 * links), links (the number of links in it), busy (a search is under
 * way), storefront, edit(next or fn), onKeyDown. A link or a line break
 * sets it dense: body-size text that grows, then scrolls.
 */
export function Compose({ text, inputRef, term, links, busy, storefront, edit, onKeyDown }) {
  const dense = links > 0 || text.includes('\n');
  useLayoutEffect(() => {
    const el = inputRef.current;
    if (SIZES_ITSELF || !el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [text, dense]);
  const glyph = links ? 'link' : 'search';
  return html`<div class="new-compose" data-dense=${dense ? '' : undefined} data-busy=${busy ? '' : undefined} onClick=${(e) => {
    if (e.target === e.currentTarget || e.target.closest('.new-compose-glyph')) inputRef.current && inputRef.current.focus();
  }}>
    <span class="new-compose-glyph" aria-hidden="true">
      <span key=${glyph} class="new-compose-icon" data-glyph=${glyph}><${Icon} name=${glyph} /></span>
    </span>
    <textarea
      id="new-input"
      class="new-compose-input"
      ref=${inputRef}
      rows="1"
      value=${text}
      placeholder="Search or paste links"
      aria-label="Search or paste links"
      aria-keyshortcuts=${term ? 'ArrowDown Enter Escape' : 'Control+Enter Meta+Enter Escape'}
      autocomplete="off"
      autocapitalize="off"
      spellcheck=${false}
      onInput=${(e) => edit(e.currentTarget.value)}
      onKeyDown=${onKeyDown}
    ></textarea>
    <span class="new-compose-trail">
      ${text
        ? html`<${Button} variant="ghost" icon="x" className="new-compose-clear" aria-label="Clear" title="Clear (Esc)" onClick=${() => clearBox(inputRef.current, edit)} />`
        : html`<${PasteButton} storefront=${storefront} onLinks=${(urls) => edit((t) => mergeLinks(t, urls, storefront))} />`}
    </span>
  </div>`;
}

/**
 * The options under the field: the quality menu and the Overwrite switch
 * (controls), then, when GET config failed, a chip that retries it.
 */
export function Options({ controls, cfgError, onRetryCfg }) {
  return html`<div class="new-options">
    ${controls}
    ${cfgError
      ? html`<${Button}
          size="sm"
          icon="triangle-alert"
          className="new-cfg-retry"
          title="Couldn't load your Settings; downloads use them as they are"
          onClick=${onRetryCfg}
        >Settings unavailable · Retry</${Button}>`
      : null}
  </div>`;
}
