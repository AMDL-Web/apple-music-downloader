// The status board at #/settings (desktop and tablet): one tile per domain
// with a status, in nav order, each a link to its section. The tiles are
// the section heads drawn small (views/settings-head.js DomainHead).

import { html } from '../ui/preact.js';
import { BOARD_SECTIONS } from '../lib/settings-sections.js';
import { DomainHead } from './settings-head.js';

/** `views` holds each domain's view (lib/domains.js) by section id. */
export function SettingsBoard({ views }) {
  return html`<section class="settings-board" aria-labelledby="settings-board-title">
    <h2 class="sr-only" id="settings-board-title">Status</h2>
    <ul class="status-tiles">
      ${BOARD_SECTIONS.map((s) => html`<li key=${s.id}><${DomainHead} variant="tile" section=${s} view=${views[s.id]} /></li>`)}
    </ul>
  </section>`;
}
