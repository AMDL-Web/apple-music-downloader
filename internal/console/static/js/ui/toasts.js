// Toasts: the list, their timers and the stack that shows them.

import { html, useState, useEffect, useRef, useCallback } from './preact.js';
import { Icon, Button, LinkButton } from './components.js';

// How long a toast stays: errors longer, a toast with an action (Open) a
// little longer; a sticky one until dismissed or the page changes. Every
// timer pauses while the pointer or focus is on the toasts. A toast raised
// with `offline` (a request that could not reach the server) also goes once
// the feed is live again, after OFFLINE_MIN_MS so it can still be read.
const TOAST_MS = 5000;
const TOAST_ACTION_MS = 8000;
const TOAST_DANGER_MS = 10000;
const OFFLINE_MIN_MS = 3000;

/** The toast list for App(): {toasts, toast, dismissToast, toastsRef}; `route` is the current route. */
export function useToasts(route) {
  const [toasts, setToasts] = useState([]);
  // Toasts: {id, message, role, sticky, offline, at, action: {label, hash}}.
  const toastTimers = useRef(new Map());
  const toastsHeld = useRef(false);
  const toastsRef = useRef(null);
  const dismissToast = useCallback((id) => {
    const t = toastTimers.current.get(id);
    if (t && t.timer !== null) clearTimeout(t.timer);
    toastTimers.current.delete(id);
    setToasts((list) => list.filter((x) => x.id !== id));
  }, []);
  const toast = useCallback(
    (t) => {
      const id = `${Date.now()}-${Math.random()}`;
      const role = t.role || 'neutral';
      const item = { id, message: t.message, role, sticky: Boolean(t.sticky), offline: Boolean(t.offline), at: Date.now(), action: t.action || null };
      setToasts((list) => [...list.slice(-3), item]);
      if (t.sticky) return;
      const ms = role === 'danger' ? TOAST_DANGER_MS : t.action ? TOAST_ACTION_MS : TOAST_MS;
      const timer = toastsHeld.current ? null : setTimeout(() => dismissToast(id), ms);
      toastTimers.current.set(id, { timer, left: ms, at: Date.now() });
    },
    [dismissToast],
  );
  // A toast that left the list (cut off by newer ones, filtered out) keeps no timer.
  useEffect(() => {
    const live = new Set(toasts.map((t) => t.id));
    for (const [id, t] of toastTimers.current) {
      if (live.has(id)) continue;
      if (t.timer !== null) clearTimeout(t.timer);
      toastTimers.current.delete(id);
    }
  }, [toasts]);
  useEffect(() => {
    const el = toastsRef.current;
    if (!el) return undefined;
    let hover = false;
    const hold = () => {
      if (toastsHeld.current) return;
      toastsHeld.current = true;
      for (const t of toastTimers.current.values()) {
        if (t.timer === null) continue;
        clearTimeout(t.timer);
        t.timer = null;
        t.left -= Date.now() - t.at;
      }
    };
    const release = () => {
      if (!toastsHeld.current || hover || el.contains(document.activeElement)) return;
      toastsHeld.current = false;
      for (const [id, t] of toastTimers.current) {
        t.at = Date.now();
        t.timer = setTimeout(() => dismissToast(id), Math.max(1500, t.left));
      }
    };
    const enter = () => {
      hover = true;
      hold();
    };
    const leave = () => {
      hover = false;
      release();
    };
    const focusOut = () => setTimeout(release, 0);
    el.addEventListener('mouseenter', enter);
    el.addEventListener('mouseleave', leave);
    el.addEventListener('focusin', hold);
    el.addEventListener('focusout', focusOut);
    return () => {
      el.removeEventListener('mouseenter', enter);
      el.removeEventListener('mouseleave', leave);
      el.removeEventListener('focusin', hold);
      el.removeEventListener('focusout', focusOut);
    };
  }, [dismissToast]);
  // A sticky toast is about the page it was raised on.
  useEffect(() => setToasts((list) => (list.some((t) => t.sticky) ? list.filter((t) => !t.sticky) : list)), [route.page, route.jobId]);
  return { toasts, toast, dismissToast, toastsRef };
}

/**
 * Back on the feed (`conn.state` turns live): the toasts raised with `offline`
 * go, once they have been up OFFLINE_MIN_MS.
 */
export function useOfflineToasts(conn, toasts, dismissToast) {
  const wasLive = useRef(true);
  const toastsNow = useRef(toasts);
  toastsNow.current = toasts;
  const state = conn && conn.state;
  useEffect(() => {
    const live = state === 'live';
    const back = live && !wasLive.current;
    wasLive.current = live;
    if (!back) return undefined;
    const timers = [];
    for (const t of toastsNow.current) {
      if (!t.offline) continue;
      const wait = OFFLINE_MIN_MS - (Date.now() - t.at);
      if (wait <= 0) dismissToast(t.id);
      else timers.push(setTimeout(() => dismissToast(t.id), wait));
    }
    return () => timers.forEach(clearTimeout);
  }, [state, dismissToast]);
}

/** The stack of toasts; `toastsRef` is the element useToasts watches for hover and focus. */
export function ToastStack({ toasts, toastsRef, dismissToast }) {
  return html`
    <div class="toasts" role="status" aria-live="polite" ref=${toastsRef}>
      ${toasts.map(
        (t) => html`<div key=${t.id} class="toast" data-role=${t.role}>
          <${Icon} name=${t.role === 'danger' ? 'circle-x' : t.role === 'success' ? 'circle-check' : t.role === 'warning' ? 'triangle-alert' : 'info'} />
          <span class="toast-msg">${t.message}</span>
          ${t.action
            ? html`<${LinkButton} variant="ghost" size="sm" className="toast-action" href=${t.action.hash} onClick=${() => dismissToast(t.id)}>${t.action.label}</${LinkButton}>`
            : null}
          <${Button} variant="ghost" size="sm" icon="x" aria-label="Dismiss" onClick=${() => dismissToast(t.id)} />
        </div>`,
      )}
    </div>
  `;
}
