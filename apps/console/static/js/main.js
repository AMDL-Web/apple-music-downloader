// Entry point: apply the stored theme, load the icon sprite into the
// document (ui/icons.js), then mount over the loading shell in index.html.

import { html, render } from './ui/preact.js';
import { App, applyTheme, loadTheme } from './app.js';
import { loadSprite } from './ui/icons.js';

applyTheme(loadTheme());
// The last input, a pointer or a key: a dialog opened by a click or a tap
// draws no ring on the button it focuses (base.css).
for (const [type, input] of [['pointerdown', 'pointer'], ['keydown', 'key']]) {
  window.addEventListener(type, () => (document.documentElement.dataset.input = input), true);
}
// Mount after the sprite so no icon references the external file; a failed
// load (bounded by its own timeout) mounts anyway with the external sprite.
loadSprite().finally(() => {
  const root = document.getElementById('app');
  root.textContent = '';
  render(html`<${App} />`, root);
});
