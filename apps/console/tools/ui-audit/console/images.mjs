// Cover images for the audit: fetched from Apple's CDN on first use and kept
// on disk ($UI_AUDIT_CACHE, default ~/.cache/amdl-ui-audit), so later runs
// draw the same covers without the network. Offline, a tinted SVG stands in.

import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';
import { createHash } from 'node:crypto';

const dir = process.env.UI_AUDIT_CACHE || join(homedir(), '.cache', 'amdl-ui-audit');
mkdirSync(dir, { recursive: true });

export async function imageRoute(route) {
  const url = route.request().url();
  const key = createHash('sha1').update(url).digest('hex');
  const file = join(dir, key);
  if (existsSync(file)) {
    const meta = JSON.parse(readFileSync(`${file}.json`, 'utf8'));
    return route.fulfill({ status: 200, contentType: meta.type, body: readFileSync(file) });
  }
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(8000) });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const body = Buffer.from(await res.arrayBuffer());
    const type = res.headers.get('content-type') || 'image/jpeg';
    writeFileSync(file, body);
    writeFileSync(`${file}.json`, JSON.stringify({ url, type }));
    return route.fulfill({ status: 200, contentType: type, body });
  } catch {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="#8a8f98"/></svg>`;
    return route.fulfill({ status: 200, contentType: 'image/svg+xml', body: svg });
  }
}
