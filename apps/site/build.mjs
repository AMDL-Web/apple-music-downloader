#!/usr/bin/env node
// Builds the AMDL website: one page per language from content/<lang>.json,
// with the stylesheet and static/site.js inlined, into a directory that any
// static host can serve at its root or under a subpath.
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { ICON, NOSCRIPT_TABS, codeBlock, esc, tabs } from './blocks.mjs';
import { agentChat, cliSession, consoleViews, desktopViews, heroStrip, hooksPanel, MCP_TOOLS, mcpConfig, specDemo, tuiScreen } from './mocks.mjs';
import { serve } from './serve.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

export const REPO = 'https://github.com/AMDL-Web/apple-music-downloader';
// Links into the repository, the install scripts and the Compose file follow the
// branch the published site describes, so a visitor reads and installs what the
// page shows.
export const REF = 'dev';
const BLOB = `${REPO}/blob/${REF}`;
const RAW = `https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/${REF}`;

export const LANGS = [
  { code: 'en', og: 'en_US' },
  { code: 'zh-CN', og: 'zh_CN' },
];

const THEME_KEY = 'amdl-site-theme';
const LANG_KEY = 'amdl-site-lang';

const USAGE = `usage: node apps/site/build.mjs [--out <dir>] [--base <url>] [--serve <port>]

  --out <dir>      where to write the site (default: apps/site/dist)
  --base <url>     absolute URL the site is served from, such as https://example.org/;
                   adds canonical, hreflang and Open Graph URLs (og.mjs draws og.png)
  --serve <port>   after building, serve the output on 127.0.0.1:<port>`;

export class UsageError extends Error {}

export function parseArgs(argv) {
  const opts = { out: path.join(here, 'dist'), base: '', serve: 0 };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    if (!['--out', '--base', '--serve'].includes(flag)) throw new UsageError(`unknown argument: ${flag}`);
    const value = argv[++i];
    if (value === undefined || value.startsWith('--')) throw new UsageError(`${flag} needs a value`);
    if (flag === '--out') opts.out = path.resolve(value);
    if (flag === '--base') {
      let url;
      try {
        url = new URL(value);
      } catch {
        throw new UsageError(`--base is not an absolute URL: ${value}`);
      }
      if (url.protocol !== 'https:' && url.protocol !== 'http:') throw new UsageError(`--base must be http or https: ${value}`);
      opts.base = url.href.endsWith('/') ? url.href : `${url.href}/`;
    }
    if (flag === '--serve') {
      const port = Number(value);
      if (!Number.isInteger(port) || port < 1 || port > 65535) throw new UsageError(`--serve needs a port number: ${value}`);
      opts.serve = port;
    }
  }
  return opts;
}

// ---- text ------------------------------------------------------------------

const expand = (href) => href.replace('{blob}', BLOB).replace('{repo}', REPO);

// Content strings carry three marks: `code`, **strong** and [text](href),
// where href may start with {blob} or {repo}.
export function inline(s) {
  const parts = String(s).split(/(`[^`]+`)/);
  return parts
    .map((part) => {
      if (part.startsWith('`') && part.endsWith('`') && part.length > 1) return `<code>${esc(part.slice(1, -1))}</code>`;
      return esc(part)
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, text, href) => `<a href="${esc(expand(href))}">${text}</a>`);
    })
    .join('');
}

// ---- code ------------------------------------------------------------------

// Code is the same in every language. Each block is a list of lines: "$ cmd"
// is a command, "# text" a comment, anything else output, shown as JSON when
// it parses as JSON.
const CODE = {
  // One list of code blocks per install step; a block may carry a label.
  install: {
    binary: [
      [
        { label: 'macOS · Linux', lines: [`$ curl -fsSL ${RAW}/deploy/install/install.sh | sh`] },
        {
          label: 'Windows · PowerShell',
          lines: [`PS> [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072; irm ${RAW}/deploy/install/install.ps1 | iex`],
        },
      ],
      [{ lines: ['$ amdl doctor'] }],
      [{ lines: ['$ amdl wrapper install'] }],
      [{ lines: ['$ amdl service install', '$ amdl service status'] }],
    ],
    docker: [
      [{ lines: ['$ mkdir amdl && cd amdl', '$ mkdir -p data/downloads && chown 1000:1000 data/downloads', `$ curl -fsSLO ${RAW}/deploy/docker-compose.yml`] }],
      [],
      [{ lines: ['$ docker compose up -d'] }],
      [{ lines: ['http://wrapper-manager:8080'] }],
    ],
  },
};

// Each product's guide in the repository.
const DOCS = {
  desktop: `${BLOB}/docs/install.md#the-desktop-app`,
  console: `${REPO}#web-console`,
  tui: `${BLOB}/docs/cli.md#terminal-interface`,
  cli: `${BLOB}/docs/cli.md`,
  mcp: `${BLOB}/docs/cli.md#mcp-server`,
  api: `${BLOB}/docs/api.md`,
  hooks: `${BLOB}/docs/automation.md#job-hooks`,
};

// The API in two calls: queue an album, then follow every job's milestones.
const API_LINES = [
  "$ curl -X POST http://nas.local:18080/api/v1/downloads \\",
  "    -H 'Content-Type: application/json' \\",
  `    -d '{"urls":["https://music.apple.com/us/album/glass-weather/920001"]}'`,
  '{"accepted":1,"rejected":0,"results":[{"status":"accepted","job":{"id":"job_7f3a…",…}}]}',
  '',
  '$ curl -N http://nas.local:18080/api/v1/downloads/{job_id}/events',
];

// Loopback and LAN addresses: the pages must not show one.
export const PRIVATE_ADDRESS = /\b(?:127\.0\.0\.1|192\.168\.\d+\.\d+|10\.\d+\.\d+\.\d+)\b/;

// ---- content security policy -----------------------------------------------

// Every script and stylesheet is inline, so the policy allows exactly those by
// hash and nothing from anywhere else.
const sha = (text) => `'sha256-${crypto.createHash('sha256').update(text, 'utf8').digest('base64')}'`;
export function csp({ scripts, styles }) {
  return [
    "default-src 'none'",
    `script-src ${scripts.map(sha).join(' ')}`,
    `style-src ${styles.map(sha).join(' ')}`,
    "img-src 'self'",
    "base-uri 'none'",
    "form-action 'none'",
  ].join('; ');
}

// ---- page ------------------------------------------------------------------

function themeSwitch(ui) {
  const item = (value, icon) =>
    `<button type="button" role="radio" aria-checked="${value === 'system'}" data-theme-set="${value}" title="${esc(ui.themes[value])}" aria-label="${esc(ui.themes[value])}">${icon}</button>`;
  return `<div class="theme" role="radiogroup" aria-label="${esc(ui.theme)}">${item('light', ICON.sun)}${item('dark', ICON.moon)}${item('system', ICON.monitor)}</div>`;
}

// A section's head: its number and name on a rule, then the title and lead.
const secHead = (s, id) =>
  `<header class="sec-head"><p class="sec-label"><span class="idx">${esc(s.index)}</span>${esc(s.kicker)}</p><h2 id="${id}">${esc(s.title)}</h2>${s.lead ? `<p class="lede">${inline(s.lead)}</p>` : ''}</header>`;

function hero(h, ui) {
  const [line1, line2 = ''] = h.title.split('\n');
  return `<section class="hero" aria-labelledby="hero-title">
<div class="wrap hero-grid">
<div class="hero-main">
<p class="spec-line"><span class="led" aria-hidden="true"></span>${esc(h.eyebrow)}</p>
<h1 id="hero-title"><span>${esc(line1)}</span> <span>${esc(line2)}</span></h1>
<p class="lede">${inline(h.lead)}</p>
<div class="actions"><a class="btn btn-primary" href="#install">${ICON.download}${esc(h.install)}</a><a class="btn btn-line" href="${REPO}">${ICON.github}${esc(h.source)}</a></div>
<p class="fine">${esc(h.fine)}</p>
</div>
<dl class="readout">${h.formats.map(([name, detail], i) => `<div><dt>${esc(name)}</dt><dd>${esc(detail)}</dd><span class="lvl lvl-${i + 1}" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i></span></div>`).join('')}</dl>
</div>
<div class="wrap">${heroStrip(h)}</div>
</section>`;
}

// A product's facts: what it is in a few rows, and the guide that has the rest.
const facts = (list, doc, href) =>
  `<dl class="facts">${list.map(([t, d]) => `<div><dt>${esc(t)}</dt><dd>${inline(d)}</dd></div>`).join('')}</dl><p class="doc-link"><a href="${href}">${esc(doc)}<span aria-hidden="true"> →</span></a></p>`;

// A section that shows one app: its screens at full width, behind tabs when
// it has several (blocks.mjs#tabs, wired by site.js), then its facts.
function appSection(id, s, shown, screens, href) {
  const keys = Object.keys(screens);
  const stage =
    keys.length > 1
      ? tabs(s.kicker, keys.map((k) => ({ id: `${id}-${k}`, label: s.views[k], html: screens[k] })))
      : screens[keys[0]];
  return `<section class="sec" id="${id}" aria-labelledby="${id}-title">
<div class="wrap">
${secHead(s, `${id}-title`)}
<div class="prod-wide">${stage}<p class="note">${esc(shown)}</p>${facts(s.facts, s.doc, href)}</div>
</div>
</section>`;
}

// A product within a section: its name, a paragraph, its facts, its screen.
const part = (p, screen, href, extra = '') =>
  `<div class="prod prod-part"><div class="prod-info"><h3>${esc(p.title)}</h3><p class="prod-text">${inline(p.text)}</p>${p.facts ? facts(p.facts, p.doc, href) : `<p class="doc-link"><a href="${href}">${esc(p.doc)}<span aria-hidden="true"> →</span></a></p>`}${extra}</div><div class="prod-stage">${screen}</div></div>`;

function terminal(t, shown) {
  return `<section class="sec" id="terminal" aria-labelledby="terminal-title">
<div class="wrap">
${secHead(t, 'terminal-title')}
${part(t.tui, tuiScreen(t.tui.label), DOCS.tui)}
${part(t.cli, cliSession(t.cli.label), DOCS.cli)}
<p class="note">${esc(shown)}</p>
</div>
</section>`;
}

function agents(a, ui) {
  const m = a.mcp;
  const tools = `<div class="tools-all"><p class="tools-head">${esc(m.tools)}</p>${Object.entries(MCP_TOOLS)
    .map(([g, list]) => `<div class="tool-group"><p>${esc(m.groups[g])}</p><span>${list.map((t) => `<code>${t}</code>`).join('')}</span></div>`)
    .join('')}<p class="tools-head">${inline(m.config)}</p><pre class="code-plain"><code>${esc(mcpConfig())}</code></pre></div>`;
  return `<section class="sec" id="agents" aria-labelledby="agents-title">
<div class="wrap">
${secHead(a, 'agents-title')}
${part(m, agentChat(m, inline), DOCS.mcp, tools)}
<div class="pair">
<div class="pair-api"><h3>${esc(a.api.title)}</h3><p class="prod-text">${inline(a.api.text)}</p>${codeBlock(ui, API_LINES, { label: 'HTTP' })}<p class="doc-link"><a href="${DOCS.api}">${esc(a.api.doc)}<span aria-hidden="true"> →</span></a></p></div>
<div class="pair-hooks"><h3>${esc(a.hooks.title)}</h3><p class="prod-text">${inline(a.hooks.text)}</p>${hooksPanel(a.hooks.label)}<p class="doc-link"><a href="${DOCS.hooks}">${esc(a.hooks.doc)}<span aria-hidden="true"> →</span></a></p></div>
</div>
</div>
</section>`;
}

function specs(s) {
  const keys = ['formats', 'records', 'library', 'accounts', 'wrapper', 'lyrics', 'sync'];
  return `<section class="sec" id="specs" aria-labelledby="specs-title">
<div class="wrap">
${secHead(s, 'specs-title')}
<dl class="specs">${keys
    .map(
      (k, i) =>
        `<div class="spec"><dt><span class="idx">.${String(i + 1).padStart(2, '0')}</span>${esc(s[k].term)}</dt><dd><p>${inline(s[k].text)}</p><div class="spec-demo">${specDemo(k, s)}</div></dd></div>`,
    )
    .join('')}</dl>
</div>
</section>`;
}

function more(m) {
  return `<section class="sec sec-more" aria-labelledby="more-title">
<div class="wrap">
${secHead(m, 'more-title')}
<div class="more-grid">${m.groups.map((g) => `<div class="more-group"><h3>${esc(g.title)}</h3><ul>${g.items.map((i) => `<li>${inline(i)}</li>`).join('')}</ul></div>`).join('')}</div>
</div>
</section>`;
}

function install(q, ui) {
  const steps = (list, code) =>
    `<ol class="qs">${list
      .map((s, i) => {
        const blocks = (code[i] ?? []).map((b) => codeBlock(ui, b.lines, { label: b.label })).join('');
        return `<li><span class="num">${String(i + 1).padStart(2, '0')}</span><div class="qs-body"><div><h3>${esc(s.title)}</h3><p>${inline(s.text)}</p></div>${blocks ? `<div class="qs-code">${blocks}</div>` : ''}</div></li>`;
      })
      .join('')}</ol>`;
  return `<section class="sec" id="install" aria-labelledby="install-title">
<div class="wrap">
${secHead(q, 'install-title')}
${tabs(q.kicker, [
  { id: 'binary', label: q.tabs.binary, html: steps(q.binary.steps, CODE.install.binary) },
  { id: 'docker', label: q.tabs.docker, html: steps(q.docker.steps, CODE.install.docker) },
])}
<aside class="callout"><p class="callout-label">2.0</p><p>${inline(q.prerelease)}</p></aside>
<aside class="callout"><p class="callout-label">${esc(q.nas.label)}</p><p>${inline(q.nas.text)} <a href="${BLOB}/docs/deployment.md#the-wrapper">${esc(q.nas.link)}</a></p></aside>
<p class="more">${inline(q.more)}</p>
</div>
</section>`;
}

// The script in <head>: the stored theme and language, before the first paint.
const HEAD_SCRIPT = `try{var d=document.documentElement,t=localStorage.getItem(${JSON.stringify(THEME_KEY)});if(t==="light"||t==="dark")d.setAttribute("data-theme",t);localStorage.setItem(${JSON.stringify(LANG_KEY)},d.lang)}catch(e){}`;

export function renderPage(c, ctx) {
  const { lang, other, base } = ctx;
  const ui = c.ui;
  const url = (code) => `${base}${code}/`;
  const head = [
    `<meta charset="utf-8">`,
    `<meta http-equiv="Content-Security-Policy" content="${csp({ scripts: [HEAD_SCRIPT, ctx.js], styles: [ctx.css, NOSCRIPT_TABS.style] })}">`,
    `<meta name="viewport" content="width=device-width, initial-scale=1">`,
    `<title>${esc(c.meta.title)}</title>`,
    `<meta name="description" content="${esc(c.meta.description)}">`,
    `<meta name="color-scheme" content="light dark">`,
    `<meta name="theme-color" media="(prefers-color-scheme: light)" content="#f4f5f7">`,
    `<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#09090b">`,
    `<link rel="icon" href="../favicon.svg" type="image/svg+xml">`,
    `<meta property="og:type" content="website">`,
    `<meta property="og:site_name" content="AMDL">`,
    `<meta property="og:title" content="${esc(c.meta.title)}">`,
    `<meta property="og:description" content="${esc(c.meta.description)}">`,
    `<meta property="og:locale" content="${lang.og}">`,
    `<meta name="twitter:card" content="summary_large_image">`,
  ];
  if (base) {
    head.push(
      `<link rel="canonical" href="${url(lang.code)}">`,
      ...LANGS.map((l) => `<link rel="alternate" hreflang="${l.code}" href="${url(l.code)}">`),
      `<link rel="alternate" hreflang="x-default" href="${base}">`,
      `<meta property="og:url" content="${url(lang.code)}">`,
      `<meta property="og:image" content="${base}og.png">`,
      `<meta property="og:image:alt" content="${esc(c.meta.ogAlt)}">`,
    );
  }
  head.push(`<script>${HEAD_SCRIPT}</script>`, `<style>${ctx.css}</style>`, NOSCRIPT_TABS.html);

  const n = c.nav;
  const header = `<header class="top">
<div class="wrap top-in">
<a class="brand" href="./" aria-label="${esc(ui.home)}"><span class="meter" aria-hidden="true"><i></i><i></i><i></i><i></i></span>AMDL</a>
<nav class="top-nav" aria-label="${esc(ui.nav)}"><a href="#desktop">${esc(n.desktop)}</a><a href="#console">${esc(n.console)}</a><a href="#terminal">${esc(n.terminal)}</a><a href="#agents">${esc(n.agents)}</a><a href="#specs">${esc(n.specs)}</a><a href="#install">${esc(n.install)}</a><a href="${BLOB}/docs/README.md">${esc(n.docs)}</a></nav>
<div class="top-tools">
<a class="lang" href="../${other.code}/" hreflang="${other.code}" lang="${other.code}" title="${esc(ui.otherLangLabel)}">${esc(ui.otherLang)}</a>
${themeSwitch(ui)}
<a class="icon-link" href="${REPO}" aria-label="${esc(ui.github)}">${ICON.github}</a>
</div>
</div>
</header>`;

  const f = c.footer;
  const footer = `<footer class="foot">
<div class="wrap foot-in">
<p class="notice">${inline(c.notice)}</p>
<div class="foot-row"><span class="brand brand-sm"><span class="meter" aria-hidden="true"><i></i><i></i><i></i><i></i></span>AMDL</span><a href="${BLOB}/LICENSE">${esc(f.license)}</a><span class="foot-links"><a href="${BLOB}/docs/README.md">${esc(f.docs)}</a><a href="${BLOB}/docs/upgrading-to-2.0.md">${esc(f.upgrade)}</a><a href="${REPO}/releases">${esc(f.releases)}</a><a href="${REPO}">${esc(f.github)}</a></span></div>
</div>
</footer>`;

  return `<!doctype html>
<html lang="${lang.code}">
<head>
${head.join('\n')}
</head>
<body>
<a class="skip" href="#main">${esc(ui.skip)}</a>
${header}
<main id="main">
${hero(c.hero, ui)}
${appSection('desktop', c.desktop, c.shown, desktopViews(c.desktop), DOCS.desktop)}
${appSection('console', c.console, c.shown, consoleViews(c.console), DOCS.console)}
${terminal(c.terminal, c.shown)}
${agents(c.agents, ui)}
${specs(c.specs)}
${more(c.more)}
${install(c.install, ui)}
</main>
${footer}
<script>${ctx.js}</script>
</body>
</html>
`;
}

// The root page sends a visitor to their language: the one they picked last,
// else the first Chinese or English entry in navigator.languages, else English.
const ROOT_SCRIPT = `(function(){var l;try{l=localStorage.getItem(${JSON.stringify(LANG_KEY)})}catch(e){}if(l!=="en"&&l!=="zh-CN"){l="en";var a=navigator.languages||[navigator.language||""];for(var i=0;i<a.length;i++){var s=String(a[i]).toLowerCase();if(s.indexOf("zh")===0){l="zh-CN";break}if(s.indexOf("en")===0)break}}location.replace(l+"/"+location.hash)})()`;
const ROOT_STYLE = 'body{font:16px/1.6 system-ui,sans-serif;margin:0;display:grid;place-items:center;min-height:100vh;background:#fbf8f4;color:#1c1b1f}@media(prefers-color-scheme:dark){body{background:#141214;color:#f5f5f7}}a{color:inherit;margin:0 .75em}';

export function renderRoot(base) {
  const links = base ? LANGS.map((l) => `<link rel="alternate" hreflang="${l.code}" href="${base}${l.code}/">`).join('\n') : '';
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp({ scripts: [ROOT_SCRIPT], styles: [ROOT_STYLE] })}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AMDL</title>
${links}
${base ? `<link rel="alternate" hreflang="x-default" href="${base}">` : ''}
<link rel="icon" href="favicon.svg" type="image/svg+xml">
<script>${ROOT_SCRIPT}</script>
<style>${ROOT_STYLE}</style>
</head>
<body>
<p><a href="en/" hreflang="en">English</a><a href="zh-CN/" hreflang="zh-CN" lang="zh-CN">简体中文</a></p>
</body>
</html>
`;
}

// Drops comments and the whitespace around punctuation. Safe for site.css,
// which has no strings that contain these characters and no selector where a
// colon or comma is followed by a space that matters.
const squeeze = (css) =>
  css
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\s*([{};])\s*/g, '$1')
    .replace(/([,:])\s+/g, '$1')
    .replace(/;}/g, '}')
    .trim();

export function loadContent(code) {
  return JSON.parse(fs.readFileSync(path.join(here, 'content', `${code}.json`), 'utf8'));
}

export function build({ out, base = '' }) {
  const staticDir = path.join(here, 'static');
  const read = (rel) => fs.readFileSync(path.join(staticDir, rel), 'utf8');
  const shared = {
    css: squeeze(`${read('site.css')}\n${read('mocks.css')}`),
    js: read('site.js').trim(),
    base,
  };

  fs.rmSync(out, { recursive: true, force: true });
  fs.mkdirSync(out, { recursive: true });
  fs.copyFileSync(path.join(staticDir, 'favicon.svg'), path.join(out, 'favicon.svg'));

  const pages = [];
  for (const lang of LANGS) {
    const other = LANGS.find((l) => l !== lang);
    const html = renderPage(loadContent(lang.code), { ...shared, lang, other });
    const leak = PRIVATE_ADDRESS.exec(html);
    if (leak) throw new Error(`${lang.code}/index.html shows the private address ${leak[0]}`);
    fs.mkdirSync(path.join(out, lang.code), { recursive: true });
    fs.writeFileSync(path.join(out, lang.code, 'index.html'), html);
    pages.push(`${lang.code}/index.html`);
  }
  fs.writeFileSync(path.join(out, 'index.html'), renderRoot(base));
  pages.push('index.html');
  return pages;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  let opts;
  try {
    opts = parseArgs(process.argv.slice(2));
  } catch (err) {
    if (!(err instanceof UsageError)) throw err;
    console.error(`${err.message}\n\n${USAGE}`);
    process.exit(2);
  }
  const pages = build(opts);
  console.log(`wrote ${pages.join(', ')} to ${opts.out}`);
  if (opts.serve) serve(opts.out, opts.serve);
}
