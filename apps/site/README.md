# apps/site

The AMDL website: one static page in English and one in Simplified Chinese, each at its
own path (`en/`, `zh-CN/`), plus a root page that sends a visitor to the language they
picked last or, failing that, the one their browser prefers.

It is plain HTML and CSS built by one zero-dependency Node script, `build.mjs`: a
template, two content files and a handful of static assets are all it takes, so a static
site generator would add more than it saves. Fonts are the system's, and the pages load
nothing from another host.

## Layout

| Path | What it holds |
| --- | --- |
| `build.mjs` | The template and the build: renders `content/<lang>.json` into a page, inlines `static/site.css` and `static/site.js`, copies the images |
| `content/` | The page text, one file per language with the same keys. Strings may use `` `code` ``, `**strong**` and `[text](href)`; an href may start with `{repo}` or `{blob}` |
| `static/` | Stylesheet, script, favicon, Open Graph card, the hero animation (`hero.html`), the terminal capture (`tui.html`), the CLI and MCP samples, and the screenshots in `img/` |
| `build.test.mjs` | Content parity, links and anchors, local-only resources, image sizes, arguments |
| `wrangler.jsonc` | The Cloudflare Workers project that serves `dist/` at the site's domain |

## Build and preview

Node.js 26 (the version the rest of the repository uses):

```bash
node apps/site/build.mjs --serve 8080
```

builds into `apps/site/dist/` and serves it on `http://127.0.0.1:8080/`. Without
`--serve` it only builds. `--out <dir>` writes elsewhere, and `--base <url>` sets the
absolute address the site will be served from, which adds the canonical, `hreflang` and
Open Graph URLs:

```bash
node apps/site/build.mjs --base https://amdl.example.org/
```

Every link inside the site is relative, so the output works at a domain's root or under
a subpath such as a GitHub Pages project site. Links into the repository point at the
branch in `build.mjs#REF`.

```bash
node --test apps/site/
```

runs the tests; `make node-test` includes them.

## Publishing

The site is Cloudflare Workers static assets, deployed by hand with the maintainer's
Cloudflare login. Build with the domain from `wrangler.jsonc` as `--base`, then deploy:

```bash
node apps/site/build.mjs --base https://amdl.lyjw.me/
npx wrangler deploy --config apps/site/wrangler.jsonc
```

Links into the repository, the install scripts and the Compose file come from the
branch in `build.mjs#REF`, so it names the branch the published page describes.

## Changing the content

- Edit both `content/en.json` and `content/zh-CN.json` in the same change. The Chinese
  is written for Chinese readers, not translated word for word.
- Every claim comes from the repository: the root README, `docs/`, or what the code
  does. A feature that is not released says so.
- Screenshots are WebP in `static/img`, named `<shot>-<light|dark>-<width>.webp`, two
  widths per theme as `build.mjs#SHOTS` lists. Take them from a simulate-mode server
  (`make dev`) or a test install, with no personal paths, addresses or account names in
  view.
- `static/hero.html` is the hero animation: one inline SVG with its own `<style>`
  (`ah-*` classes, `--ah-*` custom properties for both themes) and an optional script
  that pauses it off screen and adds pointer parallax. Its ids are `ah-*`, so a page holds
  one. `--ah-hole` is the page background, the color the record's centre hole shows.
- The terminal capture is `tmux capture-pane -e` output converted to HTML spans; a wide
  (CJK) character sits in a `<span class="w">` so the columns line up.

## Theme and language

The page follows `prefers-color-scheme` until the visitor picks a theme with the toggle,
which sets `data-theme` on `<html>` and keeps the choice in `localStorage`
(`amdl-site-theme`). Each language page records itself as the visitor's language
(`amdl-site-lang`) for the root page to use next time.
