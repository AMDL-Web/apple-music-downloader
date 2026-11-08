// New download: the quiet note under a link row or a song hit for its mark
// (lib/marks.js resourceMark): in the queue now, paused, in the library, or
// how many of its tracks are, with Open (its run while one is queued or
// running, else its record). It breaks only between its parts.

import { html } from '../ui/preact.js';
import { markFraction } from '../lib/marks.js';
import { recordHash } from '../lib/route.js';
import { Icon } from '../ui/components.js';

const ICONS = { active: 'clock', paused: 'pause', complete: 'circle-check', partial: 'circle-dashed' };

/** The note's words for a mark. */
export const markWords = (mark) =>
  mark.state === 'active' ? 'In the queue now' : mark.state === 'paused' ? 'Paused' : mark.state === 'complete' ? 'In your library' : `${markFraction(mark)} in your library`;

/** Where a mark's Open goes: its run's drawer (`jobHref(id)`) while active, else its record. */
export const markHref = (mark, jobHref) => (mark.job ? jobHref(mark.job.id) : mark.resource ? recordHash(mark.resource.kind, mark.resource.id) : '');

/** Props: mark, href, focusable (false: Open is out of the Tab order). */
export function MarkNote({ mark, href, focusable = true }) {
  const text = markWords(mark).replaceAll(' ', '\u00a0');
  return html`<span class="new-link-prior" data-state=${mark.state}>
    <${Icon} name=${ICONS[mark.state]} size="sm" />
    <span
      >${text}${href ? html` · <a class="link new-link-open" href=${href} title=${mark.job ? 'Open its download' : 'Open it in the library'} tabindex=${focusable ? undefined : '-1'}>Open</a>` : null}</span
    >
  </span>`;
}
