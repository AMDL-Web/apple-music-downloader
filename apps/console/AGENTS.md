# internal/console

The embedded web console served at `/console/`: plain ES modules and a vendored
Preact+htm bundle. [DESIGN.md](DESIGN.md) is the contract for structure, states,
accessibility and the skin layer; the one skin, `music`, is specified in
[skins/music.md](skins/music.md). `console_test.go` guards headers, embedded
references, the vendor checksum, the design rules, skin tokens, API paths and
Settings coverage.

## Rules

- No build step, no npm, no runtime CDN: the files under `static/` are what the
  browser runs and what `//go:embed` ships.
- The console uses only the public API, with every call a full `/api/v1/...`
  literal in `static/js/api.js`. A widget whose endpoint answers 404 shows a quiet
  note instead of breaking the page.
- URLs stay relative (`resolveUrl`), so the console works under any path prefix.
- The CSP allows no inline scripts or styles: dynamic values go through Preact
  style objects, and data is rendered through htm, never `innerHTML`.
- Colors live only in `static/css/tokens.css` and the skin's stylesheet; the other
  stylesheets read tokens. Theme (`data-theme`) and skin (`data-skin`) are separate
  axes, and a skin changes only its own CSS file and spec page.
- Modules in `static/js/lib/` are pure (no DOM at import, no vendor import) so
  `node --test` covers them.
- A tab holds the overview feed plus at most one secondary stream
  (`claimSecondary` in `static/js/lib/stream.js`), and the feed opens from the
  snapshot's `event_cursor`.

## Vendored bundle

Never edit a file under `static/vendor/`. To update it, download the new version
from jsDelivr and unpkg into an empty scratch directory, require identical SHA-256,
store it under a new versioned file name (vendor files are served `immutable`),
then update `vendorFile` and `vendorSHA256` in `console_test.go` and the import in
`static/js/ui/preact.js`. The bundle has no `Fragment`; return keyed arrays.

## Verify

```bash
for f in $(find internal/console/static -name '*.js' -o -name '*.mjs'); do node --check "$f"; done
node --test internal/console/jstest/*.test.mjs
go vet ./internal/console/ && go test -race -count=1 ./internal/console/
```

Then `make dev`, open `http://127.0.0.1:18080/console/`, and check the change at
desktop width and 375 px, in light and dark.
