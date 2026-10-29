// The Queue's status tabs on a phone: a chip row that scrolls sideways. The
// open chip is kept in view, and an edge fades (data-fade="start|end|both")
// while chips lie past it, so a half-shown chip reads as "more this way".

import { useEffect } from '../ui/preact.js';

const ROW = '.page-queue .seg';

/** Keeps the open tab in view and marks which edges of the chip row hide chips. Re-runs when `key` changes. */
export function useChipRow(key) {
  useEffect(() => {
    const row = document.querySelector(ROW);
    if (!row) return undefined;
    const mark = () => {
      const start = row.scrollLeft > 1;
      const end = row.scrollLeft + row.clientWidth < row.scrollWidth - 1;
      const fade = start && end ? 'both' : start ? 'start' : end ? 'end' : '';
      if (fade) row.setAttribute('data-fade', fade);
      else row.removeAttribute('data-fade');
    };
    const open = row.querySelector('.seg-btn[aria-pressed="true"]');
    if (open && open.scrollIntoView) open.scrollIntoView({ inline: 'center', block: 'nearest' });
    mark();
    row.addEventListener('scroll', mark, { passive: true });
    window.addEventListener('resize', mark);
    return () => {
      row.removeEventListener('scroll', mark);
      window.removeEventListener('resize', mark);
    };
  }, [key]);
}
