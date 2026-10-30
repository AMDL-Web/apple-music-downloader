import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { parseSums, render } from './render.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const tag = 'v2.0.0';
const hash = (c) => c.repeat(64);
// SHA256SUMS as make dist writes it: sha256sum's "<hash>  <file>" lines.
const sumsText = [
  `${hash('a')}  amdl_v2.0.0_darwin_amd64.tar.gz`,
  `${hash('b')}  amdl_v2.0.0_darwin_arm64.tar.gz`,
  `${hash('c')}  amdl_v2.0.0_linux_amd64.tar.gz`,
  `${hash('d')}  amdl_v2.0.0_linux_arm64.tar.gz`,
  `${hash('e')}  amdl_v2.0.0_windows_amd64.zip`,
  `${hash('f')}  amdl_v1.9.0_windows_amd64.zip`,
].join('\n');

test('parseSums keeps the tag\'s archives', () => {
  const sums = parseSums(sumsText, tag);
  assert.deepEqual(Object.keys(sums).sort(), ['darwin_amd64', 'darwin_arm64', 'linux_amd64', 'linux_arm64', 'windows_amd64']);
  assert.equal(sums.windows_amd64, hash('e'));
});

test('the formula names each platform\'s archive with its hash', () => {
  const rb = render(readFileSync(join(here, 'amdl.rb.tmpl'), 'utf8'), tag, parseSums(sumsText, tag));
  assert.match(rb, /version "2\.0\.0"/);
  assert.match(rb, /depends_on "ffmpeg"/);
  assert.match(rb, /download\/v2\.0\.0\/amdl_v2\.0\.0_darwin_arm64\.tar\.gz"\n\s+sha256 "b{64}"/);
  assert.match(rb, /amdl_v2\.0\.0_linux_amd64\.tar\.gz"\n\s+sha256 "c{64}"/);
  assert.doesNotMatch(rb, /\{\{/);
});

test('the Scoop manifest is JSON with ffmpeg as a dependency', () => {
  const m = JSON.parse(render(readFileSync(join(here, 'amdl.json.tmpl'), 'utf8'), tag, parseSums(sumsText, tag)));
  assert.equal(m.version, '2.0.0');
  assert.equal(m.depends, 'ffmpeg');
  assert.equal(m.architecture['64bit'].hash, hash('e'));
  assert.equal(m.architecture['64bit'].extract_dir, 'amdl_v2.0.0_windows_amd64');
  assert.equal(m.bin, 'amdl.exe');
});

test('a platform missing from SHA256SUMS fails the render', () => {
  const sums = parseSums(sumsText, tag);
  delete sums.linux_arm64;
  assert.throws(() => render(readFileSync(join(here, 'amdl.rb.tmpl'), 'utf8'), tag, sums), /linux_arm64/);
  assert.throws(() => render('{{tag}}', 'main', {}), /not a release tag/);
});
