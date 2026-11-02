// New download: the quiet note under a link row or a song hit for its mark
// (lib/on-disk.js diskMark): in the queue now, in the library, or how many
// of its tracks are, with Open when a job of it is known. It breaks only
// between its parts.

import { html } from '../ui/preact.js';
import { diskFraction } from '../lib/on-disk.js';
import { Icon } from '../ui/components.js';

const ICONS = { active: 'clock', complete: 'circle-check', partial: 'circle-dashed' };

/** The note's words for a mark. */
export const diskWords = (mark) =>
  mark.state === 'active' ? 'In the queue now' : mark.state === 'complete' ? 'In your library' : `${diskFraction(mark)} in your library`;

/** Props: mark, href (the job's drawer, when mark.job), focusable (false: Open is out of the Tab order). */
export function DiskNote({ mark, href, focusable = true }) {
  const text = diskWords(mark).replaceAll(' ', ' ');
  return html`<span class="new-link-prior" data-state=${mark.state}>
    <${Icon} name=${ICONS[mark.state]} size="sm" />
    <span
      >${text}${mark.job && href
        ? html` · <a class="link new-link-open" href=${href} title="Open that job" tabindex=${focusable ? undefined : '-1'}>Open</a>`
        : null}</span
    >
  </span>`;
}
