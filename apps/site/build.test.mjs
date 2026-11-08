import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { after, before, describe, test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { NOSCRIPT_STYLE, commands, esc } from './blocks.mjs';
import { LANGS, PRIVATE_ADDRESS, REF, REPO, UsageError, build, inline, loadContent, markLead, parseArgs } from './build.mjs';
import { ALBUMS, ARTISTS, PLAYLISTS, album, symbols, sprite } from './covers.mjs';
import { DK_FOLDER, GLASS_FILE, GLASS_QUALITY, GLASS_TREE, LIVE, MCP_TOOLS, SHOWN_TRACKS, libraryFolder, libraryPath } from './mocks.mjs';
import { OG_SIZE, findChrome, ogHtml, pngSize, renderOg } from './og.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const script = path.join(here, 'build.mjs');
const repo = (...p) => fs.readFileSync(path.join(here, '..', '..', ...p), 'utf8');

// The default library template, from the example config the binary ships.
const libraryTemplate = () => /^\s*library_path_format:\s*"([^"]+)"/m.exec(repo('configs', 'config.example.yaml'))[1];
const fromTemplate = (artist, albumName, n, title) =>
  `${libraryTemplate()
    .replace('{AlbumArtist}', artist)
    .replace('{AlbumName}', albumName)
    .replace('{SongNumber:02d}', String(n).padStart(2, '0'))
    .replace('{SongName}', title)}.m4a`;

// The leaves of a content file, by dotted key (array indexes as segments).
const leaves = (value, at = '') =>
  value && typeof value === 'object'
    ? Object.entries(value).flatMap(([k, v]) => leaves(v, at ? `${at}.${k}` : k))
    : [[at, value]];

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

  test('markLead turns the one bold phrase into the highlight', () => {
    assert.equal(markLead('a **b** c'), 'a <mark class="hl">b</mark> c');
    assert.equal(markLead('run `x` for **y**'), 'run <code>x</code> for <mark class="hl">y</mark>');
  });

  // The page lists the MCP server's tools; capability.go is where they are
  // declared, so a tool added or renamed there shows up here.
  test('the page lists exactly the MCP tools capability.go declares', () => {
    const src = repo('internal', 'capability', 'capability.go');
    const declared = [...new Set([...src.matchAll(/Tool: "([a-z_]+)"/g)].map((m) => m[1]))].sort();
    assert.deepEqual(Object.values(MCP_TOOLS).flat().sort(), declared);
  });

  // The file chip, card C's tree, the desktop app's SAVED TO and the agent's
  // reply show where a track lands: the default library_path_format.
  test('the paths the page shows follow the default library template', () => {
    assert.equal(libraryPath('Mira Solenne', 'Glass Weather', 1, 'Glass Weather'), fromTemplate('Mira Solenne', 'Glass Weather', 1, 'Glass Weather'));
    assert.equal(libraryPath('Nightjar Coast', 'Low Tide Signals', 12, 'Breakwater'), fromTemplate('Nightjar Coast', 'Low Tide Signals', 12, 'Breakwater'));
    const glass = fromTemplate('Mira Solenne', 'Glass Weather', 1, 'Glass Weather');
    assert.equal(GLASS_FILE, glass.split('/').pop());
    assert.equal(GLASS_TREE.find(([, kind, name]) => kind === 'file' && name.endsWith('.m4a'))[2], GLASS_FILE);
    assert.equal(DK_FOLDER, `/volume1/music/${glass.slice(0, glass.lastIndexOf('/') + 1)}`);
    assert.equal(libraryFolder('Mira Solenne', 'Glass Weather'), glass.slice(0, glass.lastIndexOf('/') + 1));
    const tide = fromTemplate('Nightjar Coast', 'Low Tide Signals', 1, 'x');
    for (const lang of LANGS) {
      const reply = loadContent(lang.code).apps.agents.chat.reply;
      assert.ok(reply.includes(`\`/volume1/music/${tide.slice(0, tide.lastIndexOf('/') + 1)}\``), `${lang.code}: agent reply folder`);
    }
  });
});

describe('copy', () => {
  const schema = JSON.parse(fs.readFileSync(path.join(here, 'content', 'schema.json'), 'utf8'));
  const files = Object.fromEntries(LANGS.map((l) => [l.code, Object.fromEntries(leaves(loadContent(l.code)))]));
  // Section 8's counting rule: links count as their text; backticks and ** do not count.
  const visible = (s) => [...s.replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/`/g, '').replace(/\*\*/g, '')].length;

  test('content/schema.json is the schema block of DESIGN.md 8.2', () => {
    const block = /### 8\.2[\s\S]*?```json\n([\s\S]*?)\n```/.exec(fs.readFileSync(path.join(here, 'DESIGN.md'), 'utf8'));
    assert.ok(block, 'DESIGN.md has no json block under 8.2');
    assert.deepEqual(JSON.parse(block[1]), schema);
  });

  test('the schema names exactly the keys of both content files, and facts it has', () => {
    for (const lang of LANGS) assert.deepEqual(Object.keys(files[lang.code]).sort(), Object.keys(schema.keys).sort(), lang.code);
    for (const [key, spec] of Object.entries(schema.keys)) {
      for (const id of spec.facts) assert.ok(id in schema.facts, `${key}: no fact ${id}`);
    }
  });

  test('every string is within its limit', () => {
    for (const [key, spec] of Object.entries(schema.keys)) {
      assert.ok(visible(files.en[key]) <= spec.maxChars, `en ${key}: ${visible(files.en[key])} > ${spec.maxChars}`);
      assert.ok(visible(files['zh-CN'][key]) <= spec.maxCharsZh, `zh-CN ${key}: ${visible(files['zh-CN'][key])} > ${spec.maxCharsZh}`);
    }
  });

  test('every string uses only the marks and links its key allows', () => {
    for (const [key, spec] of Object.entries(schema.keys)) {
      for (const lang of LANGS) {
        const s = files[lang.code][key];
        const at = `${lang.code} ${key}`;
        if (!spec.marks.includes('code')) assert.ok(!s.includes('`'), `${at}: code`);
        if (!spec.marks.includes('strong')) assert.ok(!s.includes('**'), `${at}: strong`);
        const links = [...s.matchAll(/\[[^\]]*\]\(([^)]*)\)/g)].map((m) => m[1]);
        if (!spec.marks.includes('link')) assert.deepEqual(links, [], `${at}: link`);
        for (const href of links) assert.ok((spec.links ?? []).includes(href), `${at}: ${href}`);
      }
    }
  });

  test('the copy follows the brief: no exclamation, verbatim strings, one highlight, two title lines', () => {
    for (const lang of LANGS) {
      const f = files[lang.code];
      for (const [key, s] of Object.entries(f)) assert.doesNotMatch(s, /[!！]/, `${lang.code} ${key}`);
      for (const [key, spec] of Object.entries(schema.keys)) {
        for (const v of spec.verbatim ?? []) assert.ok(f[key].includes(v), `${lang.code} ${key}: ${v}`);
      }
      assert.equal([...f['hero.lead'].matchAll(/\*\*[^*]+\*\*/g)].length, 1, `${lang.code} hero.lead`);
      assert.equal(f['hero.lead'].split('**').length, 3, `${lang.code} hero.lead`);
      assert.equal(f['hero.title'].split('\n').length, 2, `${lang.code} hero.title`);
    }
  });
});

describe('demo catalog', () => {
  const catalog = repo('internal', 'demo', 'catalog.go');
  const artistIds = Object.keys(ARTISTS).map(Number);
  const rows = [...catalog.matchAll(/\{id: "(\d+)", title: "([^"]+)", artist: artists\[(\d)\], year: (\d+), bg: "([0-9a-f]{6})", fg: "([0-9a-f]{6})"/g)].map(
    ([, id, title, artist, year, bg, fg]) => ({ id: Number(id), title, artist: artistIds[Number(artist)], year: Number(year), bg: `#${bg}`, fg: `#${fg}` }),
  );

  // A symbol "uses" a color when it or anything it references (gradients,
  // patterns, shared groups) paints with it.
  const spriteHtml = sprite();
  const byId = (id) => {
    const at = spriteHtml.indexOf(` id="${id}"`);
    if (at < 0) return '';
    const start = spriteHtml.lastIndexOf('<', at);
    const tag = /^<([a-zA-Z]+)/.exec(spriteHtml.slice(start))[1];
    const re = new RegExp(`<${tag}\\b[^>]*?(/?)>|</${tag}>`, 'g');
    re.lastIndex = start;
    let depth = 0;
    for (let m; (m = re.exec(spriteHtml)); ) {
      if (m[0].startsWith('</')) depth--;
      else if (!m[1]) depth++;
      if (depth === 0) return spriteHtml.slice(start, re.lastIndex);
    }
    return '';
  };
  const reach = (id) => {
    const seen = new Set();
    const parts = [];
    for (const queue = [id]; queue.length; ) {
      const next = queue.pop();
      if (seen.has(next)) continue;
      seen.add(next);
      const body = byId(next);
      parts.push(body);
      for (const m of body.matchAll(/(?:url\(#|href="#)([^)"]+)/g)) queue.push(m[1]);
    }
    return parts.join('').toLowerCase();
  };

  test('the covers module holds the eight catalog albums, in its order', () => {
    assert.equal(rows.length, 8);
    assert.deepEqual(
      ALBUMS.map(({ id, title, artist, year, bg, fg }) => ({ id, title, artist, year, bg, fg })),
      rows,
    );
  });

  test('each album has a full, a mid and a small symbol in its own colors', () => {
    for (const a of ALBUMS) {
      const s = symbols(a.key);
      assert.ok(s.full.startsWith(`<symbol id="cv-${a.key}" `), a.key);
      assert.ok(s.mid.startsWith(`<symbol id="cv-${a.key}-m" `), a.key);
      assert.ok(s.small.startsWith(`<symbol id="cv-${a.key}-s" `), a.key);
      // The mid and small symbols carry no sleeve type.
      assert.doesNotMatch(reach(`cv-${a.key}-m`), /<text/, `cv-${a.key}-m`);
      assert.doesNotMatch(reach(`cv-${a.key}-s`), /<text/, `cv-${a.key}-s`);
      for (const id of [`cv-${a.key}`, `cv-${a.key}-m`, `cv-${a.key}-s`]) {
        const used = reach(id);
        assert.ok(used.includes(a.bg), `${id} does not use ${a.bg}`);
        assert.ok(used.includes(a.fg), `${id} does not use ${a.fg}`);
      }
    }
  });

  // The years, genres and track counts the mocks show come from ALBUMS, which
  // the test above holds to catalog.go.
  test('the facts the mocks show for an album match the catalog', () => {
    const byKey = Object.fromEntries(ALBUMS.map((a) => [a.key, { ...a, row: rows.find((r) => r.id === a.id) }]));
    for (const c of LIVE) {
      const a = byKey[c.key];
      assert.match(c.facts, new RegExp(`^${a.genre} · ${a.row.year} · Started \\d\\d:\\d\\d$`), c.key);
      assert.equal(c.cells, a.tracks.length, c.key);
    }
  });

  test('every track and playlist the page shows is in the catalog', () => {
    for (const a of ALBUMS) for (const [title] of a.tracks) assert.ok(catalog.includes(`"${title}"`), `${a.title}: ${title}`);
    const glass = album('glass').tracks.map(([t]) => t);
    for (const title of SHOWN_TRACKS.glass) assert.ok(glass.includes(title), title);
    assert.ok(catalog.includes('"Slow Lightning Radio"'));
    for (const [name] of PLAYLISTS) assert.ok(catalog.includes(`"${name}"`), name);
  });
});

describe('Open Graph card', () => {
  test('the card carries both title lines, the eyebrow and three covers', () => {
    const c = loadContent('en');
    const html = ogHtml(c);
    assert.doesNotMatch(html, /\{\{/);
    const [one, two] = c.hero.title.split('\n');
    assert.ok(html.includes(esc(one)), one);
    assert.ok(html.includes(`<span>${esc(two)}</span>`), two);
    assert.ok(html.includes(esc(c.hero.eyebrow)), 'eyebrow');
    const sleeves = [...html.matchAll(/<svg class="sleeve[^"]*"[^>]*>([\s\S]*?)<\/svg>/g)].map((m) => m[1]);
    assert.equal(sleeves.length, 3);
    for (const sleeve of sleeves) assert.match(sleeve, /^<use href="#cv-[a-z]+"\/>$/);
    assert.match(html, /<svg class="sprite"/);
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

  const each = (fn) => {
    for (const lang of LANGS) fn(pages[`${lang.code}/index.html`], loadContent(lang.code), lang.code);
  };
  const attr = (text) => text.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
  const section = (html, id) => {
    const start = html.search(new RegExp(`<section [^>]*id="${id}"`));
    assert.ok(start > 0, `no #${id} section`);
    return html.slice(start, html.indexOf('</section>', start));
  };
  // The whole element that opens at `start`, by counting its tag.
  const element = (html, start) => {
    const tag = /^<([a-z]+)/.exec(html.slice(start))[1];
    const re = new RegExp(`<${tag}\\b|</${tag}>`, 'g');
    re.lastIndex = start;
    let depth = 0;
    for (let m; (m = re.exec(html)); ) {
      depth += m[0].startsWith('</') ? -1 : 1;
      if (depth === 0) return html.slice(start, re.lastIndex);
    }
    assert.fail(`unclosed <${tag}> at ${start}`);
  };
  const elements = (html, opening) => [...html.matchAll(opening)].map((m) => element(html, m.index));
  const text = (html) => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&amp;/g, '&');

  test('the site is its pages and the favicon, with no app inside', () => {
    assert.deepEqual(fs.readdirSync(out).sort(), ['en', 'favicon.svg', 'index.html', 'zh-CN']);
    each((html) => assert.doesNotMatch(html, /<iframe|<img |srcset=|demo\//));
  });

  test('each section is named by its title alone', () => {
    each((html, c, code) => {
      for (const id of ['how', 'formats', 'apps', 'install']) {
        const h2 = new RegExp(`<h2 id="${id}-title">([^<]*)</h2>`).exec(html);
        assert.ok(h2, `${code}: #${id} h2 holds only its title`);
        assert.equal(h2[1], esc(c[id].title), `${code}: #${id}`);
      }
    });
  });

  test('every nav link leads to a section', () => {
    each((html, c, code) => {
      const nav = /<nav[\s\S]*?<\/nav>/.exec(html)[0];
      const anchors = [...new Set([...nav.matchAll(/href="#([^"]+)"/g)].map((m) => m[1]))];
      assert.deepEqual(anchors, ['how', 'formats', 'apps', 'install'], code);
      for (const id of anchors) section(html, id);
    });
  });

  test('the apps are a four-step stepper, each step a stage with one mock and two arrows', () => {
    assert.match(NOSCRIPT_STYLE, /\.stage-arrow/);
    each((html, c, code) => {
      const sec = section(html, 'apps');
      const list = elements(sec, /<div[^>]+role="tablist"/g);
      assert.equal(list.length, 1, `${code}: one tab list`);
      const tabs = [...list[0].matchAll(/role="tab" id="([^"]+)" aria-controls="([^"]+)"/g)];
      const names = ['desktop', 'console', 'terminal', 'agents'];
      assert.deepEqual(tabs.map((m) => m[1]), names.map((n) => `tab-apps-${n}`), code);
      for (const [i, name] of names.entries()) {
        const at = sec.indexOf(`id="${tabs[i][2]}"`);
        assert.ok(at > 0, `${code}: no panel for ${name}`);
        const panel = element(sec, sec.lastIndexOf('<', at));
        const stages = elements(panel, /<div class="stage[ "]/g);
        assert.equal(stages.length, 1, `${code} ${name}: one stage`);
        const mocks = elements(stages[0], /<div class="mock[ "]/g);
        assert.equal(mocks.length, 1, `${code} ${name}: one mock`);
        assert.match(mocks[0], new RegExp(`^<div class="mock[^"]*" role="img" aria-label="${attr(c.apps[name].label).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}"`), `${code} ${name}: label`);
        const arrows = [...stages[0].matchAll(/<button[^>]+class="stage-arrow (prev|next)"/g)].map((m) => m[1]);
        assert.deepEqual(arrows.sort(), ['next', 'prev'], `${code} ${name}: arrows`);
        assert.doesNotMatch(mocks[0], /stage-arrow/, `${code} ${name}: arrows outside the mock`);
      }
    });
  });

  test('mocks contain no controls', () => {
    each((html, c, code) => {
      const mocks = elements(html, /<div class="mock[ "]/g);
      assert.equal(mocks.length, 4, code);
      for (const mock of mocks) assert.doesNotMatch(mock, /<button|<a |<input/, `${code}: ${mock.slice(0, 80)}`);
    });
  });

  test('the install tabs are named by install.tabsLabel', () => {
    each((html, c, code) => {
      assert.match(section(html, 'install'), new RegExp(`role="tablist" aria-label="${attr(c.install.tabsLabel)}"`), code);
    });
  });

  test('the terminal and the agent show their pictures and every MCP tool', () => {
    each((html, c, code) => {
      const sec = section(html, 'apps');
      // The TUI at 100 columns, and its own 46-column layout for a phone.
      assert.match(sec, /<pre class="tui tui-wide">/, code);
      assert.match(sec, /<pre class="tui tui-narrow">/, code);
      for (const tool of Object.values(MCP_TOOLS).flat()) assert.ok(sec.includes(`<code>${tool}</code>`), `${code}: ${tool}`);
      assert.ok(sec.includes('&quot;mcpServers&quot;') || sec.includes('"mcpServers"'), `${code}: mcpServers`);
    });
  });

  test('the mocks show each album with its catalog year and track count', () => {
    each((html, c, code) => {
      const sec = text(section(html, 'apps'));
      for (const key of ['glass', 'tidal']) {
        const a = album(key);
        assert.ok(sec.includes(`${ARTISTS[a.artist]} · ${a.year} · ${a.tracks.length} tracks`), `${code}: ${key} search row`);
      }
      for (const card of LIVE) assert.ok(sec.includes(card.facts), `${code}: ${card.key} live card`);
    });
  });

  test('mock words are tagged English inside, and the label keeps the page language', () => {
    each((html, c, code) => {
      for (const mock of elements(html, /<div class="mock[ "]/g)) {
        assert.doesNotMatch(/^<div[^>]*>/.exec(mock)[0], /\slang=/, `${code}: lang on the frame`);
        assert.match(mock, /aria-hidden="true" lang="en"|class="ag-call" lang="en"/, `${code}: no lang="en" inside`);
      }
    });
  });

  test('the hero shelf shows five covers wide and three on a phone', () => {
    each((html, c, code) => {
      const hero = html.slice(html.indexOf('class="hero'), html.indexOf('</section>', html.indexOf('class="hero')));
      for (const [cls, count] of [['shelf-wide', 5], ['shelf-phone', 3]]) {
        const [svg] = elements(hero, new RegExp(`<svg class="shelf ${cls}"`, 'g'));
        assert.ok(svg, `${code}: no ${cls}`);
        assert.equal([...svg.matchAll(/<use href="#cv-[a-z]+"/g)].length, count, `${code}: ${cls}`);
      }
    });
  });

  test('the hero lead has one highlight, and the file chip names the saved track', () => {
    each((html, c, code) => {
      assert.equal([...html.matchAll(/<mark class="hl">/g)].length, 1, code);
      assert.equal([...html.matchAll(/<mark/g)].length, 1, code);
      const [chip] = elements(html, /<div class="file-chip"/g);
      assert.ok(chip.includes(`<b>${esc(GLASS_FILE)}</b>`), code);
      assert.equal(GLASS_FILE, '01. Glass Weather.m4a');
      assert.ok(chip.includes(`<span>${esc(GLASS_QUALITY)}</span>`), code);
      assert.equal(GLASS_QUALITY, 'ALAC 24-bit · 96 kHz');
    });
  });

  test('card C draws the default folders with artist.jpg and cover.jpg in place', () => {
    const folders = libraryTemplate().split('/').slice(0, -1);
    const names = folders.map((f) => `${f.replace('{AlbumArtist}', 'Mira Solenne').replace('{AlbumName}', 'Glass Weather')}/`);
    each((html, c, code) => {
      const [tree] = elements(section(html, 'how'), /<figure class="tree"/g);
      const lines = text(tree).split('\n').map((l) => l.replace(/^[\s│├└─]+/, ''));
      const dirs = lines.filter((l) => l.endsWith('/'));
      assert.deepEqual(dirs, names, code);
      const artistAt = lines.indexOf(names.at(-2));
      const albumAt = lines.indexOf(names.at(-1));
      assert.ok(lines.indexOf('artist.jpg') > artistAt && lines.indexOf('artist.jpg') < albumAt, `${code}: artist.jpg`);
      assert.equal(lines[albumAt + 1], GLASS_FILE, `${code}: first track`);
      assert.equal(lines.at(-1), 'cover.jpg', `${code}: cover.jpg`);
      assert.ok(html.includes(esc(DK_FOLDER)), `${code}: SAVED TO`);
    });
  });

  test('two theme switches, header and menu, each Light, Dark and System', () => {
    each((html, c, code) => {
      const groups = elements(html, /<div class="theme theme-(?:head|menu)"[^>]*role="radiogroup"/g);
      assert.equal([...html.matchAll(/\srole="radiogroup"/g)].length, 2, code);
      assert.equal(groups.length, 2, code);
      for (const g of groups) assert.deepEqual([...g.matchAll(/role="radio"[^>]*data-theme-set="([a-z]+)"/g)].map((m) => m[1]), ['light', 'dark', 'system'], code);
      assert.ok(html.includes('@media (max-width:700px){.theme-head{display:none}}'), `${code}: header switch on a phone`);
      assert.ok(html.includes('@media (min-width:701px){.theme-menu{display:none}}'), `${code}: menu switch above a phone`);
    });
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
      for (const [, id] of html.matchAll(/url\(#([^)]+)\)/g)) assert.ok(ids.has(id), `${page}: no element with id ${id}`);
      for (const [, id] of html.matchAll(/aria-(?:controls|labelledby)="([^"]+)"/g)) assert.ok(ids.has(id), `${page}: no element with id ${id}`);
    }
  });

  test('pages load nothing from another host', () => {
    for (const [page, html] of Object.entries(pages)) {
      assert.doesNotMatch(html, /<script[^>]+src=/, `${page} loads a script`);
      assert.doesNotMatch(html, /<link[^>]+rel="(?:stylesheet|preload|preconnect|dns-prefetch)"/, `${page} links a resource`);
      assert.doesNotMatch(html, /@import|url\((?!#)/, `${page} pulls in CSS or images`);
    }
  });

  // The policy allows the page's own inline scripts and styles by hash, and
  // nothing else: no style attributes, no handlers, no other host.
  test('the Content-Security-Policy covers exactly the inline scripts and styles', () => {
    const hash = (t) => `'sha256-${crypto.createHash('sha256').update(t, 'utf8').digest('base64')}'`;
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
      assert.doesNotMatch(html, /javascript:/, `${page} has a javascript: URL`);
    }
  });

  test('no private addresses leak into the pages', () => {
    for (const [page, html] of Object.entries(pages)) {
      assert.doesNotMatch(html, PRIVATE_ADDRESS, page);
    }
  });
});
