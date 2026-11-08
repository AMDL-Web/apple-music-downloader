// Library page (#/library?kind=&q=): the Apple Music library of the
// account (views/library-apple.js). amdl's own records are rows on
// Activity (views/queue.js).

import { html } from '../ui/preact.js';
import { libraryFilters } from '../lib/route.js';
import { AppleLibrary } from './library-apple.js';

export function LibraryPage({ route }) {
  return html`<div class="page page-library"><${AppleLibrary} route=${route} filters=${libraryFilters(route.params)} /></div>`;
}
