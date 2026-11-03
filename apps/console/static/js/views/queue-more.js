// The Queue reads its next page as the list nears its end: while the list's
// bottom is within NEAR px of the viewport's, the stream `next` names is
// read on, one page at a time.

import { useEffect, useRef } from '../ui/preact.js';

const NEAR = 800;

/** Calls load() whenever `next` is set, nothing is `busy` and the bottom of `list` is near. */
export function useMoreNearEnd(list, next, busy, load) {
  const state = useRef({ next, busy, load });
  state.current = { next, busy, load };
  const check = () => {
    const { next: n, busy: b, load: l } = state.current;
    const el = list.current;
    if (!n || b || !el) return;
    if (el.getBoundingClientRect().bottom < window.innerHeight + NEAR) l();
  };
  useEffect(check, [next, busy]);
  useEffect(() => {
    window.addEventListener('scroll', check, { passive: true });
    window.addEventListener('resize', check);
    return () => {
      window.removeEventListener('scroll', check);
      window.removeEventListener('resize', check);
    };
  }, []);
}
