// "Show config keys and defaults": a per-device preference that every
// Settings section reads, toggled from the page head (app.js draws the
// button there while Settings is open).

import { html, useState, useEffect } from '../ui/preact.js';
import { readPref, writePref } from '../lib/prefs.js';
import { Button } from '../ui/components.js';

const KEYS_PREF = 'amdl.console.settings.keys';
const storage = () => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

let shown = readPref(storage(), KEYS_PREF, (v) => v === '1');
const listeners = new Set();

/** Whether config keys and defaults show, and the setter; every caller follows changes. */
export function useShowKeys() {
  const [on, setOn] = useState(shown);
  useEffect(() => {
    listeners.add(setOn);
    setOn(shown);
    return () => listeners.delete(setOn);
  }, []);
  const set = (v) => {
    shown = Boolean(v);
    writePref(storage(), KEYS_PREF, shown ? '1' : '0');
    for (const fn of listeners) fn(shown);
  };
  return [on, set];
}

/** The page-head button: pressed while keys show. */
export function KeysToggle() {
  const [on, set] = useShowKeys();
  return html`<${Button}
    variant="ghost"
    icon="braces"
    className="settings-keys-btn"
    aria-pressed=${on ? 'true' : 'false'}
    aria-label="Show config keys and defaults"
    title="Show config keys and defaults"
    onClick=${() => set(!on)}
  />`;
}
