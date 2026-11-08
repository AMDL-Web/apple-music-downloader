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
import { glassPath, MCP_TOOLS } from './mocks.mjs';
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
  // The page lists the MCP server's tools; capability.go is where they are
  // declared, so a tool added or renamed there shows up here.
  test('the page lists exactly the MCP tools capability.go declares', () => {
    const src = fs.readFileSync(path.join(here, '..', '..', 'internal', 'capability', 'capability.go'), 'utf8');
    const declared = [...new Set([...src.matchAll(/Tool: "([a-z_]+)"/g)].map((m) => m[1]))].sort();
    assert.deepEqual(Object.values(MCP_TOOLS).flat().sort(), declared);
  });

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

  const attr = (text) => text.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
  const section = (html, id) => {
    const start = html.search(new RegExp(`<section [^>]*id="${id}"`));
    assert.ok(start > 0, `no #${id} section`);
    return html.slice(start, html.indexOf('</section>', start));
  };

  test('every nav link leads to a section', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      const nav = /<nav[\s\S]*?<\/nav>/.exec(html)[0];
      const anchors = [...nav.matchAll(/href="#([^"]+)"/g)].map((m) => m[1]);
      assert.deepEqual(anchors, ['desktop', 'console', 'terminal', 'agents', 'specs', 'install'], lang.code);
      for (const id of anchors) section(html, id);
    }
  });

  test('the desktop app and the console each show three screens as tabs', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      const c = loadContent(lang.code);
      for (const [product, views] of [
        ['desktop', ['queue', 'library', 'album']],
        ['console', ['new', 'overview', 'logs']],
      ]) {
        const sec = section(html, product);
        for (const view of views) {
          const id = `${product}-${view}`;
          assert.match(sec, new RegExp(`role="tab" id="tab-${id}" aria-controls="panel-${id}"`), `${lang.code} ${id} tab`);
          const start = sec.indexOf(`id="panel-${id}"`);
          assert.ok(start > 0, `${lang.code}: no ${id} panel`);
          assert.ok(sec.slice(start).includes(`aria-label="${attr(c[product].labels[view])}"`), `${lang.code} ${id} label`);
        }
      }
      // The desktop frames use the app's monospaced face; the console's, its system one.
      assert.match(html, /\.dk\{[^}]*font:[^;}]*"JetBrains Mono"/);
    }
  });

  test('each mock has its one interaction, and the others are pictures', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      const c = loadContent(lang.code);
      const panel = (id) => {
        const start = html.indexOf(`id="panel-${id}"`);
        const ends = [html.indexOf('id="panel-', start + 1), html.indexOf('</section>', start)].filter((i) => i > 0);
        return html.slice(start, Math.min(...ends));
      };
      assert.match(panel('desktop-queue'), /<button[^>]+data-pause data-alt="Resume"/, `${lang.code}: Pause`);
      assert.match(panel('console-new'), new RegExp(`<button type="button" class="cn-dl" data-dl data-done="${attr(c.console.queued)}"`), `${lang.code}: Download`);
      assert.match(section(html, 'agents'), /<button type="button" class="cn-switch" role="switch" aria-checked="false"[^>]*data-switch>/, `${lang.code}: Run hooks`);
      for (const view of ['desktop-library', 'desktop-album', 'console-overview', 'console-logs']) {
        assert.doesNotMatch(panel(view), /<button/, `${lang.code}: ${view} has a button`);
      }
    }
  });

  test('the terminal, the agents and the hero show their pictures', () => {
    for (const lang of LANGS) {
      const html = pages[`${lang.code}/index.html`];
      const c = loadContent(lang.code);
      const term = section(html, 'terminal');
      assert.ok(term.includes(`class="term term-tui" role="img" aria-label="${attr(c.terminal.tui.label)}"`), `${lang.code}: TUI`);
      assert.ok(term.includes(`class="term term-cli" role="img" aria-label="${attr(c.terminal.cli.label)}"`), `${lang.code}: CLI`);
      // The TUI at 100 columns, and its own 46-column layout for a phone.
      assert.match(term, /<pre class="tui tui-wide">/);
      assert.match(term, /<pre class="tui tui-narrow">/);
      for (const phase of ['downloading', 'decrypting', 'remuxing', 'tagging']) assert.ok(term.includes(`"${phase}"`), `${lang.code}: ${phase}`);
      const agents = section(html, 'agents');
      assert.ok(agents.includes(`class="chat" role="img" aria-label="${attr(c.agents.mcp.label)}"`), `${lang.code}: chat`);
      for (const tool of Object.values(MCP_TOOLS).flat()) assert.ok(agents.includes(`<code>${tool}</code>`), `${lang.code}: ${tool}`);
      assert.ok(agents.includes('&quot;mcpServers&quot;') || agents.includes('"mcpServers"'), `${lang.code}: mcpServers`);
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
