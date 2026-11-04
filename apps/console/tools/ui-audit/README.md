# ui-audit

A repeatable hunt for small UI flaws: it drives a web app through every page
and state at several widths, themes and engines, measures each screen, and
writes one deduplicated list of findings with the element, the measured
values and a crop. It is an on-demand tool, and CI also runs it on every console
change as a record that gates nothing ([In CI](#in-ci)).

Two halves with a clean boundary:

| Part | Holds | Knows about the app |
| --- | --- | --- |
| `core/` | runner, in-page probe, checks, static CSS scan, report, diff | nothing: no app imports, no app selectors |
| `console/` | the amdl console's config: mock backend, fixtures, scenarios, selectors | everything |

Another project gets audited by writing its own config next to its code (see
[Config contract](#config-contract)); `core/` stays as it is.

## Run it

Needs Node 24+, Google Chrome and `playwright-core` 1.64.1 somewhere outside
the repo:

```bash
npm install --prefix ~/.cache/ui-audit-pw playwright-core@1.64.1
export PLAYWRIGHT_CORE=~/.cache/ui-audit-pw/node_modules/playwright-core
node apps/console/tools/ui-audit/core/cli.mjs run \
  --config apps/console/tools/ui-audit/console/audit.config.mjs --out /tmp/ui-audit
```

Then open `/tmp/ui-audit/index.html`. Useful flags:

| Flag | Effect |
| --- | --- |
| `--quick` | 1280 and 375, dark only |
| `--widths 1280,375` `--themes dark` | narrow the matrix |
| `--only queue,drawer/failed` | scenarios whose id is or starts with these |
| `--no-states` | skip the forced hover/focus/pressed pass |
| `--engine webkit` | WebKit instead of Chrome (no state pass: it has no CDP) |

`merge <shard-dir>... --out <dir>` joins the output directories of several `--widths` or `--themes` runs
into one report (the consistency and rhythm checks compare across runs, so they run on the merged result);
`summary <report.json>` prints a markdown summary and a GitHub `::warning` line when there are more than
`--max-high` high findings (default 0), a failed step or a page error. `report <result.json> --out <dir>` rebuilds the page from a saved run;
`diff <before/report.json> <after/report.json>` prints per-check counts
before → after and which findings went away or appeared.

The console needs no Go to audit: `console/mock-server.mjs` serves
`apps/console/static` under `/console/` with the Go server's CSP (read from
`apps/console/console.go`) and answers `/api/v1` from deterministic worlds
(`console/fixtures-worlds.mjs`): `empty`, `idle`, `active` and `many`, with fixed job
ids for each drawer state. Covers come from Apple's CDN once and then from
`~/.cache/amdl-ui-audit`; motion covers are blocked. Run the mock alone to
look by hand:

```bash
node apps/console/tools/ui-audit/console/mock-server.mjs --port 18571 --world many
```

Its hooks routes answer from the same in-memory hooks.yaml as the website's demo
(`apps/site/demo/fake-hooks.js`), and `GET /__mock/hook-writes` lists every hooks write
it received, to compare request bodies. Queued jobs carry `queue_order` and
start in that order; `POST /api/v1/downloads/{id}/move` reorders them as the
server does (`console/mock-world.mjs`), `GET /__mock/moves` lists every move body
it received, and a world's `queued: N` option (a scenario's `worldOpts`) adds a
long queue to reorder. A run of an album, song or playlist names its record
(`resource`), and the record routes (`GET /api/v1/library/...`, with
`sort=activity` and the feed's `resource_updated`) answer from the runs
(`console/mock-records.mjs`); the `many` world adds station runs and an add
that could not be read.

WebKit runs on a Linux host with Playwright's WebKit installed: copy the
repository there and run the same command with `--engine webkit`.

## In CI

The `ui-audit` jobs of `.github/workflows/ci.yml` run the whole matrix for each
console change (paths in `.github/scripts/changed-paths.mjs`) and for every push to
`dev`: one job per width × theme, in the runner's own Chrome,
then a report job joins them with `merge`. The console has one skin, so skin is not an
axis. The artifact `console-ui-audit-<commit>` (kept 14 days) holds `index.html`, `report.json`,
`result.json` and the screenshots; the run's summary page lists the counts and the high
findings. Nothing here fails a build: past the threshold the job only annotates a warning.
Screenshots from the runner use Linux fonts, so pixels differ from a Mac run and a
before → after `diff` is only meaningful between two CI runs.

## What it checks

Each check produces findings with a selector path, the measured values and,
where it has a box, a crop with the box drawn on it. Findings that differ
only in numbers merge across widths, themes and scenarios.

| Check | How | Flags |
| --- | --- | --- |
| **layout-shift** | `PerformanceObserver` layout-shift entries, all of them (trusted input sets `hadRecentInput`, which CLS would drop), plus a box diff of every element that survives a transition | CLS on load and per transition; each element group that moved, with the likely cause (a sibling above grew, was inserted, was removed); a live number that changes width at the same length |
| **flicker** | a per-frame recorder (rAF) of the main region's text length, visible loaders, focus, scroll, page background luminance and images loading over nothing, plus a CDP screencast for frame strips; the static scan for transitions | content blanking for a frame or more; a loader shown under the skeleton threshold (300 ms) or for data that answered faster; light ↔ dark background jumps on first paint or a transition; focus dropping to `<body>`; scrolling before the new content commits; images popping in without a tinted placeholder; `transition` on layout properties or `all` |
| **empty-space** | per container, the vertical coverage of its painted content (text line boxes, replaced elements, boxes with their own paint) | a blank band over 48 px and 25 % of the container, inside or at one edge (unbalanced composition), naming the invisible element or `min-height` behind it; any `visibility:hidden` or `opacity:0` box holding room in the flow |
| **shape** | each interactive element sampled per signature, forced into `:hover`, `:focus-visible` and `:active` (CDP `CSS.forcePseudoState`, ancestors get `:hover` or `:focus-within`), compared with rest | a highlight with square corners inside a rounded surface that does not clip; a child radius larger than the parent's minus the inset; a highlight flush on two sides but inset on the others; a focus ring clipped by an overflow ancestor; no visible focus change at all |
| **alignment** | geometry of the settled page | an icon off the centre of its text's first line (or centred on a wrapped block instead of its first line); a row whose column edge is off the edge the other rows share; side-by-side cells of one kind with different heights; a live number without `tabular-nums` |
| **rhythm** | the configured relationships (page head → content, section → section, card padding, card head → body, gutters) measured on every page | a relationship spaced differently on some pages; off the spacing scale; uneven left/right or top/bottom padding |
| **tokens** | static scan of every stylesheet with `file:line`, plus computed values | a literal length in spacing, radius or font-size (on the scale: use the token; off it: pick a step); computed paddings, gaps, margins, radii and font sizes off the scale |
| **noise** | visible text of every leaf | zero counts ("0 cancelled"), placeholders ("—", "none", "N/A"), leaked values (`undefined`, `NaN`, `Invalid Date`), the same text twice in one row |
| **prose** | the UI's own text (content marked `dir=auto` is skipped) | sentences that explain the UI instead of showing data, status or an action |
| **overflow** | scroll and client sizes, line boxes clipped by their overflow ancestors | sideways page scroll; text spilling or clipped without an ellipsis; an ellipsis on a short value; sibling rows cut to the same visible prefix; text overlapping text on the same layer |
| **a11y** | contrast composited from the ancestors' backgrounds; hit-testing with `elementFromPoint`; synthetic modifier clicks | text under 4.5:1 (3:1 large), icons under 3:1; controls without a name; nav without `aria-current`; a badge count missing from its link's label; phone targets under 44 px with no larger hit area; Cmd/Ctrl-click swallowed by an in-app link |
| **consistency** | every instance of each configured component kind across all scenarios, per width class | the same component class drawn with different height, font size, radius or padding in different places |

Known limits: contrast skips text over images and gradients; alignment and
baselines are judged in Chrome only (fonts differ elsewhere); the state pass
needs CDP, so WebKit gets scans and transitions only; the mock's data is
shaped like the API but is not the server, so a release still gets a look at
real data.

## Visual review checklist

What the scripts cannot judge. Go through the full-page shots
(`shots/`, one per scenario and variant) against the app's design document:

- **Hierarchy**: one clear focal point per screen; titles, values and
  secondary text at three distinct levels, not five.
- **Density and rhythm**: similar things spaced alike on every page; no
  cramped cluster next to a loose one.
- **Wrapping**: no orphan word on a second line, no label breaking between a
  number and its unit, no button text on two lines.
- **Iconography**: one stroke weight and size per context; icons optically
  centred, not just box-centred.
- **Copy**: data, status and actions only; no explanation of what a card is
  for; the same thing called the same name everywhere.
- **Broken-looking states**: empty, loading, error and edge states (one item,
  hundreds, very long names, missing artwork) read as designed, not as bugs;
  no frame inside a frame.
- **Phone**: primary action reachable with the thumb; page title not
  repeated by the top bar; nothing relies on hover.

## Config contract

A config module's default export:

| Key | Meaning |
| --- | --- |
| `name`, `root` | report title; repository root for `file:line` paths |
| `start()` | starts the app and returns `{baseUrl, control, stop}`; `control.prepare(scenario, variant)` puts the backend in the scenario's state |
| `scenarios` | `{id, path, require?, setup?(ctx), transitions?, widths?, themes?, engines?, storage?, noStates?}`; a transition is `{id, run(ctx), before?, after?, expectScroll?, expectThemeChange?, expectContent?, ignoreFocus?, allowMove?, hold?}` (`expectContent`: the transition is meant to change content, so what moves below the change and its CLS count low); `ctx` has `page`, `control`, `settle()`, `hash()`, `wait()` |
| `variants` | `widths`, `themes`, `stateWidths` (where the state pass runs), `phoneBelow`, viewport `heights` |
| `selectors` | `interactive`, `container`, `row`, `icon`, `badge`, `nav`, `region`, `skeleton`, `modal`, `ignore` |
| `components` | kind → selector, for the consistency check |
| `rhythm` | `{name, selector, measure: padding \| gapToNext \| edges \| firstTextLeft, within?}` |
| `allow` | selectors exempt from `noise`, `prose`, `duplicate` and `contrast`, for what the design intends |
| `css` | `files` to scan, `tokens` files and `patterns` that define the scale |
| `routes(context)`, `initScript(variant)` | network stubs and per-document setup |

Defaults are in `core/config.mjs`. Tests: `node --test apps/console/tools/ui-audit/core/*.test.mjs apps/console/tools/ui-audit/core/lib/*.test.mjs`.
