import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { after, before, describe, test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { commands } from './blocks.mjs';
import { LANGS, PRIVATE_ADDRESS, REF, REPO, UsageError, build, inline, loadContent, parseArgs } from './build.mjs';
import { glassPath } from './mocks.mjs';
import { OG_SIZE, findChrome, ogHtml, pngSize, renderOg } from './og.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const script = path.join(here, 'build.mjs');

describe('arguments', () => {
  test('defaults', () => {
    const opts = parseArgs([]);
    assert.equal(opts.out, path.join(here, 'dist'));
    assert.equal(opts.base, '');
    assert.equal(opts.serve, 0);
  });

  test('--base gains a trailing slash', () => {
    assert.equal(parseArgs(['--base', 'https://example.org/amdl']).base, 'https://example.org/amdl/');
    assert.equal(parseArgs(['--base', 'https://example.org/']).base, 'https://example.org/');
  });

  for (const [name, argv] of [
    ['an unknown flag', ['--outdir', 'x']],
    ['a flag without a value', ['--out']],
    ['a flag followed by another flag', ['--out', '--base']],
    ['a relative --base', ['--base', 'example.org']],
    ['a non-http --base', ['--base', 'ftp://example.org/']],
    ['a --serve that is not a port', ['--serve', 'abc']],
    ['a --serve out of range', ['--serve', '70000']],
    ['the retired --desktop', ['--desktop', 'x']],
    ['the retired --tui', ['--tui', 'x']],
  ]) {
    test(`rejects ${name}`, () => assert.throws(() => parseArgs(argv), UsageError));
  }

  test('bad arguments print usage and exit 2', () => {
    const run = spawnSync(process.execPath, [script, '--nope'], { encoding: 'utf8' });
    assert.equal(run.status, 2);
    assert.match(run.stderr, /unknown argument: --nope/);
    assert.match(run.stderr, /^usage: /m);
  });
});

describe('content', () => {
  // Both languages render from one template, so they need the same shape.
  const shape = (value, at) => {
    if (Array.isArray(value)) return value.map((v, i) => shape(v, `${at}[${i}]`));
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, shape(v, `${at}.${k}`)]));
    assert.equal(typeof value, 'string', `${at} is not a string`);
    assert.notEqual(value.trim(), '', `${at} is empty`);
    return typeof value;
  };

  test('every language has the same keys and list lengths, and no empty strings', () => {
    const [first, ...rest] = LANGS.map((l) => shape(loadContent(l.code), l.code));
    for (const other of rest) assert.deepEqual(other, first);
  });

  test('a template placeholder survives translation', () => {
    for (const lang of LANGS) {
      const c = loadContent(lang.code);
      assert.match(c.product.queue.track, /\{n\}.*\{total\}/, lang.code);
    }
  });

  test('a copy button takes whole commands, continuation lines included', () => {
    const block = ["$ curl -X POST http://x/downloads \\", "    -d '{}'", '{"accepted":1}', '', '$ curl -N http://x/events', 'PS> irm http://x | iex'];
    assert.equal(commands(block), "curl -X POST http://x/downloads \\\n    -d '{}'\ncurl -N http://x/events\nirm http://x | iex");
  });

  test('inline marks', () => {
    assert.equal(inline('run `a <b>` now'), 'run <code>a &lt;b&gt;</code> now');
    assert.equal(inline('**Settings › Account**'), '<strong>Settings › Account</strong>');
    assert.equal(
      inline('see [the guide]({blob}/docs/deployment.md)'),
      `see <a href="${REPO}/blob/${REF}/docs/deployment.md">the guide</a>`,
    );
    assert.equal(inline('`**not bold**`'), '<code>**not bold**</code>');
  });

  // The Library view and the specs show where a track lands; that is the
  // default library_path_format, so the page cannot drift from the config.
  test('the path the page shows follows the default library template', () => {
    const config = fs.readFileSync(path.join(here, '..', '..', 'configs', 'config.example.yaml'), 'utf8');
    const template = /^\s*library_path_format:\s*"([^"]+)"/m.exec(config)[1];
    const want = `${template
      .replace('{AlbumArtist}', 'Mira Solenne')
      .replace('{AlbumName}', 'Glass Weather')
      .replace('{SongNumber:02d}', '01')
      .replace('{SongName}', 'Glass Weather')}.m4a`;
    assert.equal(glassPath(0), want);
  });
});

describe('Open Graph card', () => {
  test('the card carries the English hero and the four stages', () => {
    const c = loadContent('en');
    const html = ogHtml(c);
    assert.doesNotMatch(html, /\{\{/);
    for (const line of c.hero.title.split('\n')) assert.ok(html.includes(line), line);
    for (const stage of c.hero.stages) assert.ok(html.includes(stage), stage);
    assert.match(html, /<svg class="wave"/);
  });

  test('pngSize reads a PNG header and rejects other files', () => {
    const head = Buffer.alloc(24);
    head.write('\x89PNG\r\n\x1a\n', 0, 'latin1');
    head.write('IHDR', 12, 'ascii');
    head.writeUInt32BE(1200, 16);
    head.writeUInt32BE(630, 20);
    assert.deepEqual(pngSize(head), { width: 1200, height: 630 });
    assert.throws(() => pngSize(Buffer.from('not an image at all, honestly')), /not a PNG/);
  });

  let chrome = '';
  try {
    chrome = findChrome();
  } catch {}
  test('Chrome draws it at its size', { skip: !chrome && 'no Chrome here' }, () => {
    const out = fs.mkdtempSync(path.join(os.tmpdir(), 'amdl-og-'));
    try {
      assert.deepEqual(pngSize(fs.readFileSync(renderOg(out, { chrome }))), OG_SIZE);
    } finally {
      fs.rmSync(out, { recursive: true, force: true });
    }
  });
});

describe('built site', () => {
  let out;
  let pages;
  before(() => {
    out = fs.mkdtempSync(path.join(os.tmpdir(), 'amdl-site-'));
    build({ out, base: 'https://example.org/amdl/' });
    pages = Object.fromEntries([...LANGS.map((l) => `${l.code}/index.html`), 'index.html'].map((p) => [p, fs.readFileSync(path.join(out, p), 'utf8')]));
  });
  after(() => fs.rmSync(out, { recursive: true, force: true }));

  test('the site is its pages and the favicon, with no app inside', () => {
    assert.deepEqual(fs.readdirSync(out).sort(), ['en', 'favicon.svg', 'index.html', 'zh-CN']);
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      assert.doesNotMatch(html, /<iframe|<img |srcset=|demo\//);
    }
  });

  test('each page shows the three product views, each with its one button', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      const c = loadContent(lang.code);
      for (const [view, button] of [
        ['search', 'data-dl'],
        ['queue', 'data-retry'],
        ['library', 'data-fix'],
      ]) {
        const start = html.indexOf(`id="panel-${view}"`);
        assert.ok(start > 0, `${lang.code}: no ${view} panel`);
        const panel = html.slice(start, html.indexOf('</div></div></div>', start));
        assert.match(panel, new RegExp(`role="group" aria-label="${c.product[view].label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}"`), `${lang.code} ${view}`);
        assert.ok(html.includes(`<p class="app-title">${c.product[view].page}</p>`), `${lang.code}: ${view} title`);
        assert.ok(html.slice(start).includes(button), `${lang.code}: ${view} has no ${button}`);
        assert.match(html, new RegExp(`role="tab" id="tab-${view}" aria-controls="panel-${view}"`));
      }
      // The hero's waveform: four stages, one group each.
      assert.equal([...html.matchAll(/<g class="st-\d">/g)].length, 4);
    }
  });

  test('each language page declares its language and links the other', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      assert.match(html, new RegExp(`<html lang="${lang.code}">`));
      assert.match(html, new RegExp(`<link rel="canonical" href="https://example.org/amdl/${lang.code}/">`));
      assert.match(html, /<link rel="alternate" hreflang="x-default" href="https:\/\/example.org\/amdl\/">/);
      for (const other of LANGS) {
        assert.match(html, new RegExp(`<link rel="alternate" hreflang="${other.code}" href="https://example.org/amdl/${other.code}/">`));
      }
    }
  });

  test('the root page offers every language', () => {
    for (const lang of LANGS) assert.match(pages['index.html'], new RegExp(`href="${lang.code}/"`));
  });

  test('every local link and anchor resolves', () => {
    for (const [page, html] of Object.entries(pages)) {
      const ids = new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));
      for (const [, ref] of html.matchAll(/\s(?:href|src)="([^"]+)"/g)) {
        if (/^https?:/.test(ref)) continue;
        if (ref.startsWith('#')) {
          assert.ok(ids.has(ref.slice(1)), `${page}: no element with id ${ref}`);
          continue;
        }
        const target = path.join(out, path.dirname(page), ref);
        const file = ref.endsWith('/') ? path.join(target, 'index.html') : target;
        assert.ok(fs.existsSync(file), `${page}: ${ref} does not exist`);
      }
      for (const [, id] of html.matchAll(/aria-(?:controls|labelledby)="([^"]+)"/g)) assert.ok(ids.has(id), `${page}: no element with id ${id}`);
    }
  });

  test('pages load nothing from another host', () => {
    for (const [page, html] of Object.entries(pages)) {
      assert.doesNotMatch(html, /<script[^>]+src=/, `${page} loads a script`);
      assert.doesNotMatch(html, /<link[^>]+rel="(?:stylesheet|preload|preconnect|dns-prefetch)"/, `${page} links a resource`);
      assert.doesNotMatch(html, /@import|url\(/, `${page} pulls in CSS or images`);
    }
  });

  // The policy allows the page's own inline scripts and styles by hash, and
  // nothing else: no style attributes, no handlers, no other host.
  test('the Content-Security-Policy covers exactly the inline scripts and styles', () => {
    const hash = (text) => `'sha256-${crypto.createHash('sha256').update(text, 'utf8').digest('base64')}'`;
    for (const [page, html] of Object.entries(pages)) {
      const policy = /<meta http-equiv="Content-Security-Policy" content="([^"]+)">/.exec(html)?.[1];
      assert.ok(policy, `${page} has no policy`);
      const rules = Object.fromEntries(policy.split('; ').map((r) => [r.split(' ')[0], r.split(' ').slice(1)]));
      assert.deepEqual(rules['default-src'], ["'none'"]);
      assert.doesNotMatch(policy, /unsafe-|https?:|\*/);
      const scripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => hash(m[1]));
      const styles = [...html.matchAll(/<style>([\s\S]*?)<\/style>/g)].map((m) => hash(m[1]));
      assert.ok(scripts.length && styles.length);
      assert.deepEqual(rules['script-src'].sort(), scripts.sort(), `${page}: script hashes`);
      assert.deepEqual(rules['style-src'].sort(), [...new Set(styles)].sort(), `${page}: style hashes`);
      assert.doesNotMatch(html, /\sstyle="/, `${page} has a style attribute`);
      assert.doesNotMatch(html, /\son[a-z]+="/, `${page} has an inline handler`);
    }
  });

  test('no private addresses leak into the pages', () => {
    for (const [page, html] of Object.entries(pages)) {
      assert.doesNotMatch(html, PRIVATE_ADDRESS, page);
    }
  });
});
