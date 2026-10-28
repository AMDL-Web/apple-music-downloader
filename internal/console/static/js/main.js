// Entry point: apply the stored theme and skin before the first paint, then mount.

import { html, render } from './ui/preact.js';
import { App, applyTheme, loadTheme, applySkin, loadSkin } from './app.js';

applyTheme(loadTheme());
applySkin(loadSkin());
const root = document.getElementById('app');
root.textContent = '';
render(html`<${App} />`, root);
