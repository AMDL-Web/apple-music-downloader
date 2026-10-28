// Display preferences: the color theme (system/light/dark) and the skin
// (music/editorial) are two independent axes, each stored per browser.
// Pure: the DOM and storage access live in app.js.

export const THEME_KEY = 'amdl.console.theme';
export const THEMES = ['system', 'light', 'dark'];
export const DEFAULT_THEME = 'system';

export const SKIN_KEY = 'amdl.console.skin';
/** Skins in the order the switch shows them; each has css/skins/<value>.css. */
export const SKINS = [
  { value: 'music', label: 'Apple Music', short: 'Music' },
  { value: 'editorial', label: 'Editorial', short: 'Editorial' },
];
export const DEFAULT_SKIN = 'music';

/** A stored theme value, or the default when it is missing or unknown. */
export function normalizeTheme(value) {
  return THEMES.includes(value) ? value : DEFAULT_THEME;
}

/** A stored skin value, or the default when it is missing or unknown. */
export function normalizeSkin(value) {
  return SKINS.some((s) => s.value === value) ? value : DEFAULT_SKIN;
}

/** The skin after `value` in switch order, wrapping around. */
export function nextSkin(value) {
  const i = SKINS.findIndex((s) => s.value === normalizeSkin(value));
  return SKINS[(i + 1) % SKINS.length].value;
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
