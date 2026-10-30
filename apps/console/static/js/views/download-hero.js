// New download: the text box that holds the links and the search term,
// with the Paste button on a touch screen.

import { html } from '../ui/preact.js';
import { mergeLinks } from '../lib/links.js';
import { Card } from '../ui/components.js';
import { PasteButton } from './download-bar.js';

/**
 * Props: text, inputRef, term (the search term, '' when the box holds
 * links), storefront, edit(next or fn), onKeyDown.
 */
export function Compose({ text, inputRef, term, storefront, edit, onKeyDown }) {
  return html`<${Card}
    section="compose"
    className="new-compose"
    title="Search or paste links"
    labelId="new-compose-title"
    actions=${html`<${PasteButton} storefront=${storefront} onLinks=${(urls) => edit((t) => mergeLinks(t, urls, storefront))} />`}
  >
    <textarea
      id="new-input"
      class="new-input"
      ref=${inputRef}
      rows="6"
      value=${text}
      placeholder="Search for a song, album or artist, or paste Apple Music links"
      aria-labelledby="new-compose-title"
      aria-keyshortcuts=${term ? 'ArrowDown Enter' : 'Control+Enter Meta+Enter'}
      autocomplete="off"
      autocapitalize="off"
      spellcheck=${false}
      onInput=${(e) => edit(e.currentTarget.value)}
      onKeyDown=${onKeyDown}
    ></textarea>
  </${Card}>`;
}
