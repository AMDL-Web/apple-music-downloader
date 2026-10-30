// The hash router and its leave guard: the route state App() renders,
// navigate() for links, and the hashchange listener, with the history
// bookkeeping that undoes a refused change (lib/route.js refusedMove).

import { useState, useEffect, useLayoutEffect, useRef, useCallback } from './preact.js';
import { parseHash, buildHash, historyIndex, refusedMove } from '../lib/route.js';

/** Route state and navigation for App(): {route, navigate, resumeLeave, setLeaveGuard}. */
export function useHashRouter() {
  const [route, setRoute] = useState(() => parseHash(window.location.hash));

  // A page holding unsaved work sets a leave guard (ctx.setLeaveGuard): it
  // gets the hash about to be shown and returns false to stay, then asks
  // the user itself and, to go after all, calls ctx.resumeLeave(hash).
  // Links, the palette, shortcuts and Back all end in hashchange, so the
  // guard runs there (and on navigate's replace). A refused change is
  // undone through the history (lib/route.js refusedMove), so Stay leaves
  // no extra entry and resumeLeave repeats the same move.
  const leaveGuard = useRef(null);
  const shownUrl = useRef(window.location.href);
  const shownIdx = useRef(0);
  const refused = useRef(null);
  const setLeaveGuard = useCallback((fn) => {
    leaveGuard.current = fn;
  }, []);

  useLayoutEffect(() => {
    const idx = historyIndex(window.history.state);
    if (idx === null) window.history.replaceState({ amdlIdx: 0 }, '');
    else shownIdx.current = idx;
  }, []);

  const navigate = useCallback((hash, opts = {}) => {
    if (opts.replace) {
      if (leaveGuard.current && !leaveGuard.current(hash)) {
        refused.current = { hash, replace: true };
        return;
      }
      window.history.replaceState({ amdlIdx: shownIdx.current }, '', hash);
      shownUrl.current = window.location.href;
      setRoute(parseHash(hash));
    } else if (window.location.hash !== hash) {
      window.location.hash = hash;
    }
  }, []);

  const resumeLeave = useCallback(
    (hash) => {
      const r = refused.current;
      refused.current = null;
      leaveGuard.current = null;
      if (r && r.hash === hash && r.redo) window.history.go(r.redo);
      else navigate(hash, { replace: Boolean(r && r.hash === hash && r.replace) });
    },
    [navigate],
  );

  useEffect(() => {
    const on = () => {
      // An undo below landing back on the entry already shown.
      if (window.location.href === shownUrl.current) return;
      const state = window.history.state;
      if (leaveGuard.current && !leaveGuard.current(window.location.hash)) {
        const move = refusedMove(shownIdx.current, state);
        refused.current = { hash: window.location.hash, redo: move.redo };
        if (move.undo) window.history.go(move.undo);
        else window.history.replaceState(state, '', shownUrl.current);
        return;
      }
      refused.current = null;
      const idx = historyIndex(state);
      shownIdx.current = idx === null ? shownIdx.current + 1 : idx;
      if (idx === null) window.history.replaceState({ amdlIdx: shownIdx.current }, '');
      shownUrl.current = window.location.href;
      setRoute(parseHash(window.location.hash));
    };
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);

  // An alias address (#/system) is rewritten in place to the page it names.
  useLayoutEffect(() => {
    if (route.replace) navigate(buildHash(route.page, route.params), { replace: true });
  }, [route]);

  return { route, navigate, resumeLeave, setLeaveGuard };
}
