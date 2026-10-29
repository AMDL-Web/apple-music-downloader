// Moving focus to a Settings field named in the address (#/settings?field=),
// which the command palette's field results open.

import { useEffect } from './preact.js';
import { settingsField } from '../lib/route.js';

const CONTROLS = 'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex="0"]';
const GIVE_UP_MS = 3000;

/**
 * Focuses the control of the Settings field `key` (a pinned field has none to
 * focus) and scrolls it into view, waiting for the page to render it (the config loads first) for at most
 * GIVE_UP_MS; any key or click of the user's own cancels. Returns the cancel.
 */
export function focusField(key) {
  const until = Date.now() + GIVE_UP_MS;
  let frame = 0;
  const stop = () => {
    cancelAnimationFrame(frame);
    window.removeEventListener('keydown', stop, true);
    window.removeEventListener('pointerdown', stop, true);
  };
  const look = () => {
    const field = document.querySelector(`.field[data-key="${key}"]`);
    const box = field && field.querySelector('.field-control');
    const control = box && box.querySelector(CONTROLS);
    if (control || box) {
      // A pinned field renders its control disabled: nothing to focus, still worth scrolling to.
      if (control) control.focus({ preventScroll: true });
      field.scrollIntoView({ block: 'center' });
      stop();
    } else if (Date.now() < until) frame = requestAnimationFrame(look);
    else stop();
  };
  window.addEventListener('keydown', stop, true);
  window.addEventListener('pointerdown', stop, true);
  frame = requestAnimationFrame(look);
  return stop;
}

/** App(): focus the field a Settings address names, once its section is shown. */
export function useFieldFocus(route) {
  const field = route.page === 'settings' ? settingsField(route.params) : '';
  useEffect(() => (field ? focusField(field) : undefined), [field, route.params.section]);
}
