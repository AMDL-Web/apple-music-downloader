// Puts the live demos into the built site. demo/console/ is a copy of
// apps/console/static whose index.html runs boot.js before the console, with
// this directory's modules beside it in demo/console/demo/; demo/desktop/ is
// the desktop app's web build, when the build is given one.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
export const CONSOLE_STATIC = path.join(here, '..', '..', 'console', 'static');
const MODULES = ['boot.js', 'fake-api.js', 'catalog.js'];

// The console's own policy (apps/console/console.go) without the hosts it
// reads covers and motion artwork from: the demo reaches only its own files.
export const DEMO_CSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; base-uri 'none'; form-action 'none'";

/** The console's index.html turned into the demo's; throws when a mark it edits has moved. */
export function demoIndex(html) {
  const edits = [
    ['<meta charset="utf-8">', `<meta charset="utf-8">\n    <meta http-equiv="Content-Security-Policy" content="${DEMO_CSP}">\n    <meta name="robots" content="noindex">`],
    ['<title>AMDL Console</title>', '<title>AMDL Console demo</title>'],
    ['<script type="module" src="js/main.js"></script>', '<script type="module" src="demo/boot.js"></script>\n    <script type="module" src="js/main.js"></script>'],
  ];
  for (const [mark, text] of edits) {
    if (html.split(mark).length !== 2) throw new Error(`apps/console/static/index.html no longer has exactly one ${mark}`);
    html = html.replace(mark, text);
  }
  return html;
}

export function buildDemo(out, { desktop = '' } = {}) {
  const dir = path.join(out, 'demo', 'console');
  fs.cpSync(CONSOLE_STATIC, dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'index.html'), demoIndex(fs.readFileSync(path.join(CONSOLE_STATIC, 'index.html'), 'utf8')));
  fs.mkdirSync(path.join(dir, 'demo'));
  for (const file of MODULES) fs.copyFileSync(path.join(here, file), path.join(dir, 'demo', file));
  if (desktop) fs.cpSync(desktop, path.join(out, 'demo', 'desktop'), { recursive: true });
}
