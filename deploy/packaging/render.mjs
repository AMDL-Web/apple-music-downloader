// Renders the Homebrew formula and the Scoop manifest for a release from the
// templates beside this file and the release's SHA256SUMS:
//
//   node deploy/packaging/render.mjs v2.0.0 dist/SHA256SUMS dist
//
// writes dist/amdl.rb and dist/amdl.json. A placeholder without a value, such
// as a platform missing from SHA256SUMS, fails the run.
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

// sums maps "darwin_arm64" to the archive's SHA-256 for the tag's archives.
export function parseSums(text, tag) {
  const sums = {};
  const name = new RegExp(`^\\*?amdl_${tag.replace(/\./g, '\\.')}_([a-z0-9]+_[a-z0-9]+)\\.(tar\\.gz|zip)$`);
  for (const line of text.split('\n')) {
    const [hash, file] = line.trim().split(/\s+/);
    const m = file && name.exec(file);
    if (m && /^[0-9a-f]{64}$/.test(hash)) sums[m[1]] = hash;
  }
  return sums;
}

export function render(template, tag, sums) {
  if (!/^v\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$/.test(tag)) throw new Error(`not a release tag: ${tag}`);
  return template.replace(/\{\{\s*(tag|version|sha256 ([a-z0-9_]+))\s*\}\}/g, (all, key, platform) => {
    if (key === 'tag') return tag;
    if (key === 'version') return tag.slice(1);
    if (!sums[platform]) throw new Error(`SHA256SUMS has no archive for ${platform}`);
    return sums[platform];
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [tag, sumsPath, out] = process.argv.slice(2);
  if (!tag || !sumsPath || !out) {
    console.error('usage: node deploy/packaging/render.mjs <tag> <SHA256SUMS> <out-dir>');
    process.exit(2);
  }
  const sums = parseSums(readFileSync(sumsPath, 'utf8'), tag);
  for (const [tmpl, file] of [['amdl.rb.tmpl', 'amdl.rb'], ['amdl.json.tmpl', 'amdl.json']]) {
    const text = render(readFileSync(join(here, tmpl), 'utf8'), tag, sums);
    if (file.endsWith('.json')) JSON.parse(text);
    writeFileSync(join(out, file), text);
    console.log(`wrote ${join(out, file)}`);
  }
}
