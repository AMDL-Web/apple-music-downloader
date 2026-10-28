// New download (#/new?url=): submit Apple Music links. The page head (title
// and summary) comes from app.js; this body is a placeholder that lists the
// links the address carries until the download form replaces it.

import { html } from '../ui/preact.js';
import { newDownloadUrls } from '../lib/route.js';
import { parseLink } from '../lib/links.js';
import { Card, Chip, Empty } from '../ui/components.js';

export function DownloadPage({ route }) {
  const urls = newDownloadUrls(route.params);
  return html`<div class="page page-new">
    <${Card} section="new-links" title="Links" labelId="new-links-title" note=${urls.length ? `${urls.length} in the address` : null}>
      ${urls.length
        ? html`<ul class="new-links">
            ${urls.map((u) => {
              const link = parseLink(u);
              const kind = link ? link.kind : 'invalid';
              return html`<li key=${u} class="new-link" data-kind=${kind}>
                <${Chip}>${link ? kind : 'not Apple Music'}</${Chip}>
                <span class="mono wrap-any">${u}</span>
              </li>`;
            })}
          </ul>`
        : html`<${Empty} icon="download" title="No links yet">Paste Apple Music links anywhere in the console, outside a text field, to bring them here.</${Empty}>`}
    </${Card}>
  </div>`;
}
