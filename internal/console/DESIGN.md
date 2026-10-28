# Console design

The spec for the embedded console as implemented. Token values live in
`internal/console/static/css/tokens.css`; this page explains them. When this page
and the code disagree, the code wins and this page is fixed in the same change.
Sources for each decision are listed in
[docs/references.md](../../docs/references.md#console-design-references).

## Principles

1. **Quiet chrome, loud state.** Neutral surfaces in an inverted-L frame (sidebar
   plus top bar). Color appears only where it carries status.
2. **One focus per element.** A KPI tile holds one number, a queue row one
   progress bar, a gallery tile one cover.
3. **Show only what the API knows.** Track counts, never an aggregate pipeline
   percentage (`ItemProgress` has none on purpose). Simulate mode is announced
   in a banner.
4. **Status is never color alone.** Every status has an icon, a label and a
   color role. Red and green are indistinguishable for deuteranopes.
5. **Dark first, light tested.** Both modes use their own Radix steps; light is
   not an inversion.
6. **English UI copy**, `lang="en"`. Apple-provided text (titles, artists,
   genres) is shown as received with `dir="auto"`.

## Information architecture

| Route | Page | Content |
| --- | --- | --- |
| `#/` | Overview | KPI tiles; live queue (8 active jobs) and system health; recently completed covers; throughput from `/stats`; log tail (12 lines) |
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

| Width | Frame | Content padding | Notes |
| --- | --- | --- | --- |
| ≥1024 | 232 px sidebar, 48 px top bar | 24 | 12-column grids, 16 gap, content max 1600 |
| 640-1023 | 56 px icon rail (labels kept for screen readers) | 20 | two-column cards stack |
| <640 | 48 px top bar, 56 px bottom tab bar + safe area | 16 | single column, 44 px targets |

Overview at ≥1024: KPI row (`auto-fit, minmax(168px, 1fr)`, 104 px tiles); live
queue (8 columns) beside system health (4); recently completed strip (whole
tiles only, counted with a ResizeObserver); throughput; log tail. Sections are
24 px apart; siblings are spaced by the parent's `gap`, never by child margins.

## Color tokens

Each token is `light-dark(<light>, <dark>)`; `:root[data-theme]` forces a mode,
otherwise `color-scheme: light dark` follows the system. Contrast is WCAG 2.x
against `--bg-surface`.

| Token | Light | Dark | Use | Contrast (light / dark) |
| --- | --- | --- | --- | --- |
| `--bg-canvas` | `#f9f9fb` | `#111113` | page | |
| `--bg-surface` | `#ffffff` | `#18191b` | cards, sidebar | |
| `--bg-raised` | `#ffffff` | `#212225` | drawer, palette, toasts | |
| `--bg-hover` / `--bg-selected` | `#f0f0f3` / `#e8e8ec` | `#272a2d` / `#2e3135` | hover, active nav, pressed segments | |
| `--border-subtle` | `#e0e1e6` | `#363a3f` | dividers (decorative) | |
| `--border-control` | `#8b8d98` | `#696e77` | input outlines | 3.30 / 3.43 |
| `--text-primary` | `#1c2024` | `#edeef0` | text, values | 16.39 / 15.15 |
| `--text-secondary` | `#60646c` | `#b0b4ba` | labels, meta | 5.94 / 8.45 |
| `--text-disabled` | `#80838d` | `#777b84` | placeholders only (fails 4.5:1) | 3.78 / 4.15 |
| `--ink` / `--on-ink` | `#1c2024` / `#ffffff` | `#edeef0` / `#111113` | primary button | 16.39 / 16.25 |
| `--focus` | `#0d74ce` | `#70b8ff` | 2 px ring, 2 px offset | 4.77 / 8.37 |
| `--brand` | `#e35e38` | same | logo mark only | |

The accent is monochrome ink, not brand orange: `#e35e38` sits 5.1 ΔE from
failure red and would read as "failed".

Status roles: the dot or icon wears the role color; labels stay in
`--text-primary` on the role tint. `--*-text` is used only on `--bg-surface`.

| Role | Used for | Dot light / dark | Tint light / dark | Text light / dark |
| --- | --- | --- | --- | --- |
| info | running, item stages, hook running | `#0588f0` / `#0090ff` | `#e6f4fe` / `#0d2847` | `#0d74ce` / `#70b8ff` |
| success | completed, healthy, ready | `#2b9a66` / `#30a46c` | `#e6f6eb` / `#132d21` | `#218358` / `#3dd68c` |
| danger | failed, degraded, unreachable, error logs | `#dc3e42` / `#e5484d` | `#feebec` / `#3b1219` | `#ce2c31` / `#ff9592` |
| warning | retry, interrupted hook, sync error, reload error, simulate, warn logs | `#ab6400` / `#ffc53d` | `#fff7c2` / `#302008` | `#ab6400` / `#ffca16` |
| neutral | queued, waiting for a slot, cancelled, off, debug logs | `#8b8d98` / `#696e77` | `#f0f0f3` / `#272a2d` | `--text-secondary` |
| skipped | "Already on disk", "Already in library" | `#6e56cf` / `#7d66d9` | `#f4f0fe` / `#291f43` | `#6550b9` / `#baa7ff` |

Light amber step 9 is 1.58:1 on white, so the light warning dot uses amber-11.
Progress fills reach 3:1 against `--track` in both modes.

**Artwork colors** come from the API: `#`+`artwork_bg_color` sits behind a cover
while it loads or when it fails. The showcase hero uses `artwork_text_color1`
and `artwork_text_color2` when they reach 4.5:1 on the background (computed in
`artTextColor`), else `--on-art-light` or `--on-art-dark`, whichever contrasts
more. A playing motion cover switches to the `motion_artwork_*` palette; the
two palettes are never mixed.

**Chart**: one series color (`--chart-bar`), recessive hairline grid
(`--chart-grid`), text in text tokens.

## Type, spacing, shape, motion

| Token | Size / line | Weight | Use |
| --- | --- | --- | --- |
| `--fs-11` | 11/16 | 500, +0.04em, uppercase | section titles, codec chips, tab labels (floor) |
| `--fs-12` | 12/16 (mono 12/18) | 400/500 | badges, meta, counts, log rows |
| `--fs-13` | 13/20 | 400/500 | dense rows, captions, track rows |
| `--fs-14` | 14/20 | 400/500/600 | body, nav, card titles (600) |
| `--fs-16` | 16/24 | 600 | drawer title, palette input |
| `--fs-20` | 20/28 | 600 | page title |
| `--fs-24` | 24/32 | 600 | hero title, mobile KPI value |
| `--fs-32` | 32/40 | 600 | KPI value |

System fonts only (`--font-sans`, `--font-mono`). Numbers that update live use
`tabular-nums`. Long titles truncate with the full text in `title`.

Spacing steps (px): 2, 4, 6, 8, 12, 16, 20, 24, 32, 48 (`--s-0_5` … `--s-12`).
Radius: 4 badges and bars, 6 buttons and covers, 12 cards and overlays, full
for dots. Controls are 32 px (28 in toolbars), 44 px under 640 px. Shadows only
on floating layers (`--shadow-float`); no gradients or blur.

Motion: 120 ms hover color fades, 200 ms drawer/palette/toast/insert,
`--ease-out` in. Movement uses only `transform` and `opacity` (stage and meter
fills: `scaleX`, linear). The running pulse is the only loop besides the
skeleton pulse. `prefers-reduced-motion` removes both, the drawer slide and
motion covers.

## Components

- **Badge**: 20 px, icon (or the running pulse) + label on the role tint.
  Mappings live in `lib/status.js` (`jobStatusView`, `itemStatusView`,
  `hookStatusView`, health views), unit-tested. A completed job whose tracks were
  all on disk is "Already in library" (skipped role). An item with
  `attempt > 1` adds a warning "Retry n/m" badge.
- **KPI tile**: label, 32 px value, "of N total" meta, role icon top right; the
  tile links to its Queue tab. Values switch without count-up.
- **Job row**: 40 px cover, title + artist/curator · type · storefront,
  segmented progress (completed, skipped, failed, cancelled of total) with
  "12 / 14", status badge, relative time, chevron. Indeterminate "Resolving"
  while `total_items` is 0. Under 640 px it is a two-line card.
- **Stage track**: seven segments (Resolve, Download, Decrypt, Remux, Verify,
  Tag, Save). Download and decrypt fill to their fraction; download at 0 while
  downloading is striped ("size unknown"); a completed item with
  `verified=false` shows a hollow "Not verified" segment, never red.
- **Job drawer**: dialog with focus trap, Esc and Back close it, focus returns
  to the row. Header with 64 px cover, artist link, meta, status, live tag;
  created/started/finished/duration; Cancel (inline confirm) for queued or
  running, Retry for failed, View logs, Open in Apple Music; palette swatches;
  per-track strip; track list (codec chip such as `ALAC 24/96`, length, size,
  error); hooks; recent non-progress events under Activity.
- **Showcase**: hero with 240 px cover (160 under 640 px); grid
  `minmax(160px, 1fr)` with `srcset` 300w/600w and lazy loading. Covers keep
  pre-signed URLs without placeholders verbatim. Motion covers play muted on
  hover or focus, one at a time, only where `video.canPlayType` accepts HLS
  and motion is allowed.
- **Throughput**: two small multiples sharing the date axis (tracks per day,
  bytes per day), each with its own 0/max axis, a hover readout, and a
  screen-reader table.
- **Log viewer**: `role="log"`, `aria-live="off"`, mono 12/18 rows (time, level
  marker, component, message), job/request chips that filter, expandable pretty
  JSON with Copy, follow-bottom within 24 px else an "N new lines" pill, at most
  2,000 rows. Warn rows get an amber rule, error rows a red rule and tint.
- **Health rows**: name · badge · detail for backend, wrapper, library sync
  (503 is "Not configured", not an error) and hooks; polled every 30 s and on
  focus.
- **Config summary**: read-only `GET /config` by section; `media_user_token`
  shows only Set/Not set; "Reload config" posts `/config/reload` and shows
  `reload_error` as a warning.
- **Primitives**: buttons (primary ink, secondary outlined, ghost, danger tint),
  segmented control, search field, select, chips, notices, toasts
  (`role="status"`, errors stay until dismissed), command palette
  (Cmd/Ctrl+K, combobox + listbox), shortcut sheet (`?`).

## States

| Surface | Loading | Empty | Error |
| --- | --- | --- | --- |
| KPI | skeleton of final size | zeros | "—" while disconnected |
| Queue | 6 skeleton rows | per tab, with Clear filters when filtered | inline note + Retry |
| Drawer | skeleton tracks | "No tracks resolved yet" | 404 "This job was deleted"; 204 on the stream means finished |
| Showcase | skeleton hero and tiles | "Completed albums will appear here." | inline note + Retry; broken cover shows bg color + glyph |
| Logs | 12 skeleton lines | "No records match" + Clear filters | the API's validation message |
| System, throughput | skeleton rows | per card | per card; a 404 endpoint shows a quiet "not available" note |
| Feed | | | Reconnecting · Ns; banner after 10 s; Disconnected after 60 s with Retry |

## Accessibility

Landmarks (`nav`, `header`, `main`), a skip button, one `h1` per page (focused on
navigation). Every control shows the 2 px focus ring. Keyboard: `g` then
`o/q/s/l/y`, `/` focuses the page filter, `j`/`k` move between rows, Enter
opens, Esc closes, Cmd/Ctrl+K, `?`; single keys are ignored while typing.
Milestones ("Album X completed") go to one polite status region, at most one
announcement per 5 s. Progress bars carry `aria-valuetext` such as "12 of 14
tracks, 1 failed". Under `forced-colors`, badges and marks get borders.

## Data flow

`GET /downloads?limit=1` seeds counts and `event_cursor`; the feed stream then
resumes from that cursor and every page applies its batches (at most 10 a
second) to its own list (`lib/store.js`). The drawer and logs page each open one
secondary stream from their snapshot cursor. Streams pause 30 s after the tab
hides and resume from the stored cursor; after a 5-minute outage pages refetch.

## Verification

Run the backend with `make dev` (simulate mode), open `/console/`, and check
1280 and 375 px wide in light and dark: Overview, Queue with the drawer, Showcase,
Logs, System. No horizontal scroll at 375 px, status icon plus label everywhere,
visible focus rings.
