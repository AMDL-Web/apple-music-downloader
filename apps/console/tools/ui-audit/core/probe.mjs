// Builds the expression page.evaluate runs to install window.__uia: the pure
// libraries with their `export` keywords stripped, then page/probe.js.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const read = (p) => readFileSync(fileURLToPath(new URL(p, import.meta.url)), 'utf8');

let cached = null;

export function probeSource() {
  if (cached) return cached;
  const libs = ['./lib/color.mjs', './lib/geometry.mjs', './lib/text.mjs'].map((p) => read(p).replace(/^export /gm, ''));
  cached = `(() => {\n${libs.join('\n')}\n${read('./page/probe.js')}\nreturn true;\n})()`;
  return cached;
}
