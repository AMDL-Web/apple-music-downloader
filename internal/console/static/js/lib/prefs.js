// Display preference: the color theme (system/light/dark), stored per browser.
// Pure: the DOM and storage access live in app.js.

export const THEME_KEY = 'amdl.console.theme';
export const THEMES = ['system', 'light', 'dark'];
export const DEFAULT_THEME = 'system';

/** A stored theme value, or the default when it is missing or unknown. */
export function normalizeTheme(value) {
  return THEMES.includes(value) ? value : DEFAULT_THEME;
}

/** Reads a preference through `normalize`; a throwing storage (private mode, blocked site data) yields the default. */
export function readPref(storage, key, normalize) {
  try {
    return normalize(storage ? storage.getItem(key) : null);
  } catch {
    return normalize(null);
  }
}

/** Writes a preference; returns false when storage refused it (the choice then lasts for this page view). */
export function writePref(storage, key, value) {
  try {
    if (!storage) return false;
    storage.setItem(key, value);
    return true;
  } catch {
    return false;
  }
}
