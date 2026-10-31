// Entry of the console demo. build.mjs puts this directory beside a copy of
// apps/console/static and loads this module before the console's main.js, so
// the fetch it installs answers every /api/v1 call, streams included, before
// the console makes one. The relative imports resolve in that copy.

import { albumProgress } from '../js/lib/album-progress.js';
import { createFakeApi, createDemoFetch } from './fake-api.js';

const TICK_MS = 800;

const api = createFakeApi({ progress: albumProgress });
window.fetch = createDemoFetch({ api, fetch: window.fetch.bind(window), origin: location.href });

// The made-up releases have no page on Apple Music, so links out of the demo
// (Open in Apple Music, an artist's page) stay where they are.
document.addEventListener(
  'click',
  (e) => {
    const a = e.target instanceof Element ? e.target.closest('a[href]') : null;
    if (a && new URL(a.href, location.href).origin !== location.origin) e.preventDefault();
  },
  true,
);

// Jobs move while the demo is on screen: the page that embeds it says when it
// scrolls out of view, and a hidden tab stops it too.
let offscreen = false;
setInterval(() => {
  if (!offscreen && !document.hidden) api.tick();
}, TICK_MS);

// The console wears the website's theme: the theme the embedding page has on
// <html>, or the one the visitor last picked on the site, else the system's.
// Stored under the console's own key (lib/prefs.js THEME_KEY) as well, so the
// console starts in it instead of putting its own back.
const wear = (theme) => {
  const t = theme === 'light' || theme === 'dark' ? theme : 'system';
  try {
    localStorage.setItem('amdl.console.theme', t);
  } catch {}
  if (t === 'system') document.documentElement.removeAttribute('data-theme');
  else document.documentElement.setAttribute('data-theme', t);
};

let host = null;
try {
  if (window.parent !== window) host = window.parent.document.documentElement;
} catch {
  // Framed by another origin: nothing to follow.
}
if (host) {
  wear(host.getAttribute('data-theme'));
  new MutationObserver(() => wear(host.getAttribute('data-theme'))).observe(host, { attributes: true, attributeFilter: ['data-theme'] });
  window.addEventListener('message', (e) => {
    if (e.origin === location.origin && e.data && typeof e.data.amdlDemoOffscreen === 'boolean') offscreen = e.data.amdlDemoOffscreen;
  });
} else {
  let site = null;
  try {
    site = localStorage.getItem('amdl-site-theme');
  } catch {}
  wear(site);
}
