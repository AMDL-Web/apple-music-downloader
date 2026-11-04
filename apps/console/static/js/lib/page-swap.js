// The page hold: a move to another page keeps drawing the page shown, its
// head included, while the next one mounts unseen, and swaps them once the
// next is ready (its root carries neither data-held nor data-pending) or
// PAGE_HOLD_MS has passed. So a page change never draws a page that is
// still empty. No DOM.

/** The longest a page change keeps the last page drawn. */
export const PAGE_HOLD_MS = 400;

/**
 * What App draws for `route`, from what it drew (`state`, null at first):
 * {shown, pending}, routes. A route on the page shown is drawn at once; one
 * on another page waits as `pending` behind the last page drawn, which
 * stays the shown one however many pages the reader passes meanwhile.
 */
export function followRoute(state, route) {
  if (!state || route.page === state.shown.page) return state && state.shown === route && !state.pending ? state : { shown: route, pending: null };
  if (state.pending === route) return state;
  return { shown: state.shown, pending: route };
}

/** The swap: the pending page becomes the one shown. */
export const settle = (state) => (state && state.pending ? { shown: state.pending, pending: null } : state);

/** Whether a page's root (its `.page` element, or null for a page without one) is ready to be shown. */
export const pageReady = (root) => !root || (!root.hasAttribute('data-held') && !root.hasAttribute('data-pending'));
