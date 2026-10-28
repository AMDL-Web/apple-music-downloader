// Entry point: apply the stored theme before the first paint, then mount.

import { html, render } from './ui/preact.js';
import { App, applyTheme, loadTheme } from './app.js';

applyTheme(loadTheme());
const root = document.getElementById('app');
root.textContent = '';
render(html`<${App} />`, root);
