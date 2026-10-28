# Console design

The shared contract for the embedded console: structure, states, accessibility,
motion and the skin mechanism. How the console *looks* belongs to a skin; each
skin's spec is in `internal/console/skins/` ([music](skins/music.md),
[editorial](skins/editorial.md)). When this page and the code disagree, the code
wins and this page is fixed in the same change. Sources are listed in
[docs/references.md](../../docs/references.md#console-design-references).

## Principles

1. **Show only what the API knows.** Track counts, never an aggregate pipeline
   percentage (`ItemProgress` has none on purpose). Simulate mode is announced
   in a banner.
2. **Status is never color alone.** Every status has an icon (or glyph), a label
   and a color role; a skin may change the look of a role, never drop the label.
3. **Artwork colors are data.** Apple's per-album palette reaches the page as
   custom properties; text colors are contrast-checked before they are used.
4. **Both themes, every skin.** Each skin ships a light and a dark theme; light
   is designed, not inverted.
5. **English UI copy**, `lang="en"`. Apple-provided text (titles, artists,
   genres) is shown as received with `dir="auto"`.

## Information architecture

| Route | Page | Content |
| --- | --- | --- |
| `#/` | Overview | Sections (in DOM order) `kpis`, `live` (8 active jobs, then up to 2 jobs that failed in the last 24 h), `health`, `recent`, `throughput` (from `/stats`), `logs` (12-line tail) |
| `#/queue?tab=active\|failed\|all&type=&q=` | Queue | Tabs with counts from the feed's `status_counts`, search, type filter, dense job list, Load more |
| `#/queue/:id`, `#/showcase/:id` | Job drawer | Over its page: header, palette, track strip, track list with stage meters, hooks, activity, Cancel/Retry |
| `#/showcase?type=album\|playlist\|song` | Showcase | "Latest" hero in Apple's colors, cover grid, Load more |
| `#/logs?level=&component=&job_id=&request_id=&q=` | Logs | Level chips, component, search, filter chips, live tail, expandable rows |
| `#/system` | System | About, runtime, health, storage, wrapper, library sync, hooks with outcomes, read-only config with Reload, API links |

Filters live in the hash query, so a filtered view is a link. KPI tiles open the
matching Queue tab; the drawer's "View logs" opens `#/logs?job_id=…`; a log
row's job chip filters to that job. The tab title reads `"{n} running · AMDL"`
while jobs run.

## Layout

The DOM frame is fixed; a skin restyles it (the editorial skin turns the
sidebar into a horizontal masthead at 640 px and up).

```
.shell
  .sidebar (nav: .brand > .brand-mark + .brand-text > .brand-name, .brand-sub; .nav > .nav-item; .sidebar-foot)
  .main-col
    header.topbar (.topbar-right: .conn, .search-trigger, .skin, .theme)
    main.main[data-page] (.page-head > h1.page-title + .page-sub; .banners; .page.page-<name>)
  nav.tabbar (> .tab, under 640 px)
```

| Width | Base frame | Notes |
| --- | --- | --- |
| ≥1024 | `--sidebar-w` sidebar, `--topbar-h` top bar | Overview is a 12-column grid: `live` spans 8, `health` 4, the rest 12 |
| 640-1023 | `--rail-w` icon rail (labels kept for screen readers) | two-column cards stack |
| <640 | top bar, `--tabbar-h` bottom tab bar + safe area, 16 px gutters | single column, 44 px targets, theme and skin become cycle buttons |

Siblings are spaced by the parent's `gap` (`--page-gap` between sections,
`--grid-gap` inside grids, `--card-gap` inside cards), never by child margins.
No horizontal page scroll at 390 px in any skin or theme.

## Skins

A skin is the console's look: colors, radii, shadows, font stacks, display
type and a few component rules. The theme (system, light, dark) is a separate
axis; every skin supports all three.

### Mechanism

- `<html data-skin="music|editorial">`, stored per browser under
  `localStorage["amdl.console.skin"]` with the same guarded read and write as
  the theme (`lib/prefs.js`: `SKINS`, `DEFAULT_SKIN`, `normalizeSkin`,
  `readPref`, `writePref`). The default is `music`, and `index.html` carries it
  statically so the first frame has every token before `main.js` runs;
  `main.js` then applies the stored skin and theme before mounting.
- The switch is a segmented control labelled "Style" in the top bar beside the
  theme control (a cycle button under 640 px), and the command palette has
  "Style: Apple Music" and "Style: Editorial".
- The theme is `<html data-theme="light|dark">` or no attribute for "system";
  `css/tokens.css` sets `color-scheme` from it, and skins write every color as
  `light-dark(<light>, <dark>)`, which resolves against that. Skins never set
  `color-scheme`.

### Files and what each may do

| File | Holds | May | May not |
| --- | --- | --- | --- |
| `static/css/tokens.css` | Skin-neutral scales: type scale `--fs-*`/`--lh-*`, weights, spacing `--s-*`, sizes, motion, `--on-art-light/dark`, system font fallbacks | change a scale every skin shares | hold a skin's color, radius, shadow or font stack |
| `static/css/*.css` (the base: `base.css`, `shell.css`, `controls.css`, `layout.css`, one file per page area, `overlays.css`, `responsive.css`) | Structure: layout, component structure and behavior, all visual values through tokens, one file per area. `index.html` links them after `tokens.css` and before the skins, and that link order is the cascade order (`responsive.css` last) | add structure both skins need, in the file of its area | contain a color literal, `!important` (outside reduced motion) or a skin-specific choice |
| `static/css/skins/<name>.css` | One `:root[data-skin="<name>"]` token block (light and dark via `light-dark()`), responsive token overrides, and that skin's component rules, every selector prefixed `:root[data-skin="<name>"]` | set any token, override any base rule (repeating its selector chain when it is deeper than two classes), show opt-in hooks, reorder overview sections, use CSS counters and `::before` text | touch another skin's scope, add JS, use `font-size` other than `var(--fs-*)`, use `transition: all`, rely on `color-scheme` it sets itself |
| `skins/<name>.md` | The skin's spec: intent, token table with contrast, component notes | | restate the shared contract |

The prefix `:root[data-skin="<name>"]` adds (0,1,1), so a skin selector wins
over any base selector of up to two classes, including those inside base media
queries; a skin therefore carries its own responsive overrides. For a deeper
base selector (`.card-flush > .card-body > .empty`,
`.level-chip[aria-pressed="false"] .log-dot`, the chart hover chain), repeat
its chain under the skin prefix.

### Tokens a skin must declare

`TestSkinsDeclareEveryBaseToken` fails when a skin misses one that a base
stylesheet reads and `tokens.css` does not declare. Declare the whole table anyway (for
example every role's `-text`, though the base reads only `--danger-text` and
`--warning-text`), so skin rules can rely on it.

| Group | Tokens |
| --- | --- |
| Surfaces | `--bg-canvas`, `--bg-surface`, `--bg-raised`, `--bg-hover`, `--bg-selected`, `--bg-chrome` (sidebar, top bar, tab bar) |
| Lines | `--border-subtle` (decorative), `--border-control` (3:1 outlines), `--chrome-border`, `--card-divider`, `--row-divider` (border shorthands) |
| Text | `--text-primary`, `--text-secondary` (4.5:1 on `--bg-surface`), `--text-disabled` (placeholders only) |
| Accent | `--ink`/`--on-ink` (skip link), `--accent`/`--on-accent` (primary button), `--accent-text` (links), `--focus` (2 px ring), `--brand`/`--on-brand` (logo mark) |
| Status roles | `--info`, `--success`, `--danger`, `--warning`, `--neutral`, `--skipped`, each with `-tint` and `-text` |
| Data | `--track` (empty bars and cells), `--chart-bar`, `--chart-grid`, `--scrim` |
| Type | `--font-sans`, `--font-display`, `--font-mono`, `--fs-body`/`--lh-body`, `--tracking-body`, `--fs-page-title`/`--lh-page-title`, `--fs-card-title`/`--lh-card-title`, `--fs-kpi`/`--lh-kpi`, `--fs-hero-title`/`--lh-hero-title`, `--w-display`, `--w-title`, `--tracking-display`, `--tracking-title` |
| Shape | `--r-badge`, `--r-bar`, `--r-control`, `--r-control-inner`, `--r-card`, `--r-art`, `--r-overlay`, `--r-drawer`, `--r-notice`, `--bar-h`, `--cell-h`, `--cell-gap` |
| Cards | `--card-bg`, `--card-border` (shorthand), `--card-shadow`, `--card-pad`, `--card-pad-x` (flush rows), `--card-gap` |
| Layout | `--page-gap`, `--grid-gap` |
| Elevation | `--shadow-art` (covers), `--shadow-float` (drawer, palette, toasts) |

### DOM hooks

Stable class names and attributes a skin can style without touching JS:

| Surface | Hooks |
| --- | --- |
| Section / card | `section.card[data-section]` > `header.card-head` (`h2.card-title`, `.card-note`, `.card-actions`) + `.card-body`; `.card-flush` for edge-to-edge lists. Overview sections: `kpis`, `live`, `health`, `recent`, `throughput`, `logs`; Queue `jobs`; Logs `log` |
| Page | `main.main[data-page]`, `.page-head`, `.page-title`, `.page-sub`, `.banners`, `.page.page-<name>` |
| KPI | `a.kpi[data-key=queued\|running\|completed\|failed\|total][data-role][data-zero]` > `.kpi-label`, `.kpi-icon`, `.kpi-value`, `.kpi-meta` |
| Job row | `a.job-row[data-status][.has-palette]` > `.art`, `.job-main` (`.job-facts` > `.job-fact[data-key=type\|genre\|year\|started]`, `.job-title`, `.job-sub`), `.job-type`, `.job-progress`, `.job-status`, `.job-updated`, `.job-chevron` |
| Progress | `.jprog[data-mode=cells\|bar]`: up to 60 tracks `.pcells` (`--n`) > `.pcell[data-state=completed\|skipped\|failed\|cancelled\|remaining][data-role]` in that order (the list payload has counts, not per-track order); above 60 `.pbar` > `.pbar-seg[data-role]`; both followed by `.jprog-text` ("12 / 14") and `.jprog-legend` > `.jprog-key[data-state][data-role][data-zero]` > `.jprog-key-n` + `.jprog-key-label` (saved, on disk, failed, cancelled, remaining; every state, zeros included). The list payload cannot tell a track in flight from a waiting one, so "remaining" covers both and there is no byte percentage |
| Health | `.health-row[data-role]` > `.health-icon`, `.health-name`, `.health-status` (badge), `.health-detail`; runtime facts `dl.facts.health-facts` |
| Facts | `dl.facts` > `.fact[data-key][data-role]` > `dt.fact-label` + `dd.fact-value` (health facts, throughput totals `.tp-totals` with `tracks`, `tracks-failed`, `bytes`, `jobs`, `jobs-failed`, the failed rows only when non-zero and `data-role=danger`, hero facts `.hero-facts`) |
| Recent strip | `a.strip-tile[.has-palette][.is-overflow]` > `.art`, `.strip-title`, `.strip-sub`, `.strip-meta`; every completed job renders, those past the whole-tile count carry `.is-overflow` |
| Showcase | `a.hero.has-palette` > `.hero-art`, `.hero-text` (`.hero-kicker` > `.hero-no`, `.hero-title`, `.hero-artist`, `.hero-facts`), then its sibling `div.hero-actions` (same palette properties) > `.btn` "Tracks" (the drawer) and "Open in Apple Music"; `a.tile[.has-palette]` > `.tile-head` (`.tile-index`, `.tile-year`), `.tile-art`, `.tile-title`, `.tile-sub`, `.tile-facts` > `.tile-fact[data-key=genre\|year\|tracks\|finished]` |
| Live failures | `ul.attention` after the live list > `li.attention-row[data-role=danger]` > `.attention-icon`, `.attention-title` (`.attention-label` "Failed", `a.attention-link` to the drawer, mono `.attention-input`), `.attention-detail` (`.attention-error`, relative time), Retry `.btn` |
| Throughput | `.tp-totals` (Facts), `.tp-formats` > `.tp-formats-title`, `.formats-bar` > `.formats-seg[data-key]` (`--w` share, `--i` index), `ul.formats-list` > `li.formats-item[data-key]` (`--i`) > `.formats-swatch`, `.formats-label`, `.formats-count`; `.chart-xaxis` > one `span[data-day]` per day, `.is-first`, `.is-mid`, `.is-latest` on the three the base shows |
| Logs | `.log-entry[data-level]` on tail rows (`.log-tail-row`) and log page rows (`.log-row`) > `.log-time`, `.log-level[data-role]`, `.log-comp`, `.log-msg` (`.log-attrs` in the tail) |
| Shell | `.brand-sub`, `.nav-item[aria-current]`, `.nav-badge`, `.tab[aria-current]`, `.skin`/`.seg-skin`/`.skin-cycle`, `.theme`/`.seg-icons`/`.theme-cycle` |
| Status | `[data-role=info\|success\|danger\|warning\|neutral\|skipped]` sets `--role` and `--role-tint` for everything inside it; `.badge`, `.status-text`, `.notice` |

**Artwork palette.** `.job-row`, `.strip-tile`, `.tile` and `.hero` carry
`--art-bg` and `--art-t1`, `--art-t2`, `--art-t3` (from `lib/format.js`
`artPalette`/`paletteStyle`) plus `.has-palette` when Apple sent one; the
drawer's `.drawer-head` carries the properties without the class. Each `--art-t*` already reaches 4.5:1 on `--art-bg` or is replaced by
`--on-art-light`/`--on-art-dark`. A playing motion cover switches to the
`motion_artwork_*` palette; the two are never mixed. `.art` also sets `--art-bg`
behind a cover while it loads or when it fails.

**Opt-in hooks** are hidden by the base and shown by a skin that wants them:
`.brand-sub`, `.job-facts`, `.health-icon`, `.hero-no`, `.hero-actions`,
`.tile-head`, `.strip-tile.is-overflow`, `.jprog-legend`,
`.attention`, and the x-axis day labels other than the first, middle and
latest.

### Adding a skin

1. Add `{ value, label, short }` to `SKINS` in `static/js/lib/prefs.js` and a
   test case in `jstest/prefs.test.mjs`.
2. Create `static/css/skins/<value>.css` with a `:root[data-skin="<value>"]`
   block declaring every token in the table above, then its rules.
3. Link it from `index.html` after the base stylesheets (`css/responsive.css`
   is the last of them).
4. Write `skins/<value>.md`.
5. `go test ./internal/console/` checks the token set, the link, the scope and
   the color and font-size guards; then check every page in light and dark at
   1440 and 390 px.

## Color and contrast rules

Every skin meets these; its spec lists the measured values.

- Body and secondary text 4.5:1 on `--bg-surface` and `--bg-canvas`;
  `--border-control`, focus rings and progress fills 3:1 against neighbors.
- `--*-text` is used only on `--bg-surface`. A role may share a color with
  another (the editorial skin keeps running and completed in ink) as long as
  the icon and label differ.
- The danger and warning roles never look like the accent of a healthy state.
- Under `forced-colors`, badges, bars, cells and dots get borders (base).

## Type, spacing, shape, motion

System fonts only: no web fonts, no CDN. A skin picks stacks for `--font-sans`,
`--font-display` and `--font-mono` and falls back to `--font-system` or
`--font-system-mono`. Every `font-size` is a `--fs-*` token (a Go test
checks every CSS file); a skin adds display roles as tokens. Numbers that
update live use `tabular-nums`. Long titles truncate with the full text in
`title`.

Spacing steps (px): 2, 4, 6, 8, 12, 16, 20, 24, 32, 40, 48, 64 (`--s-0_5` …
`--s-16`). Controls are 32 px (28 in toolbars), 44 px under 640 px. Radii and
shadows are skin tokens.

Motion: 120 ms hover color fades, 200 ms drawer/palette/toast/insert,
`--ease-out` in. Movement uses only `transform` and `opacity` (stage and meter
fills: `scaleX`, linear); never `transition: all`. The running pulse and the
skeleton pulse are the only loops a skin gets for free; any other loop (an
equalizer glyph, a live dot) is the skin's and must stop under
`prefers-reduced-motion`, which also removes the drawer slide, cover movement
and motion covers.

## Components

- **Badge**: icon (or the running pulse) + label on the role tint. Mappings
  live in `lib/status.js` (`jobStatusView`, `itemStatusView`, `hookStatusView`,
  health views), unit-tested. A completed job whose tracks were all on disk is
  "Already in library" (skipped role). An item with `attempt > 1` adds a
  warning "Retry n/m" badge.
- **KPI tile**: label, value, meta, role icon; the tile links to its Queue tab.
  Values switch without count-up; "—" while disconnected.
- **Job row**: cover, facts, title, artist/curator · type · storefront,
  progress, status badge, relative time, chevron. Indeterminate "Resolving"
  while `total_items` is 0. Under 640 px it is a two-line card.
- **Stage track**: seven segments (Resolve, Download, Decrypt, Remux, Verify,
  Tag, Save). Download and decrypt fill to their fraction; download at 0 while
  downloading is striped ("size unknown"); a completed item with
  `verified=false` shows a hollow "Not verified" segment, never red.
- **Job drawer**: dialog with focus trap, Esc and Back close it, focus returns
  to the row. Header with cover, artist link, meta, status, live tag;
  created/started/finished/duration; Cancel (inline confirm) for queued or
  running, Retry for failed, View logs, Open in Apple Music; palette swatches;
  per-track strip; track list (codec chip such as `ALAC 24/96`, length, size,
  error); hooks; recent non-progress events under Activity.
- **Showcase**: hero for the newest release, then a grid with `srcset`
  300w/600w and lazy loading. Covers keep pre-signed URLs without placeholders
  verbatim. Motion covers play muted on hover or focus, one at a time, only
  where `video.canPlayType` accepts HLS and motion is allowed. Titles underline
  on hover in every skin.
- **Throughput**: totals as facts, the formats mix of every retained job
  (`/stats` `codecs`, the top four with the rest as "Other"; nothing before
  a track completes), then two small multiples sharing the date
  axis (tracks per day, bytes per day), each with its own 0/max axis plus a
  max/2 gridline and label once max reaches 2 (for tracks only when max/2 is
  whole), a hover readout, and a screen-reader table.
- **Log viewer**: `role="log"`, `aria-live="off"`, mono rows (time, level
  marker, component, message), job/request chips that filter, expandable pretty
  JSON with Copy, follow-bottom within 24 px else an "N new lines" pill, at most
  2,000 rows. Warn and error rows are marked in the skin's warning and danger
  roles.
- **Health rows**: name · status · detail for backend, wrapper, library sync
  (503 is "Not configured", not an error) and hooks, plus workers, queue,
  downloads disk and uptime from `/system`; polled every 30 s and on focus.
- **Config summary**: read-only `GET /config` by section; `media_user_token`
  shows only Set/Not set; "Reload config" posts `/config/reload` and shows
  `reload_error` as a warning.
- **Primitives**: buttons (primary, secondary, ghost, danger), segmented
  control, search field, select, chips, notices, toasts (`role="status"`,
  errors stay until dismissed), command palette (Cmd/Ctrl+K, combobox +
  listbox), shortcut sheet (`?`).

## States

| Surface | Loading | Empty | Error |
| --- | --- | --- | --- |
| KPI | skeleton of final size | zeros | "—" while disconnected |
| Queue | 6 skeleton rows | per tab, with Clear filters when filtered | inline note + Retry |
| Drawer | skeleton tracks | "No tracks resolved yet" | 404 "This job was deleted"; 204 on the stream means finished |
| Showcase | skeleton hero and tiles (cover plus two text lines) | "Completed albums will appear here." with a link to the Queue | inline note + Retry; broken cover shows bg color + glyph |
| Logs | 12 skeleton lines | "No records match" + Clear filters | the API's validation message |
| System, throughput | skeleton rows | per card | per card; a 404 endpoint shows a quiet "not available" note |
| Feed | | | Reconnecting · Ns; banner after 10 s; Disconnected after 60 s with Retry |

## Accessibility

Landmarks (`nav`, `header`, `main`), a skip button, one `h1` per page (focused on
navigation). Every control shows the 2 px focus ring in `--focus`. Keyboard: `g`
then `o/q/s/l/y`, `/` focuses the page filter, `j`/`k` move between rows, Enter
opens, Esc closes, Cmd/Ctrl+K, `?`; single keys are ignored while typing. The
skin and theme controls are button groups with `aria-pressed` and an
accessible name ("Style", "Theme"). Milestones ("Album X completed") go to one
polite status region, at most one announcement per 5 s. Progress carries
`role="progressbar"` with `aria-valuetext` such as "12 of 14 tracks, 1 failed",
whether it renders cells or a bar. Text a skin adds with `::before` (section
numbers, "No.") is decoration and repeats nothing a screen reader needs.

## Data flow

`GET /downloads?limit=1` seeds counts and `event_cursor`; the feed stream then
resumes from that cursor and every page applies its batches (at most 10 a
second) to its own list (`lib/store.js`). The drawer and logs page each open one
secondary stream from their snapshot cursor. Streams pause 30 s after the tab
hides and resume from the stored cursor; after a 5-minute outage pages refetch.

## Verification

Run the backend with `make dev` (simulate mode), open `/console/`, switch the
style in the top bar, and check each skin at 1440 and 390 px wide in light and
dark: Overview, Queue with the drawer, Showcase, Logs, System, the command
palette. No horizontal scroll at 390 px, status icon plus label everywhere,
visible focus rings.
