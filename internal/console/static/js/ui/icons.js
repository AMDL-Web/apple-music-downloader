// The icon sprite in the document. icons.svg is served no-cache, so every
// new <use href="icons.svg#..."> revalidates it, and while the server is
// down that fails and the icon paints nothing. loadSprite() fetches the
// sprite once, imports its <symbol>s into a hidden <svg class="sprite"> at
// the start of <body> (outside #app, which main.js clears) and from then on
// iconHref() points at "#i-name" in this document. Until it has loaded, or
// when the fetch fails, icons use the external sprite.

const SVG_NS = 'http://www.w3.org/2000/svg';
const LOAD_TIMEOUT_MS = 5000;

let inline = false;
let pending = null;

/** The <use> href for an icon: the in-document symbol once the sprite is loaded. */
export const iconHref = (name) => (inline ? `#i-${name}` : `icons.svg#i-${name}`);

/** Whether the sprite is in the document. */
export const spriteLoaded = () => inline;

async function fetchSprite() {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), LOAD_TIMEOUT_MS);
  try {
    const res = await fetch('icons.svg', { signal: ctrl.signal });
    if (!res.ok) return false;
    const doc = new DOMParser().parseFromString(await res.text(), 'image/svg+xml');
    const symbols = doc.querySelectorAll('symbol[id]');
    if (symbols.length === 0) return false;
    const host = document.createElementNS(SVG_NS, 'svg');
    host.setAttribute('class', 'sprite');
    host.setAttribute('aria-hidden', 'true');
    host.setAttribute('focusable', 'false');
    for (const s of symbols) host.appendChild(document.importNode(s, true));
    document.body.prepend(host);
    inline = true;
    return true;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Loads the sprite into the document once; resolves true when it is there.
 * Calls while a load is running share it; after a failure the next call
 * tries again (the shell retries when the feed reconnects).
 */
export function loadSprite() {
  if (inline) return Promise.resolve(true);
  if (!pending) pending = fetchSprite().finally(() => (pending = null));
  return pending;
}
