# internal/console

The embedded web console: an operator dashboard and a showcase of downloaded
music, served by `Handler` at `/console/`. Plain ES modules plus a vendored
Preact+htm bundle. [DESIGN.md](DESIGN.md) is the shared contract (structure,
states, accessibility, the skin layer); the look of the one skin, `music`, is
specified in [skins/music.md](skins/music.md).

## Rules

- No build step, no npm, no runtime CDN. The files under `static/` are exactly
  what the browser runs and what `//go:embed all:static` ships.
- The console is a client of the public API only. Every call is a full
  `"/api/v1/..."` literal in `static/js/api.js`, written against
  `internal/api/openapi.yaml` and the 2.0 conventions in
  [docs/architecture.md](../../docs/architecture.md#3-http-api-conventions).
  A widget whose endpoint answers 404 hides behind a quiet note; it never
  breaks the page.
- URLs are relative (`css/base.css`, `../api/v1/...` via `resolveUrl`), so the
  console works under any path prefix.
- CSP (`ContentSecurityPolicy`): no inline scripts, styles or `style="..."`;
  dynamic colors go through Preact style objects (CSSOM custom properties).
  No `innerHTML`; htm escapes data.
- Colors live only in `static/css/tokens.css` (skin-neutral) and
  `static/css/skins/music.css` (scoped under `:root[data-skin="music"]`);
  the base stylesheets (every other `static/css/*.css`, one per area) are
  structure and read tokens only, and their link order in `index.html` is the
  cascade order. Font sizes use
  `--fs-*`; never `transition: all`; UI copy is English, Apple-provided text
  gets `dir="auto"`.
- The skin never needs JS: it changes only its own CSS file and spec page. A
  hook it needs (class, data attribute, custom property) lands in the base
  with a DESIGN.md "DOM hooks" row. The theme (`data-theme`, switchable) and
  the skin (`data-skin="music"`, static in `index.html`) are separate axes.
- Modules in `static/js/lib/` are pure (no DOM at import, no vendor import) so
  `node --test` covers them; views and hooks live in `views/` and `ui/`.
- Streams use `lib/stream.js` (fetch-based SSE, `?cursor=`, 1-30 s backoff,
  30 s hidden-tab pause). A tab holds the feed plus at most one secondary
  stream (`claimSecondary`). Never open the feed without the snapshot's
  `event_cursor`.
- No auth here, ever (root AGENTS.md).

## Vendored files

| File | Source | Version | License | SHA-256 |
| --- | --- | --- | --- | --- |
| `static/vendor/htm-preact-standalone-3.1.1.mjs` | `https://cdn.jsdelivr.net/npm/htm@3.1.1/preact/standalone.module.js` (same bytes on unpkg) | htm 3.1.1 bundling Preact 10 | htm Apache-2.0, Preact MIT | `72284e8e9079c87817145df1110f74e8a2aa040b2fc384922e18dfcb46fc1fd7` |

Never edit a vendored file. To update: download from jsDelivr and unpkg into
an empty scratch directory, require identical SHA-256, store it byte-identical
under a new versioned name (vendor files are served `immutable`), then update
`vendorFile`/`vendorSHA256` in `console_test.go`, `ui/preact.js` and this
table. The bundle has no `Fragment`; return keyed arrays instead.

`static/icons.svg` is a hand-drawn sprite in the Lucide style (not copied
paths); add a `<symbol id="i-name">` and use `Icon`. `main.js` imports its
symbols into the document before mounting (`ui/icons.js`), so icons keep
painting while the server is unreachable; nothing else references the file.

## Guards (`console_test.go`)

- headers on every response, strong ETag and 304, `no-cache` vs `immutable`;
- every `src`/`href` in `index.html` and every relative import in `static/js`
  resolves to an embedded file; every embedded file has a known type;
- vendor checksum;
- design guard (color literals outside `css/tokens.css` and `css/skins/*.css`,
  `transition: all`, CJK copy, raw HTML writes, raw font sizes in any CSS);
- skin tokens: every token a base stylesheet reads and `tokens.css` does not
  declare is declared by the skin, which is scoped by its file name;
  `index.html` links tokens, every base stylesheet, then the skin;
- contract guard: each `/api/v1/...` literal in `static/js` is a path in
  `internal/api/openapi.yaml`;
- `LOG_COMPONENTS` equals the `"component"` log tags in the Go code;
- every `RuntimeConfig` leaf in `internal/api/openapi.yaml` has a Settings field
  in `static/js/lib/settings.js` or a `NOT_EDITED` reason, and the reverse.

## Run and verify

Mounted by `internal/api` at `GET /console/`. Run `make dev`, then open
`http://127.0.0.1:18080/console/`; check it at 1440 and 390 px wide, light
and dark.

```bash
for f in $(find internal/console/static -name '*.js' -o -name '*.mjs'); do node --check "$f"; done
node --test internal/console/jstest/*.test.mjs   # also run by make node-test
go vet ./internal/console/ && go test -race -count=1 ./internal/console/
```
