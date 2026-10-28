// Entry point: apply the stored theme and skin, load the icon sprite into the
// document (ui/icons.js), then mount over the loading shell in index.html.

import { html, render } from './ui/preact.js';
import { App, applyTheme, loadTheme, applySkin, loadSkin } from './app.js';
import { loadSprite } from './ui/icons.js';

applyTheme(loadTheme());
applySkin(loadSkin());
// Mount after the sprite so no icon references the external file; a failed
// load (bounded by its own timeout) mounts anyway with the external sprite.
loadSprite().finally(() => {
  const root = document.getElementById('app');
  root.textContent = '';
  render(html`<${App} />`, root);
});
