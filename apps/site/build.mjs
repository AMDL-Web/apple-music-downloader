#!/usr/bin/env node
// Builds the AMDL website: one page per language from content/<lang>.json,
// with the stylesheet and static/site.js inlined, into a directory that any
// static host can serve at its root or under a subpath.
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { ICON, NOSCRIPT_TABS, codeBlock, esc, sized, tabs } from './blocks.mjs';
import { cover, sprite } from './covers.mjs';
import { GLASS_FILE, GLASS_QUALITY, GLASS_TREE, MCP_CONFIG, MCP_TOOLS, SHELF_DEFS, agentChat, consoleMock, desktopMock, shelf, terminalMock } from './mocks.mjs';
import { serve } from './serve.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

export const REPO = 'https://github.com/AMDL-Web/apple-music-downloader';
// Links into the repository, the install scripts and the Compose file follow the
// branch the published site describes, so a visitor reads and installs what the
// page shows.
export const REF = 'main';
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

// The hero lead's one **phrase** is a highlighter mark, not bold.
export const markLead = (s) => inline(s).replace(/<strong>([\s\S]*?)<\/strong>/, '<mark class="hl">$1</mark>');

const icon = (name, size, stroke) => sized(ICON[name], size, stroke);
const arrow = `<span class="btn-arrow">${icon('arrow', 16)}</span>`;

// The AMDL mark, four bars on a dark tile (static/favicon.svg).
const MARK = `<svg class="mark" viewBox="0 0 32 32" width="22" height="22" aria-hidden="true" focusable="false"><rect width="32" height="32" rx="8" fill="#0a0a0a"/><rect x="6.5" y="13" width="4" height="12" rx="2" fill="#fb7185"/><rect x="11.8" y="7" width="4" height="18" rx="2" fill="#fbbf24"/><rect x="17.1" y="16" width="4" height="9" rx="2" fill="#34d399"/><rect x="22.4" y="10" width="4" height="15" rx="2" fill="#818cf8"/><rect class="mark-ring" x=".5" y=".5" width="31" height="31" rx="7.5" fill="none" stroke="#ffffff" stroke-opacity="0"/></svg>`;

const NAV = ['how', 'formats', 'apps', 'install'];

function themeSwitch(ui, where) {
  const item = (value, glyph) =>
    `<button type="button" role="radio" aria-checked="${value === 'system'}" data-theme-set="${value}" title="${esc(ui.themes[value])}" aria-label="${esc(ui.themes[value])}">${icon(glyph, 16)}</button>`;
  return `<div class="theme theme-${where}" role="radiogroup" aria-label="${esc(ui.theme)}">${item('light', 'sun')}${item('dark', 'moon')}${item('system', 'monitor')}</div>`;
}

const langLink = (ui, other, cls, glyph = '') =>
  `<a class="${cls}" href="../${other.code}/" hreflang="${other.code}" lang="${other.code}" title="${esc(ui.otherLangLabel)}">${glyph}${esc(ui.otherLang)}</a>`;

function header(c, other) {
  const { ui, nav } = c;
  return `<header class="top">
<div class="wrap top-in">
<a class="brand" href="./" aria-label="${esc(ui.home)}">${MARK}<span>AMDL</span></a>
<div class="top-right">
<nav class="top-nav" aria-label="${esc(ui.nav)}">
<details class="sections">
<summary class="btn ghost sections-btn">${icon('menu', 18)}<span class="sections-label">${esc(ui.sections)}</span></summary>
<div class="sections-panel">
<ul class="sections-list">${NAV.map((id, i) => `<li><a class="menu-row" href="#${id}"><span class="sn" aria-hidden="true">${i + 1}</span>${esc(nav[id])}</a></li>`).join('')}</ul>
<div class="menu-extra">
<a class="menu-row" href="${BLOB}/docs/README.md">${icon('book', 18, 1.5)}${esc(nav.docs)}</a>
<a class="menu-row" href="${REPO}" aria-label="${esc(ui.github)}">${icon('github', 18)}${esc(c.footer.project.github)}</a>
${langLink(ui, other, 'menu-row menu-phone', icon('globe', 18))}
<div class="menu-phone menu-theme">${themeSwitch(ui, 'menu')}</div>
</div>
</div>
</details>
</nav>
<a class="btn ghost top-docs" href="${BLOB}/docs/README.md">${esc(nav.docs)}</a>
${langLink(ui, other, 'btn ghost top-lang')}
${themeSwitch(ui, 'head')}
<a class="icon-btn top-gh" href="${REPO}" aria-label="${esc(ui.github)}">${icon('github', 18)}</a>
<a class="btn primary top-install" href="#install">${esc(c.header.install)}</a>
</div>
</div>
</header>`;
}

function hero(h) {
  const [line1, line2 = ''] = h.title.split('\n');
  const os = [
    ['laptop', 'macOS'],
    ['grid', 'Windows'],
    ['terminal', 'Linux'],
    ['container', 'Docker'],
  ];
  return `<section class="hero" aria-labelledby="hero-title">
<div class="wrap">
<p class="eyebrow"><span>${esc(h.eyebrow)}</span></p>
<h1 id="hero-title"><span>${esc(line1)}</span><span class="accent">${esc(line2)}</span></h1>
${shelf(h.shelfLabel)}
<p class="lead hero-lead">${markLead(h.lead)}</p>
<p class="runs-on"><span class="runs-label">${esc(h.runsOn)}</span>${os.map(([i, name]) => `<span class="os">${icon(i, 16, 1.5)}${name}</span>`).join('')}<span class="runs-sep" aria-hidden="true">·</span><a class="runs-src" href="${REPO}">${sized(ICON.github, 14)}${esc(h.source)}</a></p>
</div>
</section>`;
}

// A section's head: the title with its anchor glyph, then the lead. The glyph
// is the h2's sibling, so the heading's name (and the section's, through
// aria-labelledby) is the title alone.
const head = (id, s, ui) =>
  `<div class="sec-head"><div class="sec-title"><h2 id="${id}-title">${esc(s.title)}</h2><a class="anchor" href="#${id}" aria-label="${esc(ui.anchor)}">${icon('link', 16, 1.5)}</a></div>${s.lead ? `<p class="lead">${inline(s.lead)}</p>` : ''}</div>`;
const section = (id, s, ui, body, cls = '') =>
  `<section class="sec${cls ? ` ${cls}` : ''}" id="${id}" aria-labelledby="${id}-title">
<div class="wrap">
${head(id, s, ui)}
${body}
</div>
</section>`;

function tree(label) {
  const html = GLASS_TREE.map(([glyph, kind, name]) => `<span class="tg">${glyph}</span><span class="${kind === 'dir' ? 'td' : 'tf'}">${esc(name)}</span>`).join('\n');
  return `<figure class="tree" role="img" aria-label="${esc(label)}"><pre aria-hidden="true">${html}</pre></figure>`;
}

function how(s, ui) {
  const card = (cls, ico, title, body, note) =>
    `<article class="how-card how-${cls}"><span class="how-icon" aria-hidden="true">${ico}</span><h3>${esc(title)}</h3><div class="how-body">${body}</div><p class="how-note">${esc(note)}</p></article>`;
  const markIcon = MARK.replace('width="22" height="22"', 'width="28" height="28"');
  // The demo link may break only after a slash; under it, the same album as a search.
  const link = esc('https://music.apple.com/us/album/glass-weather/920001').replace(/\/(?!\/)/g, '/<wbr>');
  const a = card('link', icon('link', 28, 1.5), s.link.title, `<p>${inline(s.link.text)}</p><p class="plate pill-link">${link}</p><p class="plate pill-search">${icon('search', 14, 1.75)}<span>glass weather</span></p>`, s.link.note);
  const b = card('amdl', markIcon, s.amdl.title, `<ol class="checks">${s.amdl.steps.map((st) => `<li>${icon('check', 16, 2)}<span><b>${esc(st.name)}</b> ${inline(st.text)}</span></li>`).join('')}</ol>`, s.amdl.note);
  const cc = card('file', icon('folder', 28, 1.5), s.file.title, `<p>${inline(s.file.text)}</p>${tree(s.label)}`, s.file.note);
  return section('how', s, ui, `<div class="how-row">${a}${b}${cc}</div>
<p class="sec-more"><a class="btn soft" href="${BLOB}/docs/download-pipeline.md">${esc(s.more)}${arrow}</a></p>`);
}

function formats(s, ui) {
  const f = s.file;
  const rows = [
    [f.codec, `<code>${GLASS_QUALITY}</code>`],
    [f.cover, '<code>5000 × 5000</code>'],
    [f.lyrics, inline(f.lyricsValue)],
    [f.tags, inline(f.tagsValue)],
  ];
  const rungs = [
    ['ALAC', 'alac', s.rows.alac],
    ['Dolby Atmos', 'ec3', s.rows.ec3],
    ['AAC', 'aac', s.rows.aac],
    ['AAC-LC', '', s.rows.aaclc],
  ];
  const chev = `<span class="rung-chev" aria-hidden="true">${icon('chevronDown', 12, 2)}</span>`;
  const body = `<div class="format-card">
<div class="fc-file">${cover('glass', { cls: 'fc-cover' })}<p class="fc-name">${esc(GLASS_FILE)}</p><dl class="fc-facts">${rows.map(([t, d]) => `<div><dt>${esc(t)}</dt><dd>${d}</dd></div>`).join('')}</dl></div>
<div class="fc-ladder"><p class="fc-head">${esc(s.ladder.head)}</p><p class="fc-config"><code><span class="c-k">quality_priority</span><span class="c-pu">: [</span><span class="c-s">"alac"</span><span class="c-pu">, </span><span class="c-s">"ec3"</span><span class="c-pu">, </span><span class="c-s">"aac"</span><span class="c-pu">]</span></code></p>
<ol class="rungs">${rungs.map(([name, value, note], i) => `<li class="rung${i === 0 ? ' rung-first' : ''}${value ? '' : ' rung-floor'}"><span class="rung-n" aria-hidden="true">${value ? i + 1 : ''}</span>${i ? chev : ''}<div class="rung-card"><p class="rung-name">${name}${value ? `<code class="rung-val">${value}</code>` : ''}</p><p class="rung-note">${esc(note)}</p></div></li>`).join('')}</ol>
<p class="fc-also"><span>${esc(s.also)}</span><code>ac3</code><code>aac-binaural</code><code>aac-downmix</code></p></div>
</div>
<p class="caption">${inline(s.caption)}</p>`;
  return section('formats', s, ui, body);
}

const APP_DOCS = {
  desktop: `${BLOB}/docs/install.md#the-desktop-app`,
  console: `${REPO}#web-console`,
  terminal: `${BLOB}/docs/cli.md#terminal-interface`,
  agents: `${BLOB}/docs/cli.md#mcp-server`,
};

function apps(s, ui) {
  const sideCol = () => {
    const a = s.agents;
    return `<div class="ag-side"><p class="ag-side-label">${inline(a.config)}</p>${codeBlock(ui, MCP_CONFIG)}<details class="ag-tools"><summary>${esc(a.tools)}${icon('chevronDown', 16, 2)}</summary>${Object.entries(MCP_TOOLS)
      .map(([g, list]) => `<div class="tool-group"><p>${esc(a.groups[g])}</p><p class="tool-chips">${list.map((t) => `<code>${t}</code>`).join('')}</p></div>`)
      .join('')}</details></div>`;
  };
  const mocks = {
    desktop: () => desktopMock(s.desktop.label),
    console: () => consoleMock(s.console.label),
    terminal: () => terminalMock(s.terminal.label),
    agents: () => agentChat(s.agents.chat, inline, s.agents.label) + sideCol(),
  };
  const keys = ['desktop', 'console', 'terminal', 'agents'];
  const items = keys.map((k, i) => ({
    id: `apps-${k}`,
    labelHtml: `<span class="step-n" aria-hidden="true">${i + 1}</span><span class="step-l">${esc(s[k].tab)}</span>`,
    html: `<div class="stage stage-${k}"><button type="button" class="stage-arrow prev" aria-label="${esc(ui.prev)}">${icon('chevronLeft', 18)}</button>${mocks[k]()}<button type="button" class="stage-arrow next" aria-label="${esc(ui.next)}">${icon('chevronRight', 18)}</button></div>
<p class="stage-caption">${inline(s[k].caption)}</p>
<p class="stage-doc"><a class="btn soft" href="${APP_DOCS[k]}">${esc(s[k].doc)}${arrow}</a></p>`,
  }));
  const body = `${tabs(s.title, items, { cls: 'apps', listCls: 'stepper' })}
<p class="apps-shown">${esc(s.shown)}</p>
<p class="apps-more"><span class="apps-more-label">${esc(s.more)}</span><span class="apps-more-links"><a href="${BLOB}/docs/api.md">${icon('plug', 16, 1.5)}${esc(s.api.link)}</a><a href="${BLOB}/docs/automation.md#job-hooks">${icon('bolt', 16, 1.5)}${esc(s.hooks.link)}</a></span></p>`;
  return section('apps', s, ui, body);
}

function install(s, ui) {
  const steps = (list, code) =>
    `<ol class="vsteps">${list
      .map((st, i) => {
        const blocks = (code[i] ?? []).map((b) => codeBlock(ui, b.lines, { label: b.label })).join('');
        const after = st.after ? `<p class="vs-after">${inline(st.after)}</p>` : '';
        return `<li><span class="vs-n" aria-hidden="true">${i + 1}</span><div class="vs-body"><h3>${esc(st.title)}</h3><p>${inline(st.text)}</p>${blocks}${after}</div></li>`;
      })
      .join('')}</ol>`;
  const body = `${tabs(
    s.tabsLabel,
    [
      { id: 'binary', label: s.tabs.binary, html: steps(s.binary.steps, CODE.install.binary) },
      { id: 'docker', label: s.tabs.docker, html: steps(s.docker.steps, CODE.install.docker) },
    ],
    { cls: 'install-tabs', listCls: 'seg' },
  )}
<div class="callouts">
<aside class="callout callout-nas"><p><span class="callout-label">${esc(s.nas.label)}</span>${inline(s.nas.text)} <a href="${BLOB}/docs/deployment.md#the-wrapper">${esc(s.nas.link)}</a></p></aside>
</div>
<p class="install-more">${inline(s.more)}</p>`;
  return section('install', s, ui, body);
}

const cta = (s) => `<section class="cta" aria-labelledby="cta-title">
<div class="wrap">
<h2 id="cta-title">${esc(s.title)}</h2>
<p><a class="btn primary" href="${BLOB}/docs/install.md">${esc(s.button)}${arrow}</a></p>
</div>
</section>`;

function footer(c, other) {
  const f = c.footer;
  const col = (title, links) =>
    `<div class="foot-col"><h2 class="foot-title">${esc(title)}</h2><ul>${links.map(([text, href, ico]) => `<li><a href="${href}">${icon(ico, 14, 1.5)}${esc(text)}</a></li>`).join('')}</ul></div>`;
  return `<footer class="foot">
<div class="wrap">
<div class="foot-grid">
<div class="foot-brand"><p class="brand brand-foot">${MARK}<span>AMDL</span></p><p class="foot-tagline">${esc(f.tagline)}</p><p class="foot-notice">${esc(f.notice)}</p></div>
${col(f.docs.title, [
  [f.docs.install, `${BLOB}/docs/install.md`, 'book'],
  [f.docs.deployment, `${BLOB}/docs/deployment.md`, 'container'],
  [f.docs.cli, `${BLOB}/docs/cli.md`, 'terminal'],
  [f.docs.api, `${BLOB}/docs/api.md`, 'plug'],
  [f.docs.automation, `${BLOB}/docs/automation.md`, 'bolt'],
  [f.docs.upgrade, `${BLOB}/docs/upgrading-to-2.0.md`, 'out'],
])}
${col(f.project.title, [
  [f.project.github, REPO, 'github'],
  [f.project.releases, `${REPO}/releases`, 'note'],
  [f.project.license, `${BLOB}/LICENSE`, 'book'],
  [f.project.docs, `${BLOB}/docs/README.md`, 'out'],
])}
</div>
<div class="foot-legal"><p>${esc(f.legal)}</p>${langLink(c.ui, other, 'foot-lang')}</div>
</div>
</footer>`;
}

// The script in <head>: the stored theme and language, before the first paint.
const HEAD_SCRIPT = `try{var d=document.documentElement,t=localStorage.getItem(${JSON.stringify(THEME_KEY)});if(t==="light"||t==="dark")d.setAttribute("data-theme",t);localStorage.setItem(${JSON.stringify(LANG_KEY)},d.lang)}catch(e){}`;

export function renderPage(c, ctx) {
  const { lang, other, base } = ctx;
  const url = (code) => `${base}${code}/`;
  const head = [
    `<meta charset="utf-8">`,
    `<meta http-equiv="Content-Security-Policy" content="${csp({ scripts: [HEAD_SCRIPT, ctx.js], styles: [ctx.css, NOSCRIPT_TABS.style] })}">`,
    `<meta name="viewport" content="width=device-width, initial-scale=1">`,
    `<title>${esc(c.meta.title)}</title>`,
    `<meta name="description" content="${esc(c.meta.description)}">`,
    `<meta name="color-scheme" content="light dark">`,
    `<meta name="theme-color" media="(prefers-color-scheme: light)" content="#fffdf7">`,
    `<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#0e1412">`,
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

  return `<!doctype html>
<html lang="${lang.code}">
<head>
${head.join('\n')}
</head>
<body>
${sprite(SHELF_DEFS)}
<a class="skip" href="#main">${esc(c.ui.skip)}</a>
${header(c, other)}
<main id="main">
${hero(c.hero)}
${how(c.how, c.ui)}
${formats(c.formats, c.ui)}
${apps(c.apps, c.ui)}
${install(c.install, c.ui)}
${cta(c.cta)}
</main>
${footer(c, other)}
<script>${ctx.js}</script>
</body>
</html>
`;
}

// The root page sends a visitor to their language: the one they picked last,
// else the first Chinese or English entry in navigator.languages, else English.
const ROOT_SCRIPT = `(function(){var l;try{l=localStorage.getItem(${JSON.stringify(LANG_KEY)})}catch(e){}if(l!=="en"&&l!=="zh-CN"){l="en";var a=navigator.languages||[navigator.language||""];for(var i=0;i<a.length;i++){var s=String(a[i]).toLowerCase();if(s.indexOf("zh")===0){l="zh-CN";break}if(s.indexOf("en")===0)break}}location.replace(l+"/"+location.hash)})()`;
const ROOT_STYLE = 'body{font:16px/1.6 system-ui,sans-serif;margin:0;display:grid;place-items:center;min-height:100vh;background:#fffdf7;color:#172723}@media(prefers-color-scheme:dark){body{background:#0e1412;color:#e6ece8}}a{color:inherit;margin:0 .75em}';

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

// mocks.css writes design pixels as `13u`; each becomes calc(13 * var(--u)),
// so a mock drawn at its app's size scales as one picture (DESIGN.md 5).
const units = (css) => css.replace(/(^|[\s(,:/])(-?\d*\.?\d+)u\b/g, (_, pre, n) => `${pre}calc(${n}*var(--u))`);

export function loadContent(code) {
  return JSON.parse(fs.readFileSync(path.join(here, 'content', `${code}.json`), 'utf8'));
}

export function build({ out, base = '' }) {
  const staticDir = path.join(here, 'static');
  const read = (rel) => fs.readFileSync(path.join(staticDir, rel), 'utf8');
  const shared = {
    css: squeeze(`${read('site.css')}\n${units(read('mocks.css'))}`),
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
